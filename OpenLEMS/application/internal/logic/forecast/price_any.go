package forecast

import (
	"context"
	"fmt"
	"strings"
	"time"

	"common/c_log"
	"s_db/s_db_model"

	"github.com/pkg/errors"
)

// 统一入口：按 (source, grain) 分派到 day / hour / intraday，并在返回前写留痕。
//
// 为什么要有这一层：控制器只该做参数校验和 DTO 映射，不该知道「粒度不同 → 序列源不同
// → 两个函数」这件事。留痕也必须两条路径都写，放在分派这一层才不会有漏。

// TPriceForecastResult 两种粒度统一的返回形状
type TPriceForecastResult struct {
	Market string
	Source string
	Grain  string
	// TargetDate 仅 hour：目标日（预测终止日）
	TargetDate     string
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
	// Actual 仅 hour：目标日的 24 点实际值（该日不满 24 点时为空切片，不补 0）
	Actual      []float64
	MissingDays int
	TimeMs      float64
	GeneratedOn string
	// Recorded 本次写入留痕的条数；0 表示没写进去（已在日志里告警）
	Recorded int
	// GBM 仅 hour + compare 模式：第二算法（LightGBM）对同一目标日的 24 点预测。
	// 与 Points 同构，供前端并排对比；为 nil 表示没请求对比或服务未配置。
	GBM *TPriceForecastGBM
	// GBMError 对比失败/未配置的原因。有这个字段就不该让整张图变空：
	// 对比是附加信息，TimesFM 那半仍然可用。
	GBMError string
	// LEAR 仅 hour + compare 模式：第三算法（Lasso 线性回归强基线）的 24 点预测。
	// 与 GBM 同构；为 nil 表示没请求对比或服务未配置。
	LEAR *TPriceForecastLEAR
	// LEARError 对比失败/未配置的原因；与 GBMError 同样只告警不挡主结果。
	LEARError string
}

// priceLoaders 数据源 → loader。测试可替换表项断言分派，不必起 HTTP。
var priceLoaders = map[string]func(context.Context, PriceRequest) (*TPriceForecastResult, error){
	s_db_model.PriceSourceSpotMetric:  loadDaily,
	s_db_model.PriceSourceHome24h:     loadHourCached,
	s_db_model.PriceSourceGdMetricCSV: loadIntradayCached,
}

