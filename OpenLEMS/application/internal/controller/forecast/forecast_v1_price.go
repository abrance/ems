package forecast

import (
	v1 "application/api/forecast/v1"
	forecastlogic "application/internal/logic/forecast"
	"context"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetPriceForecast 读取日报已确认的日均成交价并调用 TimesFM 预测未来多日
func (c *ControllerV1) GetPriceForecast(ctx context.Context, req *v1.GetPriceForecastReq) (res *v1.GetPriceForecastRes, err error) {
	quantiles, err := forecastlogic.ParsePriceQuantiles(req.Quantiles)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}

	result, err := forecastlogic.LoadPrice(ctx, forecastlogic.PriceRequest{
		Market:       req.Market,
		HistoryDays:  req.HistoryDays,
		HorizonDays:  req.HorizonDays,
		Quantiles:    quantiles,
		ForceRefresh: req.ForceRefresh,
	})
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "电价预测失败")
	}

	forecast := make([]*v1.PriceForecastPoint, 0, len(result.ForecastPoint))
	for i, point := range result.ForecastPoint {
		date := ""
		if i < len(result.ForecastDates) {
			date = result.ForecastDates[i]
		}
		forecast = append(forecast, &v1.PriceForecastPoint{
			RunDate:   date,
			Point:     point,
			Quantiles: quantileAt(result.ForecastQuantiles, i),
		})
	}

	return &v1.GetPriceForecastRes{Forecast: &v1.PriceForecast{
		Market:         result.Market,
		Dataset:        result.Dataset,
		Scope:          result.Scope,
		Metric:         result.Metric,
		Unit:           result.Unit,
		Model:          result.Model,
		From:           result.From,
		To:             result.To,
		HistoryDates:   result.HistoryDates,
		HistoryValues:  result.HistoryValues,
		Forecast:       forecast,
		ContextPoints:  result.ContextPoints,
		InputTruncated: result.InputTruncated,
		MissingDays:    result.MissingDays,
		TimeMs:         result.TimeMs,
	}}, nil
}

// quantileAt 把「分位键 → 整条序列」转成第 i 点的「分位键 → 值」。
func quantileAt(all map[string][]float64, i int) map[string]float64 {
	if len(all) == 0 {
		return nil
	}
	out := make(map[string]float64, len(all))
	for key, series := range all {
		if i < len(series) {
			out[key] = series[i]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
