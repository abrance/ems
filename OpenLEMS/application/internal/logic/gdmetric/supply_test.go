package gdmetric

import (
	"math"
	"testing"
)

// 反演的测试都用「解析解已知」的合成数据：
// 令 price = k*load，则供给曲线就是这条直线，任何偏差都是实现错误。
// 拿真实 CSV 测只能看到「有数」，看不出对不对。

func TestQuantileSorted(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		p    float64
		want float64
	}{
		{"中位数-奇数个", []float64{1, 2, 3, 4, 5}, 0.5, 3},
		{"中位数-偶数个取中间值", []float64{1, 2, 3, 4}, 0.5, 2.5},
		{"四分位插值", []float64{1, 2, 3, 4}, 0.25, 1.75},
		{"下界", []float64{1, 2, 3, 4}, 0, 1},
		{"上界", []float64{1, 2, 3, 4}, 1, 4},
		{"单元素", []float64{7}, 0.9, 7},
	}
	for _, c := range cases {
		if got := QuantileSorted(c.in, c.p); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: QuantileSorted = %v, 期望 %v", c.name, got, c.want)
		}
	}
}

func TestRankCorrelation(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5}
	if got := RankCorrelation(xs, []float64{2, 4, 6, 8, 10}); math.Abs(got-1) > 1e-9 {
		t.Errorf("完全单调应得 1，实际 %v", got)
	}
	if got := RankCorrelation(xs, []float64{10, 8, 6, 4, 2}); math.Abs(got+1) > 1e-9 {
		t.Errorf("完全逆序应得 -1，实际 %v", got)
	}
	// 并列：xs 全等 → 秩无方差 → 0（要由 Pairs 判断可信度，不能当成「不相关」）
	if got := RankCorrelation([]float64{1, 1, 1}, []float64{1, 2, 3}); got != 0 {
		t.Errorf("秩无方差应得 0，实际 %v", got)
	}
	// 样本不足 3 个 → 0
	if got := RankCorrelation([]float64{1, 2}, []float64{1, 2}); got != 0 {
		t.Errorf("样本 <3 应得 0，实际 %v", got)
	}
}

func TestPava(t *testing.T) {
	cases := []struct {
		name      string
		in        []float64
		want      []float64
		wantMove  float64
		expectMon bool
	}{
		{"已单调不动", []float64{1, 2, 3}, []float64{1, 2, 3}, 0, true},
		{"相邻逆序合并", []float64{1, 3, 2}, []float64{1, 2.5, 2.5}, 0.5, true},
		{"全逆序合成均值", []float64{3, 2, 1}, []float64{2, 2, 2}, 1, true},
		{"单元素", []float64{5}, []float64{5}, 0, true},
	}
	for _, c := range cases {
		got, move := pava(c.in)
		if len(got) != len(c.in) {
			t.Fatalf("%s: 长度变了 %d → %d", c.name, len(c.in), len(got))
		}
		for i := range got {
			if math.Abs(got[i]-c.want[i]) > 1e-9 {
				t.Errorf("%s: pava = %v, 期望 %v", c.name, got, c.want)
				break
			}
		}
		if math.Abs(move-c.wantMove) > 1e-9 {
			t.Errorf("%s: maxMove = %v, 期望 %v", c.name, move, c.wantMove)
		}
		for i := 1; i < len(got); i++ {
			if got[i] < got[i-1]-1e-9 {
				t.Errorf("%s: 结果不单调 %v", c.name, got)
				break
			}
		}
	}
}

// synthPairs 造 price = k*load 的观测点，每个负荷值重复 repeats 次。
func synthPairs(loadFrom, loadTo, step float64, repeats int, k, offset float64) []SupplyPair {
	out := make([]SupplyPair, 0)
	for r := 0; r < repeats; r++ {
		for v := loadFrom; v <= loadTo+1e-9; v += step {
			out = append(out, SupplyPair{Load: v, Price: k*v + offset})
		}
	}
	return out
}

func TestBuildSupplyCurveReconstructsLinearStack(t *testing.T) {
	// price = 0.05*load + 20，load ∈ [1000, 4000]
	pairs := synthPairs(1000, 4000, 10, 30, 0.05, 20)
	curve, err := BuildSupplyCurve("ahead", "2026-01-01", "2026-01-30", pairs, SupplyOptions{})
	if err != nil {
		t.Fatalf("反演失败: %v", err)
	}

	if curve.Stats.Pairs != len(pairs) {
		t.Errorf("观测点数 = %d, 期望 %d", curve.Stats.Pairs, len(pairs))
	}
	if math.Abs(curve.Stats.Spearman-1) > 1e-9 {
		t.Errorf("负荷-价格秩相关应为 1（价格是负荷的单调函数），实际 %v", curve.Stats.Spearman)
	}
	if curve.Stats.Violations != 0 {
		t.Errorf("线性单调供给栈不应有逆序，实际 %d 个", curve.Stats.Violations)
	}
	if curve.Stats.MaxPavaMove > 1e-9 {
		t.Errorf("已单调的曲线不该被 PAVA 改动，实际 %v", curve.Stats.MaxPavaMove)
	}

	// 每个分箱的中位数都应落在直线 price = 0.05*load + 20 上
	for _, b := range curve.AllBins {
		want := 0.05*b.LoadMid + 20
		got := b.Q[len(b.Q)/2]
		// 分箱内负荷有宽度，箱内中位数与箱中点的差异以「箱内负荷跨度 × 斜率」为界
		tol := 0.05*(b.LoadTo-b.LoadFrom) + 1e-9
		if math.Abs(got-want) > tol {
			t.Errorf("箱 [%.0f,%.0f] P50 = %.2f, 期望 %.2f±%.2f", b.LoadFrom, b.LoadTo, got, want, tol)
		}
	}

	// 报价段：10 段、价格单调不减、且落在直线上
	if len(curve.Steps) != 10 {
		t.Fatalf("报价段数 = %d, 期望 10", len(curve.Steps))
	}
	for i, s := range curve.Steps {
		want := 0.05*((s.LoadFrom+s.LoadTo)/2) + 20
		tol := 0.05*(s.LoadTo-s.LoadFrom) + 1e-9
		if math.Abs(s.Price-want) > tol {
			t.Errorf("第 %d 段价格 %.2f, 期望 %.2f±%.2f", i+1, s.Price, want, tol)
		}
		if i > 0 && s.Price < curve.Steps[i-1].Price-1e-9 {
			t.Errorf("第 %d 段报价低于前一段：%v", i+1, curve.Steps)
		}
		if s.Count == 0 {
			t.Errorf("第 %d 段没有观测点", i+1)
		}
	}
	// 段点数之和 = 全部分箱点数（不能丢点）
	sum := 0
	for _, s := range curve.Steps {
		sum += s.Count
	}
	binSum := 0
	for _, b := range curve.AllBins {
		binSum += b.Count
	}
	if sum != binSum {
		t.Errorf("报价段点数之和 %d ≠ 分箱点数之和 %d", sum, binSum)
	}
}

