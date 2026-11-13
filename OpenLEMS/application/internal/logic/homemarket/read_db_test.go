package homemarket

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"s_db"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
)

// 端到端：真实 sqlite + 真实 SQL。
//
// 纯函数单测守不住三件事，只有跑真库才看得出来：
//  1. ON CONFLICT(price_date, hour) 真的幂等（重采覆盖而不是堆行 / 唯一键写错就抛错）；
//  2. 「某日无数据」必须返回 empty=true + latestDate，而不是让页面拿到空数组自己猜；
//  3. 地市 42 条按 period_type、city 升序，且峰值/低谷城市数各 21。
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE gd_home_24h (
			price_date VARCHAR(10) NOT NULL, hour INTEGER NOT NULL,
			power_mw REAL NOT NULL DEFAULT 0, ahead_price REAL NOT NULL DEFAULT 0,
			realtime_price REAL NOT NULL DEFAULT 0, source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (price_date, hour))`); err != nil {
		t.Fatalf("建 gd_home_24h 失败: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE gd_node_price (
			run_date VARCHAR(10) NOT NULL, city VARCHAR(32) NOT NULL,
			period_type INTEGER NOT NULL DEFAULT 1, price REAL NOT NULL DEFAULT 0,
			source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (run_date, city, period_type))`); err != nil {
		t.Fatalf("建 gd_node_price 失败: %v", err)
	}
	// run 表：采集状态用。建表语句与 s_db 的 DDL 保持一致（唯一键只有 target_date+dataset，
	// 不带 extractor_version —— 这是 C2 与 gd_pm_fetch_run 的关键差别）。
	if _, err := db.Exec(`
		CREATE TABLE gd_home_market_fetch_run (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_date VARCHAR(10) NOT NULL, dataset VARCHAR(16) NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT '', error_kind VARCHAR(16) NOT NULL DEFAULT '',
			rows_written INTEGER NOT NULL DEFAULT 0, rows_expected INTEGER NOT NULL DEFAULT 0,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			consecutive_empties INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			first_seen_at DATETIME DEFAULT CURRENT_TIMESTAMP, last_ok_at DATETIME,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (target_date, dataset))`); err != nil {
		t.Fatalf("建 gd_home_market_fetch_run 失败: %v", err)
	}
	db.Close()

	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })
	return db
}

func seed24h(t *testing.T, date string, hours int) {
	t.Helper()
	for h := 0; h < hours; h++ {
		_, err := g.DB().Exec(context.Background(), `
			INSERT INTO gd_home_24h (price_date, hour, power_mw, ahead_price, realtime_price, source)
			VALUES (?, ?, ?, ?, ?, ?)`,
			date, h, 60000.0+float64(h), 400.0+float64(h), 500.0+float64(h), SourceHome24h)
		if err != nil {
			t.Fatalf("插 24 点失败: %v", err)
		}
	}
}

func TestLoadHome24hEmptyDayReportsEmptyNotError(t *testing.T) {
	newTestDB(t)
	seed24h(t, "2026-11-07", 24)

	// 空日期 = 源站还没发布（实测 11-08 起 details=[]）。
	// 页面要显示「该日无数据」，所以必须是 empty=true 且带上最新日，而不是 error。
	day, err := LoadHome24h(context.Background(), "2026-11-08")
	if err != nil {
		t.Fatalf("空日期不该报错: %v", err)
	}
	if !day.Empty {
		t.Fatal("该日无数据时 empty 必须为 true")
	}
	if len(day.Points) != 0 {
		t.Fatalf("空日期的 points 应为空，实际 %d 条", len(day.Points))
	}
	if day.LatestDate != "2026-11-07" {
		t.Fatalf("latestDate = %q，期望 2026-11-07（页面靠它提示「最新到哪天」）", day.LatestDate)
	}
}

func TestLoadHome24hFullDay(t *testing.T) {
	newTestDB(t)
	seed24h(t, "2026-11-07", 24)

	day, err := LoadHome24h(context.Background(), "")
	if err != nil {
		t.Fatalf("LoadHome24h 失败: %v", err)
	}
	if day.Empty || len(day.Points) != 24 {
		t.Fatalf("期望 24 点，实际 empty=%v points=%d", day.Empty, len(day.Points))
	}
	// 按 hour 升序
	for i, p := range day.Points {
		if p.Hour != i {
			t.Fatalf("第 %d 点 hour=%d，期望升序 %d", i, p.Hour, i)
		}
	}
	if day.Points[0].AheadPrice != 400 || day.Points[23].AheadPrice != 423 {
		t.Fatalf("电价解析错: 首 %v 末 %v", day.Points[0].AheadPrice, day.Points[23].AheadPrice)
	}
	if day.Source != SourceHome24h {
		t.Fatalf("source = %q", day.Source)
	}
	if day.PointCount != 24 {
		t.Fatalf("pointCount = %d，期望 24", day.PointCount)
	}
	// 均值 = (400 + 423)/2 = 411.5
	if day.MeanAhead == nil || !near(*day.MeanAhead, 411.5) {
		t.Fatalf("均值 = %v，期望 411.5", day.MeanAhead)
	}
	if day.PeakPowerMw == nil || day.LowPowerMw == nil ||
		!near(*day.PeakPowerMw, 60023) || !near(*day.LowPowerMw, 60000) {
		t.Fatalf("负荷峰谷错: %v / %v", day.PeakPowerMw, day.LowPowerMw)
	}
}

