package forecast

import (
	"context"
	"strings"
	"testing"
	"time"

	"s_db/s_db_model"
)

// 数据源与粒度必须一一对应：任何一边给错都要报错，不能靠隐式回落到日频。
func TestParsePriceSource(t *testing.T) {
	ok := []struct{ source, grain, wantSource, wantGrain string }{
		{"", "", s_db_model.PriceSourceSpotMetric, s_db_model.ForecastGrainDay},
		{"", "day", s_db_model.PriceSourceSpotMetric, s_db_model.ForecastGrainDay},
		{"", "hour", s_db_model.PriceSourceHome24h, s_db_model.ForecastGrainHour},
		{"", "hourly", s_db_model.PriceSourceHome24h, s_db_model.ForecastGrainHour},
		{"", "24", s_db_model.PriceSourceHome24h, s_db_model.ForecastGrainHour},
		{"", "intraday", s_db_model.PriceSourceGdMetricCSV, s_db_model.ForecastGrainIntraday},
		{"home24h", "", s_db_model.PriceSourceHome24h, s_db_model.ForecastGrainHour},
		{"HOME24H", "", s_db_model.PriceSourceHome24h, s_db_model.ForecastGrainHour},
		{"spot_metric", "day", s_db_model.PriceSourceSpotMetric, s_db_model.ForecastGrainDay},
		{"home24h", "hour", s_db_model.PriceSourceHome24h, s_db_model.ForecastGrainHour},
		{"gd_metric_csv", "intraday", s_db_model.PriceSourceGdMetricCSV, s_db_model.ForecastGrainIntraday},
	}
	for _, c := range ok {
		gotSource, gotGrain, err := ParsePriceSource(c.source, c.grain)
		if err != nil {
			t.Errorf("(%q,%q) 应可解析: %v", c.source, c.grain, err)
			continue
		}
		if gotSource != c.wantSource || gotGrain != c.wantGrain {
			t.Errorf("(%q,%q) → (%s,%s)，期望 (%s,%s)",
				c.source, c.grain, gotSource, gotGrain, c.wantSource, c.wantGrain)
		}
	}

	bad := []struct{ source, grain string }{
		{"home24h", "day"},        // 数据源与粒度不匹配
		{"spot_metric", "hour"},   // 同上
		{"gd_metric_csv", "hour"}, // 同上
		{"bogus", ""},             // 未知数据源
		{"", "weekly"},            // 未知粒度
		{"bogus", "day"},          // 两个都错
	}
	for _, c := range bad {
		if _, _, err := ParsePriceSource(c.source, c.grain); err == nil {
			t.Errorf("(%q,%q) 应报错（不能回落默认）", c.source, c.grain)
		}
	}
}

// 不匹配时的报错要列出该数据源允许的粒度，否则用户只能猜。
func TestParsePriceSourceErrorNamesAllowedGrain(t *testing.T) {
	_, _, err := ParsePriceSource("home24h", "day")
	if err == nil {
		t.Fatal("应报错")
	}
	if !strings.Contains(err.Error(), "hour") {
		t.Errorf("错误信息应列出允许的粒度，实际 %v", err)
	}
}

// 分派必须命中正确的 loader：三路数据源的锚点规则完全不同，走错一路出的数字就是错的。
func TestLoadPriceAnyDispatchesBySource(t *testing.T) {
	orig := priceLoaders
	defer func() { priceLoaders = orig }()

	hits := map[string]int{}
	makeLoader := func(source string) func(context.Context, PriceRequest) (*TPriceForecastResult, error) {
		return func(_ context.Context, req PriceRequest) (*TPriceForecastResult, error) {
			hits[source]++
			// Points 为空 → recordForecastPoints 直接返回 0，不碰数据库
			return &TPriceForecastResult{Market: "ahead", Grain: req.Grain}, nil
		}
	}
	priceLoaders = map[string]func(context.Context, PriceRequest) (*TPriceForecastResult, error){
		s_db_model.PriceSourceSpotMetric:  makeLoader(s_db_model.PriceSourceSpotMetric),
		s_db_model.PriceSourceHome24h:     makeLoader(s_db_model.PriceSourceHome24h),
		s_db_model.PriceSourceGdMetricCSV: makeLoader(s_db_model.PriceSourceGdMetricCSV),
	}

	cases := []struct{ source, grain, want string }{
		{"", "day", s_db_model.PriceSourceSpotMetric},
		{"", "hour", s_db_model.PriceSourceHome24h},
		{"", "intraday", s_db_model.PriceSourceGdMetricCSV},
		{"home24h", "", s_db_model.PriceSourceHome24h},
	}
	for _, c := range cases {
		hits = map[string]int{}
		got, err := LoadPriceAny(context.Background(), PriceRequest{Source: c.source, Grain: c.grain})
		if err != nil {
			t.Fatalf("(%q,%q): %v", c.source, c.grain, err)
		}
		if got.Source != c.want {
			t.Errorf("(%q,%q) 回显 source = %s，期望 %s", c.source, c.grain, got.Source, c.want)
		}
		if len(hits) != 1 || hits[c.want] != 1 {
			t.Errorf("(%q,%q) 命中 %v，期望只有 %s", c.source, c.grain, hits, c.want)
		}
	}
}

