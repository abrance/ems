package forecast

import "application/api/forecast"

type ControllerV1 struct{}

func NewV1() forecast.IForecastV1 {
	return &ControllerV1{}
}
