package s_db_model

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	TableSpotMetricAudit       = "gd_spot_metric_audit"
	FieldSpotMetricAuditReport = "report_date"
	FieldSpotMetricAuditOp     = "operation"
	FieldSpotMetricAuditOperBy = "operator"
)

// 审计操作类型
const (
	SpotMetricAuditEdit      = "edit"                   // 人工修订
	SpotMetricAuditOverwrite = "overwritten_by_extract" // 重跑解析覆盖了人工值
	SpotMetricAuditConfirm   = "confirm"                // 人工确认了采样/OCR 预填值
	// SpotMetricAuditDeprecate 该行被官方 API 直采值取代：保留可查，不再作为权威值。
	SpotMetricAuditDeprecate = "deprecated_by_pm_api"
)

// SSpotMetricAuditModel 指标变更审计。人工修订与「被解析覆盖」都留痕，
// 因此即使重跑以 OCR 为准，人工判断也不会无声消失。
type SSpotMetricAuditModel struct {
	g.Meta     `orm:"table:gd_spot_metric_audit"`
	Id         int64       `json:"id" orm:"id,primary,auto_increment"`
	ReportDate string      `json:"reportDate" orm:"report_date"`
	RunDate    string      `json:"runDate" orm:"run_date"`
	Dataset    string      `json:"dataset" orm:"dataset"`
	Scope      string      `json:"scope" orm:"scope"`
	Metric     string      `json:"metric" orm:"metric"`
	OldValue   *float64    `json:"oldValue" orm:"old_value"`
	NewValue   *float64    `json:"newValue" orm:"new_value"`
	OldOrigin  string      `json:"oldOrigin" orm:"old_origin"`
	Operation  string      `json:"operation" orm:"operation"`
	Operator   string      `json:"operator" orm:"operator"`
	CreatedAt  *gtime.Time `json:"createdAt" orm:"created_at,created_at"`
	// CreatedAtText created_at 的原始文本（查审计时用 CAST 另取一列）。
	// 库里有两种写法：ORM 与老 OCR 流水写的是带偏移的 "+00:00"，pm-api 导入脚本写的是
	// 不带时区的本地墙钟（"2026-11-11 16:27:21"，Asia/Shanghai）。后者会被 sqlite 驱动按
	// UTC 解析，再换算成北京时间就多 8 小时；只有原始文本能区分这两种，故一并读出来。
	CreatedAtText string `json:"-" orm:"created_at_text"`
}
