package internal

import (
	"context"

	"common"
	"common/c_base"
	"common/c_log"
	"common/c_type"
)

// ExecuteStrategy 执行储能策略
func ExecuteStrategy(ctx context.Context, targetPower int32, essDeviceIds []string) error {
	// 1. 获取绑定的设备列表
	devices := getDevicesByIds(essDeviceIds)

	// 2. 遍历每个设备
	for _, device := range devices {
		// 2.1 类型断言为 IEnergyStore
		essDevice, ok := device.(c_type.IEnergyStore)
		if !ok {
			c_log.Warningf(ctx, "设备 %s 不是储能设备类型，跳过", device.GetConfig().Name)
			continue
		}

		// 2.2 获取当前 SOC
		socPtr, err := essDevice.GetSoc()
		if err != nil {
			c_log.Errorf(ctx, "获取设备 %s SOC 失败: %v", device.GetConfig().Name, err)
			continue
		}

		var soc float64
		if socPtr != nil {
			soc = float64(*socPtr)
		}

		// 2.3 下发目标功率（IPcs.SetPower，正=充 负=放，单位 kW）
		pcs, ok := device.(c_type.IPcs)
		if !ok {
			c_log.Warningf(ctx, "设备 %s 不支持功率控制，跳过", device.GetConfig().Name)
			continue
		}
		if err := pcs.SetPower(targetPower); err != nil {
			c_log.Errorf(ctx, "设备 %s 下发目标功率失败: %v", device.GetConfig().Name, err)
			continue
		}

		// 2.4 日志输出
		c_log.Infof(ctx, "策略执行 - 设备: %s, 当前SOC: %.2f%%, 目标功率: %dkW",
			device.GetConfig().Name, soc, targetPower)
	}

	return nil
}

// getDevicesByIds 根据设备ID列表获取设备实例
func getDevicesByIds(deviceIds []string) []c_base.IDevice {
	devices := make([]c_base.IDevice, 0)
	deviceManager := common.GetDeviceManager()

	for _, deviceId := range deviceIds {
		device := deviceManager.GetDeviceById(deviceId)
		if device != nil {
			devices = append(devices, device)
		}
	}

	return devices
}
