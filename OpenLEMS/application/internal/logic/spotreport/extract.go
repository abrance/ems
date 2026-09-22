package spotreport

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 模板提取器：把日报图片的 OCR 结果还原成结构化指标。
//
// 版式（广东电力交易中心《现货运行日报》）在多次采集间稳定：三篇不同分辨率的日报
// （2381x3367 与 1587x2245）在归一化坐标下位置一致（误差 ±0.001），因此模板全部用
// 相对坐标表达，与图片分辨率无关。
//
//	page1 表1(一) 日前申报信息：行 发电侧/零价及以下/报价600以上，
//	      列组 A（发电侧行）最高/最低/平均电价 + 自主/默认申报台数，
//	      列组 B（另两行）申报容量/煤机容量/气机容量/新能源容量
//	page1 正文「（一）现货日前交易情况」：日前加权平均电价、燃煤/燃气均价、成交最高/最低
//	page2 表1(二) 日前成交电价：发电侧/燃煤/燃气/新能源 × 最高(+时刻)/最低(+时刻)/平均/环比
//	page4 表2(一) 日前成交电价、表2(二) 实时成交电价：发电侧/燃煤/燃气 × 同上（运行日滞后 T-2）
//
// 算法四步：坐标归一化 → 数据行聚类定列心（并用 HH:MM 识别时刻子列）→ 行标签切行 →
// 单元格碎片合并（修复括号跨行断裂）→ 语义解析 + 交叉校验。
const ExtractorVersion = "tpl-1"

// 数据集
const (
	DatasetDecl     = "day_ahead_decl"  // 日前申报信息
	DatasetTrade    = "day_ahead_trade" // 日前成交电价
	DatasetRealtime = "realtime_trade"  // 实时成交电价
	DatasetSummary  = "summary"         // 正文摘要指标
	ScopeMarket     = "市场"              // 正文摘要的对象
	ScopeSellSide   = "发电侧"
	ScopeCoal       = "燃煤"
	ScopeGas        = "燃气"
	ScopeNewEnergy  = "新能源"
	ScopeZero       = "零价及以下"
	ScopeAbove600   = "报价600以上"
)

// 指标名与单位
const (
	MetricHighPrice  = "最高电价"
	MetricLowPrice   = "最低电价"
	MetricAvgPrice   = "平均电价"
	MetricMom        = "电价环比"
	MetricDeclVolume = "申报容量"
	MetricCoalCap    = "煤机容量"
	MetricGasCap     = "气机容量"
	MetricNewCap     = "新能源容量"
	MetricSelfDecl   = "自主申报台数"
	MetricDefault    = "默认申报台数"
	MetricWeightAvg  = "日前加权平均电价"
	MetricCoalAvg    = "燃煤均价"
	MetricGasAvg     = "燃气均价"
	MetricHighTrade  = "日前成交最高价"
	MetricLowTrade   = "日前成交最低价"

	UnitPrice = "元/兆瓦时"
	UnitMW    = "MW"
	UnitCount = "台"
	UnitPct   = "%"
)

// 来源（哪张表/哪段正文）
const (
	SourceDecl     = "表1(一)"
	SourceTrade    = "表1(二)"
	SourceTradeLag = "表2(一)"
	SourceRealtime = "表2(二)"
	SourceProse    = "正文"
)

// 模板区域（归一化 y）
var (
	proseBand   = [2]float64{0.49, 0.63} // page1 正文指标
	declBand    = [2]float64{0.74, 0.90} // page1 表1(一) 数据区
	trade1Band  = [2]float64{0.12, 0.24} // page2 表1(二) 数据区
	trade2BandA = [2]float64{0.09, 0.17} // page4 表2(一) 数据区
	trade2BandB = [2]float64{0.22, 0.30} // page4 表2(二) 数据区
)

const (
	labelXMax    = 0.28  // 行标签列的最大中心 x
	colTol       = 0.022 // 列聚类容差
	cellTol      = 0.030 // 单元格取值容差
	rowLabelTol  = 0.014 // 行带上下扩展
	rowLabelGap  = 0.020 // 多行行标签合并阈值（必须小于行距 0.0265）
	timeOffsetOK = true
)

