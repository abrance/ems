package gdmetric

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// 反演：从观测到的 (负荷, 出清价) 反推市场的聚合报价（供给）曲线。
//
// 为什么这样能反演 —— 需求侧负荷基本是刚需（价格弹性极低），出清价是需求与
// 供给栈的交点。所以历史上每一对 (负荷, 出清价) **本身就是供给曲线上的一个点**：
// 供给栈不变时，负荷怎么变，价格就沿着同一条供给曲线走。按负荷分箱取价格分位，
// 得到的中位数曲线就是这条供给曲线；分位带反映供给栈在不同日子的陡缓差异。
//
// 与「逆优化」的区别（设计稿 §14.1 的两种方法）：这里**没有**网络模型、出清模型
// 与机组成本，因此重建的是**市场整体**的聚合报价曲线，是估计值，不是任何一台
// 机组的真实报价。逐机组的反演要等出清机组明细/机组台账接入。

// SupplyPair 一个观测点：系统负荷与该时刻的出清价。
type SupplyPair struct {
	Load  float64 `json:"load" dc:"系统负荷 MW"`
	Price float64 `json:"price" dc:"出清价 元/MWh"`
}

// SupplyBin 一个负荷分箱上的价格分位分布。
type SupplyBin struct {
	LoadFrom float64   `json:"loadFrom"`
	LoadTo   float64   `json:"loadTo"`
	LoadMid  float64   `json:"loadMid"`
	Count    int       `json:"count"`
	Q        []float64 `json:"q" dc:"价格分位；下标 i 对应概率 i/(len-1)"`
}

// SupplyStep 反演报价曲线的一段（对齐参考界面的「报价段一…十」）。
type SupplyStep struct {
	LoadFrom float64 `json:"loadFrom"`
	LoadTo   float64 `json:"loadTo"`
	Price    float64 `json:"price" dc:"该负荷段的报价 元/MWh"`
	Count    int     `json:"count" dc:"落入该段的观测点数"`
}

// SupplyStats 反演的可信度指标。不给这些数就没法判断曲线能不能用。
type SupplyStats struct {
	Pairs       int     `json:"pairs" dc:"配对成功的观测点数"`
	Days        int     `json:"days" dc:"覆盖天数"`
	LoadMin     float64 `json:"loadMin"`
	LoadMax     float64 `json:"loadMax"`
	PriceMin    float64 `json:"priceMin"`
	PriceMax    float64 `json:"priceMax"`
	Spearman    float64 `json:"spearman" dc:"负荷与价格的秩相关；越接近 1 说明价格越由负荷单调决定"`
	Violations  int     `json:"violations" dc:"P50 曲线的逆序数：>0 说明供给栈在区间内整体漂移过（燃料价/检修/新能源装机），曲线只能当参考"`
	MaxPavaMove float64 `json:"maxPavaMove" dc:"单调化对报价段价格的最大改动 元/MWh"`
	LatestLoad  float64 `json:"latestLoad" dc:"序列最后时刻的负荷，供前端做默认「当前负荷」"`
}

// SupplyCurve 反演结果。
type SupplyCurve struct {
	Market    string       `json:"market" dc:"ahead / real"`
	From      string       `json:"from"`
	To        string       `json:"to"`
	Bins      int          `json:"bins"`
	Quantiles []float64    `json:"quantiles" dc:"分位点（升序），如 0,0.1,…,1"`
	Points    []SupplyPair `json:"points" dc:"观测散点（超过上限按等距抽样）"`
	Sampled   bool         `json:"sampled" dc:"散点是否被抽稀"`
	AllBins   []SupplyBin  `json:"allBins"`
	Steps     []SupplyStep `json:"steps" dc:"单调化后的报价段"`
	Stats     SupplyStats  `json:"stats"`
}

// SupplyOptions 反演参数。
type SupplyOptions struct {
	Bins        int // 曲线分箱数，默认 40
	Steps       int // 报价段数，默认 10
	QuantileN   int // 分位网格段数，默认 10 → 11 个分位点
	MinBinCount int // 分箱最少观测数，不足则丢弃，默认 20
	MaxScatter  int // 返回的散点数量上限，默认 8000
}

