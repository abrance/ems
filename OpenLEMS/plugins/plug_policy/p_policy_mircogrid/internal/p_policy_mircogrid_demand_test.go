package internal

import (
	"math"
	"testing"

	"common/c_enum"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestSDemandLimitRuleDecide(t *testing.T) {
	base := &STouRule{ChargeRatio: 1.0, DischargeRatio: 0.5}
	rule := &SDemandLimitRule{Base: base}

	const rated = 100.0

	tests := []struct {
		name      string
		priceType c_enum.EPriceType
		soc       float64
		grid      float64
		target    float64
		ratedKW   float64
		wantAct   EAction
		wantRatio float64
	}{
		// 未启用需量控制 → 透传基础决策
		{"未启用需量透传", c_enum.EPriceTypeValley, 50, 999, 0, rated, EActionCharge, 1.0},
		{"额定功率未知透传", c_enum.EPriceTypeValley, 50, 999, 200, 0, EActionCharge, 1.0},

		// 充电受需量限制
		{"充电未受限", c_enum.EPriceTypeValley, 50, 100, 200, rated, EActionCharge, 1.0},
		{"充电降功率", c_enum.EPriceTypeValley, 50, 150, 200, rated, EActionCharge, 0.5},
		{"充电被完全限制", c_enum.EPriceTypeValley, 50, 200, 200, rated, EActionIdle, 0},
		{"关口超目标改放电", c_enum.EPriceTypeValley, 50, 210, 200, rated, EActionDischarge, 0.1},

		// 待机时按需放电
		{"待机关口安全", c_enum.EPriceTypeFlat, 50, 150, 200, rated, EActionIdle, 0},
		{"待机需量放电", c_enum.EPriceTypeFlat, 50, 250, 200, rated, EActionDischarge, 0.5},

		// 放电与需量取较大
		{"放电满足需量", c_enum.EPriceTypePeak, 50, 250, 200, rated, EActionDischarge, 0.5},
		{"需量要求更大放电", c_enum.EPriceTypePeak, 50, 300, 200, rated, EActionDischarge, 1.0},
		{"放电功率限幅", c_enum.EPriceTypePeak, 50, 400, 200, rated, EActionDischarge, 1.0},

		// SOC 下限保护
		{"放电但SOC达下限", c_enum.EPriceTypePeak, 10, 400, 200, rated, EActionIdle, 0},
		{"超目标但SOC达下限", c_enum.EPriceTypeValley, 10, 210, 200, rated, EActionIdle, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rule.Decide(SDecisionInput{
				PriceType:    tt.priceType,
				Soc:          tt.soc,
				SocMin:       10,
				SocMax:       95,
				GridPower:    tt.grid,
				DemandTarget: tt.target,
				RatedPowerKW: tt.ratedKW,
			})
			if got.Action != tt.wantAct {
				t.Errorf("Action = %v, want %v (reason: %s)", got.Action, tt.wantAct, got.Reason)
			}
			if !almostEqual(got.PowerRatio, tt.wantRatio) {
				t.Errorf("PowerRatio = %v, want %v", got.PowerRatio, tt.wantRatio)
			}
		})
	}
}

func TestSDemandLimitRuleWithForecast(t *testing.T) {
	base := &STouRule{ChargeRatio: 1.0, DischargeRatio: 0.5}
	rule := &SDemandLimitRule{Base: base}
	const rated = 100.0

	tests := []struct {
		name      string
		priceType c_enum.EPriceType
		soc       float64
		grid      float64
		forecast  []float64
		target    float64
		wantAct   EAction
		wantRatio float64
	}{
		{"待机+预测峰值超目标→提前放电", c_enum.EPriceTypeFlat, 50, 150, []float64{160, 250, 180}, 200, EActionDischarge, 0.5},
		{"待机+预测全部安全→待机", c_enum.EPriceTypeFlat, 50, 150, []float64{160, 180, 190}, 200, EActionIdle, 0},
		{"放电+预测峰值更大→加大放电", c_enum.EPriceTypePeak, 50, 220, []float64{300}, 200, EActionDischarge, 1.0},
		{"充电+当前安全+预测超目标→仍按当前限充", c_enum.EPriceTypeValley, 50, 150, []float64{300}, 200, EActionCharge, 0.5},
		{"预测峰值但SOC达下限→放电被拦", c_enum.EPriceTypeFlat, 10, 150, []float64{300}, 200, EActionIdle, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rule.Decide(SDecisionInput{
				PriceType:           tt.priceType,
				Soc:                 tt.soc,
				SocMin:              10,
				SocMax:              95,
				GridPower:           tt.grid,
				ForecastedGridPower: tt.forecast,
				DemandTarget:        tt.target,
				RatedPowerKW:        rated,
			})
			if got.Action != tt.wantAct {
				t.Errorf("Action = %v, want %v (reason: %s)", got.Action, tt.wantAct, got.Reason)
			}
			if !almostEqual(got.PowerRatio, tt.wantRatio) {
				t.Errorf("PowerRatio = %v, want %v", got.PowerRatio, tt.wantRatio)
			}
		})
	}
}