var (
	reNumber = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
	reMomPct = regexp.MustCompile(`\(([-+]?\d+(?:\.\d+)?)%\)`)
	reCapPct = regexp.MustCompile(`\(([\d.]+)%,([-+]?[\d.]+)%\)`)
	reTime   = regexp.MustCompile(`(\d{1,2}:\d{2})`)
	reTimeOn = regexp.MustCompile(`^\**\(?(\d{1,2}:\d{2})\)?\**$`)
)

// PageInput 单页 OCR 输入
type PageInput struct {
	Width  float64
	Height float64
	Lines  []OCRLine
}

// Metric 一条结构化指标
type Metric struct {
	RunDate  string
	Dataset  string
	Scope    string
	Metric   string
	Value    float64
	Unit     string
	AtTime   string
	MomPct   *float64
	SharePct *float64
	Source   string
	Conf     float64
	Text     string // 原始单元格文本，便于排查
}

// ExtractResult 一次模板提取的产物
type ExtractResult struct {
	RunDate   string
	TradeDate string // 表2 承载的滞后运行日
	Metrics   []Metric
	Warnings  []string
	Prose     map[string]*float64
}

// normLine 归一化后的识别行
type normLine struct {
	Text string
	Conf float64
	XC   float64 // 中心 x / 页宽
	Y    float64 // 中心 y / 页高
}

type colCluster struct {
	XC    float64
	Items []normLine
}

type rowBand struct {
	Name string
	Y0   float64
	Y1   float64
}

type cell struct {
	Text string
	Conf float64
	Time string
}

// ExtractFromPages 从四页 OCR 结果提取指标。pages[0..3] 对应 page1..page4。
func ExtractFromPages(runDate string, pages []PageInput) (*ExtractResult, error) {
	if len(pages) < 4 {
		return nil, fmt.Errorf("需要 4 页 OCR 结果，实际 %d 页", len(pages))
	}
	p1 := normalize(pages[0])
	p2 := normalize(pages[1])
	p3 := normalize(pages[2])
	p4 := normalize(pages[3])

	res := &ExtractResult{RunDate: runDate}

	// 表2 的运行日：优先从 page3 的「表2 运行日 YYYY-M-D」解析，兜底 T-2
	res.TradeDate = parseLagRunDate(p3, runDate)

	// 正文摘要指标
	res.Prose = parseProseMetrics(inBand(p1, proseBand[0], proseBand[1]))
	res.Metrics = append(res.Metrics, proseMetrics(runDate, res.Prose)...)

	// 表1(一) 日前申报信息：两个列组，按行分组分别聚类
	decl := inBand(p1, declBand[0], declBand[1])
	declRows := segmentRows(decl)
	if rows := filterRows(declRows, ScopeSellSide); len(rows) > 0 {
		table := buildTable(decl, rows, []string{MetricHighPrice, MetricLowPrice, MetricAvgPrice, MetricSelfDecl, MetricDefault}, false)
		res.Metrics = append(res.Metrics, declMetrics(runDate, table, SourceDecl, DatasetDecl, false)...)
	}
	if rows := filterRows(declRows, ScopeZero, "报价"); len(rows) > 0 {
		table := buildTable(decl, rows, []string{MetricDeclVolume, MetricCoalCap, MetricGasCap, MetricNewCap}, false)
		res.Metrics = append(res.Metrics, declMetrics(runDate, table, SourceDecl, DatasetDecl, true)...)
	}

	// 表1(二) 日前成交电价（当日运行日）
	tradeRows := filterRows(segmentRows(inBand(p2, trade1Band[0], trade1Band[1])), ScopeSellSide, ScopeCoal, ScopeGas, ScopeNewEnergy)
	res.Metrics = append(res.Metrics, tradeMetrics(runDate, buildTable(inBand(p2, trade1Band[0], trade1Band[1]), tradeRows, tradeHeaders(), true), SourceTrade, DatasetTrade)...)

	// 表2(一) 日前成交电价 / 表2(二) 实时成交电价（滞后运行日）
	rowsA := filterRows(segmentRows(inBand(p4, trade2BandA[0], trade2BandA[1])), ScopeSellSide, ScopeCoal, ScopeGas, ScopeNewEnergy)
	res.Metrics = append(res.Metrics, tradeMetrics(res.TradeDate, buildTable(inBand(p4, trade2BandA[0], trade2BandA[1]), rowsA, tradeHeaders(), true), SourceTradeLag, DatasetTrade)...)
	rowsB := filterRows(segmentRows(inBand(p4, trade2BandB[0], trade2BandB[1])), ScopeSellSide, ScopeCoal, ScopeGas, ScopeNewEnergy)
	res.Metrics = append(res.Metrics, tradeMetrics(res.TradeDate, buildTable(inBand(p4, trade2BandB[0], trade2BandB[1]), rowsB, tradeHeaders(), true), SourceRealtime, DatasetRealtime)...)

	res.Warnings = append(res.Warnings, validate(res)...)
	return res, nil
}

