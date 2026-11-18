package internal

import (
	"strings"
	"testing"
)

// sqlite 分支必须原样返回：本地的建表语句是权威写法，不能在「转一遍」的过程中漂移。
func TestDDLForDialectSqliteIsIdentity(t *testing.T) {
	stmt := `CREATE TABLE IF NOT EXISTS t (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		enabled BOOLEAN DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	if got := ddlForDialect(dialectSqlite, stmt); got != stmt {
		t.Errorf("sqlite 分支不应改写，实际:\n%s", got)
	}
}

// pgsql 认不出 AUTOINCREMENT / DATETIME，也不接受 BOOLEAN DEFAULT 1，必须转换。
func TestDDLForDialectPgsql(t *testing.T) {
	stmt := `CREATE TABLE IF NOT EXISTS t (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		enabled BOOLEAN DEFAULT 1,
		is_public BOOLEAN DEFAULT 0,
		manual_mode BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	got := ddlForDialect(dialectPgsql, stmt)

	for _, bad := range []string{"AUTOINCREMENT", "DATETIME", "DEFAULT 1", "DEFAULT 0"} {
		if strings.Contains(strings.ToUpper(got), bad) {
			t.Errorf("转换后仍含 %s:\n%s", bad, got)
		}
	}
	for _, want := range []string{"BIGSERIAL PRIMARY KEY", "BOOLEAN DEFAULT TRUE", "BOOLEAN DEFAULT FALSE", "TIMESTAMP"} {
		if !strings.Contains(got, want) {
			t.Errorf("转换后缺 %s:\n%s", want, got)
		}
	}
}

// ALTER 里的 DATETIME 同样要转（历史迁移列的补丁语句）。
func TestDDLForDialectPgsqlAlter(t *testing.T) {
	got := ddlForDialect(dialectPgsql, `ALTER TABLE gd_spot_metric ADD COLUMN edited_at DATETIME`)
	if strings.Contains(got, "DATETIME") || !strings.Contains(got, "TIMESTAMP") {
		t.Errorf("ALTER 未转换: %s", got)
	}
}

// 运维习惯贴 postgresql:// URI，gf 只认 pgsql:user:pass@tcp(host:port)/db。
func TestNormalizeDBLink(t *testing.T) {
	cases := map[string]string{
		"postgresql://test:pw@10.193.0.10:5432/ems-test": "pgsql:test:pw@tcp(10.193.0.10:5432)/ems-test",
		"postgres://u:p@h:5432/db?sslmode=disable":       "pgsql:u:p@tcp(h:5432)/db",
		"pgsql:u:p@tcp(h:5432)/db":                       "pgsql:u:p@tcp(h:5432)/db",
		"sqlite::@file(out/db.sqlite3)?journal_mode=WAL": "sqlite::@file(out/db.sqlite3)?journal_mode=WAL",
		"": "",
	}
	for in, want := range cases {
		if got := NormalizeDBLink(in); got != want {
			t.Errorf("NormalizeDBLink(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 环境变量没设时不能动默认组（本地默认必须还是 sqlite）。
func TestApplyDBLinkFromEnvNoop(t *testing.T) {
	t.Setenv(EnvDBLink, "")
	applyDBLinkFromEnv() // 不 panic、不改配置即可
}
