package impl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"common/c_base"
	"common/c_enum"
	"s_db/s_db_model"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// alarm_history 保留期从「按天」改成「全表 1000 条」，靠每次写入后按自增 id 删旧行。
// 这批用例钉住四件事：删的边界（少一条不能全清）、单条 DELETE 能一次清干净积压、
// 判新旧只看 id 不看 trigger_at 时区、以及清理失败不影响插入结果。

const alarmHistoryTestDDL = `
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
	);
	CREATE INDEX idx_alarm_history_trigger_at ON alarm_history(trigger_at DESC);
	CREATE INDEX idx_alarm_history_device_trigger ON alarm_history(device_id, trigger_at DESC);
`

// newAlarmHistoryTestDB 建一个临时 sqlite 库并建出与 Init() 一致的表与索引。
func newAlarmHistoryTestDB(t testing.TB) context.Context {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, alarmHistoryTestDDL); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return ctx
}

func testAlarmPoint() c_base.IPoint {
	return &c_base.SPoint{Key: "Voltage", Name: "电压过高"}
}

// seedAlarmHistory 直接走模型层插入（不带 trim），用来造任意行数的初始状态。
func seedAlarmHistory(t testing.TB, ctx context.Context, n int) {
	t.Helper()
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)
	for i := 0; i < n; i++ {
		m := &s_db_model.SAlarmHistoryModel{
			DeviceId:       "dev-1",
			SourceDeviceId: "dev-1",
			Point:          "Voltage",
			PointName:      "电压过高",
			Level:          "Warn",
			Detail:         fmt.Sprintf("row-%d", i+1),
			TriggerAt:      &at,
		}
		if err := m.Create(ctx); err != nil {
			t.Fatalf("造数据失败(第 %d 条): %v", i+1, err)
		}
	}
}

