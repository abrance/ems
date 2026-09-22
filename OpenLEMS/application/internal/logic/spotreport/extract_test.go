package spotreport

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// 回归用例使用真实日报的 OCR 结果（fixture 为坐标压缩后的原始输出）：
//   - 2026-09-21：4 页齐全，2381x3367
//   - 2026-09-18：1587x2245（不同分辨率），用于验证模板的坐标归一化
// fixture 里保留了 OCR 的原始碎片形态（含括号跨行断裂），因此这些用例同时覆盖
// 单元格碎片合并逻辑。

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

func loadPage(t *testing.T, name string, width, height float64) PageInput {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("读取 fixture %s 失败：%v", name, err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("解析 fixture %s 失败：%v", name, err)
	}
	page := PageInput{Width: width, Height: height}
	for _, l := range f.Lines {
		var line OCRLine
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

func metricOf(res *ExtractResult, dataset, scope, metric string) *Metric {
	for i := range res.Metrics {
		m := &res.Metrics[i]
		if m.Dataset == dataset && m.Scope == scope && m.Metric == metric && m.Source != SourceTradeLag {
			return m
		}
	}
	return nil
}

func TestExtractFromPages0918And0921(t *testing.T) {
	cases := []struct {
		date   string
		width  float64
		height float64
		pages  [4]string // 索引 0..3 对应 page1..page4，空串表示该页无 fixture
	}{
		{"2026-09-21", 2381, 3367, [4]string{
			"2026-09-21-page1.json", "2026-09-21-page2.json",
			"2026-09-21-page3.json", "2026-09-21-page4.json"}},
		// 09-18 缺 page3（只有 page1/2/4），表2 运行日退化为 T-2 兜底
		{"2026-09-18", 1587, 2245, [4]string{
			"2026-09-18-page1.json", "2026-09-18-page2.json", "", "2026-09-18-page4.json"}},
	}
	_ = cases
	for _, c := range cases {
		t.Run(c.date, func(t *testing.T) {
			pages := make([]PageInput, 0, 4)
			for _, name := range c.pages {
				if name == "" {
					pages = append(pages, PageInput{Width: c.width, Height: c.height})
					continue
				}
				pages = append(pages, loadPage(t, name, c.width, c.height))
			}
			res, err := ExtractFromPages(c.date, pages)
			if err != nil {
				t.Fatalf("提取失败：%v", err)
			}
			if len(res.Metrics) == 0 {
				t.Fatal("未提取到任何指标")
			}
			if len(res.Warnings) != 0 {
				t.Fatalf("不应有告警，实际：%v", res.Warnings)
			}

			// 正文摘要：发电侧日前加权平均电价 = 表1(二) 发电侧平均电价
			prose := res.Prose[MetricWeightAvg]
			if prose == nil {
				t.Fatal("正文未提取到日前加权平均电价")
			}
			avg := metricOf(res, DatasetTrade, ScopeSellSide, MetricAvgPrice)
			if avg == nil {
				t.Fatal("未提取到表1(二) 发电侧平均电价")
			}
			if !closeEnough(*prose, avg.Value) {
				t.Fatalf("正文 %v 与表1(二) 平均电价 %v 不一致", *prose, avg.Value)
			}

			// 表1(二) 最高电价带时刻
			high := metricOf(res, DatasetTrade, ScopeSellSide, MetricHighPrice)
			if high == nil || high.AtTime == "" {
				t.Fatalf("发电侧最高电价缺失或没有时刻：%+v", high)
			}

			// 表1(一) 申报信息：电价与台数
			declHigh := metricOf(res, DatasetDecl, ScopeSellSide, MetricHighPrice)
			if declHigh == nil || declHigh.Value != 1162 {
				t.Fatalf("表1(一) 发电侧最高电价应为 1162，实际 %+v", declHigh)
			}
			selfDecl := metricOf(res, DatasetDecl, ScopeSellSide, MetricSelfDecl)
			if selfDecl == nil || selfDecl.Unit != UnitCount {
				t.Fatalf("表1(一) 自主申报台数缺失或单位错误：%+v", selfDecl)
			}

			// 表1(一) 容量列组：碎片合并后应能解析出占比与环比
			zeroVolume := metricOf(res, DatasetDecl, ScopeZero, MetricDeclVolume)
			if zeroVolume == nil {
				t.Fatal("未提取到零价及以下/申报容量")
			}
			if zeroVolume.SharePct == nil || zeroVolume.MomPct == nil {
				t.Fatalf("容量单元格未解析出占比/环比：%+v", zeroVolume)
			}
			if zeroVolume.Unit != UnitMW {
				t.Fatalf("容量单位应为 MW，实际 %s", zeroVolume.Unit)
			}

			// 表2 承载滞后运行日，且实时成交电价存在
			if res.TradeDate == c.date {
				t.Fatalf("表2 运行日应滞后于日报日，实际 %s", res.TradeDate)
			}
			realtime := metricOf(res, DatasetRealtime, ScopeSellSide, MetricAvgPrice)
			if realtime == nil {
				t.Fatal("未提取到实时成交电价")
			}
		})
	}
}

// 具体数值锚定：防止模板漂移后仍然"看起来成功"
func TestExtractExactValues0921(t *testing.T) {
	pages := []PageInput{
		loadPage(t, "2026-09-21-page1.json", 2381, 3367),
		loadPage(t, "2026-09-21-page2.json", 2381, 3367),
		loadPage(t, "2026-09-21-page3.json", 2381, 3367),
		loadPage(t, "2026-09-21-page4.json", 2381, 3367),
	}
	res, err := ExtractFromPages("2026-09-21", pages)
	if err != nil {
		t.Fatal(err)
	}
	expect := func(dataset, scope, metric string, want float64) {
		t.Helper()
		m := metricOf(res, dataset, scope, metric)
		if m == nil {
			t.Fatalf("缺少 %s/%s/%s", dataset, scope, metric)
		}
		if m.Value != want {
			t.Fatalf("%s/%s/%s 期望 %v，实际 %v（原文 %q）", dataset, scope, metric, want, m.Value, m.Text)
		}
	}
	// 正文
	expect(DatasetSummary, ScopeMarket, MetricWeightAvg, 573.57)
	expect(DatasetSummary, ScopeMarket, MetricCoalAvg, 552.94)
	expect(DatasetSummary, ScopeMarket, MetricGasAvg, 678.4)
	// 表1(二) 日前成交电价（当日）
	expect(DatasetTrade, ScopeSellSide, MetricAvgPrice, 573.57)
	expect(DatasetTrade, ScopeCoal, MetricAvgPrice, 552.94)
	expect(DatasetTrade, ScopeGas, MetricAvgPrice, 678.4)
	expect(DatasetTrade, ScopeSellSide, MetricHighPrice, 1678.39)
	expect(DatasetTrade, ScopeSellSide, MetricLowPrice, -50)
	expect(DatasetTrade, ScopeSellSide, MetricMom, 8.39)
	// 表1(一) 申报信息
	expect(DatasetDecl, ScopeSellSide, MetricAvgPrice, 369.3)
	expect(DatasetDecl, ScopeSellSide, MetricDefault, 191)
	// 容量列组：scope 是报价区间，指标名是容量类型
	expect(DatasetDecl, ScopeAbove600, MetricDeclVolume, 25053.622)
	expect(DatasetDecl, ScopeAbove600, MetricGasCap, 14876.722)
	expect(DatasetDecl, ScopeZero, MetricNewCap, 36680.1798)
	// 表2 实时成交电价 → 运行日 2026-09-19
	if res.TradeDate != "2026-09-19" {
		t.Fatalf("表2 运行日期望 2026-09-19，实际 %s", res.TradeDate)
	}
	var realtimeAvg *Metric
	for i := range res.Metrics {
		m := &res.Metrics[i]
		if m.Dataset == DatasetRealtime && m.Scope == ScopeSellSide && m.Metric == MetricAvgPrice && m.Source == SourceRealtime {
			realtimeAvg = m
		}
	}
	if realtimeAvg == nil || realtimeAvg.Value != 561.36 {
		t.Fatalf("实时成交发电侧均价期望 561.36，实际 %+v", realtimeAvg)
	}
	if realtimeAvg.RunDate != "2026-09-19" {
		t.Fatalf("实时指标应挂在滞后运行日，实际 %s", realtimeAvg.RunDate)
	}
}

// 表2(一) 与表1(二) 是同一份日前成交电价的两次出现：同一天两份日报应给出相同数值
func TestLagDataMatchesSameRunDate(t *testing.T) {
	load := func(date string, names ...string) *ExtractResult {
		pages := make([]PageInput, 0, 4)
		w, h := 2381.0, 3367.0
		for _, n := range names {
			pages = append(pages, loadPage(t, n, w, h))
		}
		for len(pages) < 4 {
			pages = append(pages, PageInput{Width: w, Height: h})
		}
		res, err := ExtractFromPages(date, pages)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res921 := load("2026-09-21", "2026-09-21-page1.json", "2026-09-21-page2.json", "2026-09-21-page3.json", "2026-09-21-page4.json")

	// 09-21 日报里的表2(一)（滞后运行日 09-19）与 09-19 日报的表1(二) 应逐值一致
	for _, m := range res921.Metrics {
		if m.Source != SourceTradeLag {
			continue
		}
		if m.Metric != MetricAvgPrice {
			continue
		}
		switch m.Scope {
		case ScopeSellSide:
			if m.Value != 484.83 {
				t.Fatalf("表2(一) 发电侧均价期望 484.83（=09-19 的日前加权均价），实际 %v", m.Value)
			}
		case ScopeCoal:
			if m.Value != 462.7 {
				t.Fatalf("表2(一) 燃煤均价期望 462.7，实际 %v", m.Value)
			}
		case ScopeGas:
			if m.Value != 586.13 {
				t.Fatalf("表2(一) 燃气均价期望 586.13，实际 %v", m.Value)
			}
		}
	}
}

func TestParseCapCellFragments(t *testing.T) {
	cases := []struct {
		text  string
		value float64
		share float64
		mom   float64
	}{
		{"36680.1798(33.92%,-0.7%)", 36680.1798, 33.92, -0.7},
		{"82(0.08%,0.0%)", 82, 0.08, 0},
		{"40711.2(37.64%,-1.8%)", 40711.2, 37.64, -1.8},
	}
	for _, c := range cases {
		value, share, mom, ok := parseCapCell(c.text)
		if !ok || value != c.value || share == nil || *share != c.share || mom == nil || *mom != c.mom {
			t.Fatalf("%s 解析错误：value=%v share=%v mom=%v ok=%v", c.text, value, share, mom, ok)
		}
	}
}

func TestParsePriceCellVariants(t *testing.T) {
	if v, mom, ok := parsePriceCell("369.3(+1.5%)"); !ok || v != 369.3 || mom == nil || *mom != 1.5 {
		t.Fatalf("价格单元格解析错误：%v %v %v", v, mom, ok)
	}
	if v, _, ok := parsePriceCell("-50"); !ok || v != -50 {
		t.Fatalf("负价格解析错误：%v %v", v, ok)
	}
	if _, _, ok := parsePriceCell("无数据"); ok {
		t.Fatal("非数值文本不应解析成功")
	}
}

func TestExtractRejectsIncompletePages(t *testing.T) {
	if _, err := ExtractFromPages("2026-09-21", []PageInput{{}, {}}); err == nil {
		t.Fatal("页数不足应报错")
	}
}

func TestValidateDetectsInconsistency(t *testing.T) {
	res := &ExtractResult{
		RunDate: "2026-09-21",
		Prose:   map[string]*float64{MetricWeightAvg: ptr(500.0)},
		Metrics: []Metric{
			{Dataset: DatasetTrade, Source: SourceTrade, Scope: ScopeSellSide, Metric: MetricAvgPrice, Value: 573.57, Conf: 1},
		},
	}
	if len(validate(res)) == 0 {
		t.Fatal("正文与表格不一致时应产生告警")
	}
}

func ptr(v float64) *float64 { return &v }

var _ = fmt.Sprintf
