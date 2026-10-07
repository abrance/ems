package gdmetric

import (
	"context"
	"testing"
)

// 真数据体检。合成数据只能证明代码按公式算，证明不了「反演出来的东西像不像供给曲线」。
// 没有本地 CSV 时跳过（CI 上就是跳过）。
//
// 基线（2026-08 数据，广东）：负荷约 88~176 GW，日前价 209~772 元/MWh，
// 报价段随负荷单调上升 324→482；实时价 174~1133（比日前宽，符合预期）。
// 若哪天反演结果偏离这个形状（比如不再单调），大概率是配对或分箱出了问题。
func TestSupplyCurveRealData(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		from, to   string
		kind       SeriesKind
		wantMarket string
	}{
		{"近30天-日前", "2026-08-01", "2026-08-30", KindDayAhead, "ahead"},
		{"近90天-日前", "2026-06-01", "2026-08-30", KindDayAhead, "ahead"},
		{"近30天-实时", "2026-08-01", "2026-08-30", KindRealtime, "real"},
	}
	for _, c := range cases {
		curve, err := GetSupplyCurve(ctx, c.from, c.to, c.kind, SupplyOptions{})
		if err != nil {
			t.Skipf("%s 数据不可用，跳过: %v", c.name, err)
		}
		// 市场映射错了会把实时价当日前价用，这是静默的错，必须断言
		if curve.Market != c.wantMarket {
			t.Errorf("%s: market = %q, 期望 %q", c.name, curve.Market, c.wantMarket)
		}
		if curve.Stats.Pairs == 0 {
			t.Errorf("%s: 没有配对成功的观测点", c.name)
		}
		// 负荷与价格应当正相关（负荷越高越贵）
		if curve.Stats.Spearman < 0.5 {
			t.Errorf("%s: 负荷-价格秩相关 %.3f 过低，配对或分箱可能错了", c.name, curve.Stats.Spearman)
		}
		// 报价段必须单调不减（PAVA 保证了，涨不上去说明 PAVA 坏了）
		for i := 1; i < len(curve.Steps); i++ {
			if curve.Steps[i].Price < curve.Steps[i-1].Price-1e-9 {
				t.Errorf("%s: 报价段不单调: %v", c.name, curve.Steps)
				break
			}
		}
		t.Logf("[%s] ρ=%.3f 逆序=%d 负荷 %.0f~%.0f 价 %.0f~%.0f 段价 %v",
			c.name, curve.Stats.Spearman, curve.Stats.Violations,
			curve.Stats.LoadMin, curve.Stats.LoadMax, curve.Stats.PriceMin, curve.Stats.PriceMax,
			stepPriceList(curve.Steps))
	}
}

func stepPriceList(steps []SupplyStep) []int {
	out := make([]int, len(steps))
	for i, s := range steps {
		out[i] = int(s.Price)
	}
	return out
}