func tradeHeaders() []string {
	return []string{MetricHighPrice, MetricLowPrice, MetricAvgPrice, MetricMom}
}

// normalize 把像素坐标转成归一化中心坐标；页面尺寸缺失时退化为按识别结果包围盒推算
func normalize(page PageInput) []normLine {
	w, h := page.Width, page.Height
	if w <= 0 || h <= 0 {
		for _, l := range page.Lines {
			if x := l.BBox.Left + l.BBox.Width; x > w {
				w = x
			}
			if y := l.BBox.Top + l.BBox.Height; y > h {
				h = y
			}
		}
	}
	if w <= 0 || h <= 0 {
		return nil
	}
	out := make([]normLine, 0, len(page.Lines))
	for _, l := range page.Lines {
		text := strings.TrimSpace(l.Text)
		if text == "" {
			continue
		}
		out = append(out, normLine{
			Text: text,
			Conf: l.Confidence,
			XC:   (l.BBox.Left + l.BBox.Width/2) / w,
			Y:    (l.BBox.Top + l.BBox.Height/2) / h,
		})
	}
	return out
}

func inBand(lines []normLine, y0, y1 float64) []normLine {
	out := make([]normLine, 0, len(lines))
	for _, l := range lines {
		if l.Y >= y0 && l.Y <= y1 {
			out = append(out, l)
		}
	}
	return out
}

// clusterColumns 按中心 x 聚类成列（数据驱动，不依赖绝对像素）
func clusterColumns(lines []normLine) []colCluster {
	sorted := append([]normLine(nil), lines...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].XC < sorted[j].XC })
	var cols []colCluster
	for _, l := range sorted {
		placed := false
		for i := range cols {
			if abs(cols[i].XC-l.XC) < colTol {
				cols[i].Items = append(cols[i].Items, l)
				sum := 0.0
				for _, it := range cols[i].Items {
					sum += it.XC
				}
				cols[i].XC = sum / float64(len(cols[i].Items))
				placed = true
				break
			}
		}
		if !placed {
			cols = append(cols, colCluster{XC: l.XC, Items: []normLine{l}})
		}
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].XC < cols[j].XC })
	return cols
}

// segmentRows 用最左列的行标签切行；多行标签（零价及以 / 下）按 y 间距合并
func segmentRows(lines []normLine) []rowBand {
	var labels []normLine
	for _, l := range lines {
		if l.XC < labelXMax {
			labels = append(labels, l)
		}
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].Y < labels[j].Y })

	type group struct {
		parts []normLine
	}
	var groups []group
	for _, l := range labels {
		if n := len(groups); n > 0 {
			last := groups[n-1].parts[len(groups[n-1].parts)-1]
			if l.Y-last.Y < rowLabelGap {
				groups[n-1].parts = append(groups[n-1].parts, l)
				continue
			}
		}
		groups = append(groups, group{parts: []normLine{l}})
	}

	out := make([]rowBand, 0, len(groups))
	for _, g := range groups {
		var sb strings.Builder
		minY, maxY := g.parts[0].Y, g.parts[0].Y
		for _, p := range g.parts {
			sb.WriteString(p.Text)
			if p.Y < minY {
				minY = p.Y
			}
			if p.Y > maxY {
				maxY = p.Y
			}
		}
		out = append(out, rowBand{
			// OCR 会在行标签里带空格（如「报价 600以上」），去掉空白以对齐常量
			Name: strings.Join(strings.Fields(sb.String()), ""),
			Y0:   minY - rowLabelTol,
			Y1:   maxY + rowLabelTol,
		})
	}
	return out
}