func TestBuildSupplyCurveFlagsDrift(t *testing.T) {
	// 负相关：负荷越高价格越低 —— 现实中意味着供给栈在区间内整体漂移过，
	// 这时反演出的「曲线」不再是供给曲线，必须报出来（Violations > 0）。
	pairs := synthPairs(1000, 4000, 10, 30, -0.02, 300)
	curve, err := BuildSupplyCurve("ahead", "2026-01-01", "2026-01-30", pairs, SupplyOptions{})
	if err != nil {
		t.Fatalf("反演失败: %v", err)
	}
	if curve.Stats.Spearman > 0 {
		t.Errorf("应为负相关，实际 %v", curve.Stats.Spearman)
	}
	if curve.Stats.Violations == 0 {
		t.Error("负相关曲线必然有逆序，Violations 不该是 0")
	}
	if curve.Stats.MaxPavaMove <= 0 {
		t.Error("PAVA 应当改动过价格，MaxPavaMove 不该是 0")
	}
	// 即使原始曲线不可信，输出的报价段也必须单调（否则阶梯读不出意思）
	for i := 1; i < len(curve.Steps); i++ {
		if curve.Steps[i].Price < curve.Steps[i-1].Price-1e-9 {
			t.Fatalf("PAVA 后报价段仍不单调: %v", curve.Steps)
		}
	}
}

func TestBuildSupplyCurveRejectsBadInput(t *testing.T) {
	if _, err := BuildSupplyCurve("ahead", "a", "b", []SupplyPair{{1, 2}, {2, 3}}, SupplyOptions{}); err == nil {
		t.Error("观测点不足应当报错，而不是返回一条瞎编的曲线")
	}
	// 负荷恒定：没有横轴
	flat := make([]SupplyPair, 100)
	for i := range flat {
		flat[i] = SupplyPair{Load: 3000, Price: float64(i)}
	}
	if _, err := BuildSupplyCurve("ahead", "a", "b", flat, SupplyOptions{}); err == nil {
		t.Error("负荷无变化应当报错")
	}
	// 负荷为 0 的点是缺测占位，要被剔除后再统计 ——
	// 混在有效点里时，必须只按有效点计数（全 0 的情况两种实现都会报错，断言不到东西）
	// 步长 5、重复 10 次 → 每个分箱里至少 50 个点，超过 MinBinCount(20)
	mixed := append(synthPairs(2000, 3000, 5, 10, 0.05, 20), make([]SupplyPair, 50)...)
	curve, err := BuildSupplyCurve("ahead", "a", "b", mixed, SupplyOptions{})
	if err != nil {
		t.Fatalf("混入零负荷点后不该失败: %v", err)
	}
	if want := len(mixed) - 50; curve.Stats.Pairs != want {
		t.Errorf("零负荷点应被剔除：Pairs = %d, 期望 %d", curve.Stats.Pairs, want)
	}
	if curve.Stats.LoadMin <= 0 {
		t.Errorf("LoadMin 不该被 0 拉下去：%v", curve.Stats.LoadMin)
	}
}

func TestBuildSupplyCurveSamplesScatter(t *testing.T) {
	pairs := synthPairs(1000, 4000, 10, 30, 0.05, 20)
	curve, err := BuildSupplyCurve("ahead", "a", "b", pairs, SupplyOptions{MaxScatter: 1000})
	if err != nil {
		t.Fatalf("反演失败: %v", err)
	}
	if !curve.Sampled {
		t.Error("点数超过上限时 Sampled 应为 true")
	}
	if len(curve.Points) > 1000 {
		t.Errorf("散点未抽稀到上限内: %d", len(curve.Points))
	}
	if curve.Stats.Pairs != len(pairs) {
		t.Error("抽稀只影响返回的散点，统计仍应基于全部观测点")
	}
}

func TestNormalizeClock(t *testing.T) {
	// 24:00 归到次日 00:00，否则两条序列的键会差一天
	d, c := normalizeClock("2026-09-21", "24:00")
	if d != "2026-09-22" || c != "00:00" {
		t.Errorf("24:00 应归到次日 00:00，实际 %s %s", d, c)
	}
	d, c = normalizeClock("2026-09-21", "12:15")
	if d != "2026-09-21" || c != "12:15" {
		t.Errorf("普通时刻不该变，实际 %s %s", d, c)
	}
}
