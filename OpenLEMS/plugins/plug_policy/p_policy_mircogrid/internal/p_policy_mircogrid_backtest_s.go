package internal

import (
	"math"

	"common/c_enum"
	"s_effect"
)

// 回测兜底步长（小时）：相邻时间戳异常时使用
const backtestFallbackHours = 0.25

// SBacktestBattery 回测用电池参数。
// 多台储能聚合为「等效单电池」：功率与容量取各台之和；
// 对同型号设备，聚合结果与逐台控制等价。
type SBacktestBattery struct {
	RatedPowerKW float64 // 额定功率 kW
	CapacityKWh  float64 // 额定容量 kWh
	SocMinRatio  float64 // SOC 下限 0-1
	SocMaxRatio  float64 // SOC 上限 0-1
	Soc0Ratio    float64 // 初始 SOC 0-1
	ChargeEff    float64 // 充电效率 0-1（<=0 视为 1）
	DischargeEff float64 // 放电效率 0-1（<=0 视为 1）
}

// SBacktestConfig 回测配置。
type SBacktestConfig struct {
	Battery      SBacktestBattery
	DemandTarget float64 // 需量目标 kW，<=0 时不对比需量算法
	DemandPrice  float64 // 需量电价 元/kW
}

// SBacktestPoint 历史采样点。
// LoadKW 为「自然负荷」= 实测关口功率 − 实测储能功率，即无储能时的关口功率。
type SBacktestPoint struct {
	Timestamp int64
	LoadKW    float64
	Price     float64
	PriceType c_enum.EPriceType
}

// SBacktestResult 单算法回测结果。
type SBacktestResult struct {
	Algorithm   string
	Savings     s_effect.SSavings
	FinalSocPct float64
	EssPowerKW  []float64 // 与输入点一一对应，正=充 负=放
}

// idleDecider 不做任何动作的基线算法。
type idleDecider struct{}

func (idleDecider) Decide(SDecisionInput) SDecision {
	return SDecision{Action: EActionIdle, Reason: "基线：不动作"}
}

func (idleDecider) Name() string { return "idle" }

// BacktestAlgorithms 在历史负荷/电价序列上回测内置算法，返回逐算法效果对比。
// 第一个结果固定为 idle 基线；demandTarget > 0 时追加需量算法。
func BacktestAlgorithms(points []SBacktestPoint, cfg SBacktestConfig) []SBacktestResult {
	base := &STouRule{ChargeRatio: 1, DischargeRatio: 1}
	algos := []IDecisionMaker{idleDecider{}, base}
	if cfg.DemandTarget > 0 {
		algos = append(algos, &SDemandLimitRule{Base: base})
	}

	results := make([]SBacktestResult, 0, len(algos))
	for _, algo := range algos {
		results = append(results, simulate(points, cfg, algo))
	}
	return results
}

// simulate 在自然负荷序列上模拟单算法运行，输出效果与逐点储能功率。
//
// 口径约定：
//   - 决策层看到的「关口功率」为自然负荷（关口 − 储能自身出力）。控制器已知自身出力，
//     实际工程中即 readGridPower() − 储能功率；如此需量控制不会因滞后而振荡。
//   - 记录时关口功率取动作生效后的值 => GridPower = 自然负荷 + 储能功率，
//     于是「关口 − 储能 = 自然负荷」，与 ComputeSavings 的无储能基线口径一致。
func simulate(points []SBacktestPoint, cfg SBacktestConfig, algo IDecisionMaker) SBacktestResult {
	bat := cfg.Battery
	soc := clampPct(bat.Soc0Ratio*100, bat)
	essPowers := make([]float64, 0, len(points))
	intervals := make([]s_effect.SInterval, 0, len(points))

	for i := range points {
		pt := points[i]
		dt := backtestSampleHours(points, i)

		// 决策：以自然负荷为输入（不含储能自身出力）
		decision := algo.Decide(SDecisionInput{
			PriceType:    pt.PriceType,
			Price:        pt.Price,
			Soc:          soc,
			SocMin:       bat.SocMinRatio * 100,
			SocMax:       bat.SocMaxRatio * 100,
			GridPower:    pt.LoadKW,
			DemandTarget: cfg.DemandTarget,
			RatedPowerKW: bat.RatedPowerKW,
		})

		power := float64(decision.TargetPower(bat.RatedPowerKW))
		power = limitBySoc(power, soc, bat, dt)
		soc = applySoc(soc, power, bat, dt)

		essPowers = append(essPowers, power)
		intervals = append(intervals, s_effect.SInterval{
			Timestamp: pt.Timestamp,
			GridPower: pt.LoadKW + power,
			EssPower:  power,
			Price:     pt.Price,
		})
	}

	return SBacktestResult{
		Algorithm:   algo.Name(),
		Savings:     s_effect.ComputeSavings(intervals, s_effect.SSavingsConfig{DemandPrice: cfg.DemandPrice}),
		FinalSocPct: soc,
		EssPowerKW:  essPowers,
	}
}

