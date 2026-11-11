package spotmarket

import (
	"math"
	"testing"

	"s_db/s_db_model"
)

func row(dataset, scope, metric string, value float64, runDate string) *s_db_model.SSpotMetricModel {
	return &s_db_model.SSpotMetricModel{
		Dataset: dataset, Scope: scope, Metric: metric, Value: value,
		RunDate: runDate, ReportDate: runDate, Unit: "元/兆瓦时", Origin: "pm-api",
	}
}

func near(a float64, b float64) bool { return math.Abs(a-b) < 1e-9 }

// 环比只在「相邻期次真的存在」时才算：中间缺一天就留空，不能拿 09-27 当 09-30 的上一日。
func TestGroupSeriesMomOnlyWhenAdjacent(t *testing.T) {
	rows := []*s_db_model.SSpotMetricModel{
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 100, "2026-11-01"),
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 110, "2026-11-02"),
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 200, "2026-11-05"),
	}
	got := groupSeries(rows, func(d string) bool { return periodicGrain[d] == "" }, "")
	if len(got) != 1 {
		t.Fatalf("want 1 series, got %d", len(got))
	}
	s := got[0]
	if s.Days != 3 || s.Latest.Period != "2026-11-05" {
		t.Fatalf("unexpected latest/days: %+v", s.Latest)
	}
	// 11-05 的上一期是 11-04，没有点 -> 不给环比
	if s.MomPct != nil {
		t.Fatalf("non-adjacent period must not yield mom, got %v", *s.MomPct)
	}
	if s.Peak != 200 || s.Valley != 100 || !near(s.Spread, 100) {
		t.Fatalf("peak/valley/spread wrong: %+v", s)
	}
}

func TestGroupSeriesYoY(t *testing.T) {
	rows := []*s_db_model.SSpotMetricModel{
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 400, "2025-11-10"),
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 500, "2026-11-10"),
	}
	got := groupSeries(rows, func(d string) bool { return periodicGrain[d] == "" }, "")
	if got[0].YoYPct == nil || !near(*got[0].YoYPct, 25) {
		t.Fatalf("want +25%% yoy, got %v", got[0].YoYPct)
	}
}

// 同比要用窗口外的历史算：只按展示窗口取数时，去年同期那个点根本不在窗口里。
// 窗口只决定「画哪些点」，不决定「同比跟谁比」。
func TestGroupSeriesYoYUsesHistoryOutsideWindow(t *testing.T) {
	rows := []*s_db_model.SSpotMetricModel{
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 400, "2025-11-10"),
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 500, "2026-11-10"),
	}
	got := groupSeries(rows, func(d string) bool { return periodicGrain[d] == "" }, "2026-08-01")
	if len(got) != 1 {
		t.Fatalf("窗口内应有 1 条序列，got %d", len(got))
	}
	s := got[0]
	if s.Days != 1 || s.Latest.Period != "2026-11-10" {
		t.Fatalf("窗口外的点不该进 points: %+v", s.Points)
	}
	if s.YoYPct == nil || !near(*s.YoYPct, 25) {
		t.Fatalf("want +25%% yoy（用窗口外去年同期），got %v", s.YoYPct)
	}
	if s.MomPct != nil {
		t.Fatalf("窗口内只有一天，不该有环比，got %v", *s.MomPct)
	}
}

// 月频/年频落到期初日，粒度靠 dataset 区分；月频不和日频混在一个序列里。
func TestGroupSeriesSplitsPeriodicByDataset(t *testing.T) {
	rows := []*s_db_model.SSpotMetricModel{
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 400, "2026-11-10"),
		row("mid_long_term_month", "", "净合约成交均价", 454.96, "2026-10-01"),
		row("hhi", "", "HHI指标", 319329.39, "2025-01-01"),
	}
	daily := groupSeries(rows, func(d string) bool { return periodicGrain[d] == "" }, "")
	periodic := groupSeries(rows, func(d string) bool { return periodicGrain[d] != "" }, "")
	if len(daily) != 1 || daily[0].Grain != GrainDay {
		t.Fatalf("daily wrong: %+v", daily)
	}
	if len(periodic) != 2 {
		t.Fatalf("want 2 periodic series, got %d", len(periodic))
	}
	for _, s := range periodic {
		if s.Grain == GrainDay {
			t.Fatalf("%s 不该被当成日频", s.Dataset)
		}
	}
}

// 日前-实时价差按同一运行日配对；实时缺的日子不出点，不能拿别的日期顶上。
func TestAheadRealtimeSpreadPairsSameRunDate(t *testing.T) {
	daily := []Series{
		{Dataset: DatasetAhead, Scope: "发电侧", Metric: "发电侧日前加权平均电价",
			Points: []Point{{"2026-11-09", 400}, {"2026-11-10", 380}, {"2026-11-11", 360}}},
		{Dataset: DatasetRealtime, Scope: "发电侧", Metric: "发电侧实时加权平均电价",
			Points: []Point{{"2026-11-08", 300}, {"2026-11-09", 350}}},
	}
	pts, peak, valley, mean := aheadRealtimeSpread(daily)
	if len(pts) != 1 || pts[0].Period != "2026-11-09" || !near(pts[0].Value, 50) {
		t.Fatalf("unexpected spread points: %+v", pts)
	}
	if !near(peak, 50) || !near(valley, 50) || !near(mean, 50) {
		t.Fatalf("spread stats wrong: %v %v %v", peak, valley, mean)
	}
}

// 缺日只算区间内的，首末之外不算缺失。
func TestMissingDaysOnlyInsideRange(t *testing.T) {
	have := map[string]bool{"2026-11-09": true, "2026-11-11": true}
	got := missingDays(have, "2026-11-09", "2026-11-11")
	if len(got) != 1 || got[0] != "2026-11-10" {
		t.Fatalf("want [2026-11-10], got %v", got)
	}
}

func TestCoverageSkipsPeriodicGaps(t *testing.T) {
	daily := []*s_db_model.SSpotMetricModel{
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 400, "2026-11-09"),
		row(DatasetAhead, "发电侧", "发电侧日前加权平均电价", 410, "2026-11-11"),
	}
	periodic := []*s_db_model.SSpotMetricModel{
		row("mid_long_term_month", "", "净合约成交均价", 454.96, "2026-09-01"),
		row("mid_long_term_month", "", "净合约成交均价", 449.73, "2026-10-01"),
	}
	got := coverage(daily, periodic, "2026-11-09", "2026-11-11")
	byDataset := map[string]Coverage{}
	for _, c := range got {
		byDataset[c.Dataset] = c
	}
	if len(byDataset[DatasetAhead].Missing) != 1 {
		t.Fatalf("日频应报出 11-10 缺失: %+v", byDataset[DatasetAhead])
	}
	if len(byDataset["mid_long_term_month"].Missing) != 0 {
		t.Fatalf("月频不该追每日缺失: %+v", byDataset["mid_long_term_month"])
	}
}
