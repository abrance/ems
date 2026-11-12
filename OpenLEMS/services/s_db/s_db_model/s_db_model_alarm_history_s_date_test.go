package s_db_model

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// 历史告警日期筛选此前是 DATE(trigger_at) = 'yyyy-MM-dd'：
// ① 列上包函数，走不了 idx_alarm_history_trigger_at；
// ② DATE() 按 UTC 换算，而列表页 AlarmsView.vue:257 直接渲染 trigger_at 原始字符串，
// 于是「列表显示 7 条、按那天一筛返回 0 条」。
// 现在改为墙钟文本半开区间，与显示口径一致。下面用真 sqlite + 真 SQL 钉住。

// realAlarmHistoryRows 是真库 OpenLEMS/application/out/db.sqlite3 的 alarm_history 全量 24 行
// （2026-11-12 直查）。id=1 是 +00:00，其余 23 行 +08:00，同一列混着两种偏移。
var realAlarmHistoryRows = []struct {
	id        int
	triggerAt string
}{
	{1, "2026-10-06 02:37:05.266992859+00:00"},
	{2, "2026-10-06 10:58:18.525955305+08:00"},
	{3, "2026-10-08 00:31:44.525497338+08:00"},
	{4, "2026-10-08 10:32:52.095016345+08:00"},
	{5, "2026-10-08 10:32:52.095452787+08:00"},
	{6, "2026-10-08 10:32:52.105543749+08:00"},
	{7, "2026-10-08 10:32:52.096817058+08:00"},
	{8, "2026-10-08 10:32:52.139675053+08:00"},
	{9, "2026-10-08 10:32:52.113918657+08:00"},
	{10, "2026-10-08 10:32:52.114153881+08:00"},
	{11, "2026-11-08 12:19:18.327195427+08:00"},
	{12, "2026-11-08 12:19:18.327403774+08:00"},
	{13, "2026-11-08 12:19:18.327609405+08:00"},
	{14, "2026-11-08 12:19:18.327745926+08:00"},
	{15, "2026-11-08 12:19:18.435163083+08:00"},
	{16, "2026-11-08 12:19:18.439619634+08:00"},
	{17, "2026-11-08 12:19:18.512094756+08:00"},
	{18, "2026-11-10 04:15:35.638062001+08:00"},
	{19, "2026-11-10 04:15:35.728417948+08:00"},
	{20, "2026-11-10 04:15:35.729023456+08:00"},
	{21, "2026-11-10 04:15:35.729328796+08:00"},
	{22, "2026-11-10 04:15:35.824345081+08:00"},
	{23, "2026-11-10 04:15:35.730266941+08:00"},
	{24, "2026-11-10 04:15:36.028311908+08:00"},
}

// newAlarmHistoryTestDB 建一张与 s_db_init_f.go 同构的 alarm_history（含 idx_alarm_history_trigger_at），
// 插入给定的 trigger_at 文本行，返回已绑定的 ctx。
func newAlarmHistoryTestDB(t *testing.T, rows []string) context.Context {
	t.Helper()

	path := filepath.Join(t.TempDir(), "db.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, `
		CREATE TABLE alarm_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id VARCHAR(255) NOT NULL,
			source_device_id VARCHAR(255) NOT NULL,
			point VARCHAR(255) NOT NULL,
			point_name VARCHAR(255) NOT NULL,
			level VARCHAR(255) NOT NULL,
			detail TEXT,
			trigger_at DATETIME NOT NULL,
			clear_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := g.DB().Exec(ctx,
		`CREATE INDEX idx_alarm_history_trigger_at ON alarm_history (trigger_at DESC)`); err != nil {
		t.Fatalf("建索引失败: %v", err)
	}
	for i, triggerAt := range rows {
		if _, err := g.DB().Exec(ctx,
			`INSERT INTO alarm_history (id,device_id,source_device_id,point,point_name,level,detail,trigger_at)
			 VALUES (?,?,?,?,?,?,?,?)`,
			i+1, "dev-1", "dev-1", "Voltage", "电压过高", "Warn", "-", triggerAt); err != nil {
			t.Fatalf("插行失败 (%s): %v", triggerAt, err)
		}
	}
	return ctx
}

func realAlarmHistoryTexts() []string {
	out := make([]string, 0, len(realAlarmHistoryRows))
	for _, r := range realAlarmHistoryRows {
		out = append(out, r.triggerAt)
	}
	return out
}

func getPageByDate(t *testing.T, ctx context.Context, date string) []*SAlarmHistoryModel {
	t.Helper()
	records, total, err := (&SAlarmHistoryModel{}).GetPage(ctx, 1, 20, map[string]interface{}{"date": date})
	if err != nil {
		t.Fatalf("按日期 %q 筛选报错: %v", date, err)
	}
	if total != len(records) {
		t.Fatalf("日期 %q: total=%d 与返回行数 %d 不一致", date, total, len(records))
	}
	return records
}

// 验收点 1：列表页显示 2026-11-10 有 7 条，筛选必须也返回这 7 条（id 18..24）。
// 旧实现 DATE(trigger_at)='2026-11-10' 返回 0 条，正是用户看到的「筛完就空了」。
func TestAlarmHistoryGetPageDateMatchesDisplayedWallClock(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t, realAlarmHistoryTexts())

	records := getPageByDate(t, ctx, "2026-11-10")
	if len(records) != 7 {
		t.Fatalf("2026-11-10 应返回 7 行（与列表显示一致），实际 %d 行", len(records))
	}
	for _, r := range records {
		if r.Id < 18 || r.Id > 24 {
			t.Fatalf("2026-11-10 命中行 id=%d 不在 18..24 内", r.Id)
		}
	}
}

