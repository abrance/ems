package internal

import (
	"math"
	"testing"

	"common/c_enum"
)

const btStepMs = 15 * 60 * 1000

// makeTouDay 构造一天 15 分钟粒度序列：谷/平/峰 各占 1/3，负荷恒为 100kW。
func makeTouDay(n int) []SBacktestPoint {
	points := make([]SBacktestPoint, n)
	for i := 0; i < n; i++ {
		var priceType c_enum.EPriceType
		var price float64
		switch {
		case i < n/3:
			priceType, price = c_enum.EPriceTypeValley, 0.3
		case i < 2*n/3:
			priceType, price = c_enum.EPriceTypeFlat, 0.7
		default:
			priceType, price = c_enum.EPriceTypePeak, 1.2
		}
		points[i] = SBacktestPoint{
			Timestamp: int64(i+1) * btStepMs,
			LoadKW:    100,
			Price:     price,
			PriceType: priceType,
		}
	}
	return points
}

// cleanBattery 效率为 1 的电池，便于手算校验。
func cleanBattery() SBacktestBattery {
	return SBacktestBattery{
		RatedPowerKW: 50,
		CapacityKWh:  200,
		SocMinRatio:  0.10,
		SocMaxRatio:  0.95,
		Soc0Ratio:    0.10,
		ChargeEff:    1,
		DischargeEff: 1,
	}
}

func TestBacktestIdleBaseline(t *testing.T) {
	results := BacktestAlgorithms(makeTouDay(96), SBacktestConfig{Battery: cleanBattery()})
	if len(results) != 2 {
		t.Fatalf("算法数 = %d, want 2", len(results))
	}

	idle := results[0]
	if idle.Algorithm != "idle" {
		t.Errorf("首个算法应为 idle，got %s", idle.Algorithm)
	}
	if idle.Savings.TotalSaving != 0 {
		t.Errorf("基线不应有节省，got %.4f", idle.Savings.TotalSaving)
	}
	for i, p := range idle.EssPowerKW {
		if p != 0 {
			t.Fatalf("基线第 %d 点储能功率应为 0，got %.3f", i, p)
		}
	}
}

func TestBacktestTouArbitrage(t *testing.T) {
	results := BacktestAlgorithms(makeTouDay(96), SBacktestConfig{Battery: cleanBattery()})
	tou := results[1]
	if tou.Algorithm != "tou-rule" {
		t.Fatalf("第二个算法应为 tou-rule，got %s", tou.Algorithm)
	}

	// 谷段充至上限：SOC 10%→95% => 200kWh × 85% = 170kWh
	// 峰段放至下限：95%→10% => 170kWh
	// 套利 = 170×1.2 − 170×0.3 = 153 元
	assertClose(t, "充电量", tou.Savings.ChargeEnergy, 170)
	assertClose(t, "放电量", tou.Savings.DischargeEnergy, 170)
	assertClose(t, "套利净收益", tou.Savings.ArbitrageProfit, 153)
	if tou.Savings.ArbitrageProfit <= 0 {
		t.Error("峰谷套利收益应为正")
	}
}

func TestBacktestSocWithinBounds(t *testing.T) {
	bat := cleanBattery()
	bat.CapacityKWh = 60 // 小容量，强制反复触及边界

	for _, r := range BacktestAlgorithms(makeTouDay(96), SBacktestConfig{Battery: bat}) {
		if r.FinalSocPct < bat.SocMinRatio*100-1e-6 || r.FinalSocPct > bat.SocMaxRatio*100+1e-6 {
			t.Errorf("%s 终止 SOC %.3f 越界", r.Algorithm, r.FinalSocPct)
		}
		for i, p := range r.EssPowerKW {
			if math.Abs(p) > bat.RatedPowerKW+1e-6 {
				t.Fatalf("%s 第 %d 点功率 %.3f 超过额定 %.1f", r.Algorithm, i, p, bat.RatedPowerKW)
			}
		}
	}
}

func TestBacktestDemandAlgorithm(t *testing.T) {
	// 平段基础负荷 60kW，中段出现 100kW 尖峰；需量目标 80kW
	n := 96
	points := make([]SBacktestPoint, n)
	for i := range points {
		priceType := c_enum.EPriceTypeFlat
		if i < 16 {
			priceType = c_enum.EPriceTypeValley
		}
		load := 60.0
		if i >= 40 && i < 44 {
			load = 100
		}
		points[i] = SBacktestPoint{
			Timestamp: int64(i+1) * btStepMs,
			LoadKW:    load,
			Price:     0.7,
			PriceType: priceType,
		}
	}

	results := BacktestAlgorithms(points, SBacktestConfig{
		Battery:      cleanBattery(),
		DemandTarget: 80,
		DemandPrice:  40,
	})
	if len(results) != 3 {
		t.Fatalf("算法数 = %d, want 3", len(results))
	}

	demand := results[2]
	if demand.Algorithm != "tou-rule+demand" {
		t.Fatalf("第三个算法应为 tou-rule+demand，got %s", demand.Algorithm)
	}
	// 需量应被压到目标以内（基线峰值 100kW，目标 80kW => 省 20kW × 40元 = 800元）
	if demand.Savings.ActualMaxDemand > 80+0.5 {
		t.Errorf("实际最大需量 %.2f 未压到目标 80 以内", demand.Savings.ActualMaxDemand)
	}
	assertClose(t, "需量节省", demand.Savings.DemandSaving, 800)
}

func TestBacktestSampleHoursMatchesSEffect(t *testing.T) {
	points := makeTouDay(4)
	// 相邻差为 0.25h；末点沿用前一间隔
	for i := 0; i < 4; i++ {
		if got := backtestSampleHours(points, i); math.Abs(got-0.25) > 1e-9 {
			t.Errorf("第 %d 点步长 = %.6f, want 0.25", i, got)
		}
	}
	if got := backtestSampleHours(points[:1], 0); got != 0 {
		t.Errorf("单点步长应为 0，got %v", got)
	}
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1 {
		t.Errorf("%s = %.4f, want ≈%.4f", name, got, want)
	}
}
