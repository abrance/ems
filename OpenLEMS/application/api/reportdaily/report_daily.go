package reportdaily

import (
	"context"

	v1 "application/api/reportdaily/v1"
)

// IReportDailyV1 日报核对：OCR 日报原值 ↔ 官网直采。
//
// 与 spotreport 分开：那组接口的口径是「一份日报的完整提取 + 人工复核」（写 gd_spot_metric），
// 这组只写独立的 gd_report_daily，并提供只读核对视图，两边不覆盖、不合并。
type IReportDailyV1 interface {
	ExtractReportDaily(ctx context.Context, req *v1.ExtractDayReq) (res *v1.ExtractDayRes, err error)
	BatchExtractReportDaily(ctx context.Context, req *v1.BatchExtractReq) (res *v1.BatchExtractRes, err error)
	GetReportDailyBatchStatus(ctx context.Context, req *v1.BatchStatusReq) (res *v1.BatchStatusRes, err error)
	BackfillReportDaily(ctx context.Context, req *v1.BackfillReq) (res *v1.BackfillRes, err error)
	GetReportDailyReconcile(ctx context.Context, req *v1.ReconcileReq) (res *v1.ReconcileRes, err error)
}
