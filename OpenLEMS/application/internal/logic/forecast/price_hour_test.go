package forecast

import (
	"context"
	"strings"
	"testing"
	"time"

	"s_db/s_db_model"
	"s_forecast"
)

// ---- 测试替身 ----

type stubHourSource struct {
	rows []*s_db_model.SHome24hModel
	err  error
	from string
	to   string
}

func (s *stubHourSource) load(_ context.Context, from, to string) ([]*s_db_model.SHome24hModel, error) {
	s.from, s.to = from, to
	return s.rows, s.err
}

type stubForecastClient struct {
	horizon int
	quant   []float64
	err     error
}

func (c *stubForecastClient) Forecast(_ context.Context, req *s_forecast.SForecastRequest) (*s_forecast.SForecastResponse, error) {
	c.horizon = req.Horizon
	c.quant = req.Quantiles
	if c.err != nil {
		return nil, c.err
	}
	// 点值用下标，方便断言「取了最后 24 点」
	point := make([]float64, req.Horizon)
	lo := make([]float64, req.Horizon)
	hi := make([]float64, req.Horizon)
	for i := range point {
		point[i] = float64(i)
		lo[i] = float64(i) - 10
		hi[i] = float64(i) + 10
	}
	return &s_forecast.SForecastResponse{
		Model: "stub-timesfm",
		Forecasts: []*s_forecast.SForecastItem{{
			Id:        req.Series[0].Id,
			Point:     point,
			Quantiles: map[string][]float64{"0.1": lo, "0.5": point, "0.9": hi},
		}},
		TimeMs: 1,
	}, nil
}

// hourRows 造某天完整 24 点；base 决定值，便于分辨日与市场。
func hourRows(date string, baseAhead, baseReal float64) []*s_db_model.SHome24hModel {
	rows := make([]*s_db_model.SHome24hModel, 0, home24hPointsPerDay)
	for h := 0; h < home24hPointsPerDay; h++ {
		rows = append(rows, &s_db_model.SHome24hModel{
			PriceDate:     date,
			Hour:          h,
			AheadPrice:    baseAhead + float64(h),
			RealtimePrice: baseReal + float64(h),
			PowerMw:       1000,
			Source:        "官网·24点",
		})
	}
	return rows
}

func dayStr(t time.Time) string { return t.Format(priceDateLayout) }

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation(priceDateLayout, s, priceLocation())
	if err != nil {
		t.Fatalf("解析日期 %s: %v", s, err)
	}
	return d
}

// historyWindow 从 anchor 往前 days 天的完整行（升序）。
func historyWindow(anchor time.Time, days int) []*s_db_model.SHome24hModel {
	var rows []*s_db_model.SHome24hModel
	for i := days - 1; i >= 0; i-- {
		d := anchor.AddDate(0, 0, -i)
		rows = append(rows, hourRows(dayStr(d), float64(100+i*10), float64(200+i*10))...)
	}
	return rows
}

// ---- 用例 ----

// 锚点必须是「目标日之前最新的完整 24 点日」：最新一天只有 17 点时要继续往前找。
func TestLoadPriceHourPicksLatestCompleteAnchor(t *testing.T) {
	target := mustDay(t, "2026-11-18")
	anchor := mustDay(t, "2026-11-16")
	rows := historyWindow(anchor, 7)
	// 11-17 只有 17 点，不能当锚点
	rows = append(rows, hourRows("2026-11-17", 900, 999)[:17]...)

	src := &stubHourSource{rows: rows}
	cli := &stubForecastClient{}
	got, err := loadPriceHourWith(context.Background(), cli, src.load, "ahead", "2026-11-18", 7,
		defaultQuantiles, priceLocation(), target)
	if err != nil {
		t.Fatalf("预测失败: %v", err)
	}
	if got.To != "2026-11-16" {
		t.Errorf("锚点 = %s，期望 2026-11-16（11-17 只有 17 点）", got.To)
	}
	if got.TargetDate != "2026-11-18" {
		t.Errorf("目标日 = %s，期望 2026-11-18", got.TargetDate)
	}
	if cli.horizon != 2*home24hPointsPerDay {
		t.Errorf("horizon = %d，期望 48（锚点 11-16 → 目标 11-18）", cli.horizon)
	}
}