func filterRows(rows []rowBand, prefixes ...string) []rowBand {
	out := make([]rowBand, 0, len(rows))
	for _, r := range rows {
		for _, p := range prefixes {
			if strings.HasPrefix(r.Name, p) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// buildTable 数据行聚类定列心；HH:MM 列识别为时刻子列并归属左侧值列；表头名按 x 顺序配给值列
func buildTable(lines []normLine, rows []rowBand, headers []string, hasTime bool) map[string]map[string]cell {
	out := map[string]map[string]cell{}
	if len(rows) == 0 {
		return out
	}
	var body []normLine
	for _, r := range rows {
		for _, l := range lines {
			if l.Y >= r.Y0 && l.Y <= r.Y1 && l.XC >= labelXMax {
				body = append(body, l)
			}
		}
	}
	type named struct {
		name string
		xc   float64
		txc  float64
		hasT bool
	}
	var namedCols []named
	pendingHeader := append([]string(nil), headers...)
	for _, c := range clusterColumns(body) {
		timeHits := 0
		for _, it := range c.Items {
			if reTimeOn.MatchString(it.Text) {
				timeHits++
			}
		}
		isTime := len(c.Items) > 0 && float64(timeHits) >= 0.6*float64(len(c.Items))
		if isTime {
			if hasTime && len(namedCols) > 0 && !namedCols[len(namedCols)-1].hasT {
				namedCols[len(namedCols)-1].txc = c.XC
				namedCols[len(namedCols)-1].hasT = true
			}
			continue
		}
		name := "?"
		if len(pendingHeader) > 0 {
			name = pendingHeader[0]
			pendingHeader = pendingHeader[1:]
		}
		namedCols = append(namedCols, named{name: name, xc: c.XC})
	}

	for _, r := range rows {
		cells := map[string]cell{}
		for _, c := range namedCols {
			if c.name == "?" {
				continue
			}
			grab := func(x float64) (string, float64, bool) {
				var fr []normLine
				for _, l := range lines {
					if l.Y >= r.Y0 && l.Y <= r.Y1 && abs(l.XC-x) < cellTol {
						fr = append(fr, l)
					}
				}
				if len(fr) == 0 {
					return "", 0, false
				}
				return mergeFragments(fr)
			}
			text, conf, ok := grab(c.xc)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			cl := cell{Text: text, Conf: conf}
			if c.hasT {
				if tv, _, ok := grab(c.txc); ok {
					if m := reTime.FindStringSubmatch(tv); m != nil {
						cl.Time = m[1]
					}
				}
			}
			cells[c.name] = cl
		}
		if len(cells) > 0 {
			out[r.Name] = cells
		}
	}
	return out
}

// mergeFragments 同一单元格内的碎片按 y 拼接；括号未闭合时强制续接（修复跨行断裂）
func mergeFragments(fragments []normLine) (string, float64, bool) {
	if len(fragments) == 0 {
		return "", 0, false
	}
	sorted := append([]normLine(nil), fragments...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Y < sorted[j].Y })
	var sb strings.Builder
	conf := sorted[0].Conf
	for i, f := range sorted {
		if i > 0 && strings.Count(sb.String(), "(") <= strings.Count(sb.String(), ")") {
			sb.WriteString(" ")
		}
		sb.WriteString(f.Text)
		if f.Conf < conf {
			conf = f.Conf
		}
	}
	return strings.TrimSpace(sb.String()), conf, true
}

// ---------- 语义解析 ----------

// parsePriceCell 价格类单元格：值 与 (环比%)
func parsePriceCell(text string) (value float64, mom *float64, ok bool) {
	cleaned := strings.ReplaceAll(text, ",", "")
	m := reNumber.FindString(cleaned)
	if m == "" {
		return 0, nil, false
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, nil, false
	}
	if pm := reMomPct.FindStringSubmatch(text); pm != nil {
		if p, err := strconv.ParseFloat(pm[1], 64); err == nil {
			mom = &p
		}
	}
	return v, mom, true
}

// parseCapCell 容量类单元格：值 + (占比%,环比%)
func parseCapCell(text string) (value float64, share, mom *float64, ok bool) {
	cleaned := strings.ReplaceAll(text, ",", "")
	m := reNumber.FindString(cleaned)
	if m == "" {
		return 0, nil, nil, false
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, nil, nil, false
	}
	if pm := reCapPct.FindStringSubmatch(text); pm != nil {
		if s, err := strconv.ParseFloat(pm[1], 64); err == nil {
			share = &s
		}
		if p, err := strconv.ParseFloat(pm[2], 64); err == nil {
			mom = &p
		}
	}
	return v, share, mom, true
}

// parseProseMetrics 正文摘要指标
func parseProseMetrics(lines []normLine) map[string]*float64 {
	sort.Slice(lines, func(i, j int) bool { return lines[i].Y < lines[j].Y })
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l.Text)
	}
	text := strings.ReplaceAll(sb.String(), " ", "")
	find := func(pattern string) *float64 {
		m := regexp.MustCompile(pattern).FindStringSubmatch(text)
		if m == nil {
			return nil
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return nil
		}
		return &v
	}
	return map[string]*float64{
		MetricWeightAvg: find(`日前加权平均电价(-?\d+(?:\.\d+)?)`),
		MetricCoalAvg:   find(`燃煤均价(-?\d+(?:\.\d+)?)`),
		MetricGasAvg:    find(`燃气均价(-?\d+(?:\.\d+)?)`),
		MetricHighTrade: find(`成交价最高(-?\d+(?:\.\d+)?)`),
		MetricLowTrade:  find(`最低(-?\d+(?:\.\d+)?)`),
	}
}