// 验收点 2：半开区间边界 —— 当日 00:00:00.000 与 23:59:59.999 命中，次日 00:00:00 不命中。
func TestAlarmHistoryGetPageDateHalfOpenBoundaries(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t, []string{
		"2026-11-09 23:59:59.999+08:00",
		"2026-11-10 00:00:00.000+08:00",
		"2026-11-10 23:59:59.999+08:00",
		"2026-11-11 00:00:00.000+08:00",
	})

	records := getPageByDate(t, ctx, "2026-11-10")
	if len(records) != 2 {
		t.Fatalf("2026-11-10 应命中首尾两行，实际 %d 行: %v", len(records), ids(records))
	}
	got := map[int]bool{}
	for _, r := range records {
		got[r.Id] = true
	}
	if !got[2] || !got[3] {
		t.Fatalf("应命中 id 2(00:00:00.000) 与 id 3(23:59:59.999)，实际 %v", ids(records))
	}
	if got[1] || got[4] {
		t.Fatalf("不应命中前一日 23:59:59.999 或次日 00:00:00，实际 %v", ids(records))
	}
}

// 验收点 3：跨月、跨年不串日（d+1 用 AddDate 算，不是字符串加一天）。
func TestAlarmHistoryGetPageDateCrossMonthAndYear(t *testing.T) {
	cases := []struct {
		date    string
		rows    []string
		wantIDs []int
	}{
		{
			date: "2026-01-31",
			rows: []string{
				"2026-01-30 23:59:59+08:00",
				"2026-01-31 00:00:00+08:00",
				"2026-01-31 23:59:59+08:00",
				"2026-02-01 00:00:00+08:00",
			},
			wantIDs: []int{2, 3},
		},
		{
			date: "2026-12-31",
			rows: []string{
				"2026-12-30 23:59:59+08:00",
				"2026-12-31 00:00:00+08:00",
				"2026-12-31 23:59:59+08:00",
				"2027-01-01 00:00:00+08:00",
			},
			wantIDs: []int{2, 3},
		},
	}

	for _, c := range cases {
		ctx := newAlarmHistoryTestDB(t, c.rows)
		records := getPageByDate(t, ctx, c.date)
		if len(records) != len(c.wantIDs) {
			t.Fatalf("%s 应命中 %d 行，实际 %d 行: %v", c.date, len(c.wantIDs), len(records), ids(records))
		}
		got := map[int]bool{}
		for _, r := range records {
			got[r.Id] = true
		}
		for _, want := range c.wantIDs {
			if !got[want] {
				t.Fatalf("%s 应命中 id=%d，实际 %v", c.date, want, ids(records))
			}
		}
	}
}

// 验收点 4：非 yyyy-MM-dd 的输入一律 0 行且不报错。
// 旧 DATE() 比较对这些串同样返回 0 行，不能退化成忽略过滤返回全部。
func TestAlarmHistoryGetPageDateRejectsNonDateInput(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t, realAlarmHistoryTexts())

	for _, bad := range []string{"2026-11-10T00:00:00Z", "garbage", "2026-11-10 00:00:00", "2026-13-01", "2026-1-1"} {
		records, total, err := (&SAlarmHistoryModel{}).GetPage(ctx, 1, 20, map[string]interface{}{"date": bad})
		if err != nil {
			t.Fatalf("日期 %q 不应报错，实际: %v", bad, err)
		}
		if total != 0 || len(records) != 0 {
			t.Fatalf("日期 %q 应返回 0 行，实际 total=%d len=%d", bad, total, len(records))
		}
	}
}

// 验收点 5：+00:00 行按墙钟日期能筛到，且与该行返回的 triggerAt 日期部分一致。
// 混偏移是这条筛选最容易被改坏的地方，必须留一条回归。
func TestAlarmHistoryGetPageDateKeepsWallClockForUTCOffsetRows(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t, realAlarmHistoryTexts())

	records := getPageByDate(t, ctx, "2026-10-06")
	if len(records) != 2 {
		t.Fatalf("2026-10-06 应命中 2 行（id 1 为 +00:00、id 2 为 +08:00），实际 %d 行: %v", len(records), ids(records))
	}
	for _, r := range records {
		if r.TriggerAt == nil {
			t.Fatalf("id=%d 的 triggerAt 解析为空", r.Id)
		}
		if got := r.TriggerAt.Format("2006-01-02"); got != "2026-10-06" {
			t.Fatalf("id=%d 返回的 triggerAt=%s，日期部分 %s 与筛选日 2026-10-06 不一致",
				r.Id, r.TriggerAt.Format("2006-01-02T15:04:05Z07:00"), got)
		}
	}
}

func ids(records []*SAlarmHistoryModel) []int {
	out := make([]int, 0, len(records))
	for _, r := range records {
		out = append(out, r.Id)
	}
	return out
}