// 参数不合法时不能走到任何 loader（否则错误的数据源会被当成合法请求执行）。
func TestLoadPriceAnyRejectsBadSource(t *testing.T) {
	orig := priceLoaders
	defer func() { priceLoaders = orig }()
	called := false
	priceLoaders = map[string]func(context.Context, PriceRequest) (*TPriceForecastResult, error){
		s_db_model.PriceSourceSpotMetric: func(context.Context, PriceRequest) (*TPriceForecastResult, error) {
			called = true
			return &TPriceForecastResult{}, nil
		},
	}
	if _, err := LoadPriceAny(context.Background(), PriceRequest{Source: "home24h", Grain: "day"}); err == nil {
		t.Fatal("不匹配的 source/grain 应报错")
	}
	if called {
		t.Error("参数非法时不应调用 loader")
	}
}

// 小时留痕只能由每日任务写：页面请求若也写，会按唯一键覆盖掉 00:05 那批规范样本，
// 算法对比的「同锚点」前提就被污染了。day 仍然照写（复盘页的「当时预测」腿靠它）。
func TestLoadPriceAnyDoesNotRecordHourRows(t *testing.T) {
	setupForecastTable(t)
	orig := priceLoaders
	defer func() { priceLoaders = orig }()

	day := time.Date(2026, 11, 13, 0, 0, 0, 0, priceLocation())
	mk := func(grain string, points int) func(context.Context, PriceRequest) (*TPriceForecastResult, error) {
		return func(_ context.Context, req PriceRequest) (*TPriceForecastResult, error) {
			res := &TPriceForecastResult{
				Market:      "ahead",
				Grain:       grain,
				GeneratedOn: "2026-11-07",
				Model:       "stub",
				Unit:        "元/MWh",
			}
			for h := 0; h < points; h++ {
				res.Points = append(res.Points, TPriceForecastPoint{
					TargetTime: day.Add(time.Duration(h) * time.Hour),
					Point:      100 + float64(h),
				})
			}
			return res, nil
		}
	}
	priceLoaders = map[string]func(context.Context, PriceRequest) (*TPriceForecastResult, error){
		s_db_model.PriceSourceHome24h:    mk(s_db_model.ForecastGrainHour, 24),
		s_db_model.PriceSourceSpotMetric: mk(s_db_model.ForecastGrainDay, 1),
	}

	if _, err := LoadPriceAny(context.Background(), PriceRequest{
		Source: s_db_model.PriceSourceHome24h, Grain: s_db_model.ForecastGrainHour,
	}); err != nil {
		t.Fatalf("hour: %v", err)
	}
	if rows := readForecastRows(t); len(rows) != 0 {
		t.Errorf("hour 页面请求写了 %d 行留痕，want 0（唯一来源是每日任务）", len(rows))
	}

	if _, err := LoadPriceAny(context.Background(), PriceRequest{
		Source: s_db_model.PriceSourceSpotMetric, Grain: s_db_model.ForecastGrainDay,
	}); err != nil {
		t.Fatalf("day: %v", err)
	}
	if rows := readForecastRows(t); len(rows) != 1 {
		t.Errorf("day 页面请求写了 %d 行留痕，want 1", len(rows))
	}
}
