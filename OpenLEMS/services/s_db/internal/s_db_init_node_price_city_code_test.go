package internal

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/database/gdb"
)

// gd_node_price 的 city → city_code 迁移护栏。
//
// 迁移里最危险的一步是 DROP TABLE。它不是「跑不过就报错」的那种危险，而是
// 「跑得太顺」—— 一个 COUNT 写错的地方会把某个环境里已有的 42×N 行真实数据
// 静默删掉，而且没有任何报错，只有事后发现页面空了才知道。
//
// 所以这里两个方向都钉：
//  1. 空旧表 → 必须 DROP（否则新 DDL 的 CREATE IF NOT EXISTS 会把旧表留着，
//     之后每次 INSERT 都因为列不存在而失败）；
//  2. 有数据的旧表 → **不能** DROP，表要原样留着，让人工处理。
// sqlDBShim 把 *sql.DB 适配成迁移函数要的接口（*sql.DB 的 Exec 不带 ctx）。
type sqlDBShim struct{ db *sql.DB }

func (s sqlDBShim) GetValue(ctx context.Context, sqlStr string, args ...any) (gdb.Value, error) {
	var v any
	err := s.db.QueryRowContext(ctx, sqlStr, args...).Scan(&v)
	if err == sql.ErrNoRows {
		return gvar.New(nil), nil
	}
	if err != nil {
		return gvar.New(nil), err
	}
	return gvar.New(v), nil
}

func (s sqlDBShim) Exec(ctx context.Context, sqlStr string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, sqlStr, args...)
}

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "db.sqlite3"))
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// oldNodePriceDDL 是 C3 之前的建表语句（只有 city 中文名）。
const oldNodePriceDDL = `
	CREATE TABLE gd_node_price (
		run_date VARCHAR(10) NOT NULL,
		city VARCHAR(32) NOT NULL,
		period_type INTEGER NOT NULL DEFAULT 1 CHECK (period_type IN (1, 2)),
		price REAL NOT NULL DEFAULT 0,
		source VARCHAR(64) NOT NULL DEFAULT '',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (run_date, city, period_type)
	)`

func TestMigrateNodePriceCityCode_DropsEmptyOldTable(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(oldNodePriceDDL); err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}

	dropped, err := migrateNodePriceCityCodeWith(context.Background(), sqlDBShim{db})
	if err != nil {
		t.Fatalf("迁移报错: %v", err)
	}
	if !dropped {
		t.Fatal("空旧表应当被删除重建，实际没有 DROP")
	}
	// 删完之后表必须不存在：调用方紧接着会用新 DDL 的 CREATE 建出来。
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='gd_node_price'`).Scan(&n); err != nil {
		t.Fatalf("查 sqlite_master 失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("旧表应当已被删除，实际仍存在 %d 张", n)
	}
}

func TestMigrateNodePriceCityCode_KeepsTableWithData(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(oldNodePriceDDL); err != nil {
		t.Fatalf("建旧表失败: %v", err)
	}
	// 造真实数据：2 市 × 2 时段。迁移必须一行都不动。
	for _, city := range []string{"广州市", "深圳市"} {
		for _, tp := range []int{1, 2} {
			if _, err := db.Exec(`INSERT INTO gd_node_price
				(run_date,city,period_type,price,source) VALUES (?,?,?,?,?)`,
				"2026-11-13", city, tp, 465.0, "官网·节点价"); err != nil {
				t.Fatalf("插旧数据失败: %v", err)
			}
		}
	}

	dropped, err := migrateNodePriceCityCodeWith(context.Background(), sqlDBShim{db})
	if err != nil {
		t.Fatalf("迁移不该因有数据而报错（应打 Error 日志后跳过）: %v", err)
	}
	if dropped {
		t.Fatal("有数据的旧表被 DROP 了 —— 这会丢真实数据")
	}

	// 表还在，4 行数据还在，且仍是旧结构（没有 city_code 列）。
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gd_node_price`).Scan(&rows); err != nil {
		t.Fatalf("旧表已不存在: %v", err)
	}
	if rows != 4 {
		t.Fatalf("数据行数 = %d，期望 4（一行都不能动）", rows)
	}
	var hasCode int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('gd_node_price') WHERE name='city_code'`).Scan(&hasCode); err != nil {
		t.Fatalf("查列失败: %v", err)
	}
	if hasCode != 0 {
		t.Fatal("有数据的表不该被重建出 city_code 列：那是 DROP 之后才会有的形状")
	}
}

// 幂等：已经是新结构（有 city_code）的库，第二次部署不能又把表删一次。
func TestMigrateNodePriceCityCode_SkipsNewTable(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`
		CREATE TABLE gd_node_price (
			run_date VARCHAR(10) NOT NULL,
			city_code VARCHAR(16) NOT NULL,
			city_name VARCHAR(32) NOT NULL,
			period_type INTEGER NOT NULL DEFAULT 1 CHECK (period_type IN (1, 2)),
			price REAL NOT NULL DEFAULT 0,
			source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (run_date, city_code, period_type)
		)`); err != nil {
		t.Fatalf("建新表失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO gd_node_price
		(run_date,city_code,city_name,period_type,price) VALUES (?,?,?,?,?)`,
		"2026-11-13", "440100", "广州市", 1, 465.0); err != nil {
		t.Fatalf("插新数据失败: %v", err)
	}

	dropped, err := migrateNodePriceCityCodeWith(context.Background(), sqlDBShim{db})
	if err != nil {
		t.Fatalf("迁移报错: %v", err)
	}
	if dropped {
		t.Fatal("新结构（已有 city_code）的表不该被删")
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gd_node_price`).Scan(&rows); err != nil {
		t.Fatalf("表不见了: %v", err)
	}
	if rows != 1 {
		t.Fatalf("行数 = %d，期望 1（幂等迁移不能碰新表的数据）", rows)
	}
}
