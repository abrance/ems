package forecast

import (
	"context"
	"fmt"
	"sort"
	"time"

	"common/c_log"
	"s_forecast"

	"github.com/pkg/errors"
)

// 电价预测回测（日频，官方直采真值）。
//
// 为什么另起一条腿：96 点回测的真值只有 gd_metric.csv（第三方快照）一条来源，
// 那条通道 2026-11-11 起主动停更（config gd_metric.enabled=false），实际值冻结在
// 2026-10-02。继续拿它在「近 7 天」上算准确度，得到的是一个几周前区间的数字，
// 却长得像近期表现 —— 这是「复盘看着对不上」的真正原因。
//
// 这里换成官方直采的日均成交价（gd_spot_metric，origin ∈ {manual, pm-api}）：
//   - 真值：同一天同一指标（发电侧日前/实时加权平均电价），有就是有，没有就是空态；
//   - 预测：**as-of** 语义 —— 预测 D 日只用 D-1 及以前的历史，与当时能拿到的信息一致；
//   - 复用 LoadPrice 的同一套取数与模型调用，不另发明口径。
//
// 一天一点，所以不叫「96 点回测」。分时（96 点）那条腿保留给历史演示，
// 但真值一断就必须报陈旧（见 backtest.go 的 MaxBacktestLagDays）。
const (
	// dailyBacktestHistoryDays 每次 as-of 预测送入模型的历史天数（与产品默认一致）
	dailyBacktestHistoryDays = 60
	// priceTruthSourceOfficial 日频真值来源标识，随结果下发，页面原样显示
	priceTruthSourceOfficial = "gd_spot_metric（官方直采，发电侧加权平均电价）"
)

// SDailyBacktestPoint 一个目标日的「预测日均价 vs 实际日均价」
type SDailyBacktestPoint struct {
	TargetDate string
	Predicted  float64
	Actual     float64
	Q10        float64
	Q50        float64
	Q90        float64
}

// SDailyBacktest 日频回测结果
type SDailyBacktest struct {
	Market      string
	Grain       string
	Unit        string
	Model       string
	From        string
	To          string
	SampleDays  int
	HistoryDays int
	TargetDates []string
	Skipped     []string
	Points      []SDailyBacktestPoint
	TimeMs      float64
	// 新鲜度（含义同 SPriceBacktest）
	DataAsOf    string
	LagDays     int
	Stale       bool
	TruthSource string
}

