package spotreport

import (
	"context"

	v1 "application/api/spotreport/v1"
	"application/internal/logic/spotmarket"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetSpotMarketBoard 广东电力市场官方指标快照（origin=pm-api）。
//
// 与 /spot-report/* 分开：那组接口的口径是「一份日报的 OCR 提取 + 人工复核」，
// 这组是官方接口直采的时序数据，没有日报也没有复核状态。
func (c *ControllerV1) GetSpotMarketLatest(ctx context.Context, req *v1.GetSpotMarketLatestReq) (*v1.GetSpotMarketLatestRes, error) {
	date, err := spotmarket.LatestDate(ctx)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取电力市场最新运行日失败")
	}
	return &v1.GetSpotMarketLatestRes{Date: date}, nil
}

func (c *ControllerV1) GetSpotMarketBoard(ctx context.Context, req *v1.GetSpotMarketBoardReq) (*v1.GetSpotMarketBoardRes, error) {
	board, err := spotmarket.LoadBoard(ctx, req.Date, req.Days)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取电力市场指标失败")
	}
	return &v1.GetSpotMarketBoardRes{Board: board}, nil
}
