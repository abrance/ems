package effect

import "testing"

func TestAlgorithmName(t *testing.T) {
	// 与 p_policy_mircogrid 决策器 Name() 保持一致
	if got := algorithmName(false); got != "tou-rule" {
		t.Errorf("algorithmName(false) = %q, want tou-rule", got)
	}
	if got := algorithmName(true); got != "tou-rule+demand" {
		t.Errorf("algorithmName(true) = %q, want tou-rule+demand", got)
	}
}
