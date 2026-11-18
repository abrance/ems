package spotmarket

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
)

// 端到端：真实 sqlite + 真实 SQL 过滤条件。
//
// 纯函数单测守不住两件事：区间过滤用的是 run_date 而不是 report_date（实时 T+2 会把
// 日期写错就取不到数），以及 void 行必须被 SQL 层排除。这两条只有跑真库才看得出来。
func TestLoadBoardAgainstSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE gd_spot_metric (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL, run_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL, scope VARCHAR(32) NOT NULL DEFAULT '',
			metric VARCHAR(32) NOT NULL, value REAL NOT NULL DEFAULT 0,
			unit VARCHAR(16) NOT NULL DEFAULT '', at_time VARCHAR(8) NOT NULL DEFAULT '',
			text_value VARCHAR(32) NOT NULL DEFAULT '', mom_pct REAL, share_pct REAL,
			source VARCHAR(32) NOT NULL DEFAULT '', conf REAL NOT NULL DEFAULT 0,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			origin VARCHAR(8) NOT NULL DEFAULT 'ocr', edited_by VARCHAR(64) NOT NULL DEFAULT '',
			edited_at DATETIME, created_at DATETIME, updated_at DATETIME,
			UNIQUE (report_date, run_date, dataset, scope, metric))`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	// 日前：report_date = run_date；实时：report_date = run_date + 2（T+2）
	rows := [][]any{
		{"2026-11-10", "2026-11-10", "day_ahead_trade", "发电侧", "发电侧日前加权平均电价", 400.0, "元/兆瓦时", "pm-api"},
		{"2026-11-11", "2026-11-11", "day_ahead_trade", "发电侧", "发电侧日前加权平均电价", 360.0, "元/兆瓦时", "pm-api"},
		{"2026-11-11", "2026-11-11", "day_ahead_trade", "发电侧", "发电侧日前总成交电量", 1500000.0, "MWh", "pm-api"},
		{"2026-11-12", "2026-11-10", "realtime_trade", "发电侧", "发电侧实时加权平均电价", 300.0, "元/兆瓦时", "pm-api"},
		// 09-16 的 OCR 旧值已被 pm-api 取代：origin=void，不能出现在看板里
		{"2026-09-16", "2026-09-16", "day_ahead_trade", "发电侧", "平均电价", 428.95, "元/兆瓦时", "void"},
		// 作废行日期还要比有效行新：latest 锚点必须忽略 void，否则默认区间右端会落到空日
		{"2026-11-15", "2026-11-15", "day_ahead_trade", "发电侧", "作废新值", 999.0, "元/兆瓦时", "void"},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			`INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,unit,origin)
			 VALUES (?,?,?,?,?,?,?,?)`, r...); err != nil {
			t.Fatalf("插行失败: %v", err)
		}
	}
	db.Close()

	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	// 还原全局配置，别影响同包其它用例
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	board, err := LoadBoard(context.Background(), "", 90)
	if err != nil {
		t.Fatalf("LoadBoard 失败: %v", err)
	}
	if board.Date != "2026-11-11" {
		t.Fatalf("最新运行日 = %q, want 2026-11-11", board.Date)
	}
	latest, err := LatestDate(context.Background())
	if err != nil {
		t.Fatalf("LatestDate 失败: %v", err)
	}
	if latest != "2026-11-11" {
		t.Fatalf("LatestDate = %q，期望 2026-11-11（void 行不算）", latest)
	}
	for _, s := range board.Daily {
		if s.Dataset == "day_ahead_trade" && s.Metric == "平均电价" {
			t.Fatalf("void 行不该进看板: %+v", s)
		}
	}
	// 只有 11-10 两个口径都有；11-11 没有实时值，不能拿 11-10 的实时价来顶
	if len(board.AheadRealtimeSpread) != 1 || !near(board.AheadRealtimeSpread[0].Value, 100) ||
		board.AheadRealtimeSpread[0].Period != "2026-11-10" {
		t.Fatalf("日前-实时价差应按 run_date 配对，实际 %+v", board.AheadRealtimeSpread)
	}
	var ahead *Series
	for i := range board.Daily {
		if board.Daily[i].Dataset == DatasetAhead && board.Daily[i].Metric == "发电侧日前加权平均电价" {
			ahead = &board.Daily[i]
		}
	}
	if ahead == nil || ahead.Days != 2 || ahead.Latest.Value != 360 {
		t.Fatalf("日前均价序列不对: %+v", ahead)
	}
	if ahead.MomPct == nil || !near(*ahead.MomPct, -10) {
		t.Fatalf("环比应为 -10%%，实际 %v", ahead.MomPct)
	}
	if len(board.Coverage) == 0 {
		t.Fatal("coverage 不该为空")
	}
}

// TestBoardAgainstRealDB 用真实落库的官方数据跑一遍看板，打印关键数字。
// 需要 GDPM_DB 指向一份已 --apply 过官方数据的 sqlite；CI 上跳过。
func TestBoardAgainstRealDB(t *testing.T) {
	path := os.Getenv("GDPM_DB")
	if path == "" {
		t.Skip("需要 GDPM_DB，跳过")
	}
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	board, err := LoadBoard(context.Background(), "2026-11-11", 90)
	if err != nil {
		t.Fatalf("LoadBoard 失败: %v", err)
	}
	t.Logf("最新运行日 %s · 日频序列 %d 条 · 月频年频 %d 条 · 日前-实时价差点 %d 个",
		board.Date, len(board.Daily), len(board.Periodic), len(board.AheadRealtimeSpread))
	for _, s := range board.Daily {
		t.Logf("  %-16s %-6s %-22s 最新 %10.2f %-8s 环比 %v 同比 %v 峰谷 %.2f",
			s.Dataset, s.Scope, s.Metric, s.Latest.Value, s.Unit, fmtPtr(s.MomPct), fmtPtr(s.YoYPct), s.Spread)
	}
	if len(board.AheadRealtimeSpread) > 0 {
		last := board.AheadRealtimeSpread[len(board.AheadRealtimeSpread)-1]
		t.Logf("  日前-实时价差 最新 %s = %.2f（均值 %.2f）", last.Period, last.Value, board.SpreadMean)
	}
	for _, c := range board.Coverage {
		t.Logf("  覆盖 %-20s %s ~ %s（%d 期，缺 %d 天）", c.Dataset, c.From, c.To, c.Periods, len(c.Missing))
	}
}

func fmtPtr(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", *v)
}
