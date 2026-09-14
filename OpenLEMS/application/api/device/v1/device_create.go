package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

type CreateDeviceReq struct {
	g.Meta     `path:"/device/create" method:"post" tags:"设备相关" summary:"创建设备" role:"admin"`
	DeviceId   string `json:"deviceId" dc:"设备ID，空则自动生成"`
	Pid        string `json:"pid" dc:"父设备ID，默认0"`
	Name       string `json:"name" v:"required" dc:"设备名称"`
	ProtocolId string `json:"protocolId" v:"required" dc:"协议ID"`
	Driver     string `json:"driver" v:"required" dc:"驱动名称"`
	Params     string `json:"params" dc:"设备参数JSON，Modbus默认 {\"unitId\":1}"`
	Enabled    *bool  `json:"enabled" dc:"是否启用，默认true"`
	SetAsRoot  bool   `json:"setAsRoot" dc:"设为根设备并加载采集"`
}

type CreateDeviceRes struct {
	DeviceId string `json:"deviceId" dc:"设备ID"`
}
