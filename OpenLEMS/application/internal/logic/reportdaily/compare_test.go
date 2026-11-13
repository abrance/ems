package reportdaily

import (
	"testing"

	v1 "application/api/reportdaily/v1"
	"application/internal/logic/spotreport"
	"s_db/s_db_basic"
	"s_db/s_db_model"
)

func fptr(v float64) *float64 { return &v }

func reportRow(runDate, reportDate, dataset, scope, metric string, value float64) *s_db_model.SReportDailyModel {
	return &s_db_model.SReportDailyModel{
		RunDate: runDate, ReportDate: reportDate, Dataset: dataset, Scope: scope,
		Metric: metric, Value: value, Unit: spotreport.UnitPrice, Origin: OriginOCR,
	}
}

func officialRow(runDate, dataset, metric string, value float64) s_db_basic.ReportDailyOfficialRow {
	return s_db_basic.ReportDailyOfficialRow{
		RunDate: runDate, Dataset: dataset, Scope: spotreport.ScopeSellSide,
		Metric: metric, Value: value, Unit: spotreport.UnitPrice,
	}
}

// TestReportDailyReconcileCompare 三档 compare 的判定 + 缺日/缺官网的 null 语义 + 差值公式。
//
// 口径（用户拍板）：
//   - 只有 2 行真同义（日前/实时 发电侧均价 ↔ 官方加权均价）算差值；
//   - 申报均价是「简单均价 vs 加权均价」，只并排不差值（caliber）；
//   - 分电源与申报结构官网没有，一律 official_missing；
//   - 缺日报 reportValue=null 且 MissingReport=true，**不是 0**。
func TestReportDailyReconcileCompare(t *testing.T) {
	reportRows := []*s_db_model.SReportDailyModel{
		// 11-11 有日报：日前均价 463.27，官网 463.25 → diff=0.02，diffPct=0.00
		reportRow("2026-11-11", "2026-11-11", spotreport.DatasetTrade, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, 463.27),
		// 实时侧：日报 470.1，官网 462.9 → 也是 diff 档（口径真同义），差值要算出来
		reportRow("2026-11-11", "2026-11-11", spotreport.DatasetRealtime, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, 470.1),
		// 燃煤：官网没有 → official_missing
		reportRow("2026-11-11", "2026-11-11", spotreport.DatasetTrade, spotreport.ScopeCoal, spotreport.MetricAvgPrice, 412.0),
		// 申报结构：官网没有 → official_missing
		reportRow("2026-11-11", "2026-11-11", spotreport.DatasetDecl, spotreport.ScopeAbove600, spotreport.MetricDeclVolume, 98000),
		// 申报均价：简单 vs 加权 → caliber，不差值
		reportRow("2026-11-11", "2026-11-11", spotreport.DatasetDecl, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, 420.0),
	}
	officialRows := []s_db_basic.ReportDailyOfficialRow{
		officialRow("2026-11-11", spotreport.DatasetTrade, "发电侧日前加权平均电价", 463.25),
		officialRow("2026-11-11", spotreport.DatasetRealtime, "发电侧实时加权平均电价", 462.9),
		officialRow("2026-11-11", spotreport.DatasetDecl, "日前申报均价", 400.0),
		// 11-12 官网有数、日报没有 → 缺日空态，右列照常
		officialRow("2026-11-12", spotreport.DatasetTrade, "发电侧日前加权平均电价", 470.0),
	}

	days, warnings := BuildReconcile(reportRows, officialRows)
	if len(days) != 2 {
		t.Fatalf("应出 2 天（11-11 有日报 + 11-12 缺日报），实际 %d", len(days))
	}
	if len(warnings) != 0 {
		t.Fatalf("本例无多份日报，不该有 warning：%v", warnings)
	}

	d11 := days[0]
	if d11.RunDate != "2026-11-11" || d11.MissingReport {
		t.Fatalf("11-11 应有日报：%+v", d11)
	}
	if d11.ReportDate != "2026-11-11" {
		t.Fatalf("11-11 reportDate = %q", d11.ReportDate)
	}

	// ---- diff 档：只有日前与实时两行 ----
	diffCount, caliberCount, missingCount := 0, 0, 0
	for _, it := range d11.Items {
		switch it.Compare {
		case CompareDiff:
			diffCount++
		case CompareCaliber:
			caliberCount++
		case CompareOfficialMissing:
			missingCount++
		default:
			t.Fatalf("未知 compare 档：%q", it.Compare)
		}
	}
	if diffCount != 2 {
		t.Fatalf("compare=diff 只应出现在日前/实时两行，实际 %d", diffCount)
	}
	if caliberCount != 1 {
		t.Fatalf("compare=caliber 只应出现在申报均价一行，实际 %d", caliberCount)
	}
	if missingCount != 2 {
		t.Fatalf("compare=official_missing 应出现在燃煤均价与申报容量两行，实际 %d", missingCount)
	}

	// ---- 差值公式：diff = 日报 − 官网；diffPct = diff / 官网 * 100（两位小数）----
	ahead := findItem(t, d11.Items, spotreport.DatasetTrade, spotreport.ScopeSellSide, spotreport.MetricAvgPrice)
	if ahead.Diff == nil || !near(*ahead.Diff, 0.02) {
		t.Fatalf("日前均价 diff = %v，期望 0.02", ahead.Diff)
	}
	if ahead.DiffPct == nil || !near(*ahead.DiffPct, 0.0) {
		t.Fatalf("日前均价 diffPct = %v，期望 0.00", ahead.DiffPct)
	}
	if ahead.Unit != spotreport.UnitPrice {
		t.Fatalf("单位应由后端给：%q", ahead.Unit)
	}

	real := findItem(t, d11.Items, spotreport.DatasetRealtime, spotreport.ScopeSellSide, spotreport.MetricAvgPrice)
	if real.Diff == nil || !near(*real.Diff, 7.2) {
		t.Fatalf("实时均价 diff = %v，期望 7.2", real.Diff)
	}
	if real.DiffPct == nil || !near(*real.DiffPct, 1.56) {
		t.Fatalf("实时均价 diffPct = %v，期望 1.56（7.2/462.9*100，两位小数）", real.DiffPct)
	}

	// ---- caliber 档：不给差值，给口径说明 ----
	decl := findItem(t, d11.Items, spotreport.DatasetDecl, spotreport.ScopeSellSide, spotreport.MetricAvgPrice)
	if decl.Diff != nil || decl.DiffPct != nil {
		t.Fatalf("caliber 档不许算差值：diff=%v diffPct=%v", decl.Diff, decl.DiffPct)
	}
	if decl.Note == "" {
		t.Fatal("caliber 档必须带口径说明")
	}
	if decl.OfficialValue == nil || !near(*decl.OfficialValue, 400.0) {
		t.Fatalf("caliber 档仍要并排显示官网值：%v", decl.OfficialValue)
	}

	// ---- official_missing：官网值为 null（不是 0），且不算差值 ----
	coal := findItem(t, d11.Items, spotreport.DatasetTrade, spotreport.ScopeCoal, spotreport.MetricAvgPrice)
	if coal.OfficialValue != nil {
		t.Fatalf("官网无此口径时 officialValue 必须是 null，实际 %v", *coal.OfficialValue)
	}
	if coal.ReportValue == nil || !near(*coal.ReportValue, 412.0) {
		t.Fatalf("左列日报原值应照常给：%v", coal.ReportValue)
	}
	if coal.Diff != nil || coal.DiffPct != nil {
		t.Fatalf("official_missing 不许算差值")
	}

	// ---- 缺日报日：reportValue=null、MissingReport=true，右列照常 ----
	d12 := days[1]
	if d12.RunDate != "2026-11-12" || !d12.MissingReport {
		t.Fatalf("11-12 应标记缺日报：%+v", d12)
	}
	if d12.ReportDate != "" {
		t.Fatalf("缺日报时 reportDate 应为空串，实际 %q", d12.ReportDate)
	}
	if len(d12.Items) != 1 {
		t.Fatalf("11-12 应只出官网那一行，实际 %d", len(d12.Items))
	}
	only := d12.Items[0]
	if only.ReportValue != nil {
		t.Fatalf("缺日报时 reportValue 必须是 null（不是 0），实际 %v", *only.ReportValue)
	}
	if only.OfficialValue == nil || !near(*only.OfficialValue, 470.0) {
		t.Fatalf("缺日报时右列照常：%v", only.OfficialValue)
	}
	if only.Compare != CompareDiff {
		t.Fatalf("档位不因缺数而变：%q", only.Compare)
	}
	if only.Diff != nil {
		t.Fatalf("只有一边有数时不许算差值（不能拿 0 顶）")
	}
}

