package spotreport

import (
	"context"

	v1 "application/api/spotreport/v1"
)

type ISpotReportV1 interface {
	ListSpotReport(ctx context.Context, req *v1.ListSpotReportReq) (res *v1.ListSpotReportRes, err error)
	GetHomeSpotReport(ctx context.Context, req *v1.GetHomeSpotReportReq) (res *v1.GetHomeSpotReportRes, err error)
	GetSpotReportDetail(ctx context.Context, req *v1.GetSpotReportDetailReq) (res *v1.GetSpotReportDetailRes, err error)
	CollectSpotReport(ctx context.Context, req *v1.CollectSpotReportReq) (res *v1.CollectSpotReportRes, err error)
}
