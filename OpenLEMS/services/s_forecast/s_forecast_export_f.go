package s_forecast

import (
	"context"
	"time"

	"s_forecast/internal"
)

// 导出内部类型
type SSeries = internal.SSeries
type SForecastRequest = internal.SForecastRequest
type SForecastItem = internal.SForecastItem
type SForecastResponse = internal.SForecastResponse
type IClient = internal.IClient

// NewClient 创建 TimesFM 预测服务客户端
func NewClient(baseURL string, timeout time.Duration) IClient {
	return internal.NewClient(baseURL, timeout)
}

// ForecastSeries 对给定序列做预测
func ForecastSeries(ctx context.Context, client IClient, id string, values []float64, horizon int, quantiles []float64) (*SForecastItem, error) {
	return internal.ForecastSeries(ctx, client, id, values, horizon, quantiles)
}

// ForecastLoad 读取设备历史负荷并预测
func ForecastLoad(ctx context.Context, client IClient, deviceID string, lookbackHours, horizon int, quantiles []float64) (*SForecastItem, error) {
	return internal.ForecastLoad(ctx, client, deviceID, lookbackHours, horizon, quantiles)
}

// LoadHistory 读取设备历史负荷序列
func LoadHistory(deviceID string, lookbackHours int) ([]float64, error) {
	return internal.LoadHistory(deviceID, lookbackHours)
}
