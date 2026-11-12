#!/usr/bin/env python3
"""gdpm_tier_backfill.py 的回归测试：payload 取数、幂等回灌、不覆盖官方行。"""

import json
import os
import sqlite3
import tempfile
import unittest

import gdpm_sqlite as base
import gdpm_tier_backfill as tier


def payload(day, rows):
    return json.dumps({"runDate": day, "metrics": rows}, ensure_ascii=False)


def tier_row(dataset, scope, value, text, origin="cv", source="图2-1"):
    return {"RunDate": "", "Dataset": dataset, "Scope": scope, "Metric": "价格档位",
            "Value": value, "Unit": "", "AtTime": "", "TextValue": text, "MomPct": None,
            "SharePct": None, "Source": source, "Conf": 0.9, "Origin": origin, "Text": ""}


class TierBackfillTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "db.sqlite3")
        conn = sqlite3.connect(self.db)
        conn.execute(base.DDL_METRIC)
        conn.execute("""CREATE TABLE gd_spot_extract_run (
            id INTEGER PRIMARY KEY AUTOINCREMENT, report_date VARCHAR(10) NOT NULL UNIQUE,
            status VARCHAR(16), ocr_model VARCHAR(16), page_count INTEGER, ocr_ms INTEGER,
            warnings TEXT, payload TEXT, extractor_version VARCHAR(16), created_at DATETIME,
            updated_at DATETIME)""")
        conn.executemany(
            "INSERT INTO gd_spot_extract_run (report_date,status,payload) VALUES (?,?,?)",
            [("2026-09-16", "ok", payload("2026-09-16", [
                tier_row("node_price_peak", "广州", 4, "[600,800)"),
                tier_row("node_price_valley", "广州", 1, "[-inf,200)", source="图2-2"),
                # 同一天重复一份（日报重跑）→ 唯一键去重，不能写两行
                tier_row("node_price_peak", "广州", 4, "[600,800)"),
                # 非档位指标：不归本脚本管
                {"RunDate": "", "Dataset": "summary", "Scope": "市场", "Metric": "燃煤均价",
                 "Value": 429.8, "Unit": "元/兆瓦时", "TextValue": "", "Source": "正文",
                 "Conf": 1, "Origin": ""},
            ])),
             ("2026-09-17", "partial", payload("2026-09-17", [
                 tier_row("node_price_peak", "深圳", 5, "[800,+inf)")]))])
        conn.commit()
        conn.close()

    def backfill(self):
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        rows = tier.load_payload_rows(conn)
        p = tier.plan(conn, rows)
        written = tier.apply(conn, p, "2026-11-12 12:00:00+08:00")
        conn.commit()
        report = tier.build_report(p, written)
        conn.close()
        return report

    def test_only_tier_rows_and_report_date_equals_run_date(self):
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        rows = tier.load_payload_rows(conn)
        conn.close()
        self.assertEqual(len(rows), 3)                      # 去重后 3 行，summary 不要
        self.assertEqual({r["dataset"] for r in rows}, {"node_price_peak", "node_price_valley"})
        self.assertTrue(all(r["report_date"] == r["run_date"] for r in rows))
        self.assertEqual({r["text_value"] for r in rows}, {"[600,800)", "[-inf,200)", "[800,+inf)"})

    def test_backfill_inserts_and_is_idempotent(self):
        first = self.backfill()
        self.assertEqual(first["by_action"], {"insert": 3})
        self.assertEqual(first["written"], 3)
        second = self.backfill()
        self.assertEqual(second["by_action"], {"update": 3})
        conn = sqlite3.connect(self.db)
        n = conn.execute("SELECT COUNT(*) FROM gd_spot_metric").fetchone()[0]
        origins = {r[0] for r in conn.execute("SELECT DISTINCT origin FROM gd_spot_metric")}
        conn.close()
        self.assertEqual(n, 3)                              # 重跑不多出行
        self.assertEqual(origins, {"cv"})                   # 没确认过的日期就是预填值，要人核

    def test_confirmed_rows_restore_as_manual(self):
        """审计里有 confirm 记录的行（2026-09-22 确认过）回灌 manual，不降级成 cv。"""
        conn = sqlite3.connect(self.db)
        conn.execute("""CREATE TABLE gd_spot_metric_audit (
            id INTEGER PRIMARY KEY AUTOINCREMENT, report_date VARCHAR(10), run_date VARCHAR(10),
            dataset VARCHAR(32), scope VARCHAR(32), metric VARCHAR(32), old_value REAL,
            new_value REAL, old_origin VARCHAR(8), operation VARCHAR(32), operator VARCHAR(64),
            created_at DATETIME)""")
        conn.execute(
            "INSERT INTO gd_spot_metric_audit (report_date,run_date,dataset,scope,metric,"
            "old_origin,operation) VALUES ('2026-09-16','2026-09-16','node_price_peak',"
            "'广州','价格档位','cv','confirm')")
        conn.commit()
        conn.row_factory = sqlite3.Row
        rows = tier.load_payload_rows(conn)
        p = {r["scope"] + r["dataset"]: r for r in tier.plan(conn, rows)}
        conn.close()
        self.assertEqual(p["广州node_price_peak"]["origin"], "manual")
        self.assertEqual(p["广州node_price_valley"]["origin"], "cv")

    def test_voided_rows_come_back(self):
        self.backfill()
        conn = sqlite3.connect(self.db)
        conn.execute("UPDATE gd_spot_metric SET origin='void'")   # 模拟上一版一刀切
        conn.commit()
        conn.row_factory = sqlite3.Row
        rows = tier.load_payload_rows(conn)
        p = tier.plan(conn, rows)
        written = tier.apply(conn, p, "2026-11-12 12:00:00+08:00")
        conn.commit()
        origins = {r[0] for r in conn.execute("SELECT DISTINCT origin FROM gd_spot_metric")}
        conn.close()
        self.assertEqual({r["action"] for r in p}, {"restore"})
        self.assertEqual(written, 3)
        self.assertEqual(origins, {"cv"})

    def test_pm_api_rows_are_never_overwritten(self):
        conn = sqlite3.connect(self.db)
        conn.execute(
            "INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,"
            "unit,origin) VALUES ('2026-09-16','2026-09-16','node_price_peak','广州',"
            "'价格档位',9,'%s','pm-api')" % "")
        conn.commit()
        conn.row_factory = sqlite3.Row
        rows = tier.load_payload_rows(conn)
        p = tier.plan(conn, rows)
        written = tier.apply(conn, p, "2026-11-12 12:00:00+08:00")
        conn.commit()
        got = conn.execute(
            "SELECT value, origin FROM gd_spot_metric WHERE scope='广州'"
            " AND dataset='node_price_peak'").fetchone()
        conn.close()
        self.assertEqual(written, 2)                        # 广州 peak 那行被跳过
        self.assertEqual(tuple(got), (9.0, "pm-api"))


if __name__ == "__main__":
    unittest.main()
