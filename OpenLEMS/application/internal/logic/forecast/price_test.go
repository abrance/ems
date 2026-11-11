package forecast

import (
	"testing"
	"time"

	"application/internal/logic/spotreport"
	"s_db/s_db_model"
)

func metric(runDate, dataset, scope, m, source string, value float64, unit string) *s_db_model.SSpotMetricModel {
	return &s_db_model.SSpotMetricModel{
		RunDate: runDate, Dataset: dataset, Scope: scope, Metric: m,
		Source: source, Value: value, Unit: unit,
	}
}

func TestResolvePriceSpec(t *testing.T) {
	ahead, err := resolvePriceSpec("")
	if err != nil {
		t.Fatalf("空 market 应默认日前: %v", err)
	}
	if ahead.Market != "ahead" || ahead.Dataset != spotreport.DatasetTrade || ahead.Source != spotreport.SourceTrade {
		t.Errorf("日前口径错误: %+v", ahead)
	}
	real, err := resolvePriceSpec("real")
	if err != nil {
		t.Fatalf("real 应可用: %v", err)
	}
	if real.Dataset != spotreport.DatasetRealtime || real.Source != spotreport.SourceRealtime {
		t.Errorf("实时口径错误: %+v", real)
	}
	if _, err := resolvePriceSpec("bogus"); err == nil {
		t.Error("非法 market 应报错")
	}
}

// 官方直采（pm-api）用的是源站原名，与 OCR 日报的「平均电价」不同名。
// 切源后如果只认旧名，序列会整个变空 —— 表现是「数据不足 7 天」，很容易误判成数据没入库。
func TestBuildDailySeriesAcceptsOfficialMetricNames(t *testing.T) {
	for market, official := range map[string]string{
		"ahead": metricOfficialAhead,
		"real":  metricOfficialReal,
	} {
		spec, err := resolvePriceSpec(market)
		if err != nil {
			t.Fatalf("resolvePriceSpec(%s): %v", market, err)
		}
		models := []*s_db_model.SSpotMetricModel{
			metric("2026-11-09", spec.Dataset, priceScope, official, "gdpm-api", 400, "元/兆瓦时"),
			metric("2026-11-10", spec.Dataset, priceScope, official, "gdpm-api", 410, "元/兆瓦时"),
			// 同名的电量指标不能被当成电价
			metric("2026-11-10", spec.Dataset, priceScope, "发电侧日前总成交电量", "gdpm-api", 999, "MWh"),
		}
		dates, values, unit := buildDailySeries(models, spec)
		if len(dates) != 2 || len(values) != 2 {
			t.Fatalf("%s: 点数 = %d/%d, want 2/2", market, len(dates), len(values))
		}
		if values[0] != 400 || values[1] != 410 {
			t.Fatalf("%s: 值不对 %v", market, values)
		}
		if unit != "元/兆瓦时" {
			t.Fatalf("%s: 单位不对 %q", market, unit)
		}
	}
}

func TestBuildDailySeries(t *testing.T) {
	spec, _ := resolvePriceSpec("ahead")
	models := []*s_db_model.SSpotMetricModel{
		metric("2026-09-21", spotreport.DatasetTrade, priceScope, priceMetric, spotreport.SourceTrade, 400.5, "元/MWh"),
		// 同一天另有滞后来源（表2(一)）：应被 表1(二) 压掉
		metric("2026-09-21", spotreport.DatasetTrade, priceScope, priceMetric, spotreport.SourceTradeLag, 999, "元/MWh"),
		metric("2026-09-19", spotreport.DatasetTrade, priceScope, priceMetric, spotreport.SourceTrade, 380.0, "元/MWh"),
		// 非目标数据集/对象/指标，应被忽略
		metric("2026-09-20", spotreport.DatasetRealtime, priceScope, priceMetric, spotreport.SourceRealtime, 500, "元/MWh"),
		metric("2026-09-20", spotreport.DatasetTrade, "燃煤", priceMetric, spotreport.SourceTrade, 500, "元/MWh"),
		metric("2026-09-20", spotreport.DatasetTrade, priceScope, spotreport.MetricHighPrice, spotreport.SourceTrade, 500, "元/MWh"),
	}

	dates, values, unit := buildDailySeries(models, spec)
	if len(dates) != 2 || len(values) != 2 {
		t.Fatalf("点数 = %d/%d, want 2/2（dates=%v values=%v）", len(dates), len(values), dates, values)
	}
	if dates[0] != "2026-09-19" || dates[1] != "2026-09-21" {
		t.Errorf("应按运行日升序: %v", dates)
	}
	if values[1] != 400.5 {
		t.Errorf("同一天应优先 表1(二)，got %v", values[1])
	}
	if unit != "元/MWh" {
		t.Errorf("单位 = %q", unit)
	}
}

func TestBuildDailySeriesFallbackSource(t *testing.T) {
	spec, _ := resolvePriceSpec("ahead")
	// 只有滞后来源时应该照用，而不是丢点
	models := []*s_db_model.SSpotMetricModel{
		metric("2026-09-19", spotreport.DatasetTrade, priceScope, priceMetric, spotreport.SourceTradeLag, 380.0, "元/MWh"),
	}
	dates, values, _ := buildDailySeries(models, spec)
	if len(dates) != 1 || values[0] != 380.0 {
		t.Errorf("缺失首选来源时应回落: dates=%v values=%v", dates, values)
	}
}

func TestNextDates(t *testing.T) {
	got, err := nextDates("2026-09-21", 3, priceLocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"2026-09-22", "2026-09-23", "2026-09-24"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 天 = %s, want %s", i, got[i], want[i])
		}
	}
	// 跨月
	got, err = nextDates("2026-09-30", 1, priceLocation())
	if err != nil || got[0] != "2026-10-01" {
		t.Errorf("跨月错误: %v %v", got, err)
	}
	if _, err := nextDates("bad", 1, priceLocation()); err == nil {
		t.Error("非法运行日应报错")
	}
}

func TestDaysBetween(t *testing.T) {
	if got := daysBetween("2026-09-19", "2026-09-21"); got != 3 {
		t.Errorf("含首含尾应为 3，got %d", got)
	}
	if got := daysBetween("2026-09-21", "2026-09-21"); got != 1 {
		t.Errorf("同一天应为 1，got %d", got)
	}
	if got := daysBetween("bad", "2026-09-21"); got != 0 {
		t.Errorf("非法输入应为 0，got %d", got)
	}
}

func TestParsePriceQuantiles(t *testing.T) {
	got, err := ParsePriceQuantiles("0.1, 0.5,0.9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0] != 0.1 || got[2] != 0.9 {
		t.Errorf("解析结果错误: %v", got)
	}
	if q, err := ParsePriceQuantiles("  "); err != nil || q != nil {
		t.Errorf("空串应返回 nil: %v %v", q, err)
	}
	if _, err := ParsePriceQuantiles("0.5,x"); err == nil {
		t.Error("非法分位点应报错")
	}
	if _, err := ParsePriceQuantiles("1.5"); err == nil {
		t.Error("超出 (0,1) 应报错")
	}
}

func TestPriceLocation(t *testing.T) {
	loc := priceLocation()
	now := time.Now().In(loc)
	if now.Format("2006-01-02") == "" {
		t.Error("时区不可用")
	}
}
