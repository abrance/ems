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
}
