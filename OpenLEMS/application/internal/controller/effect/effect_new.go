package effect

import "application/api/effect"

type ControllerV1 struct{}

func NewV1() effect.IEffectV1 {
	return &ControllerV1{}
}
