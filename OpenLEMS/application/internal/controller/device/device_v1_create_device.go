package device

import (
	v1 "application/api/device/v1"
	"common"
	"common/c_log"
	"context"
	"s_db"
	"s_db/s_db_basic"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

func (c *ControllerV1) CreateDevice(ctx context.Context, req *v1.CreateDeviceReq) (res *v1.CreateDeviceRes, err error) {
	c_log.BizInfo(ctx, "创建设备操作开始", "name", req.Name, "driver", req.Driver)

	pid := req.Pid
	if pid == "" {
		pid = "0"
	}
	params := req.Params
	if params == "" {
		params = `{"unitId":1}`
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	device := &s_db_model.SDeviceModel{
		SDatabaseBasic: s_db_model.SDatabaseBasic{
			Id: req.DeviceId,
		},
		Pid:                pid,
		Name:               req.Name,
		ProtocolId:         req.ProtocolId,
		Driver:             req.Driver,
		Params:             params,
		Enabled:            enabled,
		StorageEnable:      true,
		StorageIntervalSec: 60,
	}

	deviceId, err := s_db.GetDeviceService().CreateDevice(ctx, device)
	if err != nil {
		c_log.BizError(ctx, "创建设备失败", "name", req.Name, "error", err)
		return nil, gerror.WrapCode(gcode.CodeBusinessValidationFailed, err, "创建设备失败")
	}

	if req.SetAsRoot {
		if err := s_db.GetSettingService().SetSettingValueById(ctx, s_db_basic.SystemSettingActiveDeviceRootId.Id, deviceId); err != nil {
			c_log.BizError(ctx, "设置根设备失败", "deviceId", deviceId, "error", err)
			return nil, gerror.WrapCode(gcode.CodeInternalError, err, "设备已创建，设置根设备失败")
		}
	}

	common.GetDeviceManager().Restart()
	c_log.BizInfo(ctx, "创建设备操作成功", "deviceId", deviceId)
	return &v1.CreateDeviceRes{DeviceId: deviceId}, nil
}
