package reportdaily

import (
	"encoding/json"
	"os"
	"testing"

	"application/internal/logic/spotreport"
)

// fixture 与 spotreport 的回归用例同源（真实日报的 OCR 结果），
// 这样「D 子集」的判定是拿真数据验的，不是手写几个 Metric 自证。
type fixture struct {
	Lines []struct {
		Text       string  `json:"t"`
		Confidence float64 `json:"c"`
		X          float64 `json:"x"`
		Y          float64 `json:"y"`
		W          float64 `json:"w"`
		H          float64 `json:"h"`
	} `json:"lines"`
}

func loadPage(t *testing.T, name string, width, height float64) spotreport.PageInput {
	t.Helper()
	raw, err := os.ReadFile("../spotreport/fixtures/" + name)
	if err != nil {
		t.Fatalf("读取 fixture %s 失败：%v", name, err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("解析 fixture %s 失败：%v", name, err)
	}
	page := spotreport.PageInput{Width: width, Height: height}
	for _, l := range f.Lines {
		var line spotreport.OCRLine
		line.Text = l.Text
		line.Confidence = l.Confidence
		line.BBox.Left = l.X
		line.BBox.Top = l.Y
		line.BBox.Width = l.W
		line.BBox.Height = l.H
		page.Lines = append(page.Lines, line)
	}
	return page
}

// TestReportDailyExtractSubset 用真实日报 fixture 验 D 子集：
//   - 只出「分电源均价」+「申报结构」；
//   - 最高/最低电价、电价环比、summary 正文、节点价档位一行都不许进；
//   - 单位只能是 元/兆瓦时 / MW / 台。
func TestReportDailyExtractSubset(t *testing.T) {
	pages := []spotreport.PageInput{
		loadPage(t, "2026-09-21-page1.json", 2381, 3367),
		loadPage(t, "2026-09-21-page2.json", 2381, 3367),
		loadPage(t, "2026-09-21-page3.json", 2381, 3367),
		loadPage(t, "2026-09-21-page4.json", 2381, 3367),
	}
	extracted, err := spotreport.ExtractFromPages("2026-09-21", pages)
	if err != nil {
		t.Fatalf("模板提取失败：%v", err)
	}
	if len(extracted.Metrics) == 0 {
		t.Fatal("fixture 提取不到任何指标，用例已失效")
	}

	subset := SubsetMetrics(extracted.Metrics)
	if len(subset) == 0 {
		t.Fatal("D 子集为空：模板提取或子集过滤有问题")
	}

	allowedUnit := map[string]bool{
		spotreport.UnitPrice: true,
		spotreport.UnitMW:    true,
		spotreport.UnitCount: true,
	}
	allowedMetric := map[string]bool{
		spotreport.MetricAvgPrice:   true,
		spotreport.MetricDeclVolume: true,
		spotreport.MetricCoalCap:    true,
		spotreport.MetricGasCap:     true,
		spotreport.MetricNewCap:     true,
		spotreport.MetricSelfDecl:   true,
		spotreport.MetricDefault:    true,
	}
	allowedDataset := map[string]bool{
		spotreport.DatasetTrade:    true,
		spotreport.DatasetRealtime: true,
		spotreport.DatasetDecl:     true,
	}

	for _, m := range subset {
		if !allowedDataset[m.Dataset] {
			t.Errorf("dataset %q 不该进 D 子集（%s/%s）", m.Dataset, m.Scope, m.Metric)
		}
		if !allowedMetric[m.Metric] {
			t.Errorf("指标 %q 不该进 D 子集（最高/最低/环比/summary/节点价都不收）", m.Metric)
		}
		if !allowedUnit[m.Unit] {
			t.Errorf("单位 %q 不在 元/兆瓦时|MW|台 之内（%s/%s）", m.Unit, m.Scope, m.Metric)
		}
		if m.Dataset == spotreport.DatasetDecl && m.Metric == spotreport.MetricAvgPrice {
			t.Errorf("申报表的「平均电价」不进 D 子集（核对页只收申报结构，不重算申报均价）")
		}
	}

	// 三个必须出现的行：日前发电侧均价、实时发电侧均价、申报结构里的申报容量。
	want := []struct {
		dataset, scope, metric string
	}{
		{spotreport.DatasetTrade, spotreport.ScopeSellSide, spotreport.MetricAvgPrice},
		{spotreport.DatasetRealtime, spotreport.ScopeSellSide, spotreport.MetricAvgPrice},
		{spotreport.DatasetDecl, spotreport.ScopeAbove600, spotreport.MetricDeclVolume},
	}
	for _, w := range want {
		if !hasMetric(subset, w.dataset, w.scope, w.metric) {
			t.Errorf("D 子集缺 %s/%s/%s", w.dataset, w.scope, w.metric)
		}
	}

	// 分电源四行（燃煤/燃气/新能源）保留：官网没有这个拆分，正是这张表存在的理由。
	for _, scope := range []string{spotreport.ScopeCoal, spotreport.ScopeGas, spotreport.ScopeNewEnergy} {
		if !hasMetric(subset, spotreport.DatasetTrade, scope, spotreport.MetricAvgPrice) {
			t.Errorf("D 子集缺分电源均价 %s/%s", scope, spotreport.MetricAvgPrice)
		}
	}

	t.Logf("模板提取 %d 条 → D 子集 %d 条", len(extracted.Metrics), len(subset))
}

func hasMetric(list []spotreport.Metric, dataset, scope, metric string) bool {
	for _, m := range list {
		if m.Dataset == dataset && m.Scope == scope && m.Metric == metric {
			return true
		}
	}
	return false
}
