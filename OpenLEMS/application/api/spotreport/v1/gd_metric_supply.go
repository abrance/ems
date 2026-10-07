package v1

import (
	"application/internal/logic/gdmetric"

	"github.com/gogf/gf/v2/frame/g"
)

// GetSupplyCurveReq 反演市场聚合报价曲线
type GetSupplyCurveReq struct {
	g.Meta `path:"/gd-metric/supply-curve" method:"get" tags:"电网日指标" summary:"反演市场聚合报价曲线（负荷-出清价）" role:"user"`
	From   string `json:"from" v:"required" dc:"起始日期 YYYY-MM-DD"`
	To     string `json:"to" v:"required" dc:"结束日期 YYYY-MM-DD"`
	Market string `json:"market" dc:"ahead（默认）/ real；决定用日前还是实时出清价"`
	Bins   int    `json:"bins" dc:"负荷分箱数，默认 40"`
	Steps  int    `json:"steps" dc:"报价段数，默认 10"`
}

// GetSupplyCurveRes 反演结果
type GetSupplyCurveRes struct {
	*gdmetric.SupplyCurve
}
