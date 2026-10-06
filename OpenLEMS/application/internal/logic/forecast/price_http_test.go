package forecast

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"application/internal/logic/spotreport"
	"s_db/s_db_model"
	"s_forecast"

	"github.com/pkg/errors"
)

// fixedNow 固定「今天」，让区间与预测日期可断言。2026-09-21 为周一。
func fixedNow(t *testing.T) time.Time {
	t.Helper()
	loc := priceLocation()
	return time.Date(2026, 9, 21, 10, 0, 0, 0, loc)
}

// fakeSource 造 n 天的已确认日前成交价，最后一天为 2026-09-21。
func fakeSource(n int) (priceSeriesSource, *[2]string) {
	var seen [2]string
	loc := priceLocation()
	last := time.Date(2026, 9, 21, 0, 0, 0, 0, loc)
	return func(_ context.Context, from, to string) ([]*s_db_model.SSpotMetricModel, error) {
		seen[0], seen[1] = from, to
		rows := make([]*s_db_model.SSpotMetricModel, 0, n)
		for i := n - 1; i >= 0; i-- {
			d := last.AddDate(0, 0, -i).Format(priceDateLayout)
			rows = append(rows, metric(d, spotreport.DatasetTrade, priceScope, priceMetric,
				spotreport.SourceTrade, 300+float64(n-i), "元/MWh"))
		}
		return rows, nil
	}, &seen
}

// timesfmStub 起一个假 TimesFM，返回刻意乱序的结果以验证按 id 取用。
func timesfmStub(t *testing.T, wantHorizon int, captured **s_forecast.SForecastRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/forecast" {
			t.Errorf("意外的请求: %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req s_forecast.SForecastRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("请求体解析失败: %v", err)
		}
		if captured != nil {
			*captured = &req
		}
		if len(req.Series) != 1 || req.Series[0].Id != "ahead" {
			t.Errorf("序列 id 错误: %+v", req.Series)
		}
		// 乱序：目标序列放在第二个
		_, _ = w.Write([]byte(`{
			"success": true, "model": "timesfm-3.0", "horizon": ` + strconv.Itoa(wantHorizon) + `,
			"quantiles": [0.1, 0.5, 0.9],
			"forecasts": [
				{"id": "noise", "point": [1, 2, 3]},
				{"id": "ahead", "point": [300, 310, 320],
				 "quantiles": {"0.1": [280, 290, 300], "0.5": [300, 310, 320], "0.9": [330, 340, 350]}}
			],
			"context_lengths": [10], "time_ms": 12.5
		}`))
	}))
}

