package forecast

import (
	v1 "application/api/forecast/v1"
	forecastlogic "application/internal/logic/forecast"
	"context"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetPriceForecast 电价预测。grain 决定序列源：day=日报日均价，intraday=电网日指标 96 点。
// 每次成功预测都会写留痕（复盘用），写失败只告警、不影响返回。
func (c *ControllerV1) GetPriceForecast(ctx context.Context, req *v1.GetPriceForecastReq) (res *v1.GetPriceForecastRes, err error) {
	quantiles, err := forecastlogic.ParsePriceQuantiles(req.Quantiles)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}

	result, err := forecastlogic.LoadPriceAny(ctx, forecastlogic.PriceRequest{
		Market:       req.Market,
		Grain:        req.Grain,
		HistoryDays:  req.HistoryDays,
		HorizonDays:  req.HorizonDays,
		Quantiles:    quantiles,
		ForceRefresh: req.ForceRefresh,
	})
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "电价预测失败")
	}

	points := make([]*v1.PriceForecastPoint, 0, len(result.Points))
	for _, p := range result.Points {
		target := p.TargetTime
		runDate, atTime := "", ""
		if !target.IsZero() {
			runDate = target.Format("2006-01-02")
			if result.Grain != "day" {
				atTime = target.Format("15:04")
			}
		}
		points = append(points, &v1.PriceForecastPoint{
			RunDate:    runDate,
			TargetTime: forecastlogic.FormatTargetTimeForDTO(result.Grain, target),
			Point:      p.Point,
			Quantiles: map[string]float64{
				"0.1": p.Q10,
				"0.5": p.Q50,
				"0.9": p.Q90,
			},
			AtTime: atTime,
		})
	}

	return &v1.GetPriceForecastRes{Forecast: &v1.PriceForecast{
		Market:         result.Market,
		Grain:          result.Grain,
		Dataset:        result.Dataset,
		Scope:          result.Scope,
		Metric:         result.Metric,
		Unit:           result.Unit,
		Model:          result.Model,
		StepMinutes:    result.StepMinutes,
		From:           result.From,
		To:             result.To,
		HistoryDates:   result.HistoryDates,
		HistoryValues:  result.HistoryValues,
		Forecast:       points,
		ContextPoints:  result.ContextPoints,
		InputChannels:  result.InputChannels,
		InputTruncated: result.InputTruncated,
		MissingDays:    result.MissingDays,
		TimeMs:         result.TimeMs,
		GeneratedOn:    result.GeneratedOn,
		Recorded:       result.Recorded,
	}}, nil
}
