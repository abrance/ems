package s_db_model

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	TableSpotMetric              = "gd_spot_metric"
	FieldSpotMetricReportDate    = "report_date"
	FieldSpotMetricRunDate       = "run_date"
	FieldSpotMetricDataset       = "dataset"
	FieldSpotMetricScope         = "scope"
	FieldSpotMetricMetric        = "metric"
	FieldSpotMetricValue         = "value"
	FieldSpotMetricUnit          = "unit"
	FieldSpotMetricAtTime        = "at_time"
	FieldSpotMetricMomPct        = "mom_pct"
	FieldSpotMetricSharePct      = "share_pct"
	FieldSpotMetricSource        = "source"
	FieldSpotMetricConf          = "conf"
	FieldSpotMetricExtractorVers = "extractor_version"
)

// SSpotMetricModel 现货日报结构化指标。
//
// 一个 (运行日, 数据集, 对象, 指标) 一行。report_date 与 run_date 必须分开：
// 日报里「表2」承载的是滞后运行日（T-2）的日前/实时成交电价，同一运行日的
// 同一指标无论来自哪一天的日报都应落到同一行。
type SSpotMetricModel struct {
	g.Meta           `orm:"table:gd_spot_metric"`
	Id               int64       `json:"id" orm:"id,primary,auto_increment"`
	ReportDate       string      `json:"reportDate" orm:"report_date"`
	RunDate          string      `json:"runDate" orm:"run_date"`
	Dataset          string      `json:"dataset" orm:"dataset"`
	Scope            string      `json:"scope" orm:"scope"`
	Metric           string      `json:"metric" orm:"metric"`
	Value            float64     `json:"value" orm:"value"`
	Unit             string      `json:"unit" orm:"unit"`
	AtTime           string      `json:"atTime" orm:"at_time"`
	MomPct           *float64    `json:"momPct" orm:"mom_pct"`
	SharePct         *float64    `json:"sharePct" orm:"share_pct"`
	Source           string      `json:"source" orm:"source"`
	Conf             float64     `json:"conf" orm:"conf"`
	ExtractorVersion string      `json:"extractorVersion" orm:"extractor_version"`
	CreatedAt        *gtime.Time `json:"createdAt" orm:"created_at,created_at"`
	UpdatedAt        *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}
