package gdmetric

import (
	"fmt"
	"math"
	"testing"
)

// clockOf 生成第 i 个 15 分钟时刻（与 CSV 一致：00:15 … 24:00）
func clockOf(i int) string {
	m := (i + 1) * 15
	return fmt.Sprintf("%02d:%02d", (m/60)%24, m%60)
}

// 夹具一律标成实际值来源：相似日只比对**实际值**（sourceId 0 视为未知来源，不参与）
func curveWith(date string, price, load func(int) float64) DayCurve {
	c := DayCurve{Date: date}
	for i := 0; i < 96; i++ {
		t := clockOf(i)
		c.DayAhead = append(c.DayAhead, Sample{Time: t, Value: price(i), SourceID: SourceExchange})
		c.Load = append(c.Load, Sample{Time: t, Value: load(i), SourceID: SourceExchange})
	}
	return c
}

func sinePrice(i int) float64   { return 300 + 100*math.Sin(2*math.Pi*float64(i)/96) }
func cosinePrice(i int) float64 { return 300 - 100*math.Sin(2*math.Pi*float64(i)/96) }
func flatLoad(i int) float64    { return 1000 + 200*math.Cos(2*math.Pi*float64(i)/96) }

func TestPearsonAndZscore(t *testing.T) {
	a := []float64{1, 2, 3, 4}
	if got := pearson(a, a); math.Abs(got-1) > 1e-12 {
		t.Fatalf("自相关应为 1，得到 %v", got)
	}
	if got := pearson(a, []float64{4, 3, 2, 1}); math.Abs(got+1) > 1e-12 {
		t.Fatalf("反向应为 -1，得到 %v", got)
	}
	if !math.IsNaN(pearson(a, []float64{1, 1, 1, 1})) {
		t.Fatal("常量序列的相关系数应为 NaN（不能当成 0 用）")
	}
	if !math.IsNaN(pearson(a, []float64{1, 2, 3})) {
		t.Fatal("长度不等应为 NaN")
	}

	z := zscore([]float64{10, 20, 30})
	if n := meanOf(z); math.Abs(n) > 1e-12 {
		t.Fatalf("z-score 均值应为 0，得到 %v", n)
	}
	if got := zscore([]float64{5, 5, 5}); meanOf(got) != 0 {
		t.Fatalf("常量序列标准化后应全 0，得到 %v", got)
	}
}

func TestRankSimilarDays(t *testing.T) {
	target := curveWith("2026-09-01", sinePrice, flatLoad)

	// 同形、只差绝对水平（×1.5）→ 价格/负荷相关都是 1 → 相似度 100%
	sameShapeHigher := curveWith("2026-09-02", func(i int) float64 { return sinePrice(i) * 1.5 }, flatLoad)
	// 反相 → ρ价格 = -1，ρ负荷 = 1 → 0.7×(-1)+0.3×1 = -0.4 → (0.6)/2×100 = 30%
	inverted := curveWith("2026-09-03", cosinePrice, flatLoad)
	// 少一个点 → 整天跳过（不插值）
	incomplete := curveWith("2026-09-04", sinePrice, flatLoad)
	incomplete.DayAhead = incomplete.DayAhead[:95]

	opts := SimilarOptions{}.normalize()
	days, compared := rankSimilarDays(target, []DayCurve{sameShapeHigher, inverted, incomplete, target}, KindDayAhead, opts)

	if compared != 2 {
		t.Fatalf("参与比较的应是 2 天（缺点的和目标日本身都跳过），得到 %d", compared)
	}
	if len(days) != 2 {
		t.Fatalf("应返回 2 条，得到 %d", len(days))
	}
	if days[0].Date != "2026-09-02" {
		t.Fatalf("最相似的应是 2026-09-02（同形不同价），得到 %s", days[0].Date)
	}
	if math.Abs(days[0].Similarity-100) > 1e-9 {
		t.Fatalf("同形应 100%%，得到 %v", days[0].Similarity)
	}
	// 均价按当日自身曲线算：×1.5 的那天均价也应是 1.5 倍
	targetAvg := meanOf(timesValues(target.DayAhead))
	if math.Abs(days[0].PriceAvg-1.5*targetAvg) > 0.01 {
		t.Fatalf("均价应按当日曲线算：期望 %.2f，得到 %.2f", 1.5*targetAvg, days[0].PriceAvg)
	}
	if days[1].Date != "2026-09-03" || math.Abs(days[1].Similarity-30) > 1e-9 {
		t.Fatalf("反相日应排第二且有 30%%，得到 %s %v", days[1].Date, days[1].Similarity)
	}
	if math.Abs(days[1].PriceCorr+1) > 1e-9 {
		t.Fatalf("反相日的价格相关应为 -1，得到 %v", days[1].PriceCorr)
	}
}

func timesValues(samples []Sample) []float64 {
	out := make([]float64, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.Value)
	}
	return out
}

func TestSimilarOptionsNormalize(t *testing.T) {
	o := SimilarOptions{}.normalize()
	if o.TopN != DefaultSimilarTopN || math.Abs(o.WPrice-DefaultSimilarWPrice) > 1e-12 || math.Abs(o.WLoad-DefaultSimilarWLoad) > 1e-12 {
		t.Fatalf("默认口径不对: %+v", o)
	}
	if o := (SimilarOptions{TopN: 999}).normalize(); o.TopN != MaxSimilarTopN {
		t.Fatalf("TopN 应被夹到 %d，得到 %d", MaxSimilarTopN, o.TopN)
	}
	// 权重可以是 0（这次不看这个特征）；两个都不传（都是 0）才用默认值
	if o := (SimilarOptions{WPrice: 2}).normalize(); o.WPrice != 1 || o.WLoad != 0 {
		t.Fatalf("只给价格权重时应归一化到 (1,0): %+v", o)
	}
	if o := (SimilarOptions{WLoad: 1}).normalize(); o.WPrice != 0 || o.WLoad != 1 {
		t.Fatalf("只看负荷形态时应是 (0,1): %+v", o)
	}
	if o := (SimilarOptions{WPrice: 0.7, WLoad: 0.7}).normalize(); math.Abs(o.WPrice-o.WLoad) > 1e-12 {
		t.Fatalf("两权重相等时应各占一半: %+v", o)
	}
	if o := (SimilarOptions{WPrice: -1, WLoad: 2}).normalize(); o.WPrice != 0 || o.WLoad != 1 {
		t.Fatalf("负权重应归零: %+v", o)
	}
}

// 目标日曲线不满 96 点时不能算（否则相关系数是拿半天的形状比整天）
func TestRankSimilarDaysRejectsShortTarget(t *testing.T) {
	target := curveWith("2026-09-01", sinePrice, flatLoad)
	target.Load = target.Load[:90]
	if days, compared := rankSimilarDays(target, []DayCurve{curveWith("2026-09-02", sinePrice, flatLoad)}, KindDayAhead, SimilarOptions{}.normalize()); days != nil || compared != 0 {
		t.Fatalf("目标日不满 96 点应直接放弃，得到 %v %d", days, compared)
	}
}