// 缺点日：均值/峰谷不给值，但点数要说清楚 —— 17 个点算出的「全天均值」会被当结论用。
func TestLoadHome24hPartialDayHasNoAllDayStats(t *testing.T) {
	newTestDB(t)
	seed24h(t, "2026-11-07", 17)

	day, err := LoadHome24h(context.Background(), "2026-11-07")
	if err != nil {
		t.Fatalf("LoadHome24h 失败: %v", err)
	}
	if day.PointCount != 17 || len(day.Points) != 17 {
		t.Fatalf("pointCount = %d / points = %d，期望都是 17", day.PointCount, len(day.Points))
	}
	if day.MeanAhead != nil || day.MeanRealtime != nil || day.PeakPowerMw != nil || day.LowPowerMw != nil {
		t.Fatalf("17 点不该给全天统计：%+v / %+v / %+v / %+v",
			day.MeanAhead, day.MeanRealtime, day.PeakPowerMw, day.LowPowerMw)
	}
}

func TestLoadNodePrice42Rows(t *testing.T) {
	newTestDB(t)
	cities := []string{"东莞市", "广州市", "深圳市"}
	for _, c := range cities {
		for _, tp := range []int{1, 2} {
			_, err := g.DB().Exec(context.Background(), `
				INSERT INTO gd_node_price (run_date, city, period_type, price, source)
				VALUES (?, ?, ?, ?, ?)`, "2026-11-13", c, tp, 500.0, SourceNodePrice)
			if err != nil {
				t.Fatalf("插地市价失败: %v", err)
			}
		}
	}

	day, err := LoadNodePrice(context.Background(), "2026-11-13")
	if err != nil {
		t.Fatalf("LoadNodePrice 失败: %v", err)
	}
	if len(day.Points) != 6 {
		t.Fatalf("条数 = %d，期望 6", len(day.Points))
	}
	if day.PeakCities != 3 || day.ValleyCities != 3 {
		t.Fatalf("高峰/低谷城市数 = %d/%d，期望 3/3", day.PeakCities, day.ValleyCities)
	}
	// 按 period_type 升序：前 3 条是高峰
	if day.Points[0].PeriodType != 1 || day.Points[5].PeriodType != 2 {
		t.Fatalf("排序错: 首 %d 末 %d", day.Points[0].PeriodType, day.Points[5].PeriodType)
	}
}

func TestLoadNodePriceEmptyDay(t *testing.T) {
	newTestDB(t)
	day, err := LoadNodePrice(context.Background(), "2026-11-14")
	if err != nil {
		t.Fatalf("空日期不该报错: %v", err)
	}
	if !day.Empty {
		t.Fatal("该日无数据时 empty 必须为 true")
	}
	if day.LatestDate != "" {
		t.Fatalf("库里一条都没有时 latestDate 应为空，实际 %q", day.LatestDate)
	}
}

func TestUpsertHome24hIsIdempotent(t *testing.T) {
	newTestDB(t)
	ctx := context.Background()
	svc := s_db.GetHomeMarketService()

	first := []*s_db_model.SHome24hModel{{PriceDate: "2026-11-07", Hour: 0, AheadPrice: 400, RealtimePrice: 500, Source: SourceHome24h}}
	if _, err := svc.UpsertHome24h(ctx, first); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	second := []*s_db_model.SHome24hModel{{PriceDate: "2026-11-07", Hour: 0, AheadPrice: 999, RealtimePrice: 888, Source: SourceHome24h}}
	if _, err := svc.UpsertHome24h(ctx, second); err != nil {
		t.Fatalf("重写失败: %v", err)
	}

	rows, err := svc.ListHome24h(ctx, "2026-11-07")
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("唯一键失效：重采后 %d 行，期望 1 行", len(rows))
	}
	if !near(rows[0].AheadPrice, 999) {
		t.Fatalf("覆盖写没生效，ahead=%v 期望 999", rows[0].AheadPrice)
	}
}

// TestAgainstRealDB 用真实落库的数据跑一遍（GDPM_DB 指向生产库副本时用）。
func TestAgainstRealDB(t *testing.T) {
	path := os.Getenv("GDPM_DB")
	if path == "" {
		t.Skip("需要 GDPM_DB，跳过")
	}
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	h, err := LoadHome24h(context.Background(), "")
	if err != nil {
		t.Fatalf("LoadHome24h 失败: %v", err)
	}
	t.Logf("24 点：日期 %s 最新 %s 滞后 %d 天 点数 %d 均值 日前 %s 实时 %s",
		h.Date, h.LatestDate, h.LagDays, h.PointCount, fmtPtr(h.MeanAhead), fmtPtr(h.MeanRealtime))
	n, err := LoadNodePrice(context.Background(), "")
	if err != nil {
		t.Fatalf("LoadNodePrice 失败: %v", err)
	}
	t.Logf("地市：日期 %s 滞后 %d 天 条数 %d（高峰 %d / 低谷 %d）",
		n.Date, n.LagDays, len(n.Points), n.PeakCities, n.ValleyCities)
}

// fmtPtr 把可空的全天统计打印出来（缺点日就是 nil，不是 0）。
func fmtPtr(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *v)
}

func near(a, b float64) bool {
	d := a - b
	return d < 1e-6 && d > -1e-6
}
