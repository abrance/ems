package internal

import "sort"

// SSeries 时间序列：时间戳（Unix 毫秒）→ 值。
type SSeries map[int64]float64

// BuildIntervals 按时间戳交集对齐关口与储能序列，并附加电价，结果按时间升序。
// priceAt 可为 nil（电价记为 0）。
func BuildIntervals(grid, ess SSeries, priceAt func(timestamp int64) float64) []SInterval {
	if len(grid) == 0 || len(ess) == 0 {
		return nil
	}

	timestamps := make([]int64, 0, len(grid))
	for ts := range grid {
		if _, ok := ess[ts]; ok {
			timestamps = append(timestamps, ts)
		}
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	intervals := make([]SInterval, 0, len(timestamps))
	for _, ts := range timestamps {
		it := SInterval{
			Timestamp: ts,
			GridPower: grid[ts],
			EssPower:  ess[ts],
		}
		if priceAt != nil {
			it.Price = priceAt(ts)
		}
		intervals = append(intervals, it)
	}
	return intervals
}
