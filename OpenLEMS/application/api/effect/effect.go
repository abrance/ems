// =================================================================================
// Key generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package effect

import (
	"context"

	v1 "application/api/effect/v1"
)

type IEffectV1 interface {
	GetEffectSummary(ctx context.Context, req *v1.GetEffectSummaryReq) (res *v1.GetEffectSummaryRes, err error)
	GetEffectBacktest(ctx context.Context, req *v1.GetEffectBacktestReq) (res *v1.GetEffectBacktestRes, err error)
}
