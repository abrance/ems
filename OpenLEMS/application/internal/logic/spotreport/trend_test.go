package spotreport

import (
	"testing"
	"time"

	"s_db/s_db_model"
)

func trendMetric(runDate, dataset, scope, metric, source string, value float64) *s_db_model.SSpotMetricModel {
	return &s_db_model.SSpotMetricModel{
		ReportDate: runDate, RunDate: runDate, Dataset: dataset, Scope: scope,
		Metric: metric, Source: source, Value: value, Unit: UnitPrice, Origin: OriginManual,
	}
}

func trendPointOf(points []TrendPoint, bucket, dataset, scope, metric string) *TrendPoint {
	for i := range points {
		p := points[i]
		if p.Bucket == bucket && p.Dataset == dataset && p.Scope == scope && p.Metric == metric {
			return &p
		}
	}
	return nil
}

func TestBucketOf(t *testing.T) {
	cases := []struct {
		grain  TrendGrain
		date   string
		bucket string
	}{
		{GrainDay, "2026-09-20", "2026-09-20"},
		{GrainMonth, "2026-09-20", "2026-09"},
		{GrainWeek, "2026-09-20", "2026-W38"}, // 2026-09-20 是周日，属第 38 周
		{GrainWeek, "2026-09-21", "2026-W39"}, // 周一是新一周
		{GrainWeek, "2026-01-01", "2026-W01"},
	}
	for _, c := range cases {
		got, ok := BucketOf(c.grain, c.date)
		if !ok || got != c.bucket {
			t.Fatalf("BucketOf(%s,%s) = %q,%v，期望 %q", c.grain, c.date, got, ok, c.bucket)
		}
	}
	if _, ok := BucketOf(GrainDay, "2026/09/20"); ok {
		t.Fatal("非法日期应返回 false")
	}
}

// 日前成交同一天会有两路：当日 表1(二) 与滞后 T-2 的 表2(一)。只取 表1(二)。
func TestBuildTrendDropsLagSource(t *testing.T) {
	points := BuildTrend([]*s_db_model.SSpotMetricModel{
		trendMetric("2026-09-18", DatasetTrade, ScopeSellSide, MetricAvgPrice, SourceTrade, 500),
		trendMetric("2026-09-18", DatasetTrade, ScopeSellSide, MetricAvgPrice, SourceTradeLag, 500),
	}, GrainDay)
	if len(points) != 1 {
		t.Fatalf("表2(一) 应被剔除，实际点数 %d：%+v", len(points), points)
	}
	if points[0].Avg != 500 || points[0].Days != 1 {
		t.Fatalf("聚合结果不对：%+v", points[0])
	}
}

// 只看已确认：未复核（ocr/cv/空串）整行剔除，桶里没有点
func TestBuildTrendOnlyConfirmed(t *testing.T) {
	rows := []*s_db_model.SSpotMetricModel{
		{ReportDate: "2026-09-18", RunDate: "2026-09-18", Dataset: DatasetSummary, Scope: ScopeMarket,
			Metric: MetricWeightAvg, Value: 529.19, Origin: OriginOCR},
		{ReportDate: "2026-09-19", RunDate: "2026-09-19", Dataset: DatasetSummary, Scope: ScopeMarket,
			Metric: MetricWeightAvg, Value: 500, Origin: "cv"},
		{ReportDate: "2026-09-20", RunDate: "2026-09-20", Dataset: DatasetSummary, Scope: ScopeMarket,
			Metric: MetricWeightAvg, Value: 480, Origin: ""},
		{ReportDate: "2026-09-21", RunDate: "2026-09-21", Dataset: DatasetSummary, Scope: ScopeMarket,
			Metric: MetricWeightAvg, Value: 510, Origin: OriginManual},
	}
	points := BuildTrend(rows, GrainDay)
	if len(points) != 1 || points[0].Bucket != "2026-09-21" {
		t.Fatalf("只应留下已确认那天：%+v", points)
	}
}

func TestBuildTrendAverageAndExtremes(t *testing.T) {
	var rows []*s_db_model.SSpotMetricModel
	for _, item := range []struct {
		date string
		val  float64
	}{{"2026-09-16", 500}, {"2026-09-17", 400}, {"2026-09-18", 600}} {
		rows = append(rows, trendMetric(item.date, DatasetTrade, ScopeSellSide, MetricAvgPrice, SourceTrade, item.val))
	}
	points := BuildTrend(rows, GrainMonth)
	p := trendPointOf(points, "2026-09", DatasetTrade, ScopeSellSide, MetricAvgPrice)
	if p == nil {
		t.Fatalf("缺少聚合点：%+v", points)
	}
	if p.Days != 3 || p.Max != 600 || p.Min != 400 || p.Avg != 500 {
		t.Fatalf("avg/max/min/days 不对：%+v", p)
	}
	if p.From != "2026-09-16" || p.To != "2026-09-18" {
		t.Fatalf("桶内范围不对：%+v", p)
	}
}

// 同一运行日的重复行只算一次（重跑留痕、表1(二) 与其它来源重叠时不能重复计数）
func TestBuildTrendDedupesSameDay(t *testing.T) {
	points := BuildTrend([]*s_db_model.SSpotMetricModel{
		trendMetric("2026-09-18", DatasetTrade, ScopeSellSide, MetricAvgPrice, SourceTrade, 500),
		trendMetric("2026-09-18", DatasetTrade, ScopeSellSide, MetricAvgPrice, SourceTrade, 900),
	}, GrainWeek)
	if len(points) != 1 || points[0].Days != 1 || points[0].Avg != 500 {
		t.Fatalf("同天重复行应按首行计一次：%+v", points)
	}
}

