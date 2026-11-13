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
	// 官网主页两路市场数据：24 点分时 + 21 地市节点价。
	// 必须在这里声明 —— 只写在 ControllerV1 具体类型上时，Bind 靠反射仍能注册路由，
	// 但响应中间件按**接口**处理返回值，拿不到具体类型就写出空 body（HTTP 200 + 空）。
	GetHome24h(ctx context.Context, req *v1.GetHome24hReq) (res *v1.GetHome24hRes, err error)
	GetNodePrice(ctx context.Context, req *v1.GetNodePriceReq) (res *v1.GetNodePriceRes, err error)
	// BackfillHomeMarket 运维手动补洞：按窗口重跑两路回补。
	BackfillHomeMarket(ctx context.Context, req *v1.BackfillHomeMarketReq) (res *v1.BackfillHomeMarketRes, err error)
}