func (o SupplyOptions) withDefaults() SupplyOptions {
	if o.Bins <= 0 {
		o.Bins = 40
	}
	if o.Bins > 200 {
		o.Bins = 200
	}
	if o.Steps <= 0 {
		o.Steps = 10
	}
	if o.Steps > 20 {
		o.Steps = 20
	}
	if o.QuantileN <= 0 {
		o.QuantileN = 10
	}
	if o.QuantileN > 20 {
		o.QuantileN = 20
	}
	if o.MinBinCount <= 0 {
		o.MinBinCount = 20
	}
	if o.MaxScatter <= 0 {
		o.MaxScatter = 8000
	}
	return o
}

// BuildSupplyCurve 由观测点构造聚合报价曲线。纯函数，便于用固定数据验证。
func BuildSupplyCurve(market, from, to string, pairs []SupplyPair, opts SupplyOptions) (*SupplyCurve, error) {
	o := opts.withDefaults()

	// 负荷为 0 的点多半是缺测/占位，参与反演会把曲线拉到 0
	clean := make([]SupplyPair, 0, len(pairs))
	for _, p := range pairs {
		if p.Load > 0 {
			clean = append(clean, p)
		}
	}
	if len(clean) < o.MinBinCount {
		return nil, errors.Errorf("有效观测点不足（%d 个），至少需要 %d 个", len(clean), o.MinBinCount)
	}

	loads := make([]float64, len(clean))
	prices := make([]float64, len(clean))
	for i, p := range clean {
		loads[i] = p.Load
		prices[i] = p.Price
	}
	loadMin, loadMax := minMax(loads)
	priceMin, priceMax := minMax(prices)
	if loadMax-loadMin < 1e-9 {
		return nil, errors.New("区间内负荷没有变化，无法反演（负荷是横轴，没有变化就没有横轴）")
	}

	// 等宽分箱
	width := (loadMax - loadMin) / float64(o.Bins)
	groups := make([][]float64, o.Bins)
	for i := range clean {
		idx := int((clean[i].Load - loadMin) / width)
		if idx >= o.Bins {
			idx = o.Bins - 1 // 最大值归入最后一箱
		}
		if idx < 0 {
			idx = 0
		}
		groups[idx] = append(groups[idx], clean[i].Price)
	}

	probs := make([]float64, o.QuantileN+1)
	for i := range probs {
		probs[i] = float64(i) / float64(o.QuantileN)
	}
	mid := o.QuantileN / 2

	bins := make([]SupplyBin, 0, o.Bins)
	p50 := make([]float64, 0, o.Bins)
	for i, g := range groups {
		if len(g) < o.MinBinCount {
			continue // 稀疏箱的分位数没有意义，宁可留空让前端断线
		}
		sort.Float64s(g)
		q := make([]float64, len(probs))
		for j, p := range probs {
			q[j] = QuantileSorted(g, p)
		}
		bins = append(bins, SupplyBin{
			LoadFrom: loadMin + float64(i)*width,
			LoadTo:   loadMin + float64(i+1)*width,
			LoadMid:  loadMin + (float64(i)+0.5)*width,
			Count:    len(g),
			Q:        q,
		})
		p50 = append(p50, q[mid])
	}
	if len(bins) < 2 {
		return nil, errors.New("有效分箱不足 2 个，负荷分布太集中；把区间拉长再试")
	}

	// P50 曲线在负荷升序下应当单调不减。逆序数说明供给栈在区间内漂移过。
	violations := 0
	for i := 1; i < len(p50); i++ {
		if p50[i] < p50[i-1]-1e-9 {
			violations++
		}
	}

	// 报价段：按观测点数等分，段价取段内 P50 均值，再做单调化（PAVA）。
	steps := buildSteps(bins, p50, o.Steps)
	mono, maxMove := pava(stepPrices(steps))
	for i := range steps {
		steps[i].Price = mono[i]
	}

	// 散点抽稀：几万个点画出来又慢又糊，等距抽样保形且便宜
	stride := 1
	sampled := len(clean) > o.MaxScatter
	if sampled {
		stride = (len(clean) + o.MaxScatter - 1) / o.MaxScatter
	}
	points := make([]SupplyPair, 0, min(len(clean), o.MaxScatter))
	for i := 0; i < len(clean); i += stride {
		points = append(points, clean[i])
	}

	return &SupplyCurve{
		Market:    market,
		From:      from,
		To:        to,
		Bins:      len(bins),
		Quantiles: probs,
		Points:    points,
		Sampled:   sampled,
		AllBins:   bins,
		Steps:     steps,
		Stats: SupplyStats{
			Pairs:       len(clean),
			LoadMin:     loadMin,
			LoadMax:     loadMax,
			PriceMin:    priceMin,
			PriceMax:    priceMax,
			Spearman:    RankCorrelation(loads, prices),
			Violations:  violations,
			MaxPavaMove: maxMove,
		},
	}, nil
}