// LoadPriceAny 按 (source, grain) 分派预测，并写留痕。
func LoadPriceAny(ctx context.Context, req PriceRequest) (*TPriceForecastResult, error) {
	source, grain, err := ParsePriceSource(req.Source, req.Grain)
	if err != nil {
		return nil, err
	}
	req.Source, req.Grain = source, grain

	load, ok := priceLoaders[source]
	if !ok {
		return nil, errors.Errorf("未知数据源: %s", source)
	}
	out, err := load(ctx, req)
	if err != nil {
		return nil, err
	}
	out.Source = source

	// 留痕：best-effort。写不进去只告警，不让用户看不到预测结果。
	//
	// hour 例外：24 点留痕由每日任务统一写（固定时刻、固定锚点、全算法），
	// 页面请求不写。否则同一天多次请求会按唯一键互相覆盖，
	// 样本就不再是稳定的「当时预测」，回测也没法公平比较。
	if out.Grain != s_db_model.ForecastGrainHour {
		out.Recorded = recordForecastPoints(ctx, out)
	}
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
		Source:         s_db_model.PriceSourceSpotMetric,
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

	key := fmt.Sprintf("forecast:price:%s:%s:%d:%d:%s", s_db_model.PriceSourceGdMetricCSV, req.Market, historyDays, horizonDays, quantileKey(quantiles))
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
		Source:         s_db_model.PriceSourceGdMetricCSV,
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

// leafSourceLabel 留痕表 gd_price_forecast.source 列的值：给人看的来源文案。
// 注意它与请求参数 source（spot_metric / home24h / gd_metric_csv）同名不同义，别合并。
func leafSourceLabel(source string) string {
	switch source {
	case s_db_model.PriceSourceHome24h:
		return "官网主页24点分时"
	case s_db_model.PriceSourceSpotMetric:
		return "官方直采日均成交价"
	default:
		return "gd_metric.csv"
	}
}

// recordForecastPoints 把预测点写成留痕行。返回写入条数。
//
// 只写 target 严格晚于 generated_on 的点。历史序列止于今天-N 时，nextDates 从最后一条
// 历史日 +1 起算，会把已经过去的日期也当预测日输出；这些行会被复盘页的
// IsBackfilledForecast 全部剔除，留着只会让「当时预测」和「事后补算」在同一张表里混住。
func recordForecastPoints(ctx context.Context, res *TPriceForecastResult) int {
	if res == nil || len(res.Points) == 0 {
		return 0
	}
	source := leafSourceLabel(res.Source)
	rows := make([]*s_db_model.SPriceForecastModel, 0, len(res.Points))
	skipped := 0
	for _, p := range res.Points {
		targetTime := formatTargetTime(res.Grain, p.TargetTime)
		if !targetAfterGeneratedOn(res.Grain, res.GeneratedOn, targetTime) {
			skipped++
			continue
		}
		rows = append(rows, &s_db_model.SPriceForecastModel{
			GeneratedOn: res.GeneratedOn,
			Market:      res.Market,
			Grain:       res.Grain,
			Algo:        s_db_model.ForecastAlgoTimesFM,
			TargetTime:  targetTime,
			Point:       p.Point,
			Q10:         p.Q10,
			Q50:         p.Q50,
			Q90:         p.Q90,
			Model:       res.Model,
			Unit:        res.Unit,
			Source:      source,
		})
	}
	if skipped > 0 {
		c_log.Infof(ctx, "电价预测留痕跳过 %d 个目标日不晚于生成日 %s 的点（历史序列截止 %s）",
			skipped, res.GeneratedOn, res.To)
	}
	if len(rows) == 0 {
		c_log.Warningf(ctx, "电价预测留痕无可用目标日：%d 个点全部不晚于生成日 %s（历史序列截止 %s）",
			skipped, res.GeneratedOn, res.To)
		return 0
	}
	n, err := RecordPriceForecast(ctx, rows)
	if err != nil {
		return 0
	}
	return n
}

// targetAfterGeneratedOn 判断一个留痕目标时刻是否值得写。
//
// 只比日期部分，与复盘页的 IsBackfilledForecast 同源（它也是拿 generated_on 和
// target_time 的日期前缀比）—— 否则会出现「写进去了但复盘又剔掉」的割裂。
// generated_on 本身只有日期精度，所以做不到 brief 说的 intraday 比到分钟。
//
// 同日怎么处理，不同粒度不一样：
//   - day：一个日期就是一个点，日期等于生成日 = 「今天的日均价」，不构成预测，剔；
//   - hour / intraday：同日剩下的是当天后续时段的点，复盘页也保留，留。
//     代价是当天已过时段的点也会留下（无生成时刻可判），可接受。
func targetAfterGeneratedOn(grain, generatedOn, targetTime string) bool {
	g := strings.TrimSpace(generatedOn)
	if g == "" {
		return true
	}
	d := forecastDatePart(targetTime)
	if d > g {
		return true
	}
	if d < g {
		return false
	}
	return grain == s_db_model.ForecastGrainIntraday || grain == s_db_model.ForecastGrainHour
}

// formatTargetTime day 粒度只存日期（便于按日 join 日报实际值）；
// hour / intraday 存到分钟（整点即 HH:00）。
func formatTargetTime(grain string, t time.Time) string {
	if grain == s_db_model.ForecastGrainDay {
		return t.Format(priceDateLayout)
	}
	return t.Format("2006-01-02 15:04")
}

// FormatTargetTimeForDTO 供控制器拼 DTO：day 粒度给日期，hour / intraday 给到分钟
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