// BacktestPriceDaily 在 [from, to] 内抽样若干目标日，逐日做「as-of 预测 vs 官方实际值」。
//
// as-of 的关键：asOf = 目标日 - 1 天，历史区间取 [asOf-59, asOf]，模型只看得到那时
// 已经发布的数据。所以这不是「用现在的模型+现在才知道的数据」，而是「当时的信息 + 现在的模型」。
func BacktestPriceDaily(ctx context.Context, market, from, to string, sampleDays int) (*SDailyBacktest, error) {
	spec, err := resolvePriceSpec(market)
	if err != nil {
		return nil, err
	}
	if sampleDays <= 0 {
		sampleDays = defaultBacktestSampleDays
	}
	if sampleDays > maxBacktestSampleDays {
		sampleDays = maxBacktestSampleDays
	}

	loc := priceLocation()
	fromT, err := time.ParseInLocation(priceDateLayout, from, loc)
	if err != nil {
		return nil, errors.Errorf("起始日期无效: %s", from)
	}
	toT, err := time.ParseInLocation(priceDateLayout, to, loc)
	if err != nil {
		return nil, errors.Errorf("结束日期无效: %s", to)
	}
	if toT.Before(fromT) {
		return nil, errors.New("结束日期不能早于起始日期")
	}

	// 真值只认「已确认」的官方指标（ListMetricsByRunDateRange 在 SQL 里已排除未复核行）。
	// 查询右端放到今天：DataAsOf 要说的是「真值本身最新到哪天」，不是「窗口里最后一天」。
	today := time.Now().In(loc).Format(priceDateLayout)
	models, err := spotMetricSource(ctx, from, today)
	if err != nil {
		return nil, errors.Wrap(err, "查询官方电价指标失败")
	}
	dates, values, unit := buildDailySeries(models, spec)
	if len(dates) == 0 {
		return nil, errors.Errorf("区间 %s ~ %s 内没有已确认的%s电价数据（官方直采）",
			from, today, marketLabel(spec.Market))
	}
	actual := make(map[string]float64, len(dates))
	for i, d := range dates {
		actual[d] = values[i]
	}
	dataAsOf := dates[len(dates)-1]
	lagDays, stale := truthFreshness(dataAsOf, time.Now(), loc)

	// 候选目标日 = 区间内**有实际值**的日子；从尾部往前抽样，取最近的样本。
	candidates := make([]string, 0, len(dates))
	for _, d := range dates {
		if d >= from && d <= to {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) == 0 {
		return nil, errors.Errorf("区间 %s ~ %s 内没有官方实际值（真值最新到 %s）", from, to, dataAsOf)
	}
	sampled := make([]string, 0, maxBacktestPoints)
	for i := len(candidates) - 1; i >= 0 && len(sampled) < maxBacktestPoints; i -= sampleDays {
		sampled = append(sampled, candidates[i])
	}
	sort.Strings(sampled)

	baseURL, _, _, err := loadForecastConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("策略未配置预测服务地址")
	}
	client := s_forecast.NewClient(baseURL, forecastTimeout)

	out := &SDailyBacktest{
		Market:      spec.Market,
		Grain:       "day",
		Unit:        unit,
		From:        sampled[0],
		To:          sampled[len(sampled)-1],
		SampleDays:  sampleDays,
		HistoryDays: dailyBacktestHistoryDays,
		TargetDates: sampled,
		DataAsOf:    dataAsOf,
		LagDays:     lagDays,
		Stale:       stale,
		TruthSource: priceTruthSourceOfficial,
	}

	var totalMs float64
	for _, date := range sampled {
		target, err := time.ParseInLocation(priceDateLayout, date, loc)
		if err != nil {
			out.Skipped = append(out.Skipped, date)
			continue
		}
		asOf := target.AddDate(0, 0, -1)
		pred, err := loadPriceWith(ctx, client, spotMetricSource, spec,
			dailyBacktestHistoryDays, 1, defaultQuantiles, loc, asOf)
		if err != nil {
			c_log.Warningf(ctx, "日频回测 %s 失败: %+v", date, err)
			out.Skipped = append(out.Skipped, date)
			continue
		}
		totalMs += pred.TimeMs
		if out.Model == "" {
			out.Model = pred.Model
		}
		// 预测窗口第一天必须是目标日；不是说明历史尾部与目标日错位，宁可跳过也不硬凑。
		if len(pred.ForecastDates) == 0 || pred.ForecastDates[0] != date || len(pred.ForecastPoint) == 0 {
			out.Skipped = append(out.Skipped, date)
			continue
		}
		out.Points = append(out.Points, SDailyBacktestPoint{
			TargetDate: date,
			Predicted:  pred.ForecastPoint[0],
			Actual:     actual[date],
			Q10:        quantileAt(pred.ForecastQuantiles, "0.1", 0),
			Q50:        quantileAt(pred.ForecastQuantiles, "0.5", 0),
			Q90:        quantileAt(pred.ForecastQuantiles, "0.9", 0),
		})
	}

	if len(out.Points) == 0 {
		return nil, errors.Errorf("日频回测未产出任何对比点（跳过 %d 天）", len(out.Skipped))
	}
	out.TimeMs = totalMs
	return out, nil
}

// DailyBacktestCached 带缓存的日频回测（一天一次推理，比 96 点便宜，缓存同样 30 分钟）。
func DailyBacktestCached(
	ctx context.Context, key string, force bool, produce func(context.Context) (*SDailyBacktest, error),
) (*SDailyBacktest, error) {
	return withForecastCacheTTL(ctx, key, force, backtestCacheDuration, produce)
}

// DailyBacktestCacheKey 日频回测的缓存键。与 96 点那条腿必须分开：
// 同样的 from/to/sampleDays 在两种粒度下结果完全不同，共用 key 会串味。
func DailyBacktestCacheKey(market, from, to string, sampleDays int) string {
	return fmt.Sprintf("forecast:backtest:day:%s:%s:%s:%d", market, from, to, sampleDays)
}
