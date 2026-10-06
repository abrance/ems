package v1

import "github.com/gogf/gf/v2/frame/g"

// GetLoadForecastReq 负荷预测请求
type GetLoadForecastReq struct {
	g.Meta        `path:"/forecast/load" method:"get" tags:"负荷预测" summary:"设备负荷预测（TimesFM）" role:"user"`
	DeviceId      string `json:"deviceId" dc:"设备ID，缺省取策略配置的关口电表"`
	LookbackHours int    `json:"lookbackHours" dc:"送入模型的历史小时数，默认 168"`
	HistoryHours  int    `json:"historyHours" dc:"返回展示的历史小时数，默认 24"`
	Horizon       int    `json:"horizon" dc:"预测步数，缺省取策略配置（默认 4）"`
}

// LoadForecast 负荷预测结果
type LoadForecast struct {
	DeviceId          string               `json:"deviceId" dc:"设备ID"`
	Model             string               `json:"model" dc:"预测模型"`
	StepMinutes       float64              `json:"stepMinutes" dc:"采样步长（分钟）"`
	LookbackPoints    int                  `json:"lookbackPoints" dc:"送入模型的点数"`
	InputTruncated    bool                 `json:"inputTruncated" dc:"历史是否被截断至模型上限"`
	HistoryTimes      []int64              `json:"historyTimes" dc:"历史时间戳（毫秒）"`
	HistoryValues     []float64            `json:"historyValues" dc:"历史功率 kW"`
	ForecastTimes     []int64              `json:"forecastTimes" dc:"预测时间戳（毫秒）"`
	ForecastPoint     []float64            `json:"forecastPoint" dc:"点预测 kW"`
	ForecastQuantiles map[string][]float64 `json:"forecastQuantiles" dc:"分位预测（键为分位字符串）"`
	TimeMs            float64              `json:"timeMs" dc:"预测耗时（毫秒）"`
}

// GetLoadForecastRes 负荷预测响应
type GetLoadForecastRes struct {
	Forecast *LoadForecast `json:"forecast" dc:"预测结果"`
}
