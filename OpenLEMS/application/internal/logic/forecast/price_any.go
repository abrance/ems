package forecast

import (
	"context"
	"fmt"
	"strings"
	"time"

	"s_db/s_db_model"
)

// 统一入口：按粒度分派到 day / intraday，并在返回前写留痕。
//
// 为什么要有这一层：控制器只该做参数校验和 DTO 映射，不该知道「粒度不同 → 序列源不同
// → 两个函数」这件事。留痕也必须两条路径都写，放在分派这一层才不会有漏。

// TPriceForecastResult 两种粒度统一的返回形状
type TPriceForecastResult struct {
	Market         string
	Grain          string
	Dataset        string
	Scope          string
	Metric         string
	Unit           string
	Model          string
	From           string
	To             string
	StepMinutes    float64
	HistoryDates   []string
	HistoryValues  []float64
	Points         []TPriceForecastPoint
	ContextPoints  int
	InputTruncated bool
	// InputChannels 送入模型的通道数（1=只用本市场历史；实时电价=3）
	InputChannels int
	MissingDays   int
	TimeMs        float64
	GeneratedOn   string
	// Recorded 本次写入留痕的条数；0 表示没写进去（已在日志里告警）
	Recorded int
}

// LoadPriceAny 按 req.Grain 分派预测，并写留痕。
func LoadPriceAny(ctx context.Context, req PriceRequest) (*TPriceForecastResult, error) {
	grain, err := ParseGrain(req.Grain)
	if err != nil {
		return nil, err
	}

	var out *TPriceForecastResult
	if grain == s_db_model.ForecastGrainIntraday {
		out, err = loadIntradayCached(ctx, req)
	} else {
		out, err = loadDaily(ctx, req)
	}
	if err != nil {
		return nil, err
	}

	// 留痕：best-effort。写不进去只告警，不让用户看不到预测结果。
	out.Recorded = recordForecastPoints(ctx, out)
	return out, nil
}

func loadDaily(ctx context.Context, req PriceRequest) (*TPriceForecastResult, error) {
	res, err := LoadPrice(ctx, req)
	if err != nil {
		return nil, err
	}
	points := make([]TPriceForecastPoint, 0, len(res.ForecastPoint))
	for i, p := range res.ForecastPoint {
		date := ""
		if i < len(res.ForecastDates) {
			date = res.ForecastDates[i]
		}
		points = append(points, TPriceForecastPoint{
			TargetTime: parseDayTarget(date),
			Point:      p,
			Q10:        quantileAt(res.ForecastQuantiles, "0.1", i),
			Q50:        quantileAt(res.ForecastQuantiles, "0.5", i),
			Q90:        quantileAt(res.ForecastQuantiles, "0.9", i),
		})
	}
	return &TPriceForecastResult{
		Market:         res.Market,
		Grain:          s_db_model.ForecastGrainDay,
		Dataset:        res.Dataset,
		Scope:          res.Scope,
		Metric:         res.Metric,
		Unit:           res.Unit,
		Model:          res.Model,
		From:           res.From,
		To:             res.To,
		StepMinutes:    24 * 60,
		HistoryDates:   res.HistoryDates,
		HistoryValues:  res.HistoryValues,
		Points:         points,
		ContextPoints:  res.ContextPoints,
		InputTruncated: res.InputTruncated,
		MissingDays:    res.MissingDays,
		TimeMs:         res.TimeMs,
		GeneratedOn:    priceLocationNow().Format(priceDateLayout),
	}, nil
}

func loadIntradayCached(ctx context.Context, req PriceRequest) (*TPriceForecastResult, error) {
	historyDays, horizonDays := req.HistoryDays, req.HorizonDays
	if historyDays <= 0 {
		historyDays = defaultIntradayHistoryDays
	}
	if horizonDays <= 0 {
		horizonDays = MaxIntradayHorizonDays
	}
	if horizonDays > MaxIntradayHorizonDays {
		horizonDays = MaxIntradayHorizonDays
	}
	quantiles := req.Quantiles
	if len(quantiles) == 0 {
		quantiles = defaultQuantiles
	}

	key := fmt.Sprintf("forecast:price96:%s:%d:%d:%s", req.Market, historyDays, horizonDays, quantileKey(quantiles))
	raw, err := withForecastCache(ctx, key, req.ForceRefresh, func(ctx context.Context) (*SPriceForecastIntraday, error) {
		return LoadPriceIntraday(ctx, req.Market, historyDays, horizonDays, quantiles)
	})
	if err != nil {
		return nil, err
	}

	dates := make([]string, 0, len(raw.HistoryTimes))
	for _, t := range raw.HistoryTimes {
		dates = append(dates, t.Format("2006-01-02 15:04"))
	}
	return &TPriceForecastResult{
		Market:         raw.Market,
		Grain:          raw.Grain,
		Dataset:        raw.Dataset,
		Scope:          "电网日指标",
		Metric:         metricLabel(raw.Market),
		Unit:           raw.Unit,
		Model:          raw.Model,
		From:           raw.From,
		To:             raw.To,
		StepMinutes:    IntradayStepMinutes,
		HistoryDates:   dates,
		HistoryValues:  raw.HistoryValues,
		Points:         raw.Points,
		ContextPoints:  raw.ContextPoints,
		InputTruncated: raw.InputTruncated,
		InputChannels:  raw.InputChannels,
		MissingDays:    daysBetween(raw.From, raw.To) - raw.CoveredDays,
		TimeMs:         raw.TimeMs,
		GeneratedOn:    raw.GeneratedOn,
	}, nil
}

// recordForecastPoints 把预测点写成留痕行。返回写入条数。
func recordForecastPoints(ctx context.Context, res *TPriceForecastResult) int {
	if res == nil || len(res.Points) == 0 {
		return 0
	}
	source := "gd_metric.csv"
	if res.Grain == s_db_model.ForecastGrainDay {
		source = "官方直采日均成交价"
	}
	rows := make([]*s_db_model.SPriceForecastModel, 0, len(res.Points))
	for _, p := range res.Points {
		rows = append(rows, &s_db_model.SPriceForecastModel{
			GeneratedOn: res.GeneratedOn,
			Market:      res.Market,
			Grain:       res.Grain,
			TargetTime:  formatTargetTime(res.Grain, p.TargetTime),
			Point:       p.Point,
			Q10:         p.Q10,
			Q50:         p.Q50,
			Q90:         p.Q90,
			Model:       res.Model,
			Unit:        res.Unit,
			Source:      source,
		})
	}
	n, err := RecordPriceForecast(ctx, rows)
	if err != nil {
		return 0
	}
	return n
}

// formatTargetTime day 粒度只存日期（便于按日 join 日报实际值）；
// intraday 存到分钟。
func formatTargetTime(grain string, t time.Time) string {
	if grain == s_db_model.ForecastGrainDay {
		return t.Format(priceDateLayout)
	}
	return t.Format("2006-01-02 15:04")
}

// FormatTargetTimeForDTO 供控制器拼 DTO：day 粒度给日期，intraday 给到分钟
func FormatTargetTimeForDTO(grain string, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatTargetTime(grain, t)
}

func parseDayTarget(date string) time.Time {
	t, err := time.ParseInLocation(priceDateLayout, strings.TrimSpace(date), priceLocation())
	if err != nil {
		return time.Time{}
	}
	return t
}

func metricLabel(market string) string {
	if market == "real" {
		return "实时电价（15 分钟）"
	}
	return "日前电价（15 分钟）"
}

func priceLocationNow() time.Time {
	return time.Now().In(priceLocation())
}
