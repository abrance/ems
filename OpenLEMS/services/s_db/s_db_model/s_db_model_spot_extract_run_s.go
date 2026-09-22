package s_db_model

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	TableSpotExtractRun          = "gd_spot_extract_run"
	FieldSpotExtractReportDate   = "report_date"
	FieldSpotExtractStatus       = "status"
	FieldSpotExtractOcrModel     = "ocr_model"
	FieldSpotExtractPageCount    = "page_count"
	FieldSpotExtractOcrMs        = "ocr_ms"
	FieldSpotExtractWarnings     = "warnings"
	FieldSpotExtractPayload      = "payload"
	FieldSpotExtractExtractorVer = "extractor_version"
)

// 提取状态
const (
	SpotExtractStatusOk      = "ok"      // 全部校验通过
	SpotExtractStatusPartial = "partial" // 提取成功但有告警（OCR 置信度低、交叉校验不一致等）
	SpotExtractStatusFailed  = "failed"  // 未产出任何指标
)

// SSpotExtractRunModel 一次提取的运行记录：可追溯 OCR 模型、耗时、告警与原始产物。
type SSpotExtractRunModel struct {
	g.Meta           `orm:"table:gd_spot_extract_run"`
	Id               int64       `json:"id" orm:"id,primary,auto_increment"`
	ReportDate       string      `json:"reportDate" orm:"report_date"`
	Status           string      `json:"status" orm:"status"`
	OcrModel         string      `json:"ocrModel" orm:"ocr_model"`
	PageCount        int         `json:"pageCount" orm:"page_count"`
	OcrMs            int         `json:"ocrMs" orm:"ocr_ms"`
	Warnings         string      `json:"warnings" orm:"warnings"`
	Payload          string      `json:"payload" orm:"payload"`
	ExtractorVersion string      `json:"extractorVersion" orm:"extractor_version"`
	CreatedAt        *gtime.Time `json:"createdAt" orm:"created_at,created_at"`
	UpdatedAt        *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}
