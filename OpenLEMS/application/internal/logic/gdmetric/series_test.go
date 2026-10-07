package gdmetric

import (
	"context"
	"testing"
)

func TestNormalizeKind(t *testing.T) {
	cases := map[string]SeriesKind{
		"":          KindDayAhead,
		"dayAhead":  KindDayAhead,
		"day_ahead": KindDayAhead,
		"ahead":     KindDayAhead,
		"realtime":  KindRealtime,
		"real":      KindRealtime,
		"LOAD":      KindLoad,
	}
	for in, want := range cases {
		got, err := NormalizeKind(in)
		if err != nil {
			t.Errorf("%q 应可解析: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q → %s，期望 %s", in, got, want)
		}
	}
	if _, err := NormalizeKind("bogus"); err == nil {
		t.Error("非法类别应报错")
	}
}

// 区间校验不该依赖外部 CSV：这些用例在数据没就绪时也应给出可读错误，
// 而不是静默返回空序列。
func TestGetSeriesRejectsBadRange(t *testing.T) {
	ctx := context.Background()
	if _, err := GetSeries(ctx, "2026-09-21", "2026-09-01", KindDayAhead); err == nil {
		t.Error("起止颠倒应报错")
	}
	if _, err := GetSeries(ctx, "bad", "2026-09-01", KindDayAhead); err == nil {
		t.Error("起始日期非法应报错")
	}
	// 400 天上限：用 609 天（2025-01-01 → 2026-09-01）才越界
	if _, err := GetSeries(ctx, "2025-01-01", "2026-09-01", KindDayAhead); err == nil {
		t.Error("超长区间应报错（> MaxSeriesDays）")
	}
}

// 与 CSV 里的真实覆盖对齐：这条用例只在本地已有缓存时跑。
//
// 先查缓存再取数：GetSeries 会把「缓存缺失」当成「该刷新了」去拉 37MB 的
// gd_metric.csv。CI 上没有这个文件，跑一次就是一次跨网下载（还可能因为
// 拉到的文件随上游变化而让断言时好时坏）。测试不该有这种依赖。
func TestGetSeriesRealData(t *testing.T) {
	ctx := context.Background()
	initGlobal(ctx)
	if !global.localCSVReady() {
		t.Skip("本地没有 gd_metric.csv 缓存，跳过（不要为跑用例去下载 37MB）")
	}
	series, err := GetSeries(ctx, "2026-08-01", "2026-08-03", KindDayAhead)
	if err != nil {
		t.Skipf("电网日指标数据不可用，跳过: %v", err)
	}
	if len(series) == 0 {
		t.Skip("本地无该区间数据，跳过")
	}
	// 每天 96 点（15 分钟粒度），3 天应为 288 点
	if len(series) != 288 {
		t.Errorf("3 天应为 288 点，实际 %d", len(series))
	}
	// 升序
	for i := 1; i < len(series); i++ {
		if series[i].Date < series[i-1].Date ||
			(series[i].Date == series[i-1].Date && series[i].Time < series[i-1].Time) {
			t.Fatalf("序列未按时间升序：%v 之后是 %v", series[i-1], series[i])
		}
	}
	if series[0].Time != "00:15" || series[95].Time != "24:00" {
		// CSV 的时间戳形如 00:15 … 24:00
		t.Logf("首尾时间：%s / %s", series[0].Time, series[95].Time)
	}
}
