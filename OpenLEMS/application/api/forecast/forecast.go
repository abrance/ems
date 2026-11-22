// =================================================================================
// Key generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package forecast

import (
	"context"

	v1 "application/api/forecast/v1"
)

type IForecastV1 interface {
	GetLoadForecast(ctx context.Context, req *v1.GetLoadForecastReq) (res *v1.GetLoadForecastRes, err error)
	GetPriceForecast(ctx context.Context, req *v1.GetPriceForecastReq) (res *v1.GetPriceForecastRes, err error)
	GetRecordedForecast(ctx context.Context, req *v1.GetRecordedForecastReq) (res *v1.GetRecordedForecastRes, err error)
	GetPriceBacktest(ctx context.Context, req *v1.GetPriceBacktestReq) (res *v1.GetPriceBacktestRes, err error)
	GetAlgoReview(ctx context.Context, req *v1.GetAlgoReviewReq) (res *v1.GetAlgoReviewRes, err error)
	PostAlgoReviewRun(ctx context.Context, req *v1.PostAlgoReviewRunReq) (res *v1.PostAlgoReviewRunRes, err error)
}
