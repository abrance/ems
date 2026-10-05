package internal

import (
	"math"

	"common/c_enum"
)

// EAction 充放电动作
type EAction int8

const (
	EActionIdle      EAction = 0 // 待机
	EActionCharge    EAction = 1 // 充电
	EActionDischarge EAction = 2 // 放电
)

// String 便于日志输出
func (a EAction) String() string {
	switch a {
	case EActionCharge:
		return "充电"
	case EActionDischarge:
		return "放电"
	default:
		return "待机"
	}
}

// SDecisionInput 决策输入。
// 纯数据结构，不依赖设备或运行时环境，便于单元测试。
type SDecisionInput struct {
	PriceType c_enum.EPriceType // 当前电价类型（谷/平/峰/尖峰/深谷）
	Price     float64           // 当前电价（元/kWh）

	Soc    float64 // 当前 SOC，单位：百分比 0-100
	SocMin float64 // SOC 下限，单位：百分比 0-100
	SocMax float64 // SOC 上限，单位：百分比 0-100

	// 需量控制输入
	// GridPower 为当前关口功率 kW（正=从电网取电，负=反送）；
	// 在每个采样周期内将其限制在目标值以下，即可保证 15 分钟均值不超限。
	GridPower    float64
	DemandTarget float64 // 需量目标 kW，<=0 表示未启用
	RatedPowerKW float64 // 储能额定功率 kW，用于将绝对功率换算为功率比例
}

// SDecision 决策输出。
type SDecision struct {
	Action     EAction // 动作
	PowerRatio float64 // 功率比例 0-1（相对额定功率），方向由 Action 决定
	Reason     string  // 决策原因，用于日志与排查
}

// TargetPower 将决策转换为带符号目标功率（单位 kW）。
// 约定：正数=充电，负数=放电，0=待机（与设备 PCS 一致）。
func (d SDecision) TargetPower(ratedPowerKW float64) int32 {
	if ratedPowerKW <= 0 || d.PowerRatio <= 0 {
		return 0
	}
	power := math.Round(d.PowerRatio * ratedPowerKW)
	switch d.Action {
	case EActionCharge:
		return int32(power)
	case EActionDischarge:
		return -int32(power)
	default:
		return 0
	}
}

// IDecisionMaker 决策器接口。
// 规则版（STouRule）实现之；后续优化器 / 需量控制只需实现同一接口即可替换，
// Run() 无需改动。
type IDecisionMaker interface {
	Decide(in SDecisionInput) SDecision
}

// STouRule 峰谷套利规则决策器。
// 规则：谷/深谷充电，峰/尖峰放电，其余待机；并在 SOC 边界处保护。
type STouRule struct {
	ChargeRatio    float64 // 充电功率比例 0-1
	DischargeRatio float64 // 放电功率比例 0-1
}

// 编译期断言：STouRule 实现 IDecisionMaker
var _ IDecisionMaker = (*STouRule)(nil)

// Decide 依据电价类型与 SOC 给出充放电决策。
func (r *STouRule) Decide(in SDecisionInput) SDecision {
	switch in.PriceType {
	case c_enum.EPriceTypeValley, c_enum.EPriceTypeDeepValley:
		if in.Soc >= in.SocMax {
			return SDecision{Action: EActionIdle, Reason: "谷段但 SOC 已达上限，禁止充电"}
		}
		return SDecision{Action: EActionCharge, PowerRatio: r.ChargeRatio, Reason: "谷电充电"}

	case c_enum.EPriceTypePeak, c_enum.EPriceTypeSharp:
		if in.Soc <= in.SocMin {
			return SDecision{Action: EActionIdle, Reason: "峰段但 SOC 已达下限，禁止放电"}
		}
		return SDecision{Action: EActionDischarge, PowerRatio: r.DischargeRatio, Reason: "峰电放电"}

	default:
		return SDecision{Action: EActionIdle, Reason: "平电待机"}
	}
}

// SDemandLimitRule 需量限制决策器。
// 包装一个基础决策器（如峰谷套利），在关口功率逼近/超过需量目标时：
//   - 限制充电功率，避免把关口推过目标；
//   - 待机/放电时按需追加放电，把关口压回目标以内。
//
// 组合式设计：Base 可为任意 IDecisionMaker，便于后续叠加更多约束。
type SDemandLimitRule struct {
	Base IDecisionMaker
}

var _ IDecisionMaker = (*SDemandLimitRule)(nil)

// Decide 在基础决策之上叠加需量约束。
func (r *SDemandLimitRule) Decide(in SDecisionInput) SDecision {
	base := r.Base.Decide(in)

	// 未启用需量控制或额定功率未知，直接透传
	if in.DemandTarget <= 0 || in.RatedPowerKW <= 0 {
		return base
	}

	need := in.GridPower - in.DemandTarget // 需削减的关口功率 kW

	if base.Action == EActionCharge {
		if need > 0 {
			// 关口已超目标，禁止充电，改为按需放电
			return r.demandDischarge(in, need, "关口超需量目标，改为放电")
		}
		// 限制充电功率，避免把关口推过目标
		allowed := in.DemandTarget - in.GridPower
		if base.PowerRatio*in.RatedPowerKW <= allowed {
			return base
		}
		ratio := allowed / in.RatedPowerKW
		if ratio <= 0 {
			return SDecision{Action: EActionIdle, Reason: "需量受限，暂停充电"}
		}
		return SDecision{Action: EActionCharge, PowerRatio: ratio, Reason: "需量受限，降功率充电"}
	}

	// 待机 / 放电：不足则补放电
	baseKW := 0.0
	if base.Action == EActionDischarge {
		baseKW = base.PowerRatio * in.RatedPowerKW
	}
	target := math.Max(baseKW, need)
	if target <= 0 {
		return base
	}
	return r.demandDischarge(in, target, "需量控制放电")
}

// demandDischarge 以指定功率放电，含 SOC 下限保护与功率比例限幅。
func (r *SDemandLimitRule) demandDischarge(in SDecisionInput, kw float64, reason string) SDecision {
	if in.Soc <= in.SocMin {
		return SDecision{Action: EActionIdle, Reason: reason + "（SOC 已达下限）"}
	}
	ratio := kw / in.RatedPowerKW
	if ratio > 1 {
		ratio = 1
	}
	if ratio <= 0 {
		return SDecision{Action: EActionIdle, Reason: reason}
	}
	return SDecision{Action: EActionDischarge, PowerRatio: ratio, Reason: reason}
}