// TestReportDailyReconcileEmpty 整段无数据 → days 为空切片，不返回任何 0 行。
func TestReportDailyReconcileEmpty(t *testing.T) {
	days, warnings := BuildReconcile(nil, nil)
	if len(days) != 0 {
		t.Fatalf("无数据时应返回空天列表，实际 %d", len(days))
	}
	if len(warnings) != 0 {
		t.Fatalf("无数据时不该有 warning：%v", warnings)
	}
}

// TestReportDailyReconcileMultiReportWarning 同一运行日多份日报 → warning 且只留一行。
func TestReportDailyReconcileMultiReportWarning(t *testing.T) {
	rows := []*s_db_model.SReportDailyModel{
		// 同一 run_date/dataset/scope/metric 的两份日报（真实库里唯一键会挡住，
		// 这里直接构造，验证 BuildReconcile 自己也不出两行、并且会提示）
		reportRow("2026-09-19", "2026-09-19", spotreport.DatasetTrade, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, 428.95),
		reportRow("2026-09-19", "2026-09-21", spotreport.DatasetTrade, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, 428.95),
	}
	days, warnings := BuildReconcile(rows, nil)
	if len(days) != 1 {
		t.Fatalf("同一运行日应只出一行，实际 %d", len(days))
	}
	if len(days[0].Items) != 1 {
		t.Fatalf("同一键应只出一项，实际 %d", len(days[0].Items))
	}
	if len(warnings) != 1 {
		t.Fatalf("多份日报应给 1 条 warning，实际 %v", warnings)
	}
	if days[0].ReportDate != "2026-09-21" {
		t.Fatalf("reportDate 应取最新那份（09-21），实际 %q", days[0].ReportDate)
	}
}

func findItem(t *testing.T, items []*v1.ReconcileItem, dataset, scope, metric string) *v1.ReconcileItem {
	t.Helper()
	for _, it := range items {
		if it.Dataset == dataset && it.Scope == scope && it.Metric == metric {
			return it
		}
	}
	t.Fatalf("找不到 %s/%s/%s", dataset, scope, metric)
	return nil
}

func near(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-6
}

var _ = fptr
