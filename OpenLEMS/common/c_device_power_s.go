package common

import (
	"strings"

	"common/c_base"
	"common/c_chart"

	"github.com/pkg/errors"
)

// DevicePowerPointKeys 设备「有功功率」点位的候选 Key，按优先级依次尝试。
//
// 为什么需要候选：不同驱动的点位命名不同。2026-10-08 在线上按设备逐个实测（存储里的真实 key）：
//
//	ess_demo / pv_demo / load_demo → "Power"
//	ammeter_demo（关口电表）       → "TotalActivePower"
//
// 而 effect / forecast / s_forecast 三处都写死了 "Power"：把「关口电表设备ID」从储能柜
// 改成真正的关口电表之后，它们读不到任何关口数据 → 效果评估与负荷预测**静默变 0**
// （实测 /api/effect/summary 的 points 从 12775 掉到 0，所有金额都是 0）。
//
// 注意：策略自身不受影响 —— p_policy_mircogrid 走的是设备接口（IStationEntrance / IAmmeter），
// 不是存储 key。这个候选表只解决「按点位 Key 读存储」这条路径。
var DevicePowerPointKeys = []string{"Power", "TotalActivePower", "ActivePower", "TotalPower", "pTotal"}

// LoadDevicePowerSeries 读取设备有功功率历史：按候选 Key 依次查，返回第一个**真正有数据**的图表。
//
// 全部候选都没有数据时返回 (nil, nil) —— 这不是错误，调用方据此走「该区间没有数据」的空态。
func LoadDevicePowerSeries(deviceID string, startMs, endMs int64) (*c_chart.ChartData, error) {
	storage := GetStorageInstanceOrNil()
	if storage == nil {
		return nil, errors.New("存储实例未就绪")
	}
	return pickFirstWithValues(DevicePowerPointKeys, func(key string) (*c_chart.ChartData, error) {
		return storage.GetStorageData(c_base.StorageTypeDevice, deviceID, []string{key}, &startMs, &endMs, 0)
	})
}

// pickFirstWithValues 纯逻辑部分：按顺序试候选 Key，返回第一个有数据的图表。
//
// 单独抽出来是为了能测（不碰存储）：不存在的 Key 不会报错，而是返回**一个空点**
// （实测响应是 `{"series":[{"data":[""]}]}`），所以必须按「有没有非空值」判断，
// 不能按「有没有返回」判断。
func pickFirstWithValues(keys []string, load func(key string) (*c_chart.ChartData, error)) (*c_chart.ChartData, error) {
	var lastErr error
	for _, key := range keys {
		chart, err := load(key)
		if err != nil {
			lastErr = err
			continue
		}
		if chartHasValues(chart) {
			return chart, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

// chartHasValues 图表里是否有至少一个非空数值点。
func chartHasValues(chart *c_chart.ChartData) bool {
	if chart == nil || len(chart.Series) == 0 || chart.Series[0] == nil {
		return false
	}
	for _, v := range chart.Series[0].Data {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}
