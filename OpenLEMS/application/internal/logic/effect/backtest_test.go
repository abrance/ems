package effect

import (
	"testing"

	"common/c_enum"
	"s_effect"
	"s_price"
)

func TestBuildBacktestPoints(t *testing.T) {
	grid := s_effect.SSeries{1000: 100, 2000: 120}
	ess := s_effect.SSeries{1000: -20, 2000: 30, 3000: 5} // 3000 不在关口序列中，应跳过

	priceAt := func(ts int64) *s_price.SPriceSegment {
		if ts == 1000 {
			return &s_price.SPriceSegment{Price: 0.5, PriceType: c_enum.EPriceTypeValley}
		}
		return nil
	}

	points := buildBacktestPoints(grid, ess, priceAt)
	if len(points) != 2 {
		t.Fatalf("点数 = %d, want 2", len(points))
	}
	// 升序
	if points[0].Timestamp != 1000 || points[1].Timestamp != 2000 {
		t.Fatalf("未按时间升序: %d, %d", points[0].Timestamp, points[1].Timestamp)
	}
	// 自然负荷 = 关口 − 储能
	if points[0].LoadKW != 120 || points[0].Price != 0.5 || points[0].PriceType != c_enum.EPriceTypeValley {
		t.Errorf("点0 = %+v", points[0])
	}
	if points[1].LoadKW != 90 || points[1].Price != 0 {
		t.Errorf("点1 = %+v", points[1])
	}
}

func TestPolicyConfigApplyDefaults(t *testing.T) {
	cfg := &policyConfig{}
	cfg.applyDefaults()
	if cfg.SocMinRatio != 0.1 || cfg.SocMaxRatio != 0.95 {
		t.Errorf("SOC 默认值错误: %v/%v", cfg.SocMinRatio, cfg.SocMaxRatio)
	}
	if cfg.ChargeEfficiency != 0.95 || cfg.DischargeEfficiency != 0.95 {
		t.Errorf("效率默认值错误: %v/%v", cfg.ChargeEfficiency, cfg.DischargeEfficiency)
	}

	// 已配置时不应被覆盖
	cfg2 := &policyConfig{SocMinRatio: 0.2, SocMaxRatio: 0.8, ChargeEfficiency: 0.9, DischargeEfficiency: 0.9}
	cfg2.applyDefaults()
	if cfg2.SocMinRatio != 0.2 || cfg2.SocMaxRatio != 0.8 || cfg2.ChargeEfficiency != 0.9 {
		t.Errorf("已配置值被覆盖: %+v", cfg2)
	}
}
