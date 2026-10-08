package control

import (
	v1 "application/api/control/v1"
	"common"
	"common/c_base"
	"common/c_log"
	"context"
	"strings"

	"s_db"
	"s_db/s_db_basic"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// manualControlEnabled 读「允许手动控制」开关。
//
// 默认关闭（见 s_db_basic.SystemSettingManualControlEnabled 的定义）：手动指令与运行中的
// 策略会互相覆盖，策略每个周期都会重写目标功率，而当前没有临时接管机制 —— 留一个会静默
// 被覆盖的按钮比不做更糟。要做临时手动控制，先把该设置改成 true。
func manualControlEnabled(ctx context.Context) bool {
	value := s_db.GetSettingService().GetSettingValueBySystemSettingDefine(ctx, s_db_basic.SystemSettingManualControlEnabled)
	return value != nil && strings.EqualFold(strings.TrimSpace(*value), "true")
}

func (c *ControllerV1) ControlDevice(ctx context.Context, req *v1.ControlDeviceReq) (res *v1.ControlDeviceRes, err error) {

	if !manualControlEnabled(ctx) {
		c_log.BizInfof(ctx, "手动控制被拒绝：开关未开启（manual_control_enabled）")
		return nil, gerror.NewCode(gcode.CodeBusinessValidationFailed,
			"手动控制当前已停用：手动指令会被运行中的策略覆盖（策略每个周期都会重写目标功率），"+
				"且暂无临时接管机制。如需临时启用，请把设置 manual_control_enabled 改为 true；"+
				"常规充放请走「管理 · 策略管理」。")
	}

	device := common.GetDeviceManager().GetDeviceById(req.DeviceId)
	if device == nil {
		c_log.BizErrorf(ctx, "手动控制设备失败！设备[%s]不存在", req.DeviceId)
		return nil, gerror.NewCode(gcode.CodeNotFound, "device not found")
	}

	config := common.GetDeviceManager().GetDeviceConfigById(req.DeviceId)
	if config == nil {
		c_log.BizErrorf(ctx, "手动控制设备失败！设备[%s]配置不存在", req.DeviceId)
		return nil, gerror.NewCode(gcode.CodeNotFound, "device not found")
	}

	// 记录开始执行服务
	c_log.BizInfof(ctx, "手动控制设备开始执行服务！设备[%s]，服务[%s]，参数[%v]",
		config.Name, req.CommandName, req.Parameters)

	err = c_base.ExecuteCustomService(req.CommandName, device, req.Parameters)
	if err != nil {
		c_log.BizErrorf(ctx, "手动控制设备执行服务失败！设备[%s]，服务[%s]，原因：%v",
			config.Name, req.CommandName, err)
		return nil, err
	}

	c_log.BizInfof(ctx, "手动控制设备执行服务成功！设备[%s]，服务[%s]",
		config.Name, req.CommandName)

	return &v1.ControlDeviceRes{}, nil
}
