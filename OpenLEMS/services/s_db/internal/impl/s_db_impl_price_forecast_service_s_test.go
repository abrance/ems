package impl

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// GeneratedDates 只该返回「至少有一条能用于复盘」的生成日。
//
// 线上实测的坏形态（2026-11-13 快照）：ahead/day 有 9 个生成日，其中 6 个（10-07 ~ 11-08）
// 是 CSV 时代的历史回填，每个生成日 5 行、target 全部早于 generated_on，复盘页
// IsBackfilledForecast 会把它们逐行剔除。但 GeneratedDates 只做 DISTINCT，全返回了，
// 于是「预测留痕自 2026-10-07 起积累 / 首个可配对交易日约在 2026-10-10」——
// 两个都是比今天早一个多月的日期，用户读到的是「早过了为什么还是空的」。
//
// 判据必须与复盘页剔除回填那条同源：generated_on > target 的日期部分 = 回填。

// DDL 与 s_db_init_f.go 里的 gd_price_forecast 保持一致（列、唯一键都要有，
// 否则用例跑在一个「比线上宽松」的表上，形状错了也发现不了）。
const priceForecastTestDDL = `
	CREATE TABLE gd_price_forecast (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		generated_on TEXT NOT NULL,
		market TEXT NOT NULL,
		grain TEXT NOT NULL,
		target_time TEXT NOT NULL,
		point REAL NOT NULL DEFAULT 0,
		q10 REAL NOT NULL DEFAULT 0,
		q50 REAL NOT NULL DEFAULT 0,
		q90 REAL NOT NULL DEFAULT 0,
		model TEXT NOT NULL DEFAULT '',
		unit TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (generated_on, market, grain, target_time)
	);
`

func newPriceForecastTestDB(t *testing.T) context.Context {
	t.Helper()
	path := filepath.Join(t.TempDir(), "price-forecast.sqlite3")
	if err := gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	}); err != nil {
		t.Fatalf("设置数据库配置失败: %v", err)
	}
	t.Cleanup(func() { _ = gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, priceForecastTestDDL); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return ctx
}

// seedForecast 写一条留痕。generatedOn 只有日期，targetTime 可为日期或带时刻。
func seedForecast(t *testing.T, ctx context.Context, generatedOn, market, grain, targetTime string) {
	t.Helper()
	if _, err := g.DB().Exec(ctx, `
		INSERT INTO gd_price_forecast (generated_on, market, grain, target_time, point)
		VALUES (?, ?, ?, ?, 400)`, generatedOn, market, grain, targetTime); err != nil {
		t.Fatalf("造留痕失败 (%s → %s): %v", generatedOn, targetTime, err)
	}
}

func generatedDatesOf(t *testing.T, ctx context.Context, market, grain string) []string {
	t.Helper()
	got, err := GetPriceForecastService().GeneratedDates(ctx, market, grain)
	if err != nil {
		t.Fatalf("GeneratedDates(%q, %q) 失败: %v", market, grain, err)
	}
	return got
}

// ① 整批回填的生成日必须被剔掉，返回的第一个必须是第一个可用日。
// 这一条直接对应线上那个「自 2026-10-07 起积累」的观感缺陷。
func TestGeneratedDatesSkipsFullyBackfilledDays(t *testing.T) {
	ctx := newPriceForecastTestDB(t)

	// 回填：生成日 11-01，5 行的 target 全在 10-27 ~ 10-31（早于生成日）。
	for _, target := range []string{"2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31"} {
		seedForecast(t, ctx, "2026-11-01", "ahead", "day", target)
	}
	// 可用：11-11 预测 11-12、11-12 预测 11-13。
	seedForecast(t, ctx, "2026-11-11", "ahead", "day", "2026-11-12")
	seedForecast(t, ctx, "2026-11-12", "ahead", "day", "2026-11-13")

	got := generatedDatesOf(t, ctx, "ahead", "day")
	want := []string{"2026-11-11", "2026-11-12"}
	if len(got) != len(want) {
		t.Fatalf("生成日 = %v，期望 %v（整批回填的 2026-11-01 必须被剔除）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("生成日 = %v，期望 %v（升序且不含回填日）", got, want)
		}
	}
	if got[0] != "2026-11-11" {
		t.Fatalf("首个生成日 = %q，期望 2026-11-11 —— 前端就是拿 [0] 印「自 X 起积累」和算首个可配对日", got[0])
	}
}

