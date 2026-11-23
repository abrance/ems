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

// LEAR 预测服务（modelman 的 price-lear）：Lago et al. 2021 的 LASSO 线性回归强基线。
//
// 与 TimesFM / GBM 互补：TimesFM 吃连续点序列，GBM 与 LEAR 吃同一套结构化特征
// （滞后/滚动/电量/日历）。90 天回测里 LEAR 是强基线：取数口径 H=8（目标日距锚点
// 8 天）时 LEAR MAE 88.7 / MAPE 18.66%，反超 TimesFM（92.7）与 GBM（101.3）。
//
// 本 client 与 price_gbm.go 完全同构（同一套 HTTP 契约），但常量独立，与 GBM 解耦。
const (
	// learForecastPath 业务端点（见 modelman services/price-lear，与 price-gbm 同契约）
	learForecastPath = "/v1/price-forecast"
	// learMaxDays LEAR 侧 MAX_DAYS 上限，与 services/price-lear 的默认值一致
	learMaxDays = 60
	// learTimeout 单次调用超时。LEAR numpy 推理毫秒级，给足余量只为挡住服务卡死
	learTimeout = 20 * time.Second
)

// TPriceForecastLEAR LEAR 的 24 点结果，与 TimesFM/GBM 的 Points 同构。
type TPriceForecastLEAR struct {
	Model       string
	TargetDate  string
	HorizonDays int
	// HistoryDays 实际送入的历史完整日数（连续、以锚点为终点）
	HistoryDays int
	Points      []TPriceForecastPoint
	TimeMs      float64
}

// learBaseURL 解析 LEAR 服务地址：环境变量 > 配置文件，都不配返回空（=不启用对比）。
// 与 GBM 同一套优先级，运维换地址不必改代码。
var learBaseURL string

func resolveLEARBaseURL(ctx context.Context) string {
	if strings.TrimSpace(learBaseURL) != "" {
		return learBaseURL
	}
	if v := strings.TrimSpace(os.Getenv("PRICE_LEAR_BASE_URL")); v != "" {
		learBaseURL = strings.TrimRight(v, "/")
		return learBaseURL
	}
	if g.Cfg() != nil {
		if v := strings.TrimSpace(g.Cfg().MustGet(ctx, "price_lear.base_url").String()); v != "" {
			learBaseURL = strings.TrimRight(v, "/")
		}
	}
	return learBaseURL
}

type learDay struct {
	Date   string    `json:"date"`
	Values []float64 `json:"values"`
	Power  []float64 `json:"power,omitempty"`
}

type learRequest struct {
	Days   []learDay `json:"days"`
	Target string    `json:"target"`
}

// 字段名照 modelman price-lear 的响应（snake_case）：与 price-gbm 同一份契约
// PriceForecastResponse，写错 tag 不会报错，只会静默拿到 0 值。
type learResponse struct {
	Success     bool                 `json:"success"`
	Error       string               `json:"error"`
	Model       string               `json:"model"`
	Target      string               `json:"target"`
	HorizonDays int                  `json:"horizon_days"`
	Point       []float64            `json:"point"`
	Quantiles   map[string][]float64 `json:"quantiles"`
	TimeMs      float64              `json:"time_ms"`
}

// attachLEAR 在 compare 模式下把 LEAR 的预测挂到结果上。
//
// 失败只写 LEARError，不返回 error：对比是附加信息，LEAR 服务没配/挂了不该让
// 整张 TimesFM 图变成空白。前端据此显示「LEAR 不可用」而不是报错。
func attachLEAR(ctx context.Context, result *TPriceForecastResult, market string) {
	base := resolveLEARBaseURL(ctx)
	if base == "" {
		result.LEARError = "未配置 LEAR 预测服务（设 PRICE_LEAR_BASE_URL 或 price_lear.base_url）"
		return
	}
	anchor, err := time.ParseInLocation(priceDateLayout, result.To, priceLocation())
	if err != nil {
		result.LEARError = "锚点日期无效: " + result.To
		return
	}

	// 从锚点往前取连续完整日（LEAR 的特征与 GBM 同源，按连续日算滞后与滚动）
	from := anchor.AddDate(0, 0, -(learMaxDays - 1)).Format(priceDateLayout)
	rows, err := defaultHourSeriesSource(ctx, from, result.To)
	if err != nil {
		result.LEARError = "读取 24 点分时失败: " + err.Error()
		return
	}
	lear, err := requestLEAR(ctx, base, rows, market, result.TargetDate, anchor)
	if err != nil {
		result.LEARError = err.Error()
		return
	}
	result.LEAR = lear
}

// requestLEAR 组装请求并解析结果。抽出来是为了测试能直接喂桩行数据。
func requestLEAR(
	ctx context.Context,
	base string,
	rows []*s_db_model.SHome24hModel,
	market, targetDate string,
	anchor time.Time,
) (*TPriceForecastLEAR, error) {
	values := groupHome24h(rows, market)
	power := groupHome24hPower(rows)

	// 从锚点倒着取完整日，跳过 0 点空洞，再反转成升序：服务要求 days 升序且最后一天是锚点。
	// 与主路径一致：某天 0 点（采集空洞）不该截断整个历史窗口。
	days := make([]learDay, 0, learMaxDays)
	fromDate := anchor.AddDate(0, 0, -(learMaxDays - 1))
	for i := 0; i < learMaxDays && len(days) < learMaxDays; i++ {
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
		day := learDay{Date: key, Values: make([]float64, home24hPointsPerDay)}
		for h := 0; h < home24hPointsPerDay; h++ {
			day.Values[h] = hours[h]
		}
		// 电量是可选的：缺了按缺失特征处理，不给它补 0（0 会被当成真实负荷）
		if pw, ok := power[key]; ok && len(pw) == home24hPointsPerDay {
			day.Power = make([]float64, home24hPointsPerDay)
			for h := 0; h < home24hPointsPerDay; h++ {
				day.Power[h] = pw[h]
			}
		}
		days = append(days, day)
	}
	if len(days) < minHourHistoryDays {
		return nil, errors.Errorf("LEAR 输入不足：锚点 %s 往前只有 %d 个连续完整日（至少 %d 天）",
			anchor.Format(priceDateLayout), len(days), minHourHistoryDays)
	}
	for i, j := 0, len(days)-1; i < j; i, j = i+1, j-1 {
		days[i], days[j] = days[j], days[i]
	}

	body, err := json.Marshal(learRequest{Days: days, Target: targetDate})
	if err != nil {
		return nil, errors.Wrap(err, "序列化 LEAR 请求失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+learForecastPath, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "构造 LEAR 请求失败")
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: learTimeout}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "调用 LEAR 预测服务失败（%s）", base)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, errors.Wrap(err, "读取 LEAR 响应失败")
	}
	var parsed learResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, errors.Wrapf(err, "解析 LEAR 响应失败（HTTP %d）", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK || !parsed.Success {
		msg := parsed.Error
		if msg == "" {
			msg = truncateText(string(raw), 160)
		}
		return nil, errors.Errorf("LEAR 预测失败（HTTP %d）：%s", res.StatusCode, msg)
	}
	if len(parsed.Point) != home24hPointsPerDay {
		return nil, errors.Errorf("LEAR 返回 %d 个点，需要 %d 点", len(parsed.Point), home24hPointsPerDay)
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
	return &TPriceForecastLEAR{
		Model:       parsed.Model,
		TargetDate:  targetDate,
		HorizonDays: parsed.HorizonDays,
		HistoryDays: len(days),
		Points:      points,
		TimeMs:      parsed.TimeMs,
	}, nil
}
