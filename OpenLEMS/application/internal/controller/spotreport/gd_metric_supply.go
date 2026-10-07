package spotreport

import (
	v1 "application/api/spotreport/v1"
	"application/internal/logic/gdmetric"
	"context"
)

// GetSupplyCurve 反演市场聚合报价曲线。
//
// 说明（页面上也要写）：这是**统计反演** —— 用观测到的 (负荷, 出清价) 重建市场
// 整体的聚合供给曲线，不是任何一台机组的真实报价。逆优化那一路需要网络模型、
// 出清模型和机组成本，EMS 目前没有。
func (c *ControllerV1) GetSupplyCurve(ctx context.Context, req *v1.GetSupplyCurveReq) (res *v1.GetSupplyCurveRes, err error) {
	kind, err := gdmetric.NormalizeKind(req.Market)
	if err != nil {
		return nil, err
	}
	// 市场只区分日前/实时；传 load 没有意义（负荷是横轴，不是价格）
	if kind == gdmetric.KindLoad {
		kind = gdmetric.KindDayAhead
	}
	curve, err := gdmetric.GetSupplyCurve(ctx, req.From, req.To, kind, gdmetric.SupplyOptions{
		Bins:  req.Bins,
		Steps: req.Steps,
	})
	if err != nil {
		return nil, err
	}
	return &v1.GetSupplyCurveRes{SupplyCurve: curve}, nil
}