func TestLoadPriceWithTimesFM(t *testing.T) {
	var captured *s_forecast.SForecastRequest
	srv := timesfmStub(t, 3, &captured)
	defer srv.Close()

	source, seen := fakeSource(10)
	client := s_forecast.NewClient(srv.URL, 5*time.Second)
	spec, _ := resolvePriceSpec("ahead")

	got, err := loadPriceWith(context.Background(), client, source, spec,
		60, 3, []float64{0.1, 0.5, 0.9}, priceLocation(), fixedNow(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 区间：今天往前推 60 天（含今天）
	if seen[0] != "2026-07-24" || seen[1] != "2026-09-21" {
		t.Errorf("读取区间 = %s~%s, want 2026-07-24~2026-09-21", seen[0], seen[1])
	}
	// 序列：10 天，升序
	if len(got.HistoryDates) != 10 || got.HistoryDates[9] != "2026-09-21" {
		t.Errorf("历史日期错误: %v", got.HistoryDates)
	}
	if got.HistoryValues[9] != 310 {
		t.Errorf("历史值末尾 = %v, want 310", got.HistoryValues[9])
	}
	// 预测：按 id 取到 ahead，而不是下标 0 的 noise
	if len(got.ForecastPoint) != 3 || got.ForecastPoint[0] != 300 {
		t.Fatalf("取到的不是 id=ahead 的序列: %v", got.ForecastPoint)
	}
	// 预测日期接在最后一个历史日之后
	want := []string{"2026-09-22", "2026-09-23", "2026-09-24"}
	for i, w := range want {
		if got.ForecastDates[i] != w {
			t.Errorf("预测日 %d = %s, want %s", i, got.ForecastDates[i], w)
		}
	}
	// 分位整条搬过来
	if len(got.ForecastQuantiles["0.9"]) != 3 || got.ForecastQuantiles["0.9"][2] != 350 {
		t.Errorf("分位错误: %v", got.ForecastQuantiles)
	}
	// 元信息
	if got.Model != "timesfm-3.0" || got.TimeMs != 12.5 {
		t.Errorf("模型/耗时错误: %s %v", got.Model, got.TimeMs)
	}
	if got.Unit != "元/MWh" {
		t.Errorf("单位 = %q", got.Unit)
	}
	if got.ContextPoints != 10 || got.InputTruncated {
		t.Errorf("上下文点数/截断错误: %d %v", got.ContextPoints, got.InputTruncated)
	}
	// 60 天区间里只有 10 天有数据
	if got.MissingDays != 50 {
		t.Errorf("缺失天数 = %d, want 50", got.MissingDays)
	}
	if got.From != "2026-09-12" || got.To != "2026-09-21" {
		t.Errorf("实际区间 = %s~%s", got.From, got.To)
	}
	// 请求体：预期 horizon 与分位原样送达
	if captured.Horizon != 3 || len(captured.Quantiles) != 3 {
		t.Errorf("请求参数错误: horizon=%d quantiles=%v", captured.Horizon, captured.Quantiles)
	}
}

func TestLoadPriceInsufficientData(t *testing.T) {
	source, _ := fakeSource(3) // 少于 minPricePoints
	spec, _ := resolvePriceSpec("ahead")
	client := s_forecast.NewClient("http://127.0.0.1:1", time.Second)

	_, err := loadPriceWith(context.Background(), client, source, spec,
		60, 3, nil, priceLocation(), fixedNow(t))
	if err == nil || !strings.Contains(err.Error(), "数据不足") {
		t.Fatalf("应报数据不足，got %v", err)
	}
}

func TestLoadPriceSourceError(t *testing.T) {
	spec, _ := resolvePriceSpec("ahead")
	fail := func(context.Context, string, string) ([]*s_db_model.SSpotMetricModel, error) {
		return nil, errors.New("boom")
	}
	client := s_forecast.NewClient("http://127.0.0.1:1", time.Second)
	if _, err := loadPriceWith(context.Background(), client, fail, spec,
		60, 3, nil, priceLocation(), fixedNow(t)); err == nil {
		t.Fatal("数据源报错时应返回错误")
	}
}

func TestPickForecast(t *testing.T) {
	a := &s_forecast.SForecastItem{Id: "noise", Point: []float64{1}}
	b := &s_forecast.SForecastItem{Id: "ahead", Point: []float64{2}}
	if got := pickForecast([]*s_forecast.SForecastItem{a, b}, "ahead"); got != b {
		t.Errorf("应按 id 命中: %+v", got)
	}
	if got := pickForecast([]*s_forecast.SForecastItem{a, b}, "missing"); got != a {
		t.Errorf("id 缺失应回落到第一项: %+v", got)
	}
	if got := pickForecast([]*s_forecast.SForecastItem{nil, b}, "ahead"); got != b {
		t.Errorf("应跳过 nil 项: %+v", got)
	}
	if got := pickForecast(nil, "ahead"); got != nil {
		t.Errorf("空列表应返回 nil: %+v", got)
	}
	if got := pickForecast([]*s_forecast.SForecastItem{nil, nil}, "ahead"); got != nil {
		t.Errorf("全 nil 应返回 nil: %+v", got)
	}
}

func TestQuantileKey(t *testing.T) {
	if got := quantileKey([]float64{0.1, 0.5, 0.9}); got != "0.1|0.5|0.9" {
		t.Errorf("缓存键 = %q", got)
	}
	if got := quantileKey(nil); got != "" {
		t.Errorf("空分位应为空串, got %q", got)
	}
	// 不同分位点不能撞同一个缓存键
	if quantileKey([]float64{0.1, 0.9}) == quantileKey([]float64{0.5}) {
		t.Error("不同分位点不应生成相同缓存键")
	}
}
