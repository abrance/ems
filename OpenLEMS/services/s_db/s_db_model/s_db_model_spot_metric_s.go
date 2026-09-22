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
	FieldSpotMetricTextValue     = "text_value"
	FieldSpotMetricMomPct        = "mom_pct"
	FieldSpotMetricSharePct      = "share_pct"
	FieldSpotMetricSource        = "source"
	FieldSpotMetricConf          = "conf"
	FieldSpotMetricOrigin        = "origin"
	FieldSpotMetricEditedBy      = "edited_by"
	FieldSpotMetricEditedAt      = "edited_at"
	FieldSpotMetricExtractorVers = "extractor_version"
)

// SSpotMetricModel 现货日报结构化指标。
//
// 指标归属「产出它的那份日报」，唯一键为 (report_date, run_date, dataset, scope, metric)。
// report_date 与 run_date 必须分开：日报里「表2」承载的是滞后运行日（T-2）的
// 日前/实时成交电价。同一运行日的同一指标会由多份日报产出（例：09-19 的日前成交电价
// 既来自 09-19 日报的表1(二)，也来自 09-21 日报的表2(一)），两份日报各自成行、
// 数值由交叉校验保证一致；按运行日聚合时优先取 source=表1(二) 的行。
type SSpotMetricModel struct {
	g.Meta     `orm:"table:gd_spot_metric"`
	Id         int64   `json:"id" orm:"id,primary,auto_increment"`
	ReportDate string  `json:"reportDate" orm:"report_date"`
	RunDate    string  `json:"runDate" orm:"run_date"`
	Dataset    string  `json:"dataset" orm:"dataset"`
	Scope      string  `json:"scope" orm:"scope"`
	Metric     string  `json:"metric" orm:"metric"`
	Value      float64 `json:"value" orm:"value"`
	Unit       string  `json:"unit" orm:"unit"`
	AtTime     string  `json:"atTime" orm:"at_time"`
	// TextValue 非数值载荷：地市电价档位区间（如 [800,+inf)）
	TextValue string   `json:"textValue" orm:"text_value"`
	MomPct    *float64 `json:"momPct" orm:"mom_pct"`
	SharePct  *float64 `json:"sharePct" orm:"share_pct"`
	Source    string   `json:"source" orm:"source"`
	Conf      float64  `json:"conf" orm:"conf"`
	// Origin 取值 ocr / manual：manual 表示该值由人工修订过（重跑解析会以 OCR 为准并先写审计）
	Origin           string      `json:"origin" orm:"origin"`
	EditedBy         string      `json:"editedBy" orm:"edited_by"`
	EditedAt         *gtime.Time `json:"editedAt" orm:"edited_at"`
	ExtractorVersion string      `json:"extractorVersion" orm:"extractor_version"`
	CreatedAt        *gtime.Time `json:"createdAt" orm:"created_at,created_at"`
	UpdatedAt        *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}