// buildSteps 把分箱按观测点数分成若干段，段价 = 段内 P50 均值。
// 按点数分而不是按负荷宽度分：负荷低端往往挤了大量观测，等宽会让低负荷只剩一两段。
func buildSteps(bins []SupplyBin, p50 []float64, steps int) []SupplyStep {
	if len(bins) == 0 || steps <= 0 {
		return nil
	}
	if steps > len(bins) {
		steps = len(bins)
	}
	total := 0
	for _, b := range bins {
		total += b.Count
	}
	target := float64(total) / float64(steps)

	out := make([]SupplyStep, 0, steps)
	start := 0
	acc := 0
	for i := 0; i < len(bins)-1; i++ {
		acc += bins[i].Count
		// 已够一段的量，且后面还留得下剩余段数时才收段
		if len(out) < steps-1 && float64(acc) >= target*(float64(len(out))+1) &&
			len(bins)-1-i > steps-1-len(out) {
			out = append(out, makeStep(bins, p50, start, i+1))
			start = i + 1
		}
	}
	out = append(out, makeStep(bins, p50, start, len(bins)))
	return out
}

func makeStep(bins []SupplyBin, p50 []float64, start, end int) SupplyStep {
	sum, count := 0.0, 0
	for i := start; i < end; i++ {
		sum += p50[i]
		count += bins[i].Count
	}
	return SupplyStep{
		LoadFrom: bins[start].LoadFrom,
		LoadTo:   bins[end-1].LoadTo,
		Price:    sum / float64(end-start),
		Count:    count,
	}
}

func stepPrices(steps []SupplyStep) []float64 {
	out := make([]float64, len(steps))
	for i, s := range steps {
		out[i] = s.Price
	}
	return out
}

// pava 保序回归（Pool Adjacent Violators），把报价段价格调整为单调不减。
// 报价段必须单调，否则「负荷越高报价越低」的阶梯读不出意思。
// 返回单调化后的序列与最大改动量 —— 改动越大说明原始曲线越不可信，要显示给用户。
func pava(values []float64) ([]float64, float64) {
	if len(values) == 0 {
		return nil, 0
	}
	type block struct{ sum, weight float64 }
	stack := make([]block, 0, len(values))
	for _, v := range values {
		stack = append(stack, block{sum: v, weight: 1})
		for len(stack) >= 2 {
			a, b := stack[len(stack)-2], stack[len(stack)-1]
			if a.sum/a.weight <= b.sum/b.weight {
				break
			}
			stack = stack[:len(stack)-2]
			stack = append(stack, block{sum: a.sum + b.sum, weight: a.weight + b.weight})
		}
	}

	out := make([]float64, 0, len(values))
	maxMove := 0.0
	idx := 0
	for _, b := range stack {
		mean := b.sum / b.weight
		for k := 0; k < int(b.weight); k++ {
			out = append(out, mean)
			if d := math.Abs(mean - values[idx]); d > maxMove {
				maxMove = d
			}
			idx++
		}
	}
	return out, maxMove
}

// QuantileSorted 在**已升序排序**的切片上取分位（线性插值）。
func QuantileSorted(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 || p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	pos := p * float64(n-1)
	lo := int(pos)
	if lo+1 >= n {
		return sorted[n-1]
	}
	return sorted[lo] + (pos-float64(lo))*(sorted[lo+1]-sorted[lo])
}

// RankCorrelation Spearman 秩相关。并列取平均秩；样本 <3 或秩无方差时返回 0，
// 由调用方结合 Pairs 判断可信度。
func RankCorrelation(xs, ys []float64) float64 {
	n := min(len(xs), len(ys))
	if n < 3 {
		return 0
	}
	rx := averageRanks(xs[:n])
	ry := averageRanks(ys[:n])
	mx, my := meanOf(rx), meanOf(ry)
	var num, dx, dy float64
	for i := 0; i < n; i++ {
		a, b := rx[i]-mx, ry[i]-my
		num += a * b
		dx += a * a
		dy += b * b
	}
	if dx == 0 || dy == 0 {
		return 0
	}
	return num / math.Sqrt(dx*dy)
}

