package forecast

import (
	"context"
	"fmt"
	"sort"
	"time"

	"application/internal/logic/gdmetric"
	"common/c_log"
	"github.com/pkg/errors"
)

// 电价预测回测（1A）。
//
// 为什么需要它：留痕（1B）今天才开始积累，要在那之前有东西可看。
//
// 与留痕的区别必须说清楚 —— 回测是「**用现在的模型**在历史数据上重跑」，
// 不是「当时真发的预测」。模型或历史数据一变，回测数字就变。
// 所以页面要把两者分开展示，不能混成一个「准确率」。
//
// 代价：TimesFM 是 CPU 推理、单次约 2s、服务端并发 2。所以必须抽样，
// 不能整段回测 —— 30 天全跑要 60s，用户会以为页面卡死。
const (
	defaultBacktestSampleDays = 3
	maxBacktestSampleDays     = 30
	// maxBacktestPoints 回测目标日上限：6 天 ≈ 6 次推理 ≈ 12s，是同步请求能接受的量级
	maxBacktestPoints = 6
)

// SPriceBacktestPoint 一个目标时刻的「预测 vs 实际」
type SPriceBacktestPoint struct {
	TargetTime time.Time
	Predicted  float64
	Actual     float64
	Q10        float64
	Q50        float64
	Q90        float64
}

// SPriceBacktest 回测结果
type SPriceBacktest struct {
	Market      string
	Grain       string
	Unit        string
	Model       string
	From        string
	To          string
	SampleDays  int
	TargetDates []string
	Skipped     []string // 序列不足等跳过的日期
	Points      []SPriceBacktestPoint
	TimeMs      float64
}

// BacktestPriceIntraday 在 [from, to] 内抽样若干目标日，逐日做 96 点回测。
func BacktestPriceIntraday(ctx context.Context, market, from, to string, sampleDays int) (*SPriceBacktest, error) {
	spec, err := resolvePriceSpec(market)
	if err != nil {
		return nil, err
	}
	kind := gdmetric.KindDayAhead
	if spec.Market == "real" {
		kind = gdmetric.KindRealtime
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

	// 目标日必须真的有实际值，所以先拿实际序列来定候选日
	series, err := gdmetric.GetSeries(ctx, from, to, kind)
	if err != nil {
		return nil, errors.Wrap(err, "读取实际序列失败")
	}
	actualByDate := groupByDate(series)
	candidates := make([]string, 0, len(actualByDate))
	for d, pts := range actualByDate {
		if len(pts) >= 96 { // 只回测数据完整的日期，否则指标会被缺点的日子拉偏
			candidates = append(candidates, d)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return nil, errors.Errorf("区间 %s ~ %s 内没有完整（96 点）的%s电价数据", from, to, marketLabel(spec.Market))
	}

	// 抽样：从区间尾部往前取，保证取到的是最近的样本
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

	out := &SPriceBacktest{
		Market:      spec.Market,
		Grain:       "intraday",
		Unit:        intradayPriceUnit,
		SampleDays:  sampleDays,
		TargetDates: sampled,
	}
	var totalMs float64

	for _, date := range sampled {
		target, err := time.ParseInLocation(priceDateLayout, date, loc)
		if err != nil {
			out.Skipped = append(out.Skipped, date)
			continue
		}
		// 以目标日 0 点为界：只用它之前的数据，预测它这一天
		asOf := target.AddDate(0, 0, -1)
		pred, err := loadPriceIntradayAsOf(ctx, market, defaultIntradayHistoryDays, 1, defaultQuantiles, asOf)
		if err != nil {
			c_log.Warningf(ctx, "回测 %s 失败: %+v", date, err)
			out.Skipped = append(out.Skipped, date)
			continue
		}
		totalMs += pred.TimeMs
		if out.Model == "" {
			out.Model = pred.Model
		}
		actual := actualByDate[date]
		// 逐点配对：按「同一天内第几个 15 分钟点」对齐，避免因 24:00 归次日而错位
		for i, p := range pred.Points {
			if i >= len(actual) {
				break
			}
			out.Points = append(out.Points, SPriceBacktestPoint{
				TargetTime: p.TargetTime,
				Predicted:  p.Point,
				Actual:     actual[i],
				Q10:        p.Q10,
				Q50:        p.Q50,
				Q90:        p.Q90,
			})
		}
	}

	if len(out.Points) == 0 {
		return nil, errors.Errorf("回测未产出任何对比点（跳过 %d 天）", len(out.Skipped))
	}
	out.From = sampled[0]
	out.To = sampled[len(sampled)-1]
	out.TimeMs = totalMs
	return out, nil
}

// groupByDate 按日期归组，组内保持原始顺序（与 CSV 的时间顺序一致）
func groupByDate(series []gdmetric.TimedSample) map[string][]float64 {
	out := make(map[string][]float64, 32)
	for _, s := range series {
		out[s.Date] = append(out[s.Date], s.Value)
	}
	return out
}

// BacktestCached 带缓存的回测。回测要跑十几次推理，缓存 30 分钟。
func BacktestCached(
	ctx context.Context, key string, force bool, produce func(context.Context) (*SPriceBacktest, error),
) (*SPriceBacktest, error) {
	return withForecastCacheTTL(ctx, key, force, backtestCacheDuration, produce)
}

// BacktestCacheKey 回测结果的缓存键（回测贵，同样的参数不该重复跑）
func BacktestCacheKey(market, from, to string, sampleDays int) string {
	return fmt.Sprintf("forecast:backtest:%s:%s:%s:%d", market, from, to, sampleDays)
}
