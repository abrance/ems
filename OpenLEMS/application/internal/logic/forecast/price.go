package forecast

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"application/internal/logic/spotreport"
	"s_db"
	"s_db/s_db_model"
	"s_forecast"

	"github.com/pkg/errors"
)

// 电价预测：复用同一套 TimesFM 服务（s_forecast），只是序列源从「设备功率点位」
// 换成「日报已确认的日均成交价」。
//
// 口径（与 logic/spotreport 提取器一致，改动前先看这里）：
//  1. 时间轴用 run_date（运行日），一天一个点。
//  2. 只看已确认（origin=manual）：未复核行在 ListMetricsByRunDateRange 的 SQL 里就剔除了。
//  3. 同一运行日可能有多份日报产出：日前成交优先取 表1(二)（当日报告口径），
//     缺失时才回落到 表2(一)（滞后 T-2 复核值）；实时成交取 表2(二)。
//  4. 96 点分时曲线日报未提取，本期输出日均价（对应前端「长周期电价预测-平均」）。
const (
	priceScope  = spotreport.ScopeSellSide
	priceMetric = spotreport.MetricAvgPrice

	// defaultPriceHistoryDays 默认送入模型的历史天数
	defaultPriceHistoryDays = 60
	// defaultPriceHorizonDays 默认预测天数
	defaultPriceHorizonDays = 5
	// maxPriceHorizonDays 上限：TimesFM max_horizon=512，按天算远够，卡 30 防止误传
	maxPriceHorizonDays = 30
	// minPricePoints 序列太短预测没意义，先卡一个下限
	minPricePoints = 7
	// priceDateLayout 运行日格式
	priceDateLayout = "2006-01-02"
)

// PriceRequest 电价预测请求。ForceRefresh=true 跳过缓存（前端手动刷新用）。
//
// Grain 决定序列源，不只是步长：
//
//	day（默认）—— 日报已确认成交价，一天一个点，出未来多日日均
//	intraday    —— 电网日指标 CSV 的 15 分钟电价，出未来 5 日 × 96 点
type PriceRequest struct {
	Market       string
	Grain        string
	HistoryDays  int
	HorizonDays  int
	Quantiles    []float64
	ForceRefresh bool
}

// ParseGrain 解析粒度参数；空值默认 day。
func ParseGrain(grain string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(grain)) {
	case "", s_db_model.ForecastGrainDay, "daily":
		return s_db_model.ForecastGrainDay, nil
	case s_db_model.ForecastGrainIntraday, "intra-day", "96":
		return s_db_model.ForecastGrainIntraday, nil
	default:
		return "", errors.Errorf("不支持的粒度: %s（可用 day / intraday）", grain)
	}
}

// priceSpec 一种市场对应的指标口径。
type priceSpec struct {
	Market  string
	Dataset string
	Source  string // 首选来源表，缺失时回落到同数据集的其他来源
}

// priceSeriesSource 历史指标读取函数，便于测试替换。
type priceSeriesSource func(ctx context.Context, from, to string) ([]*s_db_model.SSpotMetricModel, error)

// spotMetricSource 默认实现：读日报已确认的指标明细。
func spotMetricSource(ctx context.Context, from, to string) ([]*s_db_model.SSpotMetricModel, error) {
	return s_db.GetSpotExtractService().ListMetricsByRunDateRange(ctx, from, to)
}

// SPriceForecast 电价预测结果。
type SPriceForecast struct {
	Market            string
	Dataset           string
	Scope             string
	Metric            string
	Unit              string
	Model             string
	From              string
	To                string
	HistoryDates      []string
	HistoryValues     []float64
	ForecastDates     []string
	ForecastPoint     []float64
	ForecastQuantiles map[string][]float64
	ContextPoints     int
	InputTruncated    bool
	MissingDays       int
	TimeMs            float64
}

// LoadPrice 读取日报已确认的日均成交价并调用 TimesFM 预测未来多日。
// 结果按请求参数缓存 forecastCacheDuration。
func LoadPrice(ctx context.Context, req PriceRequest) (*SPriceForecast, error) {
	spec, err := resolvePriceSpec(req.Market)
	if err != nil {
		return nil, err
	}
	historyDays, horizonDays := req.HistoryDays, req.HorizonDays
	if historyDays <= 0 {
		historyDays = defaultPriceHistoryDays
	}
	if horizonDays <= 0 {
		horizonDays = defaultPriceHorizonDays
	}
	if horizonDays > maxPriceHorizonDays {
		horizonDays = maxPriceHorizonDays
	}
	quantiles := req.Quantiles
	if len(quantiles) == 0 {
		quantiles = defaultQuantiles
	}

	baseURL, _, _, err := loadForecastConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("策略未配置预测服务地址")
	}

	key := fmt.Sprintf("forecast:price:%s:%d:%d:%s", spec.Market, historyDays, horizonDays, quantileKey(quantiles))
	return withForecastCache(ctx, key, req.ForceRefresh, func(ctx context.Context) (*SPriceForecast, error) {
		client := s_forecast.NewClient(baseURL, forecastTimeout)
		return loadPriceWith(ctx, client, spotMetricSource, spec, historyDays, horizonDays, quantiles, priceLocation(), time.Now())
	})
}

