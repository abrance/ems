package s_db_model

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	TableHome24h           = "gd_home_24h"
	FieldHome24hPriceDate  = "price_date"
	FieldHome24hHour       = "hour"
	FieldHome24hPowerMw    = "power_mw"
	FieldHome24hAheadPrice = "ahead_price"
	FieldHome24hRealtime   = "realtime_price"
	FieldHome24hSource     = "source"
)

// SHome24hModel 官网主页的 24 点分时（日前/实时电价 + 系统负荷）。
//
// **为什么不并进 gd_spot_metric**：粒度与滞后都不同。gd_spot_metric 是披露接口的
// 日频口径（一天一行，实时 T+2 才发布），这里是主页的 24 个整点（T-6 左右才有数）。
// 混进同一张表，任何按 run_date 的区间查询都会把两种粒度串起来，
// as-of 语义（「这天当时能看到什么」）就废了。
//
// 唯一键 (price_date, hour)：同一天同一整点只留一行，重采覆盖不堆积。
type SHome24hModel struct {
	g.Meta        `orm:"table:gd_home_24h"`
	PriceDate     string      `json:"priceDate" orm:"price_date"` // 运行日 YYYY-MM-DD
	Hour          int         `json:"hour" orm:"hour"`            // 0~23（源站 dateTime 的整点）
	PowerMw       float64     `json:"powerMw" orm:"power_mw"`     // 系统负荷 MW
	AheadPrice    float64     `json:"aheadPrice" orm:"ahead_price"`
	RealtimePrice float64     `json:"realtimePrice" orm:"realtime_price"`
	Source        string      `json:"source" orm:"source"`
	UpdatedAt     *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}

const (
	TableNodePrice           = "gd_node_price"
	FieldNodePriceRunDate    = "run_date"
	FieldNodePriceCity       = "city"
	FieldNodePricePeriodType = "period_type"
	FieldNodePricePrice      = "price"
	FieldNodePriceSource     = "source"
	NodePricePeriodPeak      = 1 // 源站 type=1 高峰
	NodePricePeriodValley    = 2 // 源站 type=2 低谷
)

// SNodePriceModel 21 地市的节点电价（高峰/低谷两个时段）。
//
// 口径：源站 model=2 的 currentPirceAreaVOList，21 市 × 2 时段 = 42 条/日，T+0 可得。
// 与披露接口的「节点边际电价」不是一回事：那是按交易时间单元的机组节点，匿名拿不到
// （四个节点全返回 0 条，见 docs/data-source-matrix.md）；这里是地市维度的公开价格。
type SNodePriceModel struct {
	g.Meta     `orm:"table:gd_node_price"`
	RunDate    string      `json:"runDate" orm:"run_date"`
	City       string      `json:"city" orm:"city"`
	PeriodType int         `json:"periodType" orm:"period_type"` // 1 高峰 / 2 低谷
	Price      float64     `json:"price" orm:"price"`
	Source     string      `json:"source" orm:"source"`
	UpdatedAt  *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}
