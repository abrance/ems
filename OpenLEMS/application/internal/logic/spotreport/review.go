package spotreport

import "s_db/s_db_model"

// 复核状态机：不新增列，直接复用 origin。
//
//	ocr / cv  → 未复核（页面显示「待确认」）
//	manual    → 已确认（整篇/单表一键确认，或单值修订过；记录 edited_by）
//	pm-api    → 官方接口直采（2026-11-11 起唯一权威源），不需要人工复核，同样算可信值
//	void      → 已作废（被 pm-api 取代的 OCR 旧值），不算可信值
//
// 两条配套行为（缺一个状态机就漏）：
//  1. ConfirmMetrics 覆盖**所有**未复核行（含 ocr），否则整篇确认后「已确认」无处存放
//  2. ReplaceMetrics 默认保留已确认行（force=false），只有显式 force 才按「以 OCR 为准」覆盖
//
// 与旧行为的差异：OCR 表格值过去是「隐式可信」（只标 cv 为待确认），现在同样进待确认。
// 交叉校验（正文↔表1(二)、跨日报、单调性）仍在解析时产出告警，不因复核状态而变。
//
// 空 origin 视为未复核：老库有过没有该列的行。

// IsConfirmed 该 origin 是否已是可信值（人工确认过，或官方接口直采）。
//
// 为什么 pm-api 也算：趋势/复盘接口用这个函数决定「哪些行能进聚合」。只认 manual
// 会让切换数据源后的官方数据在历史复盘页整片消失 —— 而它恰恰是最权威的那份。
func IsConfirmed(origin string) bool {
	return origin == OriginManual || origin == OriginPmAPI
}

// CountReview 统计未复核/已确认条数
//
// void（已作废）两边都不计：它既不待复核也不可信，只留在库里备查。
func CountReview(metrics []*s_db_model.SSpotMetricModel) (pending, confirmed int) {
	for _, m := range metrics {
		if m.Origin == OriginVoid {
			continue
		}
		if IsConfirmed(m.Origin) {
			confirmed++
		} else {
			pending++
		}
	}
	return pending, confirmed
}