// loadPriceWith 执行一次电价预测（读指标 + 调模型），不含缓存与配置解析，便于测试注入。
func loadPriceWith(
	ctx context.Context,
	client s_forecast.IClient,
	source priceSeriesSource,
	spec priceSpec,
	historyDays, horizonDays int,
	quantiles []float64,
	loc *time.Location,
	now time.Time,
) (*SPriceForecast, error) {
	today := now.In(loc)
	// 日前价格当日发布，历史区间到「今天」为止；实时价可能今天还没有，缺就是缺。
	to := today.Format(priceDateLayout)
	from := today.AddDate(0, 0, -historyDays+1).Format(priceDateLayout)

	models, err := source(ctx, from, to)
	if err != nil {
		return nil, errors.Wrap(err, "查询日报电价指标失败")
	}

	dates, values, unit := buildDailySeries(models, spec)
	if len(dates) < minPricePoints {
		return nil, errors.Errorf("已确认的%s电价数据不足（%d 天，至少需要 %d 天）",
			marketLabel(spec.Market), len(dates), minPricePoints)
	}

	// 截断到模型上下文上限（按天算几乎不会触发，保留防御）
	truncated := false
	if len(values) > maxContextPoints {
		dates = dates[len(dates)-maxContextPoints:]
		values = values[len(values)-maxContextPoints:]
		truncated = true
	}

	resp, err := client.Forecast(ctx, &s_forecast.SForecastRequest{
		Series:    []*s_forecast.SSeries{{Id: spec.Market, Values: values}},
		Horizon:   horizonDays,
		Quantiles: quantiles,
	})
	if err != nil {
		return nil, errors.Wrap(err, "调用预测服务失败")
	}
	item := pickForecast(resp.Forecasts, spec.Market)
	if item == nil || len(item.Point) == 0 {
		return nil, errors.New("预测服务未返回点预测")
	}

	forecastDates, err := nextDates(dates[len(dates)-1], len(item.Point), loc)
	if err != nil {
		return nil, err
	}

	return &SPriceForecast{
		Market:            spec.Market,
		Dataset:           spec.Dataset,
		Scope:             priceScope,
		Metric:            priceMetric,
		Unit:              unit,
		Model:             resp.Model,
		From:              dates[0],
		To:                dates[len(dates)-1],
		HistoryDates:      dates,
		HistoryValues:     values,
		ForecastDates:     forecastDates,
		ForecastPoint:     item.Point,
		ForecastQuantiles: item.Quantiles,
		ContextPoints:     len(values),
		InputTruncated:    truncated,
		MissingDays:       daysBetween(from, to) - len(dates),
		TimeMs:            resp.TimeMs,
	}, nil
}

// resolvePriceSpec 解析 market 参数。
func resolvePriceSpec(market string) (priceSpec, error) {
	switch strings.ToLower(strings.TrimSpace(market)) {
	case "", "ahead", "day_ahead", "day-ahead":
		return priceSpec{Market: "ahead", Dataset: spotreport.DatasetTrade, Source: spotreport.SourceTrade}, nil
	case "real", "realtime", "rt":
		return priceSpec{Market: "real", Dataset: spotreport.DatasetRealtime, Source: spotreport.SourceRealtime}, nil
	default:
		return priceSpec{}, errors.Errorf("不支持的市场类型: %s（仅支持 ahead / real）", market)
	}
}

// buildDailySeries 从指标明细构造「运行日 → 日均成交价」序列（升序）。
//
// 同一运行日的重复行按首选来源优先，其余回落；单位取第一条命中的行。
func buildDailySeries(models []*s_db_model.SSpotMetricModel, spec priceSpec) (dates []string, values []float64, unit string) {
	picked := map[string]*s_db_model.SSpotMetricModel{}
	for _, m := range models {
		if m == nil || m.Dataset != spec.Dataset || m.Scope != priceScope || m.Metric != priceMetric {
			continue
		}
		prev := picked[m.RunDate]
		if prev == nil || sourceRank(m.Source, spec.Source) < sourceRank(prev.Source, spec.Source) {
			picked[m.RunDate] = m
		}
	}
	dates = make([]string, 0, len(picked))
	for d := range picked {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	values = make([]float64, 0, len(dates))
	for _, d := range dates {
		m := picked[d]
		if unit == "" {
			unit = m.Unit
		}
		values = append(values, m.Value)
	}
	return dates, values, unit
}

// sourceRank 首选来源返回 0，其余返回 1，用于同日多来源时择优。
func sourceRank(source, preferred string) int {
	if source == preferred {
		return 0
	}
	return 1
}

// nextDates 由最后一个历史运行日推 n 个后续运行日。
func nextDates(last string, n int, loc *time.Location) ([]string, error) {
	t, err := time.ParseInLocation(priceDateLayout, last, loc)
	if err != nil {
		return nil, errors.Wrapf(err, "运行日格式无效: %s", last)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = t.AddDate(0, 0, i+1).Format(priceDateLayout)
	}
	return out, nil
}

// daysBetween 含首含尾的天数；解析失败返回 0。
func daysBetween(from, to string) int {
	loc := priceLocation()
	f, err1 := time.ParseInLocation(priceDateLayout, from, loc)
	t, err2 := time.ParseInLocation(priceDateLayout, to, loc)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(t.Sub(f).Hours()/24) + 1
}

// quantileKey 把分位点拼成稳定的缓存键片段。
func quantileKey(quantiles []float64) string {
	parts := make([]string, 0, len(quantiles))
	for _, q := range quantiles {
		parts = append(parts, strconv.FormatFloat(q, 'g', -1, 64))
	}
	return strings.Join(parts, "|")
}

// ParsePriceQuantiles 解析逗号分隔的分位点；空串返回 nil（由调用方取默认值）。
func ParsePriceQuantiles(raw string) ([]float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, errors.Errorf("分位点无效: %s", p)
		}
		if v <= 0 || v >= 1 {
			return nil, errors.Errorf("分位点需在 (0,1) 之间: %v", v)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func marketLabel(market string) string {
	if market == "real" {
		return "实时"
	}
	return "日前"
}

func priceLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}
