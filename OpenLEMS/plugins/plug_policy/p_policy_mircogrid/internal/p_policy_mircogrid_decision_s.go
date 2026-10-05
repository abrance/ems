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