// 目标日与锚点距离可变：1 天与 6 天都要出 24 点，且目标时刻落在目标日。
func TestLoadPriceHourTargetTimesLandOnTargetDay(t *testing.T) {
	for _, gap := range []int{1, 6} {
		target := mustDay(t, "2026-11-18")
		anchor := target.AddDate(0, 0, -gap)
		src := &stubHourSource{rows: historyWindow(anchor, 7)}
		cli := &stubForecastClient{}
		got, err := loadPriceHourWith(context.Background(), cli, src.load, "ahead", "2026-11-18", 7,
			defaultQuantiles, priceLocation(), target)
		if err != nil {
			t.Fatalf("gap=%d 预测失败: %v", gap, err)
		}
		if len(got.Points) != home24hPointsPerDay {
			t.Fatalf("gap=%d 点数 = %d，期望 24", gap, len(got.Points))
		}
		if cli.horizon != gap*home24hPointsPerDay {
			t.Errorf("gap=%d horizon = %d，期望 %d", gap, cli.horizon, gap*home24hPointsPerDay)
		}
		if want := "2026-11-18 00:00"; got.Points[0].TargetTime.Format("2006-01-02 15:04") != want {
			t.Errorf("gap=%d 首点 = %s，期望 %s", gap, got.Points[0].TargetTime, want)
		}
		if want := "2026-11-18 23:00"; got.Points[23].TargetTime.Format("2006-01-02 15:04") != want {
			t.Errorf("gap=%d 末点 = %s，期望 %s", gap, got.Points[23].TargetTime, want)
		}
		// 取的是「最后 24 点」：点值应等于 horizon-24 ... horizon-1
		if want := float64(cli.horizon - home24hPointsPerDay); got.Points[0].Point != want {
			t.Errorf("gap=%d 首点值 = %v，期望 %v（最后 24 点）", gap, got.Points[0].Point, want)
		}
		if got.StepMinutes != Home24hStepMinutes {
			t.Errorf("步长 = %v，期望 60", got.StepMinutes)
		}
	}
}

// 历史窗口中间缺一天要报错并点名，不能跳过去凑数。
func TestLoadPriceHourRejectsGapInHistory(t *testing.T) {
	target := mustDay(t, "2026-11-18")
	anchor := mustDay(t, "2026-11-16")
	var rows []*s_db_model.SHome24hModel
	for i := 6; i >= 0; i-- {
		d := anchor.AddDate(0, 0, -i)
		if i == 3 { // 缺一整天
			continue
		}
		rows = append(rows, hourRows(dayStr(d), 100, 200)...)
	}
	src := &stubHourSource{rows: rows}
	_, err := loadPriceHourWith(context.Background(), &stubForecastClient{}, src.load, "ahead", "2026-11-18", 7,
		defaultQuantiles, priceLocation(), target)
	if err == nil {
		t.Fatal("窗口缺一天应报错")
	}
	if !strings.Contains(err.Error(), "不足 24 点") {
		t.Errorf("错误信息应说明缺点，实际 %v", err)
	}
}

// 目标日距锚点超过模型单次上限：报错并告诉用户把目标日往前调，不静默截断。
func TestLoadPriceHourRejectsHorizonOverLimit(t *testing.T) {
	target := mustDay(t, "2026-11-18")
	anchor := target.AddDate(0, 0, -25) // 25 × 24 = 600 > 512
	src := &stubHourSource{rows: historyWindow(anchor, 7)}
	_, err := loadPriceHourWith(context.Background(), &stubForecastClient{}, src.load, "ahead", "2026-11-18", 7,
		defaultQuantiles, priceLocation(), target)
	if err == nil {
		t.Fatal("超出模型上限应报错")
	}
	if !strings.Contains(err.Error(), "超过模型单次上限") {
		t.Errorf("错误信息应说明上限，实际 %v", err)
	}
}

// market=real 要取实时列；ahead 取日前列。
func TestLoadPriceHourPicksMarketColumn(t *testing.T) {
	target := mustDay(t, "2026-11-18")
	anchor := target.AddDate(0, 0, -1)
	rows := historyWindow(anchor, 7)

	gotAhead, err := loadPriceHourWith(context.Background(), &stubForecastClient{}, (&stubHourSource{rows: rows}).load,
		"ahead", "2026-11-18", 7, defaultQuantiles, priceLocation(), target)
	if err != nil {
		t.Fatalf("ahead: %v", err)
	}
	gotReal, err := loadPriceHourWith(context.Background(), &stubForecastClient{}, (&stubHourSource{rows: rows}).load,
		"real", "2026-11-18", 7, defaultQuantiles, priceLocation(), target)
	if err != nil {
		t.Fatalf("real: %v", err)
	}
	if gotAhead.HistoryValues[len(gotAhead.HistoryValues)-1] == gotReal.HistoryValues[len(gotReal.HistoryValues)-1] {
		t.Error("ahead 与 real 应取不同的列")
	}
	if !strings.Contains(gotReal.Metric, "实时") {
		t.Errorf("real 指标名 = %q，应含「实时」", gotReal.Metric)
	}
}

// 目标日不满 24 点：actual 为空数组（不是 24 个 0），避免图上画成全天 0 元。
func TestLoadPriceHourActualOnlyWhenComplete(t *testing.T) {
	target := mustDay(t, "2026-11-18")
	anchor := target.AddDate(0, 0, -2)
	rows := historyWindow(anchor, 7)

	got, err := loadPriceHourWith(context.Background(), &stubForecastClient{}, (&stubHourSource{rows: rows}).load,
		"ahead", "2026-11-18", 7, defaultQuantiles, priceLocation(), target)
	if err != nil {
		t.Fatalf("预测失败: %v", err)
	}
	if got.Actual == nil || len(got.Actual) != 0 {
		t.Errorf("目标日无数据时 actual 应为空数组，实际 %v", got.Actual)
	}

	// 目标日补齐 24 点 → actual 有 24 个值
	rows = append(rows, hourRows("2026-11-18", 300, 400)...)
	got, err = loadPriceHourWith(context.Background(), &stubForecastClient{}, (&stubHourSource{rows: rows}).load,
		"ahead", "2026-11-18", 7, defaultQuantiles, priceLocation(), target)
	if err != nil {
		t.Fatalf("预测失败: %v", err)
	}
	if len(got.Actual) != home24hPointsPerDay {
		t.Fatalf("actual 长度 = %d，期望 24", len(got.Actual))
	}
	if got.Actual[0] != 300 || got.Actual[23] != 323 {
		t.Errorf("actual 应按小时升序取日前列，实际 %v", got.Actual[:3])
	}
}

