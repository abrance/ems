#!/usr/bin/env python3
"""gdpm_db 的方言适配测试：不连 PG，只验翻译规则与 sqlite 兜底路径。"""

import os
import sqlite3
import tempfile
import unittest

import gdpm_db
import gdpm_sqlite as imp


class TestLink(unittest.TestCase):
    def test_is_pg_link(self):
        for link in ("postgresql://u:p@h:5432/db", "POSTGRES://u:p@h/db",
                     "  postgresql://u:p@h/db  "):
            self.assertTrue(gdpm_db.is_pg_link(link), link)
        for link in (None, "", "   ", "/app/out/db.sqlite3", "pgsql:u:p@tcp(h:5432)/db"):
            self.assertFalse(gdpm_db.is_pg_link(link), link)

    def test_translate_placeholders(self):
        sql = "INSERT INTO t(a,b) VALUES (?,?) ON CONFLICT (a) DO UPDATE SET b=excluded.b"
        self.assertEqual(gdpm_db.translate_placeholders(sql),
                         "INSERT INTO t(a,b) VALUES (%s,%s) ON CONFLICT (a) DO UPDATE SET b=excluded.b")

    def test_ddl_for(self):
        sql = imp.DDL_METRIC
        self.assertIn("AUTOINCREMENT", gdpm_db.ddl_for(sql, pg=False))
        self.assertNotIn("AUTOINCREMENT", gdpm_db.ddl_for(sql, pg=True))
        self.assertIn("BIGSERIAL PRIMARY KEY", gdpm_db.ddl_for(sql, pg=True))
        # 除主键外其余列名不动：避免两份 DDL 漂移
        self.assertEqual(gdpm_db.ddl_for(sql, pg=False), sql)


class TestSqliteFallback(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "db.sqlite3")
        self.conn, self.is_pg = gdpm_db.connect(None, self.db)

    def tearDown(self):
        self.conn.close()

    def test_connects_sqlite_and_creates_schema(self):
        self.assertFalse(self.is_pg)
        imp.ensure_schema(self.conn, self.is_pg)
        tables = {r[0] for r in self.conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table'").fetchall()}
        self.assertIn("gd_spot_metric", tables)
        self.assertIn("gd_pm_fetch_run", tables)

    def test_row_access_by_name(self):
        self.conn.execute("CREATE TABLE t(a INTEGER, b TEXT)")
        self.conn.execute("INSERT INTO t(a,b) VALUES (1,'x')")
        row = self.conn.execute("SELECT a,b FROM t").fetchone()
        self.assertEqual(row["a"], 1)
        self.assertEqual(row["b"], "x")

    def test_executemany_rowcount(self):
        self.conn.execute("CREATE TABLE t(a INTEGER PRIMARY KEY)")
        cur = self.conn.executemany("INSERT INTO t(a) VALUES (?)", [(1,), (2,)])
        self.assertEqual(cur.rowcount, 2)


if __name__ == "__main__":
    unittest.main()
