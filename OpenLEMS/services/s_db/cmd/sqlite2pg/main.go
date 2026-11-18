// sqlite2pg 把本地 sqlite 库（out/db.sqlite3）整表搬到 PostgreSQL。
//
// 用法（先让服务用 EMS_DB_LINK 起一次，把 schema 建出来，再跑本工具灌数据）：
//
//	cd OpenLEMS/services/s_db
//	EMS_DB_LINK=postgresql://user:pass@host:5432/ems-test go run ./cmd/sqlite2pg -src ../../application/out/db.sqlite3
//
// 只做「整表复制」，不做字段映射：两边 schema 由服务自身建表语句保证一致
// （DDL 的双方言转换见 internal/s_db_dialect_f.go）。目标表必须已存在，
// 这样搬错库不会顺手把表建歪。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"s_db/internal"

	_ "github.com/gogf/gf/contrib/drivers/pgsql/v2"
	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	srcGroup = "sqlite_src"
	dstGroup = "pgsql_dst"
	// batchSize 单次 Insert 的行数：整表一次插会在 gd_spot_metric（几万行）上爆内存
	batchSize = 200
)

func main() {
	src := flag.String("src", "./out/db.sqlite3", "源 sqlite 文件")
	truncate := flag.Bool("truncate", false, "写入前清空目标表（默认遇到非空表直接报错，避免误覆盖）")
	dryRun := flag.Bool("dry-run", false, "只打印每张表的行数与目标表状态，不写入")
	only := flag.String("tables", "", "只搬这些表（逗号分隔）；默认全部")
	flag.Parse()

	link := internal.NormalizeDBLink(os.Getenv(internal.EnvDBLink))
	if link == "" {
		fail("必须用 %s 指定目标库（例：postgresql://user:pass@host:5432/ems-test）", internal.EnvDBLink)
	}

	gdb.SetConfig(gdb.Config{
		srcGroup: {{Link: "sqlite::@file(" + *src + ")?journal_mode=WAL", Timezone: "Local"}},
		dstGroup: {{Link: link, Timezone: "Asia/Shanghai"}},
	})

	ctx := context.Background()
	srcDB := g.DB(srcGroup)
	dstDB := g.DB(dstGroup)
	if _, err := dstDB.Exec(ctx, "SELECT 1"); err != nil {
		fail("目标库连不上: %+v", err)
	}

	tables, err := srcTables(ctx, srcDB)
	if err != nil {
		fail("读取源表清单失败: %+v", err)
	}
	if *only != "" {
		want := map[string]bool{}
		for _, t := range strings.Split(*only, ",") {
			want[strings.TrimSpace(t)] = true
		}
		filtered := tables[:0]
		for _, t := range tables {
			if want[t] {
				filtered = append(filtered, t)
			}
		}
		tables = filtered
	}

	total := 0
	for _, table := range tables {
		n, err := copyTable(ctx, srcDB, dstDB, table, *truncate, *dryRun)
		if err != nil {
			fail("表 %s: %+v", table, err)
		}
		total += n
	}
	if *dryRun {
		fmt.Printf("dry-run 完成，共 %d 行待搬（未写入）\n", total)
		return
	}
	fmt.Printf("完成：%d 张表，共 %d 行\n", len(tables), total)
}

