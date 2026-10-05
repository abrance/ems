package internal

import (
	"math"
	"testing"
)

const minMs = 60 * 1000

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

// build 按 15 分钟间隔构造序列，grid/ess/price 等长。
func build(grid, ess, price []float64) []SInterval {
	out := make([]SInterval, len(grid))
	for i := range grid {
		out[i] = SInterval{
			Timestamp: int64(i) * 15 * minMs,
			GridPower: grid[i],
			EssPower:  ess[i],
			Price:     price[i],
		}
	}
	return out
}

func TestComputeSavingsEmpty(t *testing.T) {
	got := ComputeSavings(nil, SSavingsConfig{DemandPrice: 30})
	if got != (SSavings{}) {
		t.Errorf("空输入应全零，got %+v", got)
	}
}

func TestComputeSavingsArbitrage(t *testing.T) {
	// 谷价 0.3 充电 100kW×0.25h=25kWh；峰价 1.0 放电 100kW×0.25h=25kWh
	it := build(
		[]float64{100, 100, 100, 100},
		[]float64{100, -100, 0, 0},
		[]float64{0.3, 1.0, 0.5, 0.3},
	)
	got := ComputeSavings(it, SSavingsConfig{DemandPrice: 0})

	if !almostEqual(got.ChargeEnergy, 25) {
		t.Errorf("ChargeEnergy = %v, want 25", got.ChargeEnergy)
	}
	if !almostEqual(got.DischargeEnergy, 25) {
		t.Errorf("DischargeEnergy = %v, want 25", got.DischargeEnergy)
	}
	if !almostEqual(got.ChargeCost, 7.5) {
		t.Errorf("ChargeCost = %v, want 7.5", got.ChargeCost)
	}
	if !almostEqual(got.DischargeRevenue, 25) {
		t.Errorf("DischargeRevenue = %v, want 25", got.DischargeRevenue)
	}
	if !almostEqual(got.ArbitrageProfit, 17.5) {
		t.Errorf("ArbitrageProfit = %v, want 17.5", got.ArbitrageProfit)
	}
	if !almostEqual(got.TotalSaving, 17.5) {
		t.Errorf("TotalSaving = %v, want 17.5", got.TotalSaving)
	}
}

func TestComputeSavingsDemandShaving(t *testing.T) {
	// 峰段负荷 300kW，储能放电 100kW 削至 200kW；随后谷价充电
	it := build(
		[]float64{100, 200, 100, 100},
		[]float64{0, -100, 0, 50},
		[]float64{0.3, 1.0, 0.5, 0.3},
	)
	got := ComputeSavings(it, SSavingsConfig{DemandPrice: 30})

	if !almostEqual(got.ActualMaxDemand, 200) {
		t.Errorf("ActualMaxDemand = %v, want 200", got.ActualMaxDemand)
	}
	if !almostEqual(got.BaselineMaxDemand, 300) {
		t.Errorf("BaselineMaxDemand = %v, want 300", got.BaselineMaxDemand)
	}
	if !almostEqual(got.DemandSaving, 3000) {
		t.Errorf("DemandSaving = %v, want 3000", got.DemandSaving)
	}
	if !almostEqual(got.ArbitrageProfit, 21.25) {
		t.Errorf("ArbitrageProfit = %v, want 21.25", got.ArbitrageProfit)
	}
	if !almostEqual(got.TotalSaving, 3021.25) {
		t.Errorf("TotalSaving = %v, want 3021.25", got.TotalSaving)
	}
}

func TestComputeSavingsDemandBaselineBelowActual(t *testing.T) {
	// 充电反而抬高关口，基线低于实际 → 需量节省取 0
	it := build(
		[]float64{200, 100, 100, 100},
		[]float64{100, 0, 0, 0},
		[]float64{0.3, 0.3, 0.3, 0.3},
	)
	got := ComputeSavings(it, SSavingsConfig{DemandPrice: 30})
	if got.ActualMaxDemand != 200 {
		t.Errorf("ActualMaxDemand = %v, want 200", got.ActualMaxDemand)
	}
	if got.BaselineMaxDemand != 100 {
		t.Errorf("BaselineMaxDemand = %v, want 100", got.BaselineMaxDemand)
	}
	if got.DemandSaving != 0 {
		t.Errorf("DemandSaving = %v, want 0", got.DemandSaving)
	}
}

func TestComputeSavingsSkipsLargeGap(t *testing.T) {
	// 第二个间隔 2 小时 > 默认 60 分钟，其电量应被跳过
	it := []SInterval{
		{Timestamp: 0, GridPower: 100, EssPower: 100, Price: 1},
		{Timestamp: 15 * minMs, GridPower: 100, EssPower: 100, Price: 1},
		{Timestamp: 15*minMs + 2*60*minMs, GridPower: 100, EssPower: 100, Price: 1},
	}
	got := ComputeSavings(it, SSavingsConfig{})
	// 仅首个间隔计入: 100kW × 0.25h = 25kWh
	if !almostEqual(got.ChargeEnergy, 25) {
		t.Errorf("ChargeEnergy = %v, want 25（大间隔应跳过）", got.ChargeEnergy)
	}
	if !almostEqual(got.ChargeCost, 25) {
		t.Errorf("ChargeCost = %v, want 25", got.ChargeCost)
	}
}

func TestComputeSavingsAveragesWithinWindow(t *testing.T) {
	// 同一 15 分钟窗口内 (100,300) 均值 200；应取窗口均值而非瞬时最大值
	it := []SInterval{
		{Timestamp: 0, GridPower: 100},
		{Timestamp: 30 * 1000, GridPower: 300},
		{Timestamp: 15 * minMs, GridPower: 250},
	}
	got := ComputeSavings(it, SSavingsConfig{})
	if !almostEqual(got.ActualMaxDemand, 250) {
		t.Errorf("ActualMaxDemand = %v, want 250（窗口均值）", got.ActualMaxDemand)
	}
}

func TestComputeSavingsCustomWindow(t *testing.T) {
	// 窗口 30 分钟：0/15min 两点同窗口，均值 (100+200)/2=150；30min 点单独 400
	it := []SInterval{
		{Timestamp: 0, GridPower: 100},
		{Timestamp: 15 * minMs, GridPower: 200},
		{Timestamp: 30 * minMs, GridPower: 400},
	}
	got := ComputeSavings(it, SSavingsConfig{DemandWindowMin: 30})
	if !almostEqual(got.ActualMaxDemand, 400) {
		t.Errorf("ActualMaxDemand = %v, want 400", got.ActualMaxDemand)
	}
}
