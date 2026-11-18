#!/usr/bin/env python3
"""gdpm_report_gap.py 的回归测试：缺口判定、partial 留痕不重复、不降级 ok。"""

import json
import os
import sqlite3
import tempfile
import unittest

import gdpm_report_gap as gap


def make_db(path, reports, runs):
    conn = sqlite3.connect(path)
    conn.execute("""CREATE TABLE gd_spot_report (
        id INTEGER PRIMARY KEY AUTOINCREMENT, run_date VARCHAR(10) NOT NULL UNIQUE,
        title VARCHAR(255), source_url TEXT, pages TEXT, collected_at DATETIME,
        created_at DATETIME, updated_at DATETIME)""")
    conn.execute("""CREATE TABLE gd_spot_extract_run (
        id INTEGER PRIMARY KEY AUTOINCREMENT, report_date VARCHAR(10) NOT NULL UNIQUE,
        status VARCHAR(16) DEFAULT '', ocr_model VARCHAR(16) DEFAULT '', page_count INTEGER DEFAULT 0,
        ocr_ms INTEGER DEFAULT 0, warnings TEXT, payload TEXT,
        extractor_version VARCHAR(16) DEFAULT '', created_at DATETIME, updated_at DATETIME)""")
    conn.executemany("INSERT INTO gd_spot_report (run_date,title,source_url,pages)"
                     " VALUES (?,?,?,'[]')", reports)
    conn.executemany("INSERT INTO gd_spot_extract_run (report_date,status,warnings)"
                     " VALUES (?,?,?)", runs)
    conn.commit()
    conn.close()


class GapTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "db.sqlite3")
        make_db(self.db,
                [("2026-09-26", "日报", "https://example.test/s/a"),
                 ("2026-09-27", "日报", "https://example.test/s/b"),
                 ("2026-09-29", "日报", "https://example.test/s/d")],
                [("2026-09-26", "ok", ""),
                 ("2026-09-27", "error", "[]"),
                 ("2026-09-29", "partial",
                  json.dumps([gap.PARTIAL_WARNING], ensure_ascii=False))])

    def linkfile(self, text):
        path = os.path.join(self.dir, "links.tsv")
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        return path

    def test_actions(self):
        links = gap.read_links(self.linkfile(
            "# 注释\n2026-09-28\thttps://example.test/s/c\n"))
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        rows = {r["date"]: r for r in gap.plan(conn, "2026-09-26", "2026-09-29", links)}
        conn.close()
        self.assertEqual(rows["2026-09-26"]["action"], "have_report")
        self.assertEqual(rows["2026-09-27"]["action"], "recollect")   # 采过但解析失败
        self.assertEqual(rows["2026-09-28"]["action"], "collect")     # 有链接
        self.assertEqual(rows["2026-09-29"]["action"], "have_report")  # partial 不重复采

    def test_missing_link_marks_partial_once(self):
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        rows = gap.plan(conn, "2026-09-30", "2026-10-01", {})
        self.assertEqual({r["action"] for r in rows}, {"mark_partial"})
        self.assertEqual(gap.mark_partial(conn, [r["date"] for r in rows], "2026-11-12"), 2)
        conn.commit()
        # 再跑：已留痕的日期不再重复写，也不重复计数
        again = gap.plan(conn, "2026-09-30", "2026-10-01", {})
        self.assertEqual({r["action"] for r in again}, {"already_marked_partial"})
        self.assertEqual(gap.mark_partial(conn, [], "2026-11-12"), 0)
        got = dict(conn.execute(
            "SELECT report_date, status FROM gd_spot_extract_run"
            " WHERE report_date >= '2026-09-30'").fetchall())
        conn.close()
        self.assertEqual(got, {"2026-09-30": "partial", "2026-10-01": "partial"})

    def test_partial_marking_never_downgrades_ok(self):
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        n = gap.mark_partial(conn, ["2026-09-26"], "2026-11-12")
        conn.commit()
        status = conn.execute("SELECT status FROM gd_spot_extract_run"
                              " WHERE report_date='2026-09-26'").fetchone()[0]
        warnings = conn.execute("SELECT warnings FROM gd_spot_extract_run"
                                " WHERE report_date='2026-09-26'").fetchone()[0]
        conn.close()
        self.assertEqual(n, 0)
        self.assertEqual(status, "ok")
        self.assertNotIn(gap.PARTIAL_WARNING, warnings or "")


if __name__ == "__main__":
    unittest.main()
