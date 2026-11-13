package gdmetric

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// 历史相似日：给定一个运行日，在 CSV 全量历史里找出 96 点「形态」最像的日子。
//
// 口径（设计稿 §10.9 里标「相似度算法与阈值待定」的那一条，在这里定下来；
// 改动公式前先看这段注释）：
//
//  1. **特征**：当日 96 点的「价格」与「负荷」两条曲线。两条都先做 z-score 标准化 ——
//     只看**形态**。绝对价位/负荷水平不算作「相似」：夏天 600 元/MWh 的午峰与
//     冬天 300 元/MWh 的午峰完全可以同形，而做价格预测要借鉴的正是形态。
//  2. **相似度** = wPrice × ρ(价格) + wLoad × ρ(负荷)，ρ 为皮尔逊相关系数；
//     再线性映射到 0~100%：similarity = (ρ + 1) / 2 × 100（ρ = 1 → 100%，ρ = 0 → 50%）。
//  3. **默认权重** 0.7 / 0.3：价格是预测目标，形态权重更高；负荷是价格的横轴驱动，作辅助。
//  4. **只统计两条曲线都满 96 点的日子**，缺点的日子整日跳过（不补洞、不插值）——
//     插值出来的「相似」是假的。
//  5. 排除目标日自身；返回相似度降序的 TOP-N（默认 5，上限 20）。
//
// 与「预测」的关系：相似日是**参考**，不直接产出预测值。页面上要写清这一点，
// 别让人以为相似日曲线取平均就是预测（那是另一套算法，需要单独回测）。
const (
	DefaultSimilarTopN   = 5
	MaxSimilarTopN       = 20
	DefaultSimilarWPrice = 0.7
	DefaultSimilarWLoad  = 0.3
	// similarDayPoints 一天的点数：两条曲线都必须满一天才参与比较
	similarDayPoints = 96
)

// SimilarDay 一个候选相似日。
type SimilarDay struct {
	Date       string  `json:"date" dc:"运行日"`
	Similarity float64 `json:"similarity" dc:"相似度 0~100（= (ρ+1)/2×100）"`
	PriceCorr  float64 `json:"priceCorr" dc:"价格形态相关系数 ρ（-1~1）"`
	LoadCorr   float64 `json:"loadCorr" dc:"负荷形态相关系数 ρ（-1~1）"`
	PriceAvg   float64 `json:"priceAvg" dc:"当日价格均值（只看价格曲线）"`
	LoadMax    float64 `json:"loadMax" dc:"当日负荷峰值（只看负荷曲线）"`
}

// SimilarDaysResult 相似日查询结果。
type SimilarDaysResult struct {
	TargetDate string       `json:"targetDate" dc:"目标运行日"`
	Market     string       `json:"market" dc:"ahead / real（决定用日前还是实时价格曲线）"`
	TopN       int          `json:"topN"`
	WPrice     float64      `json:"wPrice"`
	WLoad      float64      `json:"wLoad"`
	Method     string       `json:"method" dc:"算法名，页面直接展示"`
	Formula    string       `json:"formula" dc:"公式说明，页面直接展示"`
	From       string       `json:"from" dc:"参与比较的最早运行日"`
	To         string       `json:"to" dc:"参与比较的最晚运行日"`
	Candidates int          `json:"candidates" dc:"通过 96 点完整性筛选取出的历史日数"`
	Days       []SimilarDay `json:"days" dc:"相似度降序"`
	TimeMs     float64      `json:"timeMs"`
}

// SimilarOptions 相似日查询参数。零值即默认口径（0.7 / 0.3）。
//
// 权重允许显式传 0，意思是「这次不看这个特征」；只有**两个都是 0** 才回落默认值。
type SimilarOptions struct {
	TopN   int
	WPrice float64
	WLoad  float64
}

func (o SimilarOptions) normalize() SimilarOptions {
	if o.TopN <= 0 {
		o.TopN = DefaultSimilarTopN
	}
	if o.TopN > MaxSimilarTopN {
		o.TopN = MaxSimilarTopN
	}
	if o.WPrice < 0 {
		o.WPrice = 0
	}
	if o.WLoad < 0 {
		o.WLoad = 0
	}
	// 两个都是 0（含都没传）→ 用默认口径；否则按给定值归一化 ——
	// 传 0 是合法的，意思是「这次不看这个特征」（比如只看价格形态）。
	if o.WPrice == 0 && o.WLoad == 0 {
		o.WPrice, o.WLoad = DefaultSimilarWPrice, DefaultSimilarWLoad
	}
	if sum := o.WPrice + o.WLoad; sum != 1 {
		o.WPrice, o.WLoad = o.WPrice/sum, o.WLoad/sum
	}
	return o
}

