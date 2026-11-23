package forecast

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/pkg/errors"

	"s_db/s_db_model"
)

// GBM 预测服务（modelman 的 price-gbm）：LightGBM 分位回归，吃结构化特征。
//
// 与 TimesFM 是互补关系，不是替代：TimesFM 吃连续点序列（通用时序先验），
// GBM 吃滞后/滚动/电量/日历特征（本地规律）。同一份历史日 as-of 重放里两者
// 的误差源不同，取均值通常比任一单模型稳（90 天回测：GBM 19.7% / TimesFM
// 20.6% / 融合 19.4% MAPE，口径 = 目标日距锚点 5 天）。
const (
	// gbmForecastPath 业务端点（见 modelman services/price-gbm）
	gbmForecastPath = "/v1/price-forecast"
	// gbmMaxDays GBM 侧 MAX_DAYS 上限，与 services/price-gbm 的默认值一致
	gbmMaxDays = 60
	// gbmTimeout 单次调用超时。GBM 实测毫秒级，给足余量只为挡住服务卡死
	gbmTimeout = 20 * time.Second
)

// TPriceForecastGBM 第二算法的 24 点结果，与 TimesFM 的 Points 同构，便于前端并排画。
type TPriceForecastGBM struct {
	Model       string
	TargetDate  string
	HorizonDays int
	// HistoryDays 实际送入的历史完整日数（连续、以锚点为终点）
	HistoryDays int
	Points      []TPriceForecastPoint
	TimeMs      float64
}

// gbmBaseURL 解析 GBM 服务地址：环境变量 > 配置文件，都不配返回空（=不启用对比）。
// 与官网网关同一套优先级，运维换地址不必改代码。
var gbmBaseURL string

func resolveGBMBaseURL(ctx context.Context) string {
	if strings.TrimSpace(gbmBaseURL) != "" {
		return gbmBaseURL
	}
	if v := strings.TrimSpace(os.Getenv("PRICE_GBM_BASE_URL")); v != "" {
		gbmBaseURL = strings.TrimRight(v, "/")
		return gbmBaseURL
	}
	if g.Cfg() != nil {
		if v := strings.TrimSpace(g.Cfg().MustGet(ctx, "price_gbm.base_url").String()); v != "" {
			gbmBaseURL = strings.TrimRight(v, "/")
		}
	}
	return gbmBaseURL
}

type gbmDay struct {
	Date   string    `json:"date"`
	Values []float64 `json:"values"`
	Power  []float64 `json:"power,omitempty"`
}

type gbmRequest struct {
	Days   []gbmDay `json:"days"`
	Target string   `json:"target"`
}

// 字段名照 modelman price-gbm 的响应（snake_case）：服务侧契约是
// PriceForecastResponse，写错 tag 不会报错，只会静默拿到 0 值。
type gbmResponse struct {
	Success     bool                 `json:"success"`
	Error       string               `json:"error"`
	Model       string               `json:"model"`
	Target      string               `json:"target"`
	HorizonDays int                  `json:"horizon_days"`
	Point       []float64            `json:"point"`
	Quantiles   map[string][]float64 `json:"quantiles"`
	TimeMs      float64              `json:"time_ms"`
}

// attachGBM 在 compare 模式下把 GBM 的预测挂到结果上。
//
// 失败只写 GBMError，不返回 error：对比是附加信息，GBM 服务没配/挂了不该让
// 整张 TimesFM 图变成空白。前端据此显示「GBM 不可用」而不是报错。
func attachGBM(ctx context.Context, result *TPriceForecastResult, market string) {
	base := resolveGBMBaseURL(ctx)
	if base == "" {
		result.GBMError = "未配置 GBM 预测服务（设 PRICE_GBM_BASE_URL 或 price_gbm.base_url）"
		return
	}
	anchor, err := time.ParseInLocation(priceDateLayout, result.To, priceLocation())
	if err != nil {
		result.GBMError = "锚点日期无效: " + result.To
		return
	}

	// 从锚点往前取连续完整日（GBM 的特征就是按连续日算滞后与滚动的）
	from := anchor.AddDate(0, 0, -(gbmMaxDays - 1)).Format(priceDateLayout)
	rows, err := defaultHourSeriesSource(ctx, from, result.To)
	if err != nil {
		result.GBMError = "读取 24 点分时失败: " + err.Error()
		return
	}
	gbm, err := requestGBM(ctx, base, rows, market, result.TargetDate, anchor)
	if err != nil {
		result.GBMError = err.Error()
		return
	}
	result.GBM = gbm
}