// backtestSampleHours 返回第 i 点代表的时长（小时），规则与 s_effect 一致：
// 取相邻点时间差，末点沿用前一间隔，异常时返回 0。
func backtestSampleHours(points []SBacktestPoint, i int) float64 {
	n := len(points)
	if n < 2 {
		return 0
	}
	var ms int64
	if i < n-1 {
		ms = points[i+1].Timestamp - points[i].Timestamp
	} else {
		ms = points[i].Timestamp - points[i-1].Timestamp
	}
	if ms <= 0 {
		return 0
	}
	return float64(ms) / 3_600_000.0
}

// limitBySoc 依据 SOC 余量限制功率，避免越界（含效率折算）。
func limitBySoc(power, soc float64, bat SBacktestBattery, dt float64) float64 {
	if power == 0 || bat.CapacityKWh <= 0 {
		return 0
	}
	if dt <= 0 {
		dt = backtestFallbackHours
	}

	if power > 0 { // 充电：受限于上限余量
		headroomKWh := (bat.SocMaxRatio*100 - soc) / 100 * bat.CapacityKWh
		if headroomKWh <= 0 {
			return 0
		}
		maxChargeKW := headroomKWh / (dt * chargeEff(bat))
		if power > maxChargeKW {
			power = maxChargeKW
		}
	} else { // 放电：受限于下限余量
		availableKWh := (soc - bat.SocMinRatio*100) / 100 * bat.CapacityKWh
		if availableKWh <= 0 {
			return 0
		}
		maxDischargeKW := availableKWh * dischargeEff(bat) / dt
		if -power > maxDischargeKW {
			power = -maxDischargeKW
		}
	}
	if math.Abs(power) < 1e-9 {
		return 0
	}
	return power
}

// applySoc 依据充放电功率更新 SOC（百分比），并夹在上下限内。
func applySoc(soc, power float64, bat SBacktestBattery, dt float64) float64 {
	if bat.CapacityKWh <= 0 || power == 0 {
		return soc
	}
	if dt <= 0 {
		dt = backtestFallbackHours
	}

	switch {
	case power > 0: // 充电：入池电量按充电效率折算
		soc += power * dt * chargeEff(bat) / bat.CapacityKWh * 100
	case power < 0: // 放电：取用能量按放电效率折算
		soc -= (-power) * dt / dischargeEff(bat) / bat.CapacityKWh * 100
	}
	return math.Max(bat.SocMinRatio*100, math.Min(bat.SocMaxRatio*100, soc))
}

// clampPct 将初始 SOC 夹在上下限内。
func clampPct(soc float64, bat SBacktestBattery) float64 {
	return math.Max(bat.SocMinRatio*100, math.Min(bat.SocMaxRatio*100, soc))
}

// chargeEff / dischargeEff：效率，未配置（<=0）时视为 1。
func chargeEff(bat SBacktestBattery) float64 {
	if bat.ChargeEff <= 0 || bat.ChargeEff > 1 {
		return 1
	}
	return bat.ChargeEff
}

func dischargeEff(bat SBacktestBattery) float64 {
	if bat.DischargeEff <= 0 || bat.DischargeEff > 1 {
		return 1
	}
	return bat.DischargeEff
}
