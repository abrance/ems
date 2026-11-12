package impl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"

	"s_db/internal"
)

// configFileName 是生产用的数据库配置：单容器部署下它决定实际的 journal_mode。
const configFileName = "../../../../application/manifest/config/config.yaml"

// PR #124 的两个改动都在「配置/DDL 里的一行」，代码不引用它们，所以删掉也不会有任何
// 编译错误或调用失败——只能靠跑一遍真实 Init() 把行为钉住，防止被静默改回去。
func TestInitEnablesWalAndLogIndexes(t *testing.T) {
	link := readDatabaseLink(t)

	// 生产 link 指向 out/db.sqlite3，这里换成本次测试的临时库，保证是全新库。
	dbPath := filepath.Join(t.TempDir(), "db.sqlite3")
	link = strings.Replace(link, "out/db.sqlite3", dbPath, 1)
	if !strings.Contains(link, dbPath) {
		t.Fatalf("config.yaml 的 link 不再指向 out/db.sqlite3，测试需要跟着改: %s", link)
	}

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

	// ① 新连接必须是 WAL。link 里丢了 ?journal_mode=WAL 时这里会拿到 delete，
	// 也就是读写重新互斥（PR #124 修的正是这个）。
	journalMode, err := g.DB().GetValue(ctx, "PRAGMA journal_mode")
	if err != nil {
		t.Fatalf("查询 PRAGMA journal_mode 失败: %v", err)
	}
	if got := strings.ToLower(journalMode.String()); got != "wal" {
		t.Fatalf("journal_mode = %q，期望 wal（config.yaml 的 link 里 ?journal_mode=WAL 被改掉了？）", got)
	}

	// ② log 表的两条索引必须由 Init() 建出来：日志分页深翻页靠 created_at，
	// 按设备过滤靠 device_id，缺任一条都会退回全表扫描。
	names, err := g.DB().GetArray(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='log'`)
	if err != nil {
		t.Fatalf("查询 log 表索引失败: %v", err)
	}
	indexes := make(map[string]bool, len(names))
	for _, name := range names {
		indexes[name.String()] = true
	}
	for _, want := range []string{"idx_log_created_at", "idx_log_device_id"} {
		if !indexes[want] {
			t.Fatalf("log 表缺少索引 %s，实际索引: %v", want, names)
		}
	}
}

// readDatabaseLink 取 config.yaml 里 default 组的 link。
// 刻意不 hardcode link 内容：护栏要守的就是配置文件那一行本身。
func readDatabaseLink(t *testing.T) string {
	t.Helper()

	content, err := os.ReadFile(configFileName)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", configFileName, err)
	}
	j, err := gjson.LoadContentType(gjson.ContentTypeYaml, content)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", configFileName, err)
	}
	link := j.Get("database.default.link").String()
	if link == "" {
		t.Fatalf("%s 里没有 database.default.link", configFileName)
	}
	return link
}