// SimilarDays 找 date 这个运行日的历史相似日。
//
// 已停用：官方披露没有 96 点分时/负荷口径，本能力无法迁移，返回明确的停更错误而非旧数据。
func SimilarDays(ctx context.Context, date string, kind SeriesKind, opts SimilarOptions) (*SimilarDaysResult, error) {
	if err := guard(ctx, "相似日分析"); err != nil {
		return nil, err
	}
	return FrozenSimilarDays(ctx, date, kind, opts)
}

// FrozenSimilarDays 冻结通道版本（历史演示，名字里的 Frozen 就是提醒）。
func FrozenSimilarDays(ctx context.Context, date string, kind SeriesKind, opts SimilarOptions) (*SimilarDaysResult, error) {
	start := time.Now()
	initGlobal(ctx)
	if err := global.ensureFresh(ctx); err != nil {
		global.mu.RLock()
		empty := len(global.days) == 0
		global.mu.RUnlock()
		if empty {
			return nil, err
		}
		// 有缓存就沿用：相似日是看历史，不该因为一次拉取失败而打不开
	}

	date = strings.TrimSpace(date)
	opts = opts.normalize()

	// 不给日期就用最新**有实际值**的运行日（而不是最新运行日）：
	// 最新那天往往只有电查查预测占位，拿它当目标日就是拿预测当历史观测。
	if date == "" {
		global.mu.RLock()
		date = latestRealDate(global.days, similarDayPoints)
		if date == "" && len(global.days) > 0 {
			date = global.days[len(global.days)-1].Date
		}
		global.mu.RUnlock()
	}
	if date == "" {
		return nil, errors.New("没有可用的运行日数据")
	}

	target, err := global.getCurve(date)
	if err != nil {
		return nil, errors.Wrap(err, "读取目标日曲线失败")
	}
	if target == nil {
		return nil, errors.Errorf("目标日 %s 没有 96 点曲线数据（可能不在 CSV 覆盖范围内）", date)
	}
	// 目标日必须真有实际值：只有电查查预测占位的那天，与历史比形态没有意义
	if len(curveSeries(*target, kind)) < similarDayPoints {
		return nil, errors.Errorf("目标日 %s 的%s实际值不足 96 点（电查查预测占位不算），换个有实际值的日期",
			date, marketLabelOf(kind))
	}

	global.mu.RLock()
	dates := make([]string, 0, len(global.days))
	from, to := "", ""
	for _, d := range global.days {
		if d.Date == date {
			continue
		}
		dates = append(dates, d.Date)
		if from == "" || d.Date < from {
			from = d.Date
		}
		if d.Date > to {
			to = d.Date
		}
	}
	global.mu.RUnlock()

	candidates := make([]DayCurve, 0, len(dates))
	for _, d := range dates {
		curve, err := global.getCurve(d)
		if err != nil || curve == nil {
			continue
		}
		candidates = append(candidates, *curve)
	}

	days, compared := rankSimilarDays(*target, candidates, kind, opts)

	return &SimilarDaysResult{
		TargetDate: date,
		Market:     marketLabelOf(kind),
		TopN:       opts.TopN,
		WPrice:     opts.WPrice,
		WLoad:      opts.WLoad,
		Method:     "皮尔逊形态相关（96 点，z-score 标准化）",
		Formula:    "相似度 = (0.7×ρ价格 + 0.3×ρ负荷 + 1) / 2 × 100%",
		From:       from,
		To:         to,
		Candidates: compared,
		Days:       days,
		TimeMs:     float64(time.Since(start).Microseconds()) / 1000,
	}, nil
}

