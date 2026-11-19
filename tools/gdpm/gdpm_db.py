#!/usr/bin/env python3
"""gdpm 的库连接层：PostgreSQL 优先，sqlite 兜底。

2026-11-19 起 ems 关系库切到 PostgreSQL（服务侧 `EMS_DB_LINK`）。gdpm 是把官方披露
数据写进同一批表的外部采集器，如果继续写 `/app/out/db.sqlite3`，数据就落进一块
已经没人读的库（页面看不到，也不会报错）。所以连接由本模块统一决定：

    GDPM_DB_LINK 非空且是 postgres URI → psycopg；否则 sqlite（本地调试与测试）

SQL 一律沿用 sqlite 的 `?` 占位符写，PG 分支在 execute 里翻成 `%s`；DDL 里的
`INTEGER PRIMARY KEY AUTOINCREMENT` 在 PG 分支换成 `BIGSERIAL PRIMARY KEY`。
只做这两处方言差异，不引入 ORM —— 现有语句（含 ON CONFLICT ... DO UPDATE）两边都认。
"""

PG_PREFIXES = ("postgres://", "postgresql://")


def is_pg_link(link):
    return (link or "").strip().lower().startswith(PG_PREFIXES)


def translate_placeholders(sql):
    return sql.replace("?", "%s")


def ddl_for(sql, pg):
    """sqlite DDL → PG DDL；只有自增主键这一个差异。"""
    if not pg:
        return sql
    return sql.replace("INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY")


class _PGCursor:
    """把 psycopg cursor 收窄到 sqlite cursor 用到的几个方法。"""

    def __init__(self, cur):
        self._cur = cur

    def fetchone(self):
        return self._cur.fetchone()

    def fetchall(self):
        return self._cur.fetchall()

    def __iter__(self):
        return iter(self._cur)

    @property
    def rowcount(self):
        return self._cur.rowcount


class _PGConn:
    def __init__(self, conn):
        self._conn = conn

    # psycopg3 的 Connection 只有 execute()（且 3.2 起已弃用），没有 executemany()；
    # 统一走 cursor，行为与 sqlite3 的 conn.execute/executemany 对齐。
    def execute(self, sql, params=()):
        cur = self._conn.cursor()
        cur.execute(translate_placeholders(sql), params)
        return _PGCursor(cur)

    def executemany(self, sql, seq):
        cur = self._conn.cursor()
        cur.executemany(translate_placeholders(sql), seq)
        return _PGCursor(cur)

    def commit(self):
        self._conn.commit()

    def rollback(self):
        self._conn.rollback()

    def close(self):
        self._conn.close()


def connect(link=None, db_path=None):
    """返回 (conn, is_pg)。link 非空且是 PG URI 时连 PG，否则开 sqlite。"""
    if is_pg_link(link):
        import psycopg
        from psycopg.rows import dict_row

        return _PGConn(psycopg.connect(link.strip(), row_factory=dict_row)), True
    # sqlite3 延迟导入：只写 PG 的环境（镜像里也留了 sqlite 分支，但精简镜像可能没编 _sqlite3）
    # 不该因为缺这个标准库模块就起不来。
    import sqlite3

    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    return conn, False
