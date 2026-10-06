package forecast

import (
	v1 "application/api/forecast/v1"
	forecastlogic "application/internal/logic/forecast"
	"context"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetLoadForecast 读取设备历史负荷并调用 TimesFM 预测
func (c *ControllerV1) GetLoadForecast(ctx context.Context, req *v1.GetLoadForecastReq) (res *v1.GetLoadForecastRes, err error) {
	result, err := forecastlogic.Load(ctx, req.DeviceId, req.LookbackHours, req.HistoryHours, req.Horizon)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "负荷预测失败")
	}

	return &v1.GetLoadForecastRes{Forecast: &v1.LoadForecast{
		DeviceId:          result.DeviceId,
		Model:             result.Model,
		StepMinutes:       result.StepMinutes,
		LookbackPoints:    result.LookbackPoints,
		InputTruncated:    result.InputTruncated,
		HistoryTimes:      result.HistoryTimes,
		HistoryValues:     result.HistoryValues,
		ForecastTimes:     result.ForecastTimes,
		ForecastPoint:     result.ForecastPoint,
		ForecastQuantiles: result.ForecastQuantiles,
		TimeMs:            result.TimeMs,
	}}, nil
}
