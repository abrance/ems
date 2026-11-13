package reportdaily

import (
	"math"
	"sort"

	v1 "application/api/reportdaily/v1"
	"application/internal/logic/spotreport"
	"s_db/s_db_basic"
	"s_db/s_db_model"
)

// compare 三档（后端定，前端不判）
const (
	CompareDiff            = "diff"             // 真同义，算差值
	CompareCaliber         = "caliber"          // 口径不同，只并排不差值
	CompareOfficialMissing = "official_missing" // 官网无此口径
)

// diffPair 真同义的一对：日报键 ↔ 官方指标原名。
//
// 直接抄 tools/gdpm/gdpm_sqlite.py 的 METRIC_MAP（已用生产库实测差 < 0.01），
// 不在这里另立第三套口径。改动前先看那边。
type diffPair struct {
	Dataset  string // 日报侧 dataset
	Scope    string
	Metric   string
	Official string // 官方指标原名
}

var diffPairs = []diffPair{
	{spotreport.DatasetTrade, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, "发电侧日前加权平均电价"},
	{spotreport.DatasetRealtime, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, "发电侧实时加权平均电价"},
}

// caliberPair 口径不同（同名不同义/简单均价 vs 加权均价），只并排不差值。
// 同样来自 gdpm_sqlite.py 的 METRIC_NOTE。
type caliberPair struct {
	Dataset  string
	Scope    string
	Metric   string
	Official string
	Note     string
}

var caliberPairs = []caliberPair{
	{spotreport.DatasetDecl, spotreport.ScopeSellSide, spotreport.MetricAvgPrice, "日前申报均价", "简单均价 vs 加权均价"},
}

// 官方还有「发电侧日前加权最高/最低电价」，与日报的最高/最低电价同名不同义
// （限价维度 vs 加权价，见 gdpm_sqlite.py 的 METRIC_NOTE）。但日报的最高/最低电价
// **不进** gd_report_daily（口径：本表只存分电源均价 + 申报结构），左列没有值，
// 并排出来只是一列孤立的官方数字，所以这一页不列它们。

// itemKey 核对页的一行（日报侧的键）
type itemKey struct {
	Dataset string
	Scope   string
	Metric  string
}

var datasetOrder = map[string]int{
	spotreport.DatasetTrade:    0,
	spotreport.DatasetRealtime: 1,
	spotreport.DatasetDecl:     2,
}

var scopeOrder = map[string]int{
	spotreport.ScopeSellSide:  0,
	spotreport.ScopeCoal:      1,
	spotreport.ScopeGas:       2,
	spotreport.ScopeNewEnergy: 3,
	spotreport.ScopeZero:      4,
	spotreport.ScopeAbove600:  5,
}

var metricOrder = map[string]int{
	spotreport.MetricAvgPrice:   0,
	spotreport.MetricDeclVolume: 1,
	spotreport.MetricCoalCap:    2,
	spotreport.MetricGasCap:     3,
	spotreport.MetricNewCap:     4,
	spotreport.MetricSelfDecl:   5,
	spotreport.MetricDefault:    6,
}

// BuildReconcile 把左列（日报原值）与右列（官方直采）拼成核对结果。
//
// 纯函数：不读库、不算 MAE、不写任何东西。差值只在 diffPairs 的 2 行上算，
// 前端拿到的 compare 已经是最终判定，不再自己算。
//
// 空态语义：reportValue=null ⇔ 该 runDate 无日报（MissingReport=true）；
// officialValue=null ⇔ 官网该日无此指标。任何情况下都不填 0。
func BuildReconcile(reportRows []*s_db_model.SReportDailyModel, officialRows []s_db_basic.ReportDailyOfficialRow) ([]v1.ReconcileDay, []string) {
	warnings := make([]string, 0, 4)

	// 官方行按 (dataset|metric) 建索引：核对页要的是「这天这个指标的官方值」。
	officialByDay := map[string]map[itemKey]float64{}
	officialUnit := map[itemKey]string{}
	officialDays := map[string]bool{}
	for _, row := range officialRows {
		key, ok := mapOfficialToReportKey(row.Dataset, row.Metric)
		if !ok {
			// 官方有、但核对页不收的指标（加权最高/最低价）：丢弃，不生成行。
			continue
		}
		if officialByDay[row.RunDate] == nil {
			officialByDay[row.RunDate] = map[itemKey]float64{}
		}
		officialByDay[row.RunDate][key] = row.Value
		officialUnit[key] = row.Unit
		officialDays[row.RunDate] = true
	}

	// 左列按 run_date 分组；同一天同键只应有一行（唯一键保证），
	// 但同一 run_date 可能来自不同 report_date，交叉校验要提示。
	reportByDay := map[string]map[itemKey]*s_db_model.SReportDailyModel{}
	reportDates := map[string]map[string]bool{}
	daySet := map[string]bool{}
	for _, row := range reportRows {
		if row == nil {
			continue
		}
		key := itemKey{Dataset: row.Dataset, Scope: row.Scope, Metric: row.Metric}
		if reportByDay[row.RunDate] == nil {
			reportByDay[row.RunDate] = map[itemKey]*s_db_model.SReportDailyModel{}
		}
		reportByDay[row.RunDate][key] = row
		if reportDates[row.RunDate] == nil {
			reportDates[row.RunDate] = map[string]bool{}
		}
		reportDates[row.RunDate][row.ReportDate] = true
		daySet[row.RunDate] = true
	}
	for day := range officialDays {
		daySet[day] = true
	}

	days := make([]string, 0, len(daySet))
	for day := range daySet {
		days = append(days, day)
	}
	sort.Strings(days)

	out := make([]v1.ReconcileDay, 0, len(days))
	for _, day := range days {
		items := buildDayItems(day, reportByDay[day], officialByDay[day], officialUnit)
		day_ := v1.ReconcileDay{
			RunDate:       day,
			MissingReport: len(reportByDay[day]) == 0,
			Items:         items,
		}
		if !day_.MissingReport {
			dates := make([]string, 0, len(reportDates[day]))
			for d := range reportDates[day] {
				dates = append(dates, d)
			}
			sort.Strings(dates)
			day_.ReportDate = dates[len(dates)-1]
			if len(dates) > 1 {
				warnings = append(warnings, "运行日 "+day+" 的日报原值来自多份日报（"+
					joinStrings(dates, "、")+"），已按「报告日最接近运行日」只留一行")
			}
		}
		out = append(out, day_)
	}
	return out, warnings
}

