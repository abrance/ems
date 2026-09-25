package spotreport

import "s_db/s_db_model"

// 复核状态机：不新增列，直接复用 origin 三态。
//
//	ocr / cv  → 未复核（页面显示「待确认」）
//	manual    → 已确认（整篇/单表一键确认，或单值修订过；记录 edited_by）
//
// 两条配套行为（缺一个状态机就漏）：
//  1. ConfirmMetrics 覆盖**所有**未复核行（含 ocr），否则整篇确认后「已确认」无处存放
//  2. ReplaceMetrics 默认保留已确认行（force=false），只有显式 force 才按「以 OCR 为准」覆盖
//
// 与旧行为的差异：OCR 表格值过去是「隐式可信」（只标 cv 为待确认），现在同样进待确认。
// 交叉校验（正文↔表1(二)、跨日报、单调性）仍在解析时产出告警，不因复核状态而变。
//
// 空 origin 视为未复核：老库有过没有该列的行。

// IsConfirmed 该 origin 是否已人工确认
func IsConfirmed(origin string) bool { return origin == OriginManual }

// CountReview 统计未复核/已确认条数
func CountReview(metrics []*s_db_model.SSpotMetricModel) (pending, confirmed int) {
	for _, m := range metrics {
		if IsConfirmed(m.Origin) {
			confirmed++
		} else {
			pending++
		}
	}
	return pending, confirmed
}
