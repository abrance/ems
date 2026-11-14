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

// gd_spot_metric.origin 的全部取值，与「哪些 origin 算数」的三组口径。
//
// **为什么要提到 model 层写成一处常量**：origin 是复核状态机（见 logic/spotreport/review.go），
// 而它被两类完全不同的查询消费，两类口径必须分开，混用就会得出相反结论：
//
//	SpotOriginsTrusted    = {manual, pm-api}      业务口径：趋势/复盘真值/均价统计只认这两个
//	SpotOriginsUnreviewed = {ocr, cv}             待人工复核，不进业务口径
//	SpotOriginsInventory  = 上面四个 + void       数据面：库里到底有多少行
//
// 踩过的坑（2026-11-14）：用业务口径的白名单去数「库里还剩多少行」，cv 的 453 行档位价
// 全被漏掉，得出「数据丢了」的错误结论（线上 453 行一直在位）。所以：
//
//   - 「数据有没有丢 / 一共多少行 / 覆盖率」→ 只能用 SpotOriginsInventory
//   - 「这条值能不能进聚合」→ 用 SpotOriginsTrusted
//
// 新增 origin 时三组都要过一遍：只加进 Trusted 会让它凭空出现在业务口径里，
// 只加进 Inventory 会让它永久停在待复核。
const (
	SpotOriginOCR    = "ocr"
	SpotOriginCV     = "cv"
	SpotOriginManual = "manual"
	SpotOriginPmAPI  = "pm-api"
	// SpotOriginVoid 已被官方直采值取代的旧值：保留可查，但永远不进业务口径
	SpotOriginVoid = "void"
)

var (
	// SpotOriginsTrusted 业务口径：这些行可以进趋势聚合、复盘真值、日均价统计
	SpotOriginsTrusted = []string{SpotOriginManual, SpotOriginPmAPI}
	// SpotOriginsUnreviewed 待复核：既不进业务口径，也不属于作废
	SpotOriginsUnreviewed = []string{SpotOriginOCR, SpotOriginCV}
	// SpotOriginsInventory 数据面：统计行数/覆盖率时用这个，**不许**用 Trusted 代替
	SpotOriginsInventory = []string{SpotOriginOCR, SpotOriginCV, SpotOriginManual, SpotOriginPmAPI, SpotOriginVoid}
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