// 目标日缺失时不能拿「今天」顶上：用户明确选了日期，悄悄改会让人以为在看那天的预测。
func TestNormalizeTargetDate(t *testing.T) {
	loc := priceLocation()
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, loc)

	if got, err := normalizeTargetDate("", loc, now); err != nil || got != "2026-11-18" {
		t.Errorf("空目标日应默认今天，实际 %q / %v", got, err)
	}
	if got, err := normalizeTargetDate("2026-11-01", loc, now); err != nil || got != "2026-11-01" {
		t.Errorf("合法历史日应原样返回，实际 %q / %v", got, err)
	}
	if _, err := normalizeTargetDate("2026-12-01", loc, now); err == nil {
		t.Error("晚于今天的目标日应报错")
	}
	if _, err := normalizeTargetDate("2026/11/01", loc, now); err == nil {
		t.Error("非法格式应报错")
	}
}

// 留痕来源文案按数据源给，不能都写成 gd_metric.csv（复盘时看不出这条是哪份数据）。
func TestLeafSourceLabel(t *testing.T) {
	cases := map[string]string{
		s_db_model.PriceSourceHome24h:     "官网主页24点分时",
		s_db_model.PriceSourceSpotMetric:  "官方直采日均成交价",
		s_db_model.PriceSourceGdMetricCSV: "gd_metric.csv",
	}
	for in, want := range cases {
		if got := leafSourceLabel(in); got != want {
			t.Errorf("leafSourceLabel(%s) = %q，期望 %q", in, got, want)
		}
	}
}

// 留痕的 generated_on 用锚点日（不是今天，也不是目标日）。
func TestLoadPriceHourGeneratedOnIsAnchor(t *testing.T) {
	target := mustDay(t, "2026-11-18")
	anchor := target.AddDate(0, 0, -4)
	got, err := loadPriceHourWith(context.Background(), &stubForecastClient{},
		(&stubHourSource{rows: historyWindow(anchor, 7)}).load,
		"ahead", "2026-11-18", 7, defaultQuantiles, priceLocation(), target)
	if err != nil {
		t.Fatalf("预测失败: %v", err)
	}
	if got.GeneratedOn != dayStr(anchor) {
		t.Errorf("generatedOn = %s，期望锚点 %s", got.GeneratedOn, dayStr(anchor))
	}
	if got.Source != s_db_model.PriceSourceHome24h {
		t.Errorf("source = %s，期望 home24h", got.Source)
	}
	if got.Grain != s_db_model.ForecastGrainHour {
		t.Errorf("grain = %s，期望 hour", got.Grain)
	}
}

// 历史长度按 historyDays 截取，且 From 是最早那天。
func TestLoadPriceHourHistoryWindow(t *testing.T) {
	target := mustDay(t, "2026-11-18")
	anchor := target.AddDate(0, 0, -1)
	// 给 10 天，只要 7 天
	rows := historyWindow(anchor, 10)
	got, err := loadPriceHourWith(context.Background(), &stubForecastClient{}, (&stubHourSource{rows: rows}).load,
		"ahead", "2026-11-18", 7, defaultQuantiles, priceLocation(), target)
	if err != nil {
		t.Fatalf("预测失败: %v", err)
	}
	if len(got.HistoryValues) != 7*home24hPointsPerDay {
		t.Fatalf("历史点数 = %d，期望 %d", len(got.HistoryValues), 7*home24hPointsPerDay)
	}
	if want := dayStr(anchor.AddDate(0, 0, -6)); got.From != want {
		t.Errorf("From = %s，期望 %s", got.From, want)
	}
	if got.ContextPoints != len(got.HistoryValues) {
		t.Errorf("contextPoints = %d，应与历史点数一致", got.ContextPoints)
	}
}

// 目标日之前完全没有完整日 → 明确报错，不返回空图。
func TestLoadPriceHourNoCompleteDay(t *testing.T) {
	src := &stubHourSource{rows: hourRows("2026-11-17", 1, 2)[:17]}
	_, err := loadPriceHourWith(context.Background(), &stubForecastClient{}, src.load,
		"ahead", "2026-11-18", 7, defaultQuantiles, priceLocation(), mustDay(t, "2026-11-18"))
	if err == nil {
		t.Fatal("没有完整 24 点日应报错")
	}
	if !strings.Contains(err.Error(), "没有完整的 24 点运行日") {
		t.Errorf("错误信息不对: %v", err)
	}
}