func averageRanks(values []float64) []float64 {
	idx := make([]int, len(values))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return values[idx[a]] < values[idx[b]] })
	ranks := make([]float64, len(values))
	i := 0
	for i < len(idx) {
		j := i
		for j+1 < len(idx) && values[idx[j+1]] == values[idx[i]] {
			j++
		}
		avg := (float64(i)+float64(j))/2 + 1
		for k := i; k <= j; k++ {
			ranks[idx[k]] = avg
		}
		i = j + 1
	}
	return ranks
}

func meanOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var s float64
	for _, v := range values {
		s += v
	}
	return s / float64(len(values))
}

func minMax(values []float64) (float64, float64) {
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	return lo, hi
}

// GetSupplyCurve 拉取区间内的出清价与负荷，配对后反演。
//
// GetSupplyCurve 已停用：官方披露没有 96 点分时/负荷口径，本能力无法迁移，返回明确的停更错误而非旧数据。
func GetSupplyCurve(ctx context.Context, from, to string, kind SeriesKind, opts SupplyOptions) (*SupplyCurve, error) {
	if err := guard(ctx, "供给曲线"); err != nil {
		return nil, err
	}
	return GetFrozenSupplyCurve(ctx, from, to, kind, opts)
}

// GetFrozenSupplyCurve 冻结通道版本（历史演示，名字里的 Frozen 就是提醒）。
func GetFrozenSupplyCurve(ctx context.Context, from, to string, kind SeriesKind, opts SupplyOptions) (*SupplyCurve, error) {
	priceKind := KindDayAhead
	market := "ahead"
	if kind == KindRealtime {
		priceKind = KindRealtime
		market = "real"
	}

	// 用 GetRealSeries 而不是 GetSeries：反演拿的是「历史上每一对 (负荷, 出清价) 都是
	// 供给曲线上的一个观测点」这个假设，而 999 是电查查的预测占位（实际值还没出）。
	// 把预测当观测，反出来的曲线就不再有观测依据。
	prices, err := GetFrozenRealSeries(ctx, from, to, priceKind)
	if err != nil {
		return nil, errors.Wrap(err, "读取出清价失败")
	}
	loads, err := GetFrozenRealSeries(ctx, from, to, KindLoad)
	if err != nil {
		return nil, errors.Wrap(err, "读取负荷失败")
	}

	// 按 (日期, 时刻) 配对。两边都把 24:00 归到次日 00:00，键才一致。
	priceByKey := make(map[string]float64, len(prices))
	for _, s := range prices {
		d, t := normalizeClock(s.Date, s.Time)
		priceByKey[d+" "+t] = s.Value
	}

	pairs := make([]SupplyPair, 0, min(len(prices), len(loads)))
	daySet := map[string]bool{}
	latestLoad := 0.0
	for _, s := range loads {
		d, t := normalizeClock(s.Date, s.Time)
		pv, ok := priceByKey[d+" "+t]
		if !ok {
			continue // 只有一边有的时刻直接丢，不插值（插出来的价格没有依据）
		}
		pairs = append(pairs, SupplyPair{Load: s.Value, Price: pv})
		daySet[d] = true
		if s.Value > 0 {
			latestLoad = s.Value
		}
	}
	if len(pairs) == 0 {
		return nil, errors.New("区间内负荷与出清价没有对齐的时刻，无法反演")
	}

	curve, err := BuildSupplyCurve(market, from, to, pairs, opts)
	if err != nil {
		return nil, err
	}
	curve.Stats.Days = len(daySet)
	curve.Stats.LatestLoad = latestLoad
	return curve, nil
}

// normalizeClock 把 24:00 归到次日 00:00。
// CSV 的日曲线写作 00:15…24:00，最后一点落在**次日 00:00**；不归一化会与另一条
// 序列的同名时刻差一天，配对全错。
func normalizeClock(date, clock string) (string, string) {
	if strings.TrimSpace(clock) == "24:00" {
		if t, err := time.ParseInLocation(dateLayout, date, time.UTC); err == nil {
			return t.AddDate(0, 0, 1).Format(dateLayout), "00:00"
		}
	}
	return date, clock
}
