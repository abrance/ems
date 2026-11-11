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
}