// buildDayItems 生成一天的行：左列已有的行全部保留（含官网无对应口径的），
// 官网有、左列没有的行（缺日报日）也要出一行，让右列照常显示。
func buildDayItems(_ string, report map[itemKey]*s_db_model.SReportDailyModel,
	official map[itemKey]float64, officialUnit map[itemKey]string) []*v1.ReconcileItem {
	keys := map[itemKey]bool{}
	for k := range report {
		keys[k] = true
	}
	for k := range official {
		keys[k] = true
	}
	ordered := make([]itemKey, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if datasetOrder[a.Dataset] != datasetOrder[b.Dataset] {
			return datasetOrder[a.Dataset] < datasetOrder[b.Dataset]
		}
		if scopeOrder[a.Scope] != scopeOrder[b.Scope] {
			return scopeOrder[a.Scope] < scopeOrder[b.Scope]
		}
		if metricOrder[a.Metric] != metricOrder[b.Metric] {
			return metricOrder[a.Metric] < metricOrder[b.Metric]
		}
		return a.Metric < b.Metric
	})

	items := make([]*v1.ReconcileItem, 0, len(ordered))
	for _, k := range ordered {
		item := &v1.ReconcileItem{Dataset: k.Dataset, Scope: k.Scope, Metric: k.Metric}
		if row := report[k]; row != nil {
			value := row.Value
			item.ReportValue = &value
			item.Unit = row.Unit
			item.Source = row.Source
		}
		if officialValue, ok := official[k]; ok {
			value := officialValue
			item.OfficialValue = &value
			if item.Unit == "" {
				item.Unit = officialUnit[k]
			}
		}
		classify(item, k)
		items = append(items, item)
	}
	return items
}

// classify 定 compare 档并算差值（仅 diff 档）。
func classify(item *v1.ReconcileItem, k itemKey) {
	if note, ok := caliberNote(k); ok {
		item.Compare = CompareCaliber
		item.Note = note
		return
	}
	if isDiffPair(k) {
		item.Compare = CompareDiff
		if item.ReportValue == nil || item.OfficialValue == nil {
			// 有一边缺数：档位仍是 diff（说明「本来可比」），但不算差值 —— 不填 0。
			return
		}
		diff := *item.ReportValue - *item.OfficialValue
		item.Diff = &diff
		if *item.OfficialValue != 0 {
			pct := round2(diff / *item.OfficialValue * 100)
			item.DiffPct = &pct
		}
		return
	}
	item.Compare = CompareOfficialMissing
}

func caliberNote(k itemKey) (string, bool) {
	for _, p := range caliberPairs {
		if p.Dataset == k.Dataset && p.Scope == k.Scope && p.Metric == k.Metric {
			return p.Note, true
		}
	}
	return "", false
}

func isDiffPair(k itemKey) bool {
	for _, p := range diffPairs {
		if p.Dataset == k.Dataset && p.Scope == k.Scope && p.Metric == k.Metric {
			return true
		}
	}
	return false
}

// mapOfficialToReportKey 官方指标 → 核对页的日报侧键。只有 METRIC_MAP 与 METRIC_NOTE
// 里的指标能对上；其余（官方加权最高/最低）返回 false，不进核对页。
func mapOfficialToReportKey(dataset, officialMetric string) (itemKey, bool) {
	for _, p := range diffPairs {
		if p.Official == officialMetric && dataset == p.Dataset {
			return itemKey{Dataset: p.Dataset, Scope: p.Scope, Metric: p.Metric}, true
		}
	}
	for _, p := range caliberPairs {
		if p.Official == officialMetric {
			return itemKey{Dataset: p.Dataset, Scope: p.Scope, Metric: p.Metric}, true
		}
	}
	return itemKey{}, false
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func joinStrings(in []string, sep string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
