package control

import (
	v1 "application/api/control/v1"
	"common"
	"context"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetCustomServices 返回设备驱动声明的自定义服务（build.yaml 的 service 段）。
//
// 手动控制页据此渲染「可执行服务」列表，参数按驱动声明顺序下发；
// 这里只做透传，不在服务端自造业务规则（校验在前端 + 驱动方法内）。
func (c *ControllerV1) GetCustomServices(ctx context.Context, req *v1.GetCustomServicesReq) (res *v1.GetCustomServicesRes, err error) {

	deviceWrapper := common.GetDeviceManager().GetDeviceById(req.DeviceId)
	if deviceWrapper == nil {
		return nil, gerror.NewCode(gcode.CodeNotFound, "device not found")
	}

	config := deviceWrapper.GetConfig()
	if config == nil || config.DriverInfo == nil {
		// 驱动没加载出来：没有服务可下发，但设备本身存在，不是错误
		return &v1.GetCustomServicesRes{}, nil
	}

	return &v1.GetCustomServicesRes{Services: config.DriverInfo.Service}, nil
}