// parseLagRunDate 解析 page3 的「表2 运行日 YYYY-M-D」；缺失时按 T-2 兜底
func parseLagRunDate(lines []normLine, reportDate string) string {
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l.Text)
	}
	re := regexp.MustCompile(`表\s*2\s*运行日\s*(\d{4})\s*[-—年]\s*(\d{1,2})\s*[-—月]\s*(\d{1,2})`)
	if m := re.FindStringSubmatch(strings.ReplaceAll(sb.String(), " ", "")); m != nil {
		year, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		if year > 2000 && month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
		}
	}
	// 兜底：实时交易数据滞后两个运行日（三篇日报实测一致）
	if t, err := parseDate(reportDate); err == nil {
		return t.AddDate(0, 0, -2).Format("2006-01-02")
	}
	return reportDate
}

// ---------- 指标组装 ----------

func proseMetrics(runDate string, prose map[string]*float64) []Metric {
	order := []string{MetricWeightAvg, MetricCoalAvg, MetricGasAvg, MetricHighTrade, MetricLowTrade}
	var out []Metric
	for _, name := range order {
		v := prose[name]
		if v == nil {
			continue
		}
		out = append(out, Metric{
			RunDate: runDate, Dataset: DatasetSummary, Scope: ScopeMarket, Metric: name,
			Value: *v, Unit: UnitPrice, Source: SourceProse, Conf: 1,
		})
	}
	return out
}

func tradeMetrics(runDate string, table map[string]map[string]cell, source, dataset string) []Metric {
	scopes := sortedKeys(table)
	var out []Metric
	for _, scope := range scopes {
		cells := table[scope]
		for _, name := range []string{MetricHighPrice, MetricLowPrice, MetricAvgPrice, MetricMom} {
			cl, ok := cells[name]
			if !ok {
				continue
			}
			value, mom, ok := parsePriceCell(cl.Text)
			if !ok {
				continue
			}
			m := Metric{
				RunDate: runDate, Dataset: dataset, Scope: scope, Metric: name,
				Value: value, Unit: UnitPrice, Source: source, Conf: cl.Conf, Text: cl.Text,
			}
			if name == MetricMom {
				m.Unit = UnitPct
			} else {
				m.MomPct = mom
			}
			if name == MetricHighPrice || name == MetricLowPrice {
				m.AtTime = cl.Time
			}
			out = append(out, m)
		}
	}
	return out
}

