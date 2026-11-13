package forecast

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	// MaxBacktestLagDays 实际值允许的最大滞后天数。
	//
	// 为什么要有：真值只有一个来源（gd_metric.csv 第三方快照），它一停更，
	// 回测就会在几周前的区间上算出一个很漂亮的数字，页面上看不出这是最近几周的，
	// 读的人自然以为「当前模型最近表现」。超过这个天数就不再默认给结果，
	// 让调用方显式 opt-in（或改用别的数据源），不静默返回旧区间。
	MaxBacktestLagDays = 7
	// priceTruthSourceGD 回测真值的来源标识，随结果下发，页面必须显示它。
	priceTruthSourceGD = "gd_metric.csv（第三方快照，实际值 sourceId=9）"
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
	// ActualMissing 因该时刻没有实际值而丢弃的预测点数（按时间戳配对配不上的）
	ActualMissing int
	TimeMs        float64
	// DataAsOf 本次回测实际用到的**实际值**最新日期（YYYY-MM-DD）。
	// 与 To（抽到的目标日）不是一回事：目标日可能比真值还早，真值断了才是关键信息。
	DataAsOf string
	// LagDays 实际值最新日期距今的天数（>=0）
	LagDays int
	// Stale 滞后超过 MaxBacktestLagDays —— 数字只能代表历史，不代表近期
	Stale bool
	// TruthSource 真值来源说明，页面原样显示，避免「数据从哪来的」考猜
	TruthSource string
}

// truthFreshness 由实际值最新日期算滞后天数与是否陈旧。
//
// 纯函数：单测不依赖网络与当前时钟。dataAsOf 为空 → 不知道有多旧，按陈旧处理
// （宁可让调用方 opt-in，也不拿「不知道」当「新鲜」）。
func truthFreshness(dataAsOf string, today time.Time, loc *time.Location) (int, bool) {
	if strings.TrimSpace(dataAsOf) == "" {
		return -1, true
	}
	asOf, err := time.ParseInLocation(priceDateLayout, dataAsOf, loc)
	if err != nil {
		return -1, true
	}
	d := today.In(loc)
	y, m, dd := d.Date()
	lag := int(time.Date(y, m, dd, 0, 0, 0, 0, loc).Sub(asOf).Hours() / 24)
	if lag < 0 {
		lag = 0
	}
	return lag, lag > MaxBacktestLagDays
}

// latestRealDate 实际序列里最新的日期（不是「96 点齐全」的最新日）。
//
// 用「有实际值」而不是「96 点齐全」：页面要告诉用户的是「实际出清价截到哪天」，
// 最后一天少几个点（CSV 常见）不该把这句话说得更旧。
func latestRealDate(series []gdmetric.TimedSample) string {
	latest := ""
	for _, s := range series {
		if s.Date > latest {
			latest = s.Date
		}
	}
	return latest
}

// backtestGroundTruth 从区间序列里挑出「可以当基准真值」的东西：
//   - candidates：有 ≥96 个实际点的日期（缺点的一天会把指标拉偏，整日不用）
//   - byTime：时刻 → 实际值，供按时间戳配对
//
// 这里再挡一次占位来源（999）：回测的「实际值」必须是实际值，
// 拿电查查的预测当基准，算出来的是两个模型谁更准。
func backtestGroundTruth(series []gdmetric.TimedSample) ([]string, map[string]float64) {
	byTime := make(map[string]float64, len(series))
	perDay := make(map[string]int, 32)
	loc := priceLocation()
	for _, s := range series {
		if s.SourceID == gdmetric.SourceForecastPlaceholder {
			continue
		}
		ts, err := parseSampleClock(s.Date, s.Time, loc)
		if err != nil {
			continue
		}
		byTime[sampleKey(ts)] = s.Value
		perDay[s.Date]++
	}
	candidates := make([]string, 0, len(perDay))
	for d, n := range perDay {
		if n >= 96 {
			candidates = append(candidates, d)
		}
	}
	sort.Strings(candidates)
	return candidates, byTime
}

// sampleKey 时刻索引键："2006-01-02 15:04"，用于按时间戳配对预测与实际。
func sampleKey(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// pairByTimestamp 按目标时刻配对预测与实际值，返回配对结果与配不上的点数。
//
// 不能按「当天第几个 15 分钟点」按下标配对：预测起点是「历史序列最后一点 +15min」，
// 历史那天只要少几个点（CSV 常有缺行），整个预测窗口就相对实际日偏移，
// 按下标配会把每个预测点都对到错误的小时上，误差看起来很大而且完全假。
func pairByTimestamp(preds []TPriceForecastPoint, actualByTime map[string]float64) ([]SPriceBacktestPoint, int) {
	out := make([]SPriceBacktestPoint, 0, len(preds))
	missing := 0
	for _, p := range preds {
		v, ok := actualByTime[sampleKey(p.TargetTime)]
		if !ok {
			missing++
			continue
		}
		out = append(out, SPriceBacktestPoint{
			TargetTime: p.TargetTime,
			Predicted:  p.Point,
			Actual:     v,
			Q10:        p.Q10,
			Q50:        p.Q50,
			Q90:        p.Q90,
		})
	}
	return out, missing
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

	// 目标日必须真的有**实际值**，所以先拿实际序列来定候选日。
	// 用 GetRealSeries 而不是 GetSeries：CSV 里最近几天往往只有电查查预测占位（999），
	// 拿它当基准真值去算准确度，算出来的是「两个模型差多少」，与预测准不准无关。
	series, err := gdmetric.GetFrozenRealSeries(ctx, from, to, kind)
	if err != nil {
		return nil, errors.Wrap(err, "读取实际序列失败")
	}
	candidates, actualByTime := backtestGroundTruth(series)
	if len(candidates) == 0 {
		return nil, errors.Errorf("区间 %s ~ %s 内没有完整（96 点）的%s电价**实际值**（电查查预测占位不算）",
			from, to, marketLabel(spec.Market))
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

	dataAsOf := latestRealDate(series)
	lagDays, stale := truthFreshness(dataAsOf, time.Now(), loc)

	out := &SPriceBacktest{
		Market:      spec.Market,
		Grain:       "intraday",
		Unit:        intradayPriceUnit,
		SampleDays:  sampleDays,
		TargetDates: sampled,
		DataAsOf:    dataAsOf,
		LagDays:     lagDays,
		Stale:       stale,
		TruthSource: priceTruthSourceGD,
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
		// 逐点按**时刻**配对：配不上的（该时刻没有实际值，比如预测窗口前半段
		// 落在历史那天缺点的时段）就丢弃，不硬凑 —— 硬凑出来的误差没有意义。
		paired, missing := pairByTimestamp(pred.Points, actualByTime)
		out.Points = append(out.Points, paired...)
		out.ActualMissing += missing
	}

	if len(out.Points) == 0 {
		return nil, errors.Errorf("回测未产出任何对比点（跳过 %d 天）", len(out.Skipped))
	}
	out.From = sampled[0]
	out.To = sampled[len(sampled)-1]
	out.TimeMs = totalMs
	return out, nil
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
