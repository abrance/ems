package research

import (
	"context"

	v1 "application/api/research/v1"
)

// IResearchV1 分时研究（第三方 · 非官方）。
//
// 与 spotreport / forecast 分开：那两组的口径是「官方直采」与「复盘/预测」，
// 这一组的数据源明确是第三方的冻结快照，不进复盘 MAE，也不做任何采集。
type IResearchV1 interface {
	GetIntradayResearch(ctx context.Context, req *v1.GetIntradayResearchReq) (res *v1.GetIntradayResearchRes, err error)
}
