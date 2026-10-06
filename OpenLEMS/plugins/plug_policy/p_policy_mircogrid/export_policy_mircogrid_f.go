package p_policy_mircogrid

import (
	"common/c_base"
	"p_policy_mircogrid/internal"
)

// NewPolicyMircogrid 创建微电网策略实例
func NewPolicyMircogrid() c_base.IPolicy {
	return internal.NewPolicyMircogrid()
}

// =================================================================================
// 算法回测：在历史负荷/电价序列上对比内置算法的省电费效果
// =================================================================================

// SConfigField 配置字段的表单元数据（反射 struct tag 得到，struct 是唯一真源）
type SConfigField = internal.SConfigField

// ConfigFields 返回储能/微电网策略配置的表单字段元数据（含分组、上下限、单位）
func ConfigFields() []*SConfigField {
	return internal.ConfigFields()
}

type (
	SBacktestBattery = internal.SBacktestBattery
	SBacktestConfig  = internal.SBacktestConfig
	SBacktestPoint   = internal.SBacktestPoint
	SBacktestResult  = internal.SBacktestResult
)

// BacktestAlgorithms 在历史序列上回测内置算法，返回逐算法效果对比（含 idle 基线）。
func BacktestAlgorithms(points []SBacktestPoint, cfg SBacktestConfig) []SBacktestResult {
	return internal.BacktestAlgorithms(points, cfg)
}
