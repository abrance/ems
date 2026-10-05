package s_effect

import "s_effect/internal"

// 导出内部类型
type SInterval = internal.SInterval
type SSavingsConfig = internal.SSavingsConfig
type SSavings = internal.SSavings
type SSeries = internal.SSeries

// ComputeSavings 计算峰谷套利收益与需量节省
func ComputeSavings(intervals []SInterval, cfg SSavingsConfig) SSavings {
	return internal.ComputeSavings(intervals, cfg)
}

// BuildIntervals 按时间戳交集对齐关口与储能序列，并附加电价
func BuildIntervals(grid, ess SSeries, priceAt func(timestamp int64) float64) []SInterval {
	return internal.BuildIntervals(grid, ess, priceAt)
}
