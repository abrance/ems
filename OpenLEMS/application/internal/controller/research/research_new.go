package research

import "application/api/research"

type ControllerV1 struct{}

func NewV1() research.IResearchV1 {
	return &ControllerV1{}
}
