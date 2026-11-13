package reportdaily

import (
	"context"

	v1 "application/api/reportdaily/v1"
	logic "application/internal/logic/reportdaily"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// ControllerV1 日报核对。
//
// 这组接口只碰 gd_report_daily 与只读的官方行；旧链路 /api/spot-report/extract
// 仍写 gd_spot_metric，两者互不影响（口径见 logic/reportdaily 包注释）。
func (c *ControllerV1) ExtractReportDaily(ctx context.Context, req *v1.ExtractDayReq) (*v1.ExtractDayRes, error) {
	res, err := logic.ExtractDay(ctx, req.Date, req.Force)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "解析日报失败")
	}
	return res, nil
}

func (c *ControllerV1) BatchExtractReportDaily(ctx context.Context, req *v1.BatchExtractReq) (*v1.BatchExtractRes, error) {
	res, err := logic.StartBatch(ctx, req.From, req.To, req.IncludeManual, req.DryRun)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "批量解析失败")
	}
	return res, nil
}

func (c *ControllerV1) GetReportDailyBatchStatus(ctx context.Context, req *v1.BatchStatusReq) (*v1.BatchStatusRes, error) {
	res, err := logic.GetBatchStatus(req.Id)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "查询批量解析进度失败")
	}
	return res, nil
}

func (c *ControllerV1) BackfillReportDaily(ctx context.Context, req *v1.BackfillReq) (*v1.BackfillRes, error) {
	res, err := logic.Backfill(ctx)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "回填日报原值失败")
	}
	return res, nil
}

func (c *ControllerV1) GetReportDailyReconcile(ctx context.Context, req *v1.ReconcileReq) (*v1.ReconcileRes, error) {
	res, err := logic.LoadReconcile(ctx, req.From, req.To)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInvalidParameter, err, "获取核对数据失败")
	}
	return res, nil
}
