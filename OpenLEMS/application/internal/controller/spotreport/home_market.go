package spotreport

import (
	"context"

	v1 "application/api/spotreport/v1"
	"application/internal/logic/homemarket"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetHome24h 官网主页 24 点分时（电价 + 负荷）。
//
// 与 /api/gd-metric/series（96 点第三方通道，已停更）和 /api/spot-market/board
// （披露接口日频）都不是一路：这是主页的 24 个整点，滞后 6 天左右，独立表 gd_home_24h。
func (c *ControllerV1) GetHome24h(ctx context.Context, req *v1.GetHome24hReq) (*v1.GetHome24hRes, error) {
	day, err := homemarket.LoadHome24h(ctx, req.Date)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取 24 点分时失败")
	}
	return &v1.GetHome24hRes{Home24hDay: day}, nil
}

// GetNodePrice 21 地市节点电价（高峰/低谷各 21 条）。
func (c *ControllerV1) GetNodePrice(ctx context.Context, req *v1.GetNodePriceReq) (*v1.GetNodePriceRes, error) {
	day, err := homemarket.LoadNodePrice(ctx, req.Date)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取地市节点价失败")
	}
	return &v1.GetNodePriceRes{NodePriceDay: day}, nil
}
