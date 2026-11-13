package s_db_model

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	TableReportDaily              = "gd_report_daily"
	FieldReportDailyRunDate       = "run_date"
	FieldReportDailyReportDate    = "report_date"
	FieldReportDailyDataset       = "dataset"
	FieldReportDailyScope         = "scope"
	FieldReportDailyMetric        = "metric"
	FieldReportDailyValue         = "value"
	FieldReportDailyUnit          = "unit"
	FieldReportDailySource        = "source"
	FieldReportDailyOrigin        = "origin"
	FieldReportDailyExtractorVers = "extractor_version"
	FieldReportDailyReportTitle   = "report_title"
	FieldReportDailyCollectedAt   = "collected_at"
	ReportDailyOriginOCR          = "ocr"
	ReportDailyOriginManual       = "manual"
)

// SReportDailyModel 日报核对用的「日报原值」表（OCR 链路 D 的落库目标）。
//
// **为什么不是 gd_spot_metric**：那张表的语义是「一份日报的完整提取 + 人工复核」，
// 唯一键含 report_date，同一运行日会被多份日报各写一行；核对页要与官网 1 天 1 行对齐，
// 所以这里唯一键是 (run_date, scope, metric)，不含 report_date（report_date 只留痕）。
//
// origin 只有 ocr / manual：官方值永远在 gd_spot_metric（origin=pm-api），
// 这张表**没有**官方行，所以核对页的右列另取一路，两边不覆盖、不合并。
// 唯一键是 (run_date, dataset, scope, metric)，不含 report_date（report_date 只留痕）。
//
// 为什么必须带 dataset（与规范原文「不含 report_date」并列的一处补充）：要算差值的两行
// —— 日前均价与实时均价 —— run_date / scope / metric 完全相同（都是「发电侧 / 平均电价」），
// 只有 dataset 不同。唯一键不含 dataset 时它们互相覆盖，核对页永远只能看到一行，
// 差值三档里的 diff 就只剩一半。
type SReportDailyModel struct {
	g.Meta           `orm:"table:gd_report_daily"`
	Id               int64       `json:"id" orm:"id,primary,auto_increment"`
	RunDate          string      `json:"runDate" orm:"run_date"`
	ReportDate       string      `json:"reportDate" orm:"report_date"`
	Dataset          string      `json:"dataset" orm:"dataset"`
	Scope            string      `json:"scope" orm:"scope"`
	Metric           string      `json:"metric" orm:"metric"`
	Value            float64     `json:"value" orm:"value"`
	Unit             string      `json:"unit" orm:"unit"`
	Source           string      `json:"source" orm:"source"`
	Origin           string      `json:"origin" orm:"origin"`
	ExtractorVersion string      `json:"extractorVersion" orm:"extractor_version"`
	ReportTitle      string      `json:"reportTitle" orm:"report_title"`
	CollectedAt      *gtime.Time `json:"collectedAt" orm:"collected_at"`
	CreatedAt        *gtime.Time `json:"createdAt" orm:"created_at,created_at"`
	UpdatedAt        *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}
