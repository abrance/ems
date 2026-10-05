package internal

import "testing"

func TestDecisionMakerNames(t *testing.T) {
	tou := &STouRule{}
	if got := tou.Name(); got != "tou-rule" {
		t.Errorf("STouRule.Name() = %q, want tou-rule", got)
	}

	demand := &SDemandLimitRule{Base: tou}
	if got := demand.Name(); got != "tou-rule+demand" {
		t.Errorf("SDemandLimitRule.Name() = %q, want tou-rule+demand", got)
	}
}

// 断言：newDecider 产出的算法名与效果测算侧的推导保持一致
func TestNewDeciderNames(t *testing.T) {
	base := &sPolicyMircogrid{config: &SPolicyMircogridConfig{}}
	if got := base.newDecider().Name(); got != "tou-rule" {
		t.Errorf("未启用需量控制时 = %q, want tou-rule", got)
	}

	withDemand := &sPolicyMircogrid{config: &SPolicyMircogridConfig{EnableDemandControl: true}}
	if got := withDemand.newDecider().Name(); got != "tou-rule+demand" {
		t.Errorf("启用需量控制时 = %q, want tou-rule+demand", got)
	}
}