func declMetrics(runDate string, table map[string]map[string]cell, source, dataset string, capacity bool) []Metric {
	var out []Metric
	for _, scope := range sortedKeys(table) {
		cells := table[scope]
		for _, name := range sortedKeys(cells) {
			cl := cells[name]
			if capacity {
				value, share, mom, ok := parseCapCell(cl.Text)
				if !ok {
					continue
				}
				out = append(out, Metric{
					RunDate: runDate, Dataset: dataset, Scope: scope, Metric: name,
					Value: value, Unit: UnitMW, SharePct: share, MomPct: mom,
					Source: source, Conf: cl.Conf, Text: cl.Text,
				})
				continue
			}
			value, mom, ok := parsePriceCell(cl.Text)
			if !ok {
				continue
			}
			unit := UnitPrice
			if name == MetricSelfDecl || name == MetricDefault {
				unit = UnitCount
			}
			out = append(out, Metric{
				RunDate: runDate, Dataset: dataset, Scope: scope, Metric: name,
				Value: value, Unit: unit, MomPct: mom, Source: source, Conf: cl.Conf, Text: cl.Text,
			})
		}
	}
	return out
}

// ---------- 校验 ----------

func validate(res *ExtractResult) []string {
	var warnings []string
	avg := map[string]float64{}
	for _, m := range res.Metrics {
		if m.Dataset == DatasetTrade && m.Source == SourceTrade && m.Metric == MetricAvgPrice {
			avg[m.Scope] = m.Value
		}
	}
	// 正文与表1(二) 必须一致（正文保留 1 位小数，容差取相对 0.1%）
	pairs := map[string]string{ScopeSellSide: MetricWeightAvg, ScopeCoal: MetricCoalAvg, ScopeGas: MetricGasAvg}
	for scope, proseName := range pairs {
		pv := res.Prose[proseName]
		tv, ok := avg[scope]
		if pv == nil || !ok {
			continue
		}
		if !closeEnough(*pv, tv) {
			warnings = append(warnings, fmt.Sprintf("%s 平均电价=%.4f 与正文 %s=%.4f 不一致", scope, tv, proseName, *pv))
		}
	}
	if pv := res.Prose[MetricHighTrade]; pv != nil {
		for _, m := range res.Metrics {
			if m.Dataset == DatasetTrade && m.Source == SourceTrade && m.Scope == ScopeSellSide && m.Metric == MetricHighPrice {
				if !closeEnough(*pv, m.Value) {
					warnings = append(warnings, fmt.Sprintf("发电侧最高电价=%.4f 与正文=%.4f 不一致", m.Value, *pv))
				}
			}
		}
	}
	// 单调性：最高 ≥ 平均 ≥ 最低
	byScope := map[string]map[string]float64{}
	for _, m := range res.Metrics {
		if m.Dataset != DatasetTrade {
			continue
		}
		key := m.Source + "|" + m.Scope
		if byScope[key] == nil {
			byScope[key] = map[string]float64{}
		}
		byScope[key][m.Metric] = m.Value
	}
	for key, vals := range byScope {
		hi, okHi := vals[MetricHighPrice]
		av, okAv := vals[MetricAvgPrice]
		lo, okLo := vals[MetricLowPrice]
		if okHi && okAv && okLo && !(hi >= av && av >= lo) {
			warnings = append(warnings, fmt.Sprintf("%s 最高%.2f≥平均%.2f≥最低%.2f 不成立", key, hi, av, lo))
		}
	}
	// 低置信度单元格
	for _, m := range res.Metrics {
		if m.Conf > 0 && m.Conf < 0.8 {
			warnings = append(warnings, fmt.Sprintf("%s/%s/%s OCR 置信度偏低(%.2f)：%s", m.Scope, m.Metric, m.Source, m.Conf, m.Text))
		}
	}
	return dedupe(warnings)
}

func closeEnough(a, b float64) bool {
	diff := abs(a - b)
	return diff <= 0.05 || (b != 0 && diff/abs(b) <= 0.001)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ---------- 小工具 ----------

// parseDate 解析 YYYY-MM-DD
func parseDate(v string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(v), shanghaiLocation())
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
