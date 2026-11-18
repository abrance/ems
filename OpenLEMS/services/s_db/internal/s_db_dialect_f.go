package internal

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// 数据库方言：同一份建表语句要能落到 sqlite（本地/单测）和 pgsql（部署）上。
//
// 两边的差异只有四处，都在 DDL 里：
//
//	sqlite                                  pgsql
//	INTEGER PRIMARY KEY AUTOINCREMENT       BIGSERIAL PRIMARY KEY
//	DATETIME                                TIMESTAMP
//	BOOLEAN DEFAULT 1 / 0                   BOOLEAN DEFAULT TRUE / FALSE
//
// 运行时 SQL 里不再出现方言函数（时间统一走 CURRENT_TIMESTAMP，两种库都认）。
const (
	dialectSqlite = "sqlite"
	dialectPgsql  = "pgsql"
)

// EnvDBLink 覆盖 default 组的连接串。留空时仍用配置文件里的 sqlite。
//
// 为什么要这个开关：连接串带口令，不能写进仓库；部署时由 k8s secret 注入。
// 例：EMS_DB_LINK=postgresql://user:pass@host:5432/ems-test？不行 —— gf 认的是
// pgsql:user:pass@tcp(host:5432)/db 形式，标准 URI 需要先转换（见 NormalizeDBLink）。
const EnvDBLink = "EMS_DB_LINK"

// NormalizeDBLink 把 postgresql:// URI 转成 gf 的 pgsql link；已经是 gf 形式的原样返回。
//
// 运维更习惯贴 URI（psql/各语言 SDK 都用它），而 gf 只认 pgsql:user:pass@tcp(host:port)/db。
// 转换放这里，避免每处部署脚本各写一遍。
func NormalizeDBLink(link string) string {
	link = strings.TrimSpace(link)
	lower := strings.ToLower(link)
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := link[len(prefix):]
		at := strings.LastIndex(rest, "@")
		if at < 0 {
			return link
		}
		cred, hostPart := rest[:at], rest[at+1:]
		// 去掉 query（gf 参数另配），保留 db 名
		if i := strings.IndexAny(hostPart, "?"); i >= 0 {
			hostPart = hostPart[:i]
		}
		slash := strings.Index(hostPart, "/")
		if slash < 0 {
			return link
		}
		host, dbName := hostPart[:slash], hostPart[slash+1:]
		return "pgsql:" + cred + "@tcp(" + host + ")/" + dbName
	}
	return link
}

// applyDBLinkFromEnv 用 EMS_DB_LINK 覆盖 default 连接组。必须在第一次 g.DB() 之前调用。
//
// 时区不能沿用配置里的 "Local"：pgsql 会直接拒绝（pq: invalid value for parameter
// "TimeZone": "Local"），必须给 IANA 名。
func applyDBLinkFromEnv() {
	link := NormalizeDBLink(os.Getenv(EnvDBLink))
	if link == "" {
		return
	}
	node := gdb.ConfigNode{Link: link}
	if strings.HasPrefix(strings.ToLower(link), dialectPgsql+":") {
		node.Timezone = "Asia/Shanghai"
	} else {
		node.Timezone = "Local"
	}
	gdb.SetConfigGroup(gdb.DefaultGroupName, gdb.ConfigGroup{node})
}

// dbDialect 当前连接使用的方言（小写）。拿不到配置时按 sqlite 处理 —— 本地默认就是它。
func dbDialect(db gdb.DB) string {
	if db == nil {
		db = g.DB()
	}
	cfg := db.GetConfig()
	if cfg == nil {
		return dialectSqlite
	}
	if t := strings.ToLower(strings.TrimSpace(cfg.Type)); t != "" {
		return t
	}
	// 配置里没写 type 时按 link 前缀判断
	return strings.ToLower(strings.SplitN(cfg.Link, ":", 2)[0])
}

var pgDDLRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\bINTEGER\s+PRIMARY\s+KEY\s+AUTOINCREMENT\b`), "BIGSERIAL PRIMARY KEY"},
	{regexp.MustCompile(`(?i)\bDATETIME\b`), "TIMESTAMP"},
	{regexp.MustCompile(`(?i)\bBOOLEAN\s+DEFAULT\s+1\b`), "BOOLEAN DEFAULT TRUE"},
	{regexp.MustCompile(`(?i)\bBOOLEAN\s+DEFAULT\s+0\b`), "BOOLEAN DEFAULT FALSE"},
}

// ddlForDialect 把按 sqlite 写的 DDL 转成目标方言。sqlite 分支原样返回，
// 免得「改一处写法，两个库都变」这种隐蔽漂移。
func ddlForDialect(dialect, stmt string) string {
	if dialect != dialectPgsql {
		return stmt
	}
	for _, r := range pgDDLRules {
		stmt = r.re.ReplaceAllString(stmt, r.repl)
	}
	return stmt
}

// execSchema 执行建表/建索引语句，按方言做必要转换。
//
// 只给 schema 用：运行时的增删改一律写方言无关的 SQL（时间用 CURRENT_TIMESTAMP）。
func execSchema(ctx context.Context, stmt string) (sql.Result, error) {
	return g.DB().Exec(ctx, ddlForDialect(dbDialect(nil), stmt))
}
