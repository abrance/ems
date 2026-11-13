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
	FieldNodePriceCityCode   = "city_code"
	FieldNodePriceCity       = "city_name"
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
//
// CityCode 是行政区划编码（GB/T 2260，如 广州市=440100），主键与唯一键都建在它上面；
// City 是源站给的中文名，仅作展示。源站只给中文名、没有编码，编码由 homemarket.CityCodeOf
// 的静态表映射得到 —— 不改名成 CityName 是因为 status.go 等调用方按 City 取展示名。
type SNodePriceModel struct {
	g.Meta     `orm:"table:gd_node_price"`
	RunDate    string      `json:"runDate" orm:"run_date"`
	CityCode   string      `json:"cityCode" orm:"city_code"`
	City       string      `json:"city" orm:"city_name"`
	PeriodType int         `json:"periodType" orm:"period_type"` // 1 高峰 / 2 低谷
	Price      float64     `json:"price" orm:"price"`
	Source     string      `json:"source" orm:"source"`
	UpdatedAt  *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}

const (
	TableFetchRun = "gd_home_market_fetch_run"
	// DatasetHome24h / DatasetNodePrice 两路的 dataset 名（run 表的键之一）
	DatasetHome24h   = "home24h"
	DatasetNodePrice = "nodeprice"
	// FetchStatusOK 有数并已落库 / FetchStatusEmpty 请求成功但 payload 明确为空 / FetchStatusFailed 请求失败
	FetchStatusOK     = "ok"
	FetchStatusEmpty  = "empty"
	FetchStatusFailed = "failed"
)

// SFetchRunModel 一次采集尝试（按 target_date + dataset 覆盖，不追加）。
//
// 与 gd_home_24h / gd_node_price 分开：数据表是「有没有数」，这张表是「为什么没数」。
// 失败不写数据表，只写这里 —— 页面显示「暂无数据」，状态端点显示「采集失败[blocked]」。
//
// ConsecutiveFailures / ConsecutiveEmpties 是**同一 target_date 的连续次数**（不是全局）。
// 24 点每轮会探 14 个日期各一次，全局计数在这种探法下没有意义。
type SFetchRunModel struct {
	g.Meta              `orm:"table:gd_home_market_fetch_run"`
	ID                  int64       `json:"id" orm:"id"`
	TargetDate          string      `json:"targetDate" orm:"target_date"`
	Dataset             string      `json:"dataset" orm:"dataset"`
	Status              string      `json:"status" orm:"status"`
	ErrorKind           string      `json:"errorKind" orm:"error_kind"`
	RowsWritten         int         `json:"rowsWritten" orm:"rows_written"`
	RowsExpected        int         `json:"rowsExpected" orm:"rows_expected"`
	AttemptCount        int         `json:"attemptCount" orm:"attempt_count"`
	ConsecutiveFailures int         `json:"consecutiveFailures" orm:"consecutive_failures"`
	ConsecutiveEmpties  int         `json:"consecutiveEmpties" orm:"consecutive_empties"`
	LastError           string      `json:"lastError" orm:"last_error"`
	FirstSeenAt         *gtime.Time `json:"firstSeenAt" orm:"first_seen_at"`
	LastOkAt            *gtime.Time `json:"lastOkAt" orm:"last_ok_at"`
	UpdatedAt           *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}