// ② intraday 同日不算回填：当天更晚时刻的点复盘页是保留的。
// 判据写成严格 > 时这条必须变红 —— 那会把所有 intraday 留痕日一起剔光。
func TestGeneratedDatesKeepsSameDayIntraday(t *testing.T) {
	ctx := newPriceForecastTestDB(t)

	// 生成日 2026-11-13，target 是当天 14:00 / 14:15 / 14:30。
	// 当天已过时段（更早的时刻）技术上也会留下，但那不影响本日可用性。
	for _, target := range []string{"2026-11-13 14:00", "2026-11-13 14:15", "2026-11-13 14:30"} {
		seedForecast(t, ctx, "2026-11-13", "ahead", "intraday", target)
	}
	// 再放一个整批回填日，确保不是「只要有一条就全留」。
	seedForecast(t, ctx, "2026-11-14", "ahead", "intraday", "2026-11-08 10:00")

	got := generatedDatesOf(t, ctx, "ahead", "intraday")
	if len(got) != 1 || got[0] != "2026-11-13" {
		t.Fatalf("生成日 = %v，期望 [2026-11-13]：同日更晚时段的点必须让本日算可用，整批回填日仍要剔", got)
	}
}

// ③ 空表、以及全部都是回填 → 返回空切片而不是 nil。
// ReviewView 靠 `generatedDates[0] || '—'` 兜底，前端对 null 不做防御，nil 会让页面报错。
func TestGeneratedDatesEmptyWhenNoUsableDay(t *testing.T) {
	ctx := newPriceForecastTestDB(t)

	got := generatedDatesOf(t, ctx, "ahead", "day")
	if got == nil {
		t.Fatal("空表时返回了 nil，期望空切片（前端按数组用）")
	}
	if len(got) != 0 {
		t.Fatalf("空表时返回 %v，期望空切片", got)
	}

	// 只有回填行。
	seedForecast(t, ctx, "2026-11-05", "ahead", "day", "2026-11-01")
	seedForecast(t, ctx, "2026-11-05", "ahead", "day", "2026-11-02")

	got = generatedDatesOf(t, ctx, "ahead", "day")
	if got == nil {
		t.Fatal("全部回填时返回了 nil，期望空切片")
	}
	if len(got) != 0 {
		t.Fatalf("全部回填时返回 %v，期望空切片", got)
	}
}

// ④ market / grain 过滤不能因为换了分组查询就丢掉。
func TestGeneratedDatesFiltersByMarketAndGrain(t *testing.T) {
	ctx := newPriceForecastTestDB(t)

	seedForecast(t, ctx, "2026-11-11", "ahead", "day", "2026-11-12")
	seedForecast(t, ctx, "2026-11-12", "real", "day", "2026-11-13")
	seedForecast(t, ctx, "2026-11-13", "ahead", "intraday", "2026-11-13 18:00")

	if got := generatedDatesOf(t, ctx, "ahead", "day"); len(got) != 1 || got[0] != "2026-11-11" {
		t.Fatalf("ahead/day = %v，期望 [2026-11-11]", got)
	}
	if got := generatedDatesOf(t, ctx, "real", "day"); len(got) != 1 || got[0] != "2026-11-12" {
		t.Fatalf("real/day = %v，期望 [2026-11-12]", got)
	}
	if got := generatedDatesOf(t, ctx, "ahead", "intraday"); len(got) != 1 || got[0] != "2026-11-13" {
		t.Fatalf("ahead/intraday = %v，期望 [2026-11-13]", got)
	}
	// 不传 market/grain 时不过滤（HistoryView 各市场分别取，但接口允许空）。
	if got := generatedDatesOf(t, ctx, "", ""); len(got) != 3 {
		t.Fatalf("不传 market/grain = %v，期望 3 个生成日", got)
	}
}
