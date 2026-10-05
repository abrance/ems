package s_effect

import "s_effect/internal"

// 导出内部类型
type SInterval = internal.SInterval
type SSavingsConfig = internal.SSavingsConfig
type SSavings = internal.SSavings

// ComputeSavings 计算峰谷套利收益与需量节省
func ComputeSavings(intervals []SInterval, cfg SSavingsConfig) SSavings {
	return internal.ComputeSavings(intervals, cfg)
}
