package impl

import (
	"path/filepath"
	"strings"
	"testing"

	"s_db/internal"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

// gd_home_market_fetch_run 的形状护栏。
//
// 代码只按列名读写这张表，DDL 写错（多带 extractor_version、唯一键换了列、
// 少了 status）不会有任何编译错误，只有跑到生产才发现「状态永远查不到最近一次」。
// 这里跑一次真实 Init() 把形状钉住。
func TestInitCreatesHomeMarketFetchRunShape(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite3")
	link := strings.Replace(readDatabaseLink(t), "out/db.sqlite3", dbPath, 1)

	oldPath := internal.DefaultSqliteDbPath
	internal.DefaultSqliteDbPath = dbPath
	t.Cleanup(func() { internal.DefaultSqliteDbPath = oldPath })

	if err := gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: link}},
	}); err != nil {
		t.Fatalf("设置数据库配置失败: %v", err)
	}
	t.Cleanup(func() { _ = gdb.SetConfig(gdb.Config{}) })

	if err := internal.Init(); err != nil {
		t.Fatalf("Init() 失败: %v", err)
	}
	ctx := gctx.New()

	var cols []struct {
		Name string `orm:"name"`
	}
	if err := g.DB().GetScan(ctx, &cols,
		`SELECT name FROM pragma_table_info('gd_home_market_fetch_run')`); err != nil {
		t.Fatalf("表 gd_home_market_fetch_run 不存在或查询失败: %v", err)
	}
	if len(cols) == 0 {
		t.Fatal("Init() 没建出 gd_home_market_fetch_run")
	}
	have := make(map[string]bool, len(cols))
	for _, c := range cols {
		have[c.Name] = true
	}
	for _, want := range []string{"target_date", "dataset", "status", "error_kind", "rows_written",
		"rows_expected", "attempt_count", "consecutive_failures", "last_error",
		"first_seen_at", "last_ok_at", "updated_at"} {
		if !have[want] {
			t.Errorf("缺列 %q，实际列: %v", want, cols)
		}
	}
	// 这一条是这张表与 gd_pm_fetch_run 的关键差别：接口口径没有「提取器版本」，
	// 带上它会让每次改代码都多出一组行，状态判定（最近一次尝试是哪条）失效。
	if have["extractor_version"] {
		t.Error("gd_home_market_fetch_run 不该有 extractor_version")
	}

	// 唯一键必须**只有** (target_date, dataset)：一天一路只留一行，重跑覆盖不堆积。
	// UNIQUE 在 SQLite 里是隐式 autoindex（sql 列为空），只能从 pragma_index_info 读构成列。
	var idxCols []struct {
		Idx  string `orm:"idx"`
		Uniq int    `orm:"uniq"`
		Col  string `orm:"col"`
	}
	if err := g.DB().GetScan(ctx, &idxCols, `
		SELECT il.name AS idx, il.[unique] AS uniq, ii.name AS col
		  FROM pragma_index_list('gd_home_market_fetch_run') il
		  JOIN pragma_index_info(il.name) ii
		 ORDER BY il.name, ii.seqno`); err != nil {
		t.Fatalf("查询索引列失败: %v", err)
	}
	colsOf := map[string][]string{}
	uniqOf := map[string]int{}
	for _, c := range idxCols {
		colsOf[c.Idx] = append(colsOf[c.Idx], c.Col)
		uniqOf[c.Idx] = c.Uniq
	}
	found := false
	for name, cols := range colsOf {
		if uniqOf[name] == 1 && len(cols) == 2 && cols[0] == "target_date" && cols[1] == "dataset" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺 UNIQUE (target_date, dataset)，实际索引列: %+v", colsOf)
	}
}