// 容量占比按桶内 Σ容量 ÷ Σ申报容量 重算，而不是平均各天 share_pct
func TestBuildTrendCapacityShareUsesSum(t *testing.T) {
	var rows []*s_db_model.SSpotMetricModel
	for _, day := range []struct {
		date       string
		capacity   float64
		declVolume float64
	}{{"2026-09-16", 100, 1000}, {"2026-09-17", 300, 1000}} {
		rows = append(rows,
			trendMetric(day.date, DatasetDecl, ScopeZero, MetricCoalCap, SourceDecl, day.capacity),
			trendMetric(day.date, DatasetDecl, ScopeZero, MetricDeclVolume, SourceDecl, day.declVolume))
	}
	points := BuildTrend(rows, GrainMonth)
	cap := trendPointOf(points, "2026-09", DatasetDecl, ScopeZero, MetricCoalCap)
	if cap == nil || cap.SharePct == nil {
		t.Fatalf("缺容量占比：%+v", points)
	}
	if got := *cap.SharePct; got != 20 { // (100+300)/(1000+1000) = 20%
		t.Fatalf("占比应为 20%%，实际 %v", got)
	}
	if cap.Sum != 400 {
		t.Fatalf("Σ容量应为 400，实际 %v", cap.Sum)
	}
}

// 地市档位是分类值：按桶输出「各档位城市-天计数」，不做均值
func TestBuildTrendBucketCounts(t *testing.T) {
	mk := func(date, city, textValue string, value float64) *s_db_model.SSpotMetricModel {
		m := trendMetric(date, "node_price_peak", city, MetricPriceBucket, "图2-1", value)
		m.TextValue = textValue
		return m
	}
	points := BuildTrend([]*s_db_model.SSpotMetricModel{
		mk("2026-09-16", "广州", "[800,+inf)", 5),
		mk("2026-09-16", "深圳", "[800,+inf)", 5),
		mk("2026-09-17", "广州", "[600,800)", 4),
	}, GrainWeek)
	if len(points) != 2 {
		t.Fatalf("应有两个档位点（800+ 计数 2、600-800 计数 1）：%+v", points)
	}
	for _, p := range points {
		switch p.TextValue {
		case "[800,+inf)":
			if p.Count != 2 || p.Value != 5 || p.Days != 1 {
				t.Fatalf("800+ 档不对：%+v", p)
			}
		case "[600,800)":
			if p.Count != 1 || p.Value != 4 {
				t.Fatalf("600-800 档不对：%+v", p)
			}
		default:
			t.Fatalf("意外档位：%+v", p)
		}
	}
}

func TestBuildTrendEmpty(t *testing.T) {
	if points := BuildTrend(nil, GrainDay); len(points) != 0 {
		t.Fatalf("空输入应返回空：%+v", points)
	}
}

func TestNormalizeTrendRange(t *testing.T) {
	today := time.Now().In(shanghaiLocation()).Format(trendDateLayout)
	from, to, err := NormalizeTrendRange("", "")
	if err != nil {
		t.Fatalf("缺参应兜底而不是报错：%v", err)
	}
	if from != today[:8]+"01" || to != today {
		t.Fatalf("缺参兜底应为本月到今天，实际 %s~%s", from, to)
	}

	if _, _, err := NormalizeTrendRange("2026-09-20", "2026-09-01"); err == nil {
		t.Fatal("起止颠倒应报错")
	}
	if _, _, err := NormalizeTrendRange("2026-01-01", "2029-01-01"); err == nil {
		t.Fatal("超过 730 天应报错")
	}
	// 非法值单边兜底，另一边保留：锚点取有效 to 所在月 1 日，避免随时间推移 to 落到更早月份而误报
	prevMonth := time.Now().In(shanghaiLocation()).AddDate(0, -1, 0).Format(trendDateLayout)[:7]
	from, to, err = NormalizeTrendRange("bad-date", prevMonth+"-10")
	if err != nil || to != prevMonth+"-10" || from != prevMonth+"-01" {
		t.Fatalf("非法起始应兜底为 to 所在月 1 日：%s~%s err=%v", from, to, err)
	}
}

func TestDefaultTrendGrain(t *testing.T) {
	cases := []struct {
		from, to string
		want     TrendGrain
	}{
		{"2026-09-01", "2026-09-25", GrainDay},   // 25 天
		{"2026-09-01", "2026-10-31", GrainDay},   // 61 天（阈值内仍按日）
		{"2026-09-01", "2026-11-30", GrainWeek},  // 91 天
		{"2026-01-01", "2026-06-30", GrainWeek},  // 181 天
		{"2025-01-01", "2026-06-01", GrainMonth}, // 517 天
	}
	for _, c := range cases {
		if got := DefaultTrendGrain(c.from, c.to); got != c.want {
			t.Fatalf("DefaultTrendGrain(%s,%s) = %s，期望 %s", c.from, c.to, got, c.want)
		}
	}
	if _, ok := ParseTrendGrain("hour"); ok {
		t.Fatal("非法粒度应返回 false")
	}
	if g, ok := ParseTrendGrain(" week "); !ok || g != GrainWeek {
		t.Fatalf("粒度应支持带空格：%q %v", g, ok)
	}
}
