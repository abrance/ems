package internal

import (
	"testing"

	"common/c_enum"
)

func TestSTouRuleDecide(t *testing.T) {
	rule := &STouRule{ChargeRatio: 0.8, DischargeRatio: 0.6}

	tests := []struct {
		name      string
		priceType c_enum.EPriceType
		soc       float64
		socMin    float64
		socMax    float64
		wantAct   EAction
		wantRatio float64
	}{
		{"谷电充电", c_enum.EPriceTypeValley, 50, 10, 95, EActionCharge, 0.8},
		{"深谷充电", c_enum.EPriceTypeDeepValley, 50, 10, 95, EActionCharge, 0.8},
		{"峰电放电", c_enum.EPriceTypePeak, 50, 10, 95, EActionDischarge, 0.6},
		{"尖峰放电", c_enum.EPriceTypeSharp, 50, 10, 95, EActionDischarge, 0.6},
		{"平电待机", c_enum.EPriceTypeFlat, 50, 10, 95, EActionIdle, 0},
		{"谷电但SOC达上限", c_enum.EPriceTypeValley, 95, 10, 95, EActionIdle, 0},
		{"谷电但SOC超上限", c_enum.EPriceTypeDeepValley, 96, 10, 95, EActionIdle, 0},
		{"峰电但SOC达下限", c_enum.EPriceTypePeak, 10, 10, 95, EActionIdle, 0},
		{"峰电但SOC低于下限", c_enum.EPriceTypeSharp, 9, 10, 95, EActionIdle, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rule.Decide(SDecisionInput{
				PriceType: tt.priceType,
				Soc:       tt.soc,
				SocMin:    tt.socMin,
				SocMax:    tt.socMax,
			})
			if got.Action != tt.wantAct {
				t.Errorf("Action = %v, want %v", got.Action, tt.wantAct)
			}
			if got.PowerRatio != tt.wantRatio {
				t.Errorf("PowerRatio = %v, want %v", got.PowerRatio, tt.wantRatio)
			}
		})
	}
}

func TestSDecisionTargetPower(t *testing.T) {
	tests := []struct {
		name       string
		decision   SDecision
		ratedPower float64
		want       int32
	}{
		{"充电为正", SDecision{Action: EActionCharge, PowerRatio: 0.5}, 100, 50},
		{"放电为负", SDecision{Action: EActionDischarge, PowerRatio: 0.5}, 100, -50},
		{"待机为零", SDecision{Action: EActionIdle, PowerRatio: 1}, 100, 0},
		{"满功率充电", SDecision{Action: EActionCharge, PowerRatio: 1}, 100, 100},
		{"四舍五入", SDecision{Action: EActionCharge, PowerRatio: 0.333}, 100, 33},
		{"额定功率未知", SDecision{Action: EActionCharge, PowerRatio: 1}, 0, 0},
		{"比例为零", SDecision{Action: EActionCharge, PowerRatio: 0}, 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.decision.TargetPower(tt.ratedPower); got != tt.want {
				t.Errorf("TargetPower() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	// 缺省配置应填充默认值
	empty := &sPolicyMircogrid{config: &SPolicyMircogridConfig{}}
	empty.applyDefaults()
	if empty.config.SocMaxRatio != 0.95 {
		t.Errorf("SocMaxRatio = %v, want 0.95", empty.config.SocMaxRatio)
	}
	if empty.config.SocMinRatio != 0.1 {
		t.Errorf("SocMinRatio = %v, want 0.1", empty.config.SocMinRatio)
	}
	if empty.config.ChargePowerRatio != 1 {
		t.Errorf("ChargePowerRatio = %v, want 1", empty.config.ChargePowerRatio)
	}
	if empty.config.DischargePowerRatio != 1 {
		t.Errorf("DischargePowerRatio = %v, want 1", empty.config.DischargePowerRatio)
	}

	// 已显式配置的值不应被覆盖
	configured := &sPolicyMircogrid{config: &SPolicyMircogridConfig{
		SocMinRatio:         0.2,
		SocMaxRatio:         0.9,
		ChargePowerRatio:    0.5,
		DischargePowerRatio: 0.6,
	}}
	configured.applyDefaults()
	if configured.config.SocMinRatio != 0.2 {
		t.Errorf("SocMinRatio 被错误覆盖为 %v", configured.config.SocMinRatio)
	}
	if configured.config.SocMaxRatio != 0.9 {
		t.Errorf("SocMaxRatio 被错误覆盖为 %v", configured.config.SocMaxRatio)
	}
	if configured.config.ChargePowerRatio != 0.5 {
		t.Errorf("ChargePowerRatio 被错误覆盖为 %v", configured.config.ChargePowerRatio)
	}
	if configured.config.DischargePowerRatio != 0.6 {
		t.Errorf("DischargePowerRatio 被错误覆盖为 %v", configured.config.DischargePowerRatio)
	}
}
