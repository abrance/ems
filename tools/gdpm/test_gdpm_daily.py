#!/usr/bin/env python3
"""gdpm_daily.py 的回归测试：抓取失败必须留痕且不写指标表。

线上反馈的原问题是「失败在库里完全不可见」：`gd_pm_fetch_run` 的 status 被写死 'ok'，
`--apply` 只在解析成功后调用，抓取一失败就什么都不写，看库永远像正常。
这里锁住两件事：失败落一行 status='failed'，且 gd_spot_metric 一行都不许有。
"""

import csv
import os
import sqlite3
import tempfile
import unittest

import gdpm_daily as daily
import gdpm_sqlite as imp


def fake_fetch_failing(out_dir, only=None, log=print):
    raise RuntimeError("http=000 timeout 15s bytes=0")


def fake_fetch_ok(out_dir, only=None, log=print):
    """写一份最小长表，形状与 gdpm_dataset.dump() 的产物一致。"""
    os.makedirs(out_dir, exist_ok=True)
    with open(os.path.join(out_dir, "gdpm_long.csv"), "w", newline="",
              encoding="utf-8-sig") as f:
        w = csv.writer(f)
        w.writerow(["date", "dataset", "scope", "metric", "value", "unit", "source"])
        w.writerow(["2026-11-13", "day_ahead_trade", "发电侧", "发电侧日前加权平均电价",
                    "456.55", "元/兆瓦时", "pm.gd.csg.cn"])


class FetchFailureTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "db.sqlite3")
        self.work = os.path.join(self.dir, "work")

    def rows(self, sql, params=()):
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        try:
            return conn.execute(sql, params).fetchall()
        finally:
            conn.close()

    def metric_table_missing(self):
        conn = sqlite3.connect(self.db)
        try:
            return conn.execute(
                "SELECT COUNT(*) FROM sqlite_master WHERE type='table'"
                " AND name='gd_spot_metric'").fetchone()[0] == 0
        finally:
            conn.close()

    def test_fetch_error_writes_failed_row_and_exits_nonzero(self):
        rc = daily.run(db=self.db, work_dir=self.work,
                       fetch=fake_fetch_failing, today="2026-11-14")
        self.assertEqual(rc, 1, "抓取失败必须非零退出，否则 CronJob 看着是成功的")

        runs = self.rows("SELECT * FROM gd_pm_fetch_run")
        self.assertEqual(len(runs), 1, f"应只有一行采集元数据，实际 {len(runs)}")
        run = runs[0]
        self.assertEqual(run["status"], "failed")
        self.assertEqual(run["dataset"], "fetch")
        self.assertEqual(run["target_date"], "2026-11-14")
        self.assertIn("http=000", run["warnings"])
        self.assertIn("RuntimeError", run["warnings"])

        if not self.metric_table_missing():
            got = self.rows("SELECT COUNT(*) AS n FROM gd_spot_metric")[0]["n"]
            self.assertEqual(got, 0, "抓取失败时不许写任何 gd_spot_metric 行")

    def test_unwritable_db_does_not_crash_process(self):
        """库打不开时留痕写不进去，但退出码必须还是非零，不能抛异常。"""
        bad_db = os.path.join(self.dir, "nope", "deep", "db.sqlite3")
        rc = daily.run(db=bad_db, work_dir=self.work,
                       fetch=fake_fetch_failing, today="2026-11-14")
        self.assertEqual(rc, 1, "留痕失败也必须非零退出，否则 CronJob 看着是成功的")

    def test_failure_row_is_idempotent(self):
        for _ in range(2):
            daily.run(db=self.db, work_dir=self.work,
                      fetch=fake_fetch_failing, today="2026-11-14")
        self.assertEqual(len(self.rows("SELECT * FROM gd_pm_fetch_run")), 1,
                         "同一天重跑不许堆行（target_date+dataset+extractor_version 唯一）")

    def test_success_still_writes_ok_rows_and_metrics(self):
        rc = daily.run(db=self.db, work_dir=self.work,
                       fetch=fake_fetch_ok, today="2026-11-14")
        self.assertEqual(rc, 0)

        self.assertEqual(self.rows("SELECT COUNT(*) AS n FROM gd_spot_metric"
                                   " WHERE origin=?", (imp.ORIGIN,))[0]["n"], 1)
        statuses = [r["status"] for r in self.rows("SELECT status FROM gd_pm_fetch_run")]
        self.assertEqual(statuses, ["ok"], "成功路径保持原样：只写 status='ok'")

    def test_empty_fetch_is_failure(self):
        def fake_fetch_empty(out_dir, only=None, log=print):
            os.makedirs(out_dir, exist_ok=True)
            with open(os.path.join(out_dir, "gdpm_long.csv"), "w", newline="",
                      encoding="utf-8-sig") as f:
                csv.writer(f).writerow(["date", "dataset", "scope", "metric",
                                        "value", "unit", "source"])

        rc = daily.run(db=self.db, work_dir=self.work,
                       fetch=fake_fetch_empty, today="2026-11-14")
        self.assertEqual(rc, 1, "抓到 0 行比抓取异常更可疑，不能静默成功")
        run = self.rows("SELECT * FROM gd_pm_fetch_run")[0]
        self.assertEqual(run["status"], "failed")
        self.assertIn("0 行", run["warnings"])


if __name__ == "__main__":
    unittest.main()