func copyTable(ctx context.Context, srcDB, dstDB gdb.DB, table string, truncate, dryRun bool) (int, error) {
	srcCount, err := srcDB.GetCount(ctx, "SELECT COUNT(*) FROM "+quote(table))
	if err != nil {
		return 0, err
	}
	if srcCount == 0 {
		fmt.Printf("  %-26s 0 行，跳过\n", table)
		return 0, nil
	}

	// 目标表必须存在：schema 由服务自己的建表语句建，工具不参一脚。
	if _, err := dstDB.GetCount(ctx, "SELECT COUNT(*) FROM "+quote(table)); err != nil {
		return 0, fmt.Errorf("目标表不存在或不可读（先用 EMS_DB_LINK 起一次服务建表）: %w", err)
	}
	dstCount, _ := dstDB.GetCount(ctx, "SELECT COUNT(*) FROM "+quote(table))
	if dryRun {
		fmt.Printf("  %-26s 源 %6d 行 → 目标已有 %6d 行\n", table, srcCount, dstCount)
		return int(srcCount), nil
	}
	if dstCount > 0 && !truncate {
		return 0, fmt.Errorf("目标表已有 %d 行（加 -truncate 才会覆盖）", dstCount)
	}
	if truncate {
		if _, err := dstDB.Exec(ctx, "TRUNCATE TABLE "+quote(table)); err != nil {
			return 0, fmt.Errorf("清空目标表失败: %w", err)
		}
	}

	rows, err := srcDB.GetAll(ctx, "SELECT * FROM "+quote(table))
	if err != nil {
		return 0, err
	}
	rows = normalizeBoolColumns(table, rows)

	written := 0
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := make([]gdb.Record, 0, end-i)
		for _, r := range rows[i:end] {
			batch = append(batch, gdb.Record(r))
		}
		if _, err := dstDB.Model(table).Data(batch).Insert(); err != nil {
			return written, fmt.Errorf("写入第 %d~%d 行失败: %w", i, end, err)
		}
		written += len(batch)
	}
	if err := resetSequence(ctx, dstDB, table); err != nil {
		fmt.Printf("  %-26s 序列重置失败（不影响数据）: %v\n", table, err)
	}
	fmt.Printf("  %-26s 源 %6d 行 → 已写 %6d 行\n", table, srcCount, written)
	return written, nil
}

// boolColumns 各表的布尔列。sqlite 存 0/1，pgsql 的 boolean 只认 true/false 字面量；
// 走模型 Insert 时驱动会把整数当字符串发过去，PG 虽然能接受 '1'/'0'，但显式换成
// bool 更稳（也避免以后换驱动踩坑）。
var boolColumns = map[string][]string{
	"protocol":       {"enabled"},
	"device":         {"enabled", "enable_debug", "manual_mode", "storage_enable"},
	"setting":        {"enabled", "is_public"},
	"alarm_ignore":   {"enabled"},
	"automation":     {"enabled"},
	"price":          {"enabled"},
	"energy_storage": {"enabled"},
}

func normalizeBoolColumns(table string, rows []gdb.Record) []gdb.Record {
	cols := boolColumns[table]
	if len(cols) == 0 {
		return rows
	}
	for _, r := range rows {
		for _, c := range cols {
			v, ok := r[c]
			if !ok || v == nil || v.IsNil() {
				continue
			}
			r[c] = gvar.New(v.Int() != 0)
		}
	}
	return rows
}

// resetSequence 把自增列序列推到 max(id)，否则后续插入会撞主键。
func resetSequence(ctx context.Context, db gdb.DB, table string) error {
	// 没有 id 列的表（如 gd_node_price 用复合唯一键）直接跳过；
	// 有 id 但不是自增（device/protocol/setting 是 VARCHAR 主键）时下面会取到 NULL。
	hasID, err := db.GetValue(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_name = ? AND column_name = 'id'", table)
	if err != nil {
		return err
	}
	if hasID.Int() == 0 {
		return nil
	}
	seq, err := db.GetValue(ctx, "SELECT pg_get_serial_sequence(?, 'id')", table)
	if err != nil {
		return err
	}
	if seq.IsNil() || seq.String() == "" {
		return nil
	}
	if _, err := db.Exec(ctx, fmt.Sprintf("SELECT setval('%s', (SELECT MAX(id) FROM %s))", seq.String(), table)); err != nil {
		return err
	}
	return nil
}

func srcTables(ctx context.Context, db gdb.DB) ([]string, error) {
	vals, err := db.GetArray(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, v.String())
	}
	sort.Strings(out)
	return out, nil
}

func quote(ident string) string { return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"` }

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
