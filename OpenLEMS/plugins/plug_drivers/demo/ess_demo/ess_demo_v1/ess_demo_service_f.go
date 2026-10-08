package ess_demo_v1

import (
	"common/c_enum"
	"common/c_log"
	"common/c_proto"
	"errors"
	"math/rand"
)

func (s *sEssDemo) CustomPowerZeroService() error {
	return s.SetPower(0)
}

// CustomStartService 开机。ess_demo 的 SetStatus 只在「关机」时写寄存器，
// 开机必须自己写 DeviceControl=1 —— 否则设备管理器每次 Restart（建/改设备）
// 调过 Shutdown() 之后，储能柜会一直停在关机态，只能靠重建容器恢复。
func (s *sEssDemo) CustomStartService() error {
	return s.ExecuteProtocolMethod(func(protocol c_proto.IModbusProtocol) error {
		return protocol.WriteSingleRegister(DeviceControl, 1)
	})
}

// CustomStopService 关机：清目标功率 + 写 DeviceControl=0（与 Shutdown 同一条路径）。
func (s *sEssDemo) CustomStopService() error {
	return s.SetStatus(c_enum.EPcsStatusOff)
}

// CustomSetPowerService 人工设定充放功率（kW，正=充电 负=放电），
// 先按设备上报的充/放电能力钳位，再下发目标功率。
func (s *sEssDemo) CustomSetPowerService(power float64) error {
	clamped := power
	if mx, err := s.GetMaxInputPower(); err == nil && mx != nil && clamped > float64(*mx) {
		clamped = float64(*mx)
	}
	if mn, err := s.GetMaxOutputPower(); err == nil && mn != nil && clamped < -float64(*mn) {
		clamped = -float64(*mn)
	}
	if clamped != power {
		c_log.BizInfof(s.DeviceCtx, "手动设定功率 %.1f kW 超出设备能力，钳位到 %.1f kW", power, clamped)
	}
	return s.SetPower(int32(clamped))
}
func (s *sEssDemo) CustomPowerRandomService() error {
	mx, err := s.GetMaxInputPower()
	if err != nil {
		return err
	}
	if mx == nil {
		return errors.New("无法获取最大输入功率")
	}

	mi, err := s.GetMaxOutputPower()
	if err != nil {
		return err
	}
	if mi == nil {
		return errors.New("无法获取最大输出功率")
	}

	// 生成在最大输入功率到最大输出功率之间的随机数
	random := rand.Intn(int(*mx-*mi)+1) + int(*mi)

	c_log.BizDebugf(s.DeviceCtx, "设置随机功率为：%v", random)
	return s.SetPower(int32(random))
}