// keptAlarmHistoryIds 返回表里剩下的 id，升序。
func keptAlarmHistoryIds(t testing.TB, ctx context.Context) []int {
	t.Helper()
	rows, err := g.DB().GetArray(ctx, `SELECT id FROM alarm_history ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("查 id 失败: %v", err)
	}
	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.Int())
	}
	return ids
}

func assertAlarmHistoryIds(t *testing.T, got []int, wantFrom, wantTo int) {
	t.Helper()
	want := wantTo - wantFrom + 1
	if len(got) != want {
		t.Fatalf("应剩 %d 行（id %d..%d），实际 %d 行", want, wantFrom, wantTo, len(got))
	}
	for i, id := range got {
		if id != wantFrom+i {
			t.Fatalf("第 %d 行 id=%d，期望 %d（最小的 %d 条应被删）", i, id, wantFrom+i, wantFrom-1)
		}
	}
}

// ① 1005 条 → 走真实写入路径（CreateAlarmHistory 内含 trim）后只剩 id 最大的 1000 条。
func TestCreateAlarmHistoryKeepsNewest1000(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctx, 1005)

	svc := GetAlarmService()
	if err := svc.CreateAlarmHistory(ctx, "dev-1", "dev-1", testAlarmPoint(), c_enum.EAlarmLevelWarn, "新告警", time.Now()); err != nil {
		t.Fatalf("CreateAlarmHistory 失败: %v", err)
	}

	count, err := (&s_db_model.SAlarmHistoryModel{}).GetCount(ctx)
	if err != nil {
		t.Fatalf("GetCount 失败: %v", err)
	}
	if count != 1000 {
		t.Fatalf("1005 条写入后应剩 1000 条，实际 %d", count)
	}
	// 原 1..1005，插入 1006 后 trim 保留 7..1006。
	assertAlarmHistoryIds(t, keptAlarmHistoryIds(t, ctx), 7, 1006)
}

// ② 999 条：不足上限时子查询取不到行（NULL），比较不成立，一行都不能删。
// 这条防的是「少一条就把表全清」。
func TestCreateAlarmHistoryUnderLimitDeletesNothing(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctx, 998)

	svc := GetAlarmService()
	if err := svc.CreateAlarmHistory(ctx, "dev-1", "dev-1", testAlarmPoint(), c_enum.EAlarmLevelWarn, "新告警", time.Now()); err != nil {
		t.Fatalf("CreateAlarmHistory 失败: %v", err)
	}

	count, err := (&s_db_model.SAlarmHistoryModel{}).GetCount(ctx)
	if err != nil {
		t.Fatalf("GetCount 失败: %v", err)
	}
	if count != 999 {
		t.Fatalf("998 条 + 1 条应剩 999 条，实际 %d", count)
	}
	assertAlarmHistoryIds(t, keptAlarmHistoryIds(t, ctx), 1, 999)
}

// ③ 正好 1000 条：不删。
func TestCreateAlarmHistoryAtLimitDeletesNothing(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctx, 999)

	svc := GetAlarmService()
	if err := svc.CreateAlarmHistory(ctx, "dev-1", "dev-1", testAlarmPoint(), c_enum.EAlarmLevelWarn, "新告警", time.Now()); err != nil {
		t.Fatalf("CreateAlarmHistory 失败: %v", err)
	}

	assertAlarmHistoryIds(t, keptAlarmHistoryIds(t, ctx), 1, 1000)
}

// ④ 首次大积压：3000 条一次 trim 到 1000，说明单条 DELETE 能全清，不是删一批留一批。
func TestTrimToNewestClearsLargeBacklogInOneStatement(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctx, 3000)

	model := &s_db_model.SAlarmHistoryModel{}
	affected, err := model.TrimToNewest(ctx, 1000)
	if err != nil {
		t.Fatalf("TrimToNewest 失败: %v", err)
	}
	if affected != 2000 {
		t.Fatalf("应删 2000 行，实际 RowsAffected=%d", affected)
	}
	assertAlarmHistoryIds(t, keptAlarmHistoryIds(t, ctx), 2001, 3000)
}

// ⑤ 混时区偏移：trigger_at 里有 +00:00 也有 +08:00，trim 只按 id 判新旧，
// 保留的必须是 id 最大的 1000 条，不能被时间字符串的偏移影响。
func TestTrimToNewestIgnoresTriggerAtOffset(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	offsets := []string{"+00:00", "+08:00"}
	for i := 1; i <= 1200; i++ {
		off := offsets[i%2]
		// 故意让 offset 与 id 反序：+08:00 的行写成更晚的钟点，+00:00 写成更早的钟点。
		hour := i % 24
		if _, err := g.DB().Exec(ctx,
			`INSERT INTO alarm_history (device_id,source_device_id,point,point_name,level,detail,trigger_at)
			 VALUES ('dev-1','dev-1','Voltage','电压过高','Warn',?,?)`,
			fmt.Sprintf("row-%d", i),
			fmt.Sprintf("2026-11-12 %02d:00:00%s", hour, off)); err != nil {
			t.Fatalf("插行失败(第 %d 条): %v", i, err)
		}
	}

	model := &s_db_model.SAlarmHistoryModel{}
	if _, err := model.TrimToNewest(ctx, 1000); err != nil {
		t.Fatalf("TrimToNewest 失败: %v", err)
	}
	assertAlarmHistoryIds(t, keptAlarmHistoryIds(t, ctx), 201, 1200)
}

// ⑥ 清理失败不阻断：表没了，CreateAlarmHistory 返回的 error 只可能来自 Insert；
// 单独调 TrimToNewest 应返回 error 而不是 panic。
func TestTrimFailureDoesNotPanicOrChangeInsertResult(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctx, 3)

	if _, err := g.DB().Exec(ctx, `DROP TABLE alarm_history`); err != nil {
		t.Fatalf("drop 表失败: %v", err)
	}

	svc := GetAlarmService()
	if err := svc.CreateAlarmHistory(ctx, "dev-1", "dev-1", testAlarmPoint(), c_enum.EAlarmLevelWarn, "新告警", time.Now()); err == nil {
		t.Fatalf("表已 drop，CreateAlarmHistory 应因 Insert 失败返回 error")
	}

	if _, err := (&s_db_model.SAlarmHistoryModel{}).TrimToNewest(ctx, 1000); err == nil {
		t.Fatalf("表已 drop，TrimToNewest 应返回 error")
	}
}

// ⑦ keep < 1 直接返回，不执行任何 DELETE（否则 keep=0 会把整表清空）。
func TestTrimToNewestWithNonPositiveKeepDeletesNothing(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctx, 5)

	model := &s_db_model.SAlarmHistoryModel{}
	for _, keep := range []int{0, -1} {
		affected, err := model.TrimToNewest(ctx, keep)
		if err != nil {
			t.Fatalf("keep=%d 时 TrimToNewest 报错: %v", keep, err)
		}
		if affected != 0 {
			t.Fatalf("keep=%d 时应删 0 行，实际 %d", keep, affected)
		}
	}
	assertAlarmHistoryIds(t, keptAlarmHistoryIds(t, ctx), 1, 5)
}

// ⑧ 删除条件必须落在主键上：计划里出现 USE TEMP B-TREE 就说明退化成排序后逐行删，
// 1000 行库里每次写入都要付这个代价。
func TestTrimToNewestDeletePlanUsesPrimaryKey(t *testing.T) {
	ctx := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctx, 20)

	rows, err := g.DB().GetAll(ctx,
		`EXPLAIN QUERY PLAN DELETE FROM alarm_history WHERE id <= (SELECT id FROM alarm_history ORDER BY id DESC LIMIT 1 OFFSET ?)`, 1000)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN 失败: %v", err)
	}
	var plan []string
	for _, r := range rows {
		line := r["detail"].String()
		plan = append(plan, line)
		t.Logf("PLAN %s", line)
	}
	joined := strings.Join(plan, "\n")
	// 删除本身必须按主键定位（rowid<? 的范围扫描）。
	if !strings.Contains(joined, "SEARCH alarm_history USING INTEGER PRIMARY KEY (rowid<?)") {
		t.Fatalf("删除没有按主键范围扫描，实际计划: %v", plan)
	}
	// 任何一步排序都会在这里出现临时 B 树，说明子查询没走到有序路径。
	if strings.Contains(joined, "USE TEMP B-TREE") {
		t.Fatalf("计划里出现 USE TEMP B-TREE，删除没走主键有序路径: %v", plan)
	}
}

// BenchmarkAlarmHistoryInsert 量的是「1000 行库里多一次 trim」的代价：
// 同一张表、同一批数据，只差 svc.CreateAlarmHistory 里那一条 DELETE。
// 对照基线用 model.Create（等价于 trim 之前的写入路径）。
func benchSeed(b *testing.B, ctx context.Context) {
	b.Helper()
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)
	for i := 0; i < alarmHistoryKeepRows; i++ {
		m := &s_db_model.SAlarmHistoryModel{
			DeviceId: "dev-1", SourceDeviceId: "dev-1", Point: "Voltage",
			PointName: "电压过高", Level: "Warn", Detail: "seed", TriggerAt: &at,
		}
		if err := m.Create(ctx); err != nil {
			b.Fatalf("造数据失败: %v", err)
		}
	}
}

func BenchmarkAlarmHistoryInsertWithoutTrim(b *testing.B) {
	ctx := newAlarmHistoryTestDB(b)
	benchSeed(b, ctx)
	point := testAlarmPoint()
	at := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := &s_db_model.SAlarmHistoryModel{
			DeviceId: "dev-1", SourceDeviceId: "dev-1", Point: point.GetKey(),
			PointName: point.GetName(), Level: "Warn", Detail: "bench", TriggerAt: &at,
		}
		if err := m.Create(ctx); err != nil {
			b.Fatalf("插入失败: %v", err)
		}
	}
}

func BenchmarkAlarmHistoryInsertWithTrim(b *testing.B) {
	ctx := newAlarmHistoryTestDB(b)
	benchSeed(b, ctx)
	svc := GetAlarmService()
	point := testAlarmPoint()
	at := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := svc.CreateAlarmHistory(ctx, "dev-1", "dev-1", point, c_enum.EAlarmLevelWarn, "bench", at); err != nil {
			b.Fatalf("插入失败: %v", err)
		}
	}
}
