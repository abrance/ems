package spotreport

import "application/api/spotreport"

type ControllerV1 struct{}

func NewV1() spotreport.ISpotReportV1 {
	return &ControllerV1{}
}
