package internal

import (
	"context"
	"strconv"
	"time"

	"common"
	"common/c_base"
	"common/c_chart"

	"github.com/pkg/errors"
)

// loadPointKey 设备实时有功功率点位 Key（负载 / 关口电表均使用 "Power"）。
const loadPointKey = "Power"

// defaultLookbackHours 默认历史回看时长（7 天，覆盖 15 分钟 x 96 点 x 7）。
const defaultLookbackHours = 24 * 7

// LoadHistory 读取设备近 lookbackHours 小时的有功功率历史（时间升序，跳过缺失点）。
func LoadHistory(deviceID string, lookbackHours int) ([]float64, error) {
	if deviceID == "" {
		return nil, errors.New("设备ID为空")
	}
	if lookbackHours <= 0 {
		lookbackHours = defaultLookbackHours
	}
	storage := common.GetStorageInstanceOrNil()
	if storage == nil {
		return nil, errors.New("存储实例未就绪")
	}

	end := time.Now().UnixMilli()
	start := end - int64(lookbackHours)*3600*1000

	chart, err := storage.GetStorageData(c_base.StorageTypeDevice, deviceID, []string{loadPointKey}, &start, &end, 0)
	if err != nil {
		return nil, errors.Wrap(err, "查询设备历史负荷失败")
	}
	return parseChartSeries(chart), nil
}

// parseChartSeries 取图表首个系列解析为浮点序列，跳过空字符串。
func parseChartSeries(chart *c_chart.ChartData) []float64 {
	if chart == nil || len(chart.Series) == 0 || chart.Series[0] == nil {
		return nil
	}
	data := chart.Series[0].Data
	out := make([]float64, 0, len(data))
	for _, s := range data {
		if s == "" {
			continue
		}
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// ForecastSeries 对给定序列做预测，返回首条结果。
func ForecastSeries(ctx context.Context, client IClient, id string, values []float64, horizon int, quantiles []float64) (*SForecastItem, error) {
	if client == nil {
		return nil, errors.New("预测客户端为空")
	}
	if len(values) == 0 {
		return nil, errors.New("预测输入序列为空")
	}
	resp, err := client.Forecast(ctx, &SForecastRequest{
		Series:    []*SSeries{{Id: id, Values: values}},
		Horizon:   horizon,
		Quantiles: quantiles,
	})
	if err != nil {
		return nil, err
	}
	return resp.Forecasts[0], nil
}

// ForecastLoad 读取设备历史负荷并调用预测服务。
func ForecastLoad(ctx context.Context, client IClient, deviceID string, lookbackHours, horizon int, quantiles []float64) (*SForecastItem, error) {
	values, err := LoadHistory(deviceID, lookbackHours)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.Errorf("设备 %s 无历史负荷数据", deviceID)
	}
	return ForecastSeries(ctx, client, deviceID, values, horizon, quantiles)
}