// requestGBM 组装请求并解析结果。抽出来是为了测试能直接喂桩行数据。
func requestGBM(
	ctx context.Context,
	base string,
	rows []*s_db_model.SHome24hModel,
	market, targetDate string,
	anchor time.Time,
) (*TPriceForecastGBM, error) {
	values := groupHome24h(rows, market)
	power := groupHome24hPower(rows)

	// 从锚点倒着取完整日，跳过 0 点空洞，再反转成升序：GBM 要求 days 升序且最后一天是锚点。
	// 与主路径一致：某天 0 点（采集空洞）不该截断整个历史窗口。
	days := make([]gbmDay, 0, gbmMaxDays)
	fromDate := anchor.AddDate(0, 0, -(gbmMaxDays - 1))
	for i := 0; i < gbmMaxDays && len(days) < gbmMaxDays; i++ {
		d := anchor.AddDate(0, 0, -i)
		if d.Before(fromDate) {
			break
		}
		key := d.Format(priceDateLayout)
		hours, ok := values[key]
		if !ok || len(hours) == 0 {
			continue // 0 点空洞：跳过
		}
		if len(hours) != home24hPointsPerDay {
			break // 不满 24 点：这段历史不取，主路径会点名报错
		}
		day := gbmDay{Date: key, Values: make([]float64, home24hPointsPerDay)}
		for h := 0; h < home24hPointsPerDay; h++ {
			day.Values[h] = hours[h]
		}
		// 电量是可选的：缺了 GBM 侧按缺失特征处理，不给它补 0（0 会被当成真实负荷）
		if pw, ok := power[key]; ok && len(pw) == home24hPointsPerDay {
			day.Power = make([]float64, home24hPointsPerDay)
			for h := 0; h < home24hPointsPerDay; h++ {
				day.Power[h] = pw[h]
			}
		}
		days = append(days, day)
	}
	if len(days) < minHourHistoryDays {
		return nil, errors.Errorf("GBM 输入不足：锚点 %s 往前只有 %d 个连续完整日（至少 %d 天）",
			anchor.Format(priceDateLayout), len(days), minHourHistoryDays)
	}
	for i, j := 0, len(days)-1; i < j; i, j = i+1, j-1 {
		days[i], days[j] = days[j], days[i]
	}

	body, err := json.Marshal(gbmRequest{Days: days, Target: targetDate})
	if err != nil {
		return nil, errors.Wrap(err, "序列化 GBM 请求失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+gbmForecastPath, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "构造 GBM 请求失败")
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: gbmTimeout}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "调用 GBM 预测服务失败（%s）", base)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, errors.Wrap(err, "读取 GBM 响应失败")
	}
	var parsed gbmResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, errors.Wrapf(err, "解析 GBM 响应失败（HTTP %d）", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK || !parsed.Success {
		msg := parsed.Error
		if msg == "" {
			msg = truncateText(string(raw), 160)
		}
		return nil, errors.Errorf("GBM 预测失败（HTTP %d）：%s", res.StatusCode, msg)
	}
	if len(parsed.Point) != home24hPointsPerDay {
		return nil, errors.Errorf("GBM 返回 %d 个点，需要 %d 点", len(parsed.Point), home24hPointsPerDay)
	}

	target, err := time.ParseInLocation(priceDateLayout, targetDate, priceLocation())
	if err != nil {
		return nil, errors.Errorf("目标日格式无效: %s", targetDate)
	}
	points := make([]TPriceForecastPoint, 0, home24hPointsPerDay)
	for h := 0; h < home24hPointsPerDay; h++ {
		points = append(points, TPriceForecastPoint{
			TargetTime: target.Add(time.Duration(h) * time.Hour),
			Point:      parsed.Point[h],
			Q10:        atOr(parsed.Quantiles["0.1"], h),
			Q50:        atOr(parsed.Quantiles["0.5"], h),
			Q90:        atOr(parsed.Quantiles["0.9"], h),
		})
	}
	return &TPriceForecastGBM{
		Model:       parsed.Model,
		TargetDate:  targetDate,
		HorizonDays: parsed.HorizonDays,
		HistoryDays: len(days),
		Points:      points,
		TimeMs:      parsed.TimeMs,
	}, nil
}

// groupHome24hPower 按日期分组存电量（GBM 的特征之一）。market 无关，两个市场共用一列。
func groupHome24hPower(rows []*s_db_model.SHome24hModel) map[string]map[int]float64 {
	out := make(map[string]map[int]float64, len(rows)/home24hPointsPerDay+1)
	for _, r := range rows {
		if r == nil || r.PriceDate == "" {
			continue
		}
		if out[r.PriceDate] == nil {
			out[r.PriceDate] = make(map[int]float64, home24hPointsPerDay)
		}
		out[r.PriceDate][r.Hour] = r.PowerMw
	}
	return out
}

func atOr(v []float64, i int) float64 {
	if i < len(v) {
		return v[i]
	}
	return 0
}

func truncateText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
