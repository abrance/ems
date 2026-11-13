package reportdaily

import "application/api/reportdaily"

type ControllerV1 struct{}

func NewV1() reportdaily.IReportDailyV1 {
	return &ControllerV1{}
}
