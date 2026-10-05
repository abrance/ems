package internal

const (
	defaultDemandWindowMin = 15
	defaultMaxGapMin       = 60
	msPerMinute            = 60 * 1000
	msPerHour              = 60 * msPerMinute
)

// ComputeSavings 根据实测序列计算峰谷套利收益与需量节省。
//
// 思路（无需反事实仿真）：
//   - 套利：充电量按当时电价计成本，放电量按当时电价计收益，差值即套利净收益。
//   - 需量：无储能时的关口功率 = 实际关口功率 − 储能功率；
//     分别按需量窗口取平均、再取周期内最大值，两者之差即被储能削掉的需量。
//
// 传入序列应为同一时间轴上的等间隔采样（如 1 分钟或 15 分钟）。
func ComputeSavings(intervals []SInterval, cfg SSavingsConfig) SSavings {
	var out SSavings
	if len(intervals) == 0 {
		return out
	}

	windowMin := cfg.DemandWindowMin
	if windowMin <= 0 {
		windowMin = defaultDemandWindowMin
	}
	maxGapMin := cfg.MaxGapMin
	if maxGapMin <= 0 {
		maxGapMin = defaultMaxGapMin
	}
	windowMs := int64(windowMin) * msPerMinute
	maxGapHours := float64(maxGapMin) / 60.0

	// 1. 电量与套利
	for i := range intervals {
		dt := sampleHours(intervals, i)
		if dt <= 0 || dt > maxGapHours {
			continue
		}
		it := intervals[i]
		switch {
		case it.EssPower > 0: // 充电
			kwh := it.EssPower * dt
			out.ChargeEnergy += kwh
			out.ChargeCost += kwh * it.Price
		case it.EssPower < 0: // 放电
			kwh := -it.EssPower * dt
			out.DischargeEnergy += kwh
			out.DischargeRevenue += kwh * it.Price
		}
	}
	out.ArbitrageProfit = out.DischargeRevenue - out.ChargeCost

	// 2. 需量窗口统计
	type windowAcc struct {
		n        int
		grid     float64
		baseline float64
	}
	windows := make(map[int64]*windowAcc)
	for _, it := range intervals {
		key := it.Timestamp / windowMs
		acc := windows[key]
		if acc == nil {
			acc = &windowAcc{}
			windows[key] = acc
		}
		acc.n++
		acc.grid += it.GridPower
		acc.baseline += it.GridPower - it.EssPower
	}

	first := true
	for _, acc := range windows {
		if acc.n == 0 {
			continue
		}
		grid := acc.grid / float64(acc.n)
		baseline := acc.baseline / float64(acc.n)
		if first {
			out.ActualMaxDemand = grid
			out.BaselineMaxDemand = baseline
			first = false
			continue
		}
		if grid > out.ActualMaxDemand {
			out.ActualMaxDemand = grid
		}
		if baseline > out.BaselineMaxDemand {
			out.BaselineMaxDemand = baseline
		}
	}

	if saved := out.BaselineMaxDemand - out.ActualMaxDemand; saved > 0 {
		out.DemandSaving = saved * cfg.DemandPrice
	}

	out.TotalSaving = out.ArbitrageProfit + out.DemandSaving
	return out
}

// sampleHours 返回第 i 个采样点代表的时长（小时）。
// 取相邻点时间差；末点沿用前一间隔；不足两点或时间非递增时返回 0（跳过该点电量）。
func sampleHours(intervals []SInterval, i int) float64 {
	n := len(intervals)
	if n < 2 {
		return 0
	}
	var ms int64
	if i < n-1 {
		ms = intervals[i+1].Timestamp - intervals[i].Timestamp
	} else {
		ms = intervals[i].Timestamp - intervals[i-1].Timestamp
	}
	if ms <= 0 {
		return 0
	}
	return float64(ms) / float64(msPerHour)
}