// rankSimilarDays 是纯函数：口径（标准化、加权、映射到 0~100、排序）全在这里，
// 不碰 global/CSV，所以能直接拿合成曲线测。
func rankSimilarDays(target DayCurve, candidates []DayCurve, kind SeriesKind, opts SimilarOptions) ([]SimilarDay, int) {
	// 横轴取目标日的时刻序列（CSV 里就是 00:15…24:00 的 96 个点），
	// 候选用同一组时刻对齐 —— 不靠下标对齐，避免某天起点不同就整体错位。
	axis := timesOf(curveSeries(target, kind))
	targetPrice := valuesOn(target, kind, axis)
	targetLoad := valuesOn(target, KindLoad, axis)
	if len(targetPrice) < similarDayPoints || len(targetLoad) < similarDayPoints {
		return nil, 0
	}
	targetPriceNorm := zscore(targetPrice)
	targetLoadNorm := zscore(targetLoad)

	days := make([]SimilarDay, 0, len(candidates))
	compared := 0
	for _, curve := range candidates {
		if curve.Date == target.Date {
			continue
		}
		price := valuesOn(curve, kind, axis)
		load := valuesOn(curve, KindLoad, axis)
		if len(price) < similarDayPoints || len(load) < similarDayPoints {
			continue
		}
		priceCorr := pearson(targetPriceNorm, zscore(price))
		loadCorr := pearson(targetLoadNorm, zscore(load))
		// 常量曲线（标准差为 0）算不出相关系数，跳过而不是当成 0
		if math.IsNaN(priceCorr) || math.IsNaN(loadCorr) {
			continue
		}
		_, loadMax := minMax(load)
		compared++
		score := opts.WPrice*priceCorr + opts.WLoad*loadCorr
		days = append(days, SimilarDay{
			Date:       curve.Date,
			Similarity: roundTo((score+1)/2*100, 1),
			PriceCorr:  roundTo(priceCorr, 4),
			LoadCorr:   roundTo(loadCorr, 4),
			PriceAvg:   roundTo(meanOf(price), 2),
			LoadMax:    roundTo(loadMax, 1),
		})
	}

	sort.Slice(days, func(i, j int) bool {
		if days[i].Similarity == days[j].Similarity {
			return days[i].Date > days[j].Date // 同分取更近的日子
		}
		return days[i].Similarity > days[j].Similarity
	})
	if len(days) > opts.TopN {
		days = days[:opts.TopN]
	}
	return days, compared
}

func curveSeries(c DayCurve, kind SeriesKind) []Sample {
	var samples []Sample
	switch kind {
	case KindRealtime:
		samples = c.Realtime
	case KindLoad:
		samples = c.Load
	default:
		samples = c.DayAhead
	}
	// 只留实际值：相似日是拿历史**观测**做形态比对，
	// 而 999 是电查查的预测占位（该时刻实际值还没出），把它当历史观测就是拿预测当事实。
	out := make([]Sample, 0, len(samples))
	for _, s := range samples {
		if IsRealSource(s.SourceID) {
			out = append(out, s)
		}
	}
	return out
}

func timesOf(samples []Sample) []string {
	out := make([]string, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.Time)
	}
	return out
}

// valuesOn 按 axis 指定的时刻取值；缺任何一个时刻就整体放弃这一天（返回 nil）。
func valuesOn(c DayCurve, kind SeriesKind, axis []string) []float64 {
	byTime := make(map[string]float64, len(axis))
	for _, s := range curveSeries(c, kind) {
		byTime[s.Time] = s.Value
	}
	out := make([]float64, 0, len(axis))
	for _, t := range axis {
		v, ok := byTime[t]
		if !ok {
			return nil
		}
		out = append(out, v)
	}
	return out
}

// zscore 标准化：消去绝对水平，只留形态。标准差为 0 时全部置 0（后续相关系数会是 NaN，调用方跳过）。
func zscore(values []float64) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 {
		return out
	}
	mean := meanOf(values)
	variance := 0.0
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(values))
	std := math.Sqrt(variance)
	if std == 0 {
		return out
	}
	for i, v := range values {
		out[i] = (v - mean) / std
	}
	return out
}

// pearson 皮尔逊相关系数。长度不等或任一序列无波动时返回 NaN。
func pearson(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.NaN()
	}
	var sumA, sumB, sumAB, sumA2, sumB2 float64
	for i := range a {
		sumA += a[i]
		sumB += b[i]
		sumAB += a[i] * b[i]
		sumA2 += a[i] * a[i]
		sumB2 += b[i] * b[i]
	}
	n := float64(len(a))
	num := n*sumAB - sumA*sumB
	den := math.Sqrt((n*sumA2 - sumA*sumA) * (n*sumB2 - sumB*sumB))
	if den == 0 {
		return math.NaN()
	}
	r := num / den
	// 浮点误差可能给出 1.0000000000000002
	if r > 1 {
		r = 1
	}
	if r < -1 {
		r = -1
	}
	return r
}

func roundTo(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

func marketLabelOf(kind SeriesKind) string {
	switch kind {
	case KindRealtime:
		return "real"
	case KindLoad:
		return "load"
	default:
		return "ahead"
	}
}
