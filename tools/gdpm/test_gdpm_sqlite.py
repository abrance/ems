#!/usr/bin/env python3
"""gdpm_sqlite.py 的回归测试：口径、作废留痕、重跑幂等。"""

import csv
import os
import sqlite3
import tempfile
import unittest

import gdpm_sqlite as imp


def make_csv(path, rows):
    with open(path, "w", newline="", encoding="utf-8-sig") as f:
        w = csv.writer(f)
        w.writerow(["date", "dataset", "scope", "metric", "value", "unit", "source"])
        w.writerows(rows)


class ImportTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "db.sqlite3")
        conn = sqlite3.connect(self.db)
        conn.execute(imp.DDL_METRIC)
        conn.execute("""CREATE TABLE gd_spot_metric_audit (
            id INTEGER PRIMARY KEY AUTOINCREMENT, report_date VARCHAR(10), run_date VARCHAR(10),
            dataset VARCHAR(32), scope VARCHAR(32), metric VARCHAR(32), old_value REAL,
            new_value REAL, old_origin VARCHAR(8), operation VARCHAR(32), operator VARCHAR(64),
            created_at DATETIME)""")
        # 两条 OCR 旧行：一条与新值同义（平均电价），一条同名不同义（最高电价）
        conn.executemany(
            "INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,unit,origin,source)"
            " VALUES (?,?,?,?,?,?,?,?,?)",
            [("2026-09-16", "2026-09-16", "day_ahead_trade", "发电侧", "平均电价", 428.95,
              "元/兆瓦时", "manual", "表1(二)"),
             ("2026-09-16", "2026-09-16", "day_ahead_trade", "发电侧", "最高电价", 1420.14,
              "元/兆瓦时", "manual", "表1(二)")])
        conn.commit()
        conn.close()
        self.csv = os.path.join(self.dir, "long.csv")
        make_csv(self.csv, [
            ("2026-09-16", "day_ahead_trade", "发电侧", "发电侧日前加权平均电价", 428.947306, "元/兆瓦时", "pm.gd.csg.cn"),
            ("2026-09-16", "day_ahead_trade", "发电侧", "发电侧日前加权最高电价", 514.995477, "元/兆瓦时", "pm.gd.csg.cn"),
            ("2026-09-16", "realtime_trade", "发电侧", "发电侧实时加权平均电价", 420.534632, "元/兆瓦时", "pm.gd.csg.cn"),
        ])

    def run_import(self):
        records = imp.load_long(self.csv)
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        p = imp.plan(conn, records)
        written = imp.apply(conn, records, p, log=lambda *_: None)
        out = imp.build_report(conn, records, p, imp.anomalies(records), written)
        conn.close()
        return out

    def test_report_date_rules(self):
        self.run_import()
        conn = sqlite3.connect(self.db)
        got = dict(conn.execute(
            "SELECT dataset, report_date FROM gd_spot_metric WHERE origin='pm-api'").fetchall())
        self.assertEqual(got["day_ahead_trade"], "2026-09-16")   # 日前：与数据日同日
        self.assertEqual(got["realtime_trade"], "2026-09-18")    # 实时：T+2 发布日
        conn.close()

    def test_old_rows_voided_with_audit(self):
        self.run_import()
        conn = sqlite3.connect(self.db)
        self.assertEqual(conn.execute(
            "SELECT COUNT(*) FROM gd_spot_metric WHERE origin='void'").fetchone()[0], 2)
        ops = [r[0] for r in conn.execute(
            "SELECT operation FROM gd_spot_metric_audit")]
        self.assertEqual(ops, [imp.AUDIT_DEPRECATE] * 2)
        conn.close()

    def test_overlap_diff_only_for_same_semantics(self):
        out = self.run_import()
        self.assertEqual(len(out["overlap_diff"]), 1)
        d = out["overlap_diff"][0]
        self.assertEqual(d["metric_api"], "发电侧日前加权平均电价")
        self.assertAlmostEqual(d["ocr"], 428.95)
        self.assertAlmostEqual(d["delta"], -0.002694, places=6)
        # 最高电价同名不同义：只出现在「口径不同」里，不进 diff
        self.assertFalse(any(x["metric_api"] == "发电侧日前加权最高电价"
                             for x in out["overlap_diff"]))

    def test_rerun_is_idempotent(self):
        self.run_import()
        first = self.run_import()
        conn = sqlite3.connect(self.db)
        total = conn.execute("SELECT COUNT(*) FROM gd_spot_metric").fetchone()[0]
        audit = conn.execute("SELECT COUNT(*) FROM gd_spot_metric_audit").fetchone()[0]
        runs = conn.execute("SELECT COUNT(*) FROM gd_pm_fetch_run").fetchone()[0]
        conn.close()
        self.assertEqual(total, 5)          # 2 作废 + 3 pm-api，重跑不多出行
        self.assertEqual(audit, 2)          # 已作废的行不会被再作废一次
        self.assertEqual(runs, 2)           # 每天每 dataset 一条
        # 首跑已把旧行作废；重跑时没有「待比对的 OCR 行」，diff 自然为空
        self.assertEqual(len(first["overlap_diff"]), 0)

    def test_fetch_run_metadata(self):
        self.run_import()
        conn = sqlite3.connect(self.db)
        rows = dict(conn.execute(
            "SELECT dataset, rows_written FROM gd_pm_fetch_run").fetchall())
        conn.close()
        self.assertEqual(rows, {"day_ahead_trade": 2, "realtime_trade": 1})

    # ---- 月频/年频 ----

    def write_periodic(self):
        """月频、年频各一份（列名用源站中文原样，含单位括号与 % 后缀）。"""
        with open(os.path.join(self.dir, "metrics_mid_long_term_month.csv"),
                  "w", newline="", encoding="utf-8-sig") as f:
            w = csv.writer(f)
            w.writerow(["月份", "净合约成交电量（MWh）", "累计合约成交电量（MWh）",
                        "净合约成交均价（元/MWh）", "累计合约成交均价（元/MWh）"])
            w.writerow(["2026-10", "7624266.682", "9309767.053", "454.966444", "449.738034"])
            w.writerow(["2026-09", "", "8800000.0", "447.1", ""])  # 空值跳行
        with open(os.path.join(self.dir, "metrics_hhi.csv"), "w", newline="",
                  encoding="utf-8-sig") as f:
            w = csv.writer(f)
            w.writerow(["年份", "HHI指标"])
            w.writerow(["2025", "319329.39"])
        with open(os.path.join(self.dir, "metrics_top_m.csv"), "w", newline="",
                  encoding="utf-8-sig") as f:
            w = csv.writer(f)
            w.writerow(["年份", "月份", "TOP1", "TOP4"])
            w.writerow(["2025", "12", "26.5758273646145%", "54.4430293709741%"])

    def test_green_power_blank_name_falls_back_to_serial(self):
        """2024 绿电行序列名称为空：scope 必须回落序列编号，否则同键覆盖丢交易。"""
        with open(os.path.join(self.dir, "metrics_green_power_year.csv"),
                  "w", newline="", encoding="utf-8-sig") as f:
            w = csv.writer(f)
            w.writerow(["序列编号", "序列名称", "年份", "成交电量（MWh）",
                        "电能量成交均价（元/MWh）", "绿色环境价值成交均价（元/MWh）"])
            w.writerow(["22000", "", "2024", "30199.0", "", "8.691943"])
            w.writerow(["21952", "", "2024", "62505.98", "464.6235", "8.941141"])
            w.writerow(["1088", "2025年度绿电双边协商交易（2024-12-11）", "2025",
                        "3499387.0", "385.842796", "5.977884"])
        records = [r for r in imp.load_periodic(self.dir)[0] if r[1] == "green_power_year"]
        scopes = {s for _d, _ds, s, _m, _v, _u in records if s}
        # 有名称用名称（页面按名称分组），没名称用编号
        self.assertEqual(scopes, {"22000", "21952", "2025年度绿电双边协商交易（2024-12-11）"})
        # 关键：三行交易的 (期次, dataset, scope, metric) 必须互不相同
        keys = [(d, ds, s, m) for d, ds, s, m, _v, _u in records]
        self.assertEqual(len(keys), len(set(keys)))


    def test_periodic_dates_and_units(self):
        self.write_periodic()
        records, loaded, skipped = imp.load_periodic(self.dir)
        self.assertEqual(sorted(loaded), ["metrics_hhi.csv", "metrics_mid_long_term_month.csv",
                                         "metrics_top_m.csv"])
        self.assertEqual(len(skipped), len(imp.PERIODIC) - 3)
        # 期次落到期初第一天：月 -> 01 日，年 -> 01-01；否则月频行会漏进日频区间查询
        months = {(ds, day) for day, ds, _s, _m, _v, _u in records if ds == "mid_long_term_month"}
        self.assertEqual(months, {("mid_long_term_month", "2026-10-01"),
                                  ("mid_long_term_month", "2026-09-01")})
        years = {day for day, ds, _s, _m, _v, _u in records if ds == "hhi"}
        self.assertEqual(years, {"2025-01-01"})
        units = {m: u for _d, _ds, _s, m, _v, u in records if _ds == "mid_long_term_month"}
        self.assertEqual(units["净合约成交均价"], "元/兆瓦时")
        self.assertEqual(units["净合约成交电量"], "MWh")
        # 空单元格不生成 0 值行，也不留空 sc ope 之外的脏行
        self.assertFalse(any(v == 0.0 for _d, _ds, _s, _m, v, _u in records))
        # "26.5%" 要变 26.5，不能丢掉整行
        top = {m: v for _d, ds, _s, m, v, _u in records if ds == "top_m"}
        self.assertAlmostEqual(top["TOP1"], 26.5758273646145)

    def test_periodic_imported_and_idempotent(self):
        self.write_periodic()
        records = imp.load_long(self.csv) + imp.load_periodic(self.dir)[0]
        for _ in range(2):
            conn = sqlite3.connect(self.db)
            conn.row_factory = sqlite3.Row
            p = imp.plan(conn, records)
            written = imp.apply(conn, records, p, log=lambda *_: None)
            out = imp.build_report(conn, records, p, imp.anomalies(records), written)
            conn.close()
        conn = sqlite3.connect(self.db)
        got = dict(conn.execute(
            "SELECT dataset, COUNT(*) FROM gd_spot_metric WHERE origin='pm-api'"
            " GROUP BY dataset").fetchall())
        conn.close()
        self.assertEqual(got["mid_long_term_month"], 6)   # 4 + 2（空单元格不生成行）
        self.assertEqual(got["hhi"], 1)
        self.assertEqual(got["top_m"], 2)
        # 月频不当成日频：coverage 不追每日缺失
        self.assertEqual(out["coverage"]["mid_long_term_month"]["missing"], [])
        self.assertEqual(out["coverage"]["mid_long_term_month"]["periods"], 2)


class VoidScopeTest(unittest.TestCase):
    """作废范围：只有官方整表取代的「日前/实时成交电价」四类指标，其余行原样不动。"""

    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.db = os.path.join(self.dir, "db.sqlite3")
        conn = sqlite3.connect(self.db)
        conn.execute(imp.DDL_METRIC)
        conn.execute("""CREATE TABLE gd_spot_metric_audit (
            id INTEGER PRIMARY KEY AUTOINCREMENT, report_date VARCHAR(10), run_date VARCHAR(10),
            dataset VARCHAR(32), scope VARCHAR(32), metric VARCHAR(32), old_value REAL,
            new_value REAL, old_origin VARCHAR(8), operation VARCHAR(32), operator VARCHAR(64),
            created_at DATETIME)""")
        rows = [
            # 要被官方取代的：发电侧四类 + 燃煤/新能源分电源行（同一张成交电价表）
            ("2026-09-16", "day_ahead_trade", "发电侧", "平均电价", 428.95),
            ("2026-09-16", "day_ahead_trade", "发电侧", "最高电价", 1420.14),
            ("2026-09-16", "day_ahead_trade", "燃煤", "平均电价", 430.10),
            ("2026-09-16", "realtime_trade", "发电侧", "电价环比", -1.2),
            # 官方不给数的：不能动
            ("2026-09-16", "node_price_peak", "广州", "价格档位", 4),
            ("2026-09-16", "day_ahead_decl", "发电侧", "申报容量", 12345.0),
            ("2026-09-16", "day_ahead_decl", "发电侧", "新能源容量", 6789.0),
            ("2026-09-16", "summary", "市场", "燃煤均价", 429.85),
            # 官方当日没有数据（实时 T+2 未发布）→ 不当作已被取代
            ("2026-09-30", "realtime_trade", "发电侧", "平均电价", 400.0),
        ]
        conn.executemany(
            "INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,"
            "unit,origin,source) VALUES (?,?,?,?,?,?,'元/兆瓦时','manual','表1(二)')",
            [(d, d, ds, sc, mt, v) for d, ds, sc, mt, v in rows])
        conn.commit()
        conn.close()
        self.csv = os.path.join(self.dir, "long.csv")
        make_csv(self.csv, [
            ("2026-09-16", "day_ahead_trade", "发电侧", "发电侧日前加权平均电价", 428.9, "元/兆瓦时", "pm.gd.csg.cn"),
            ("2026-09-16", "realtime_trade", "发电侧", "发电侧实时加权平均电价", 420.5, "元/兆瓦时", "pm.gd.csg.cn"),
        ])

    def run_import(self):
        records = imp.load_long(self.csv)
        conn = sqlite3.connect(self.db)
        conn.row_factory = sqlite3.Row
        p = imp.plan(conn, records)
        written = imp.apply(conn, records, p, log=lambda *_: None)
        out = imp.build_report(conn, records, p, imp.anomalies(records), written)
        conn.close()
        return out, p

    def test_only_replaced_categories_are_voided(self):
        _out, p = self.run_import()
        self.assertEqual(len(p["voids"]), 4)
        conn = sqlite3.connect(self.db)
        voided = dict(conn.execute(
            "SELECT metric||'@'||scope||'@'||dataset, COUNT(*) FROM gd_spot_metric"
            " WHERE origin='void' GROUP BY 1").fetchall())
        kept = dict(conn.execute(
            "SELECT dataset||'/'||metric, origin FROM gd_spot_metric"
            " WHERE origin<>'void' AND origin<>'pm-api'").fetchall())
        conn.close()
        self.assertEqual(voided, {"平均电价@发电侧@day_ahead_trade": 1,
                                  "最高电价@发电侧@day_ahead_trade": 1,
                                  "平均电价@燃煤@day_ahead_trade": 1,
                                  "电价环比@发电侧@realtime_trade": 1})
        # 档位、申报容量/台数、燃煤均价、以及官方还没发布那天的实时行，origin 全不变
        self.assertEqual(kept, {"node_price_peak/价格档位": "manual",
                                "day_ahead_decl/申报容量": "manual",
                                "day_ahead_decl/新能源容量": "manual",
                                "summary/燃煤均价": "manual",
                                "realtime_trade/平均电价": "manual"})

    def test_every_void_has_audit_row_and_basis(self):
        out, p = self.run_import()
        conn = sqlite3.connect(self.db)
        ops = conn.execute(
            "SELECT dataset, metric, old_origin, operation FROM gd_spot_metric_audit"
            " ORDER BY id").fetchall()
        conn.close()
        self.assertEqual(len(ops), 4)
        self.assertTrue(all(o[3] == imp.AUDIT_DEPRECATE and o[2] == "manual" for o in ops))
        # 每条作废都写清依据哪条官方指标取代（报告里逐条可查）
        self.assertEqual(out["void_summary"], {"day_ahead_trade/平均电价": 2,
                                               "day_ahead_trade/最高电价": 1,
                                               "realtime_trade/电价环比": 1})
        self.assertTrue(all(v["replaced_by"] in ("发电侧日前加权平均电价",
                                                 "发电侧实时加权平均电价")
                            for v in out["void_plan"]))
        for v in p["voids"]:
            self.assertIsNotNone(v["api_value"])

    def test_previously_voided_rows_outside_scope_are_restored(self):
        """上一版一刀切把档位/容量/均价埋了：按审计里的原 origin 放回去，且只放一次。"""
        conn = sqlite3.connect(self.db)
        conn.executemany(
            "UPDATE gd_spot_metric SET origin='void' WHERE dataset=? AND metric=?",
            [("node_price_peak", "价格档位"), ("day_ahead_decl", "申报容量"),
             ("day_ahead_decl", "新能源容量"), ("summary", "燃煤均价")])
        conn.executemany(
            "INSERT INTO gd_spot_metric_audit (report_date,run_date,dataset,scope,metric,"
            "old_value,new_value,old_origin,operation,operator)"
            " SELECT report_date,run_date,dataset,scope,metric,value,NULL,'manual',?,?"
            " FROM gd_spot_metric WHERE dataset=? AND metric=?",
            [(imp.AUDIT_DEPRECATE, imp.OPERATOR, "node_price_peak", "价格档位"),
             (imp.AUDIT_DEPRECATE, imp.OPERATOR, "day_ahead_decl", "申报容量"),
             (imp.AUDIT_DEPRECATE, imp.OPERATOR, "day_ahead_decl", "新能源容量"),
             (imp.AUDIT_DEPRECATE, imp.OPERATOR, "summary", "燃煤均价")])
        conn.commit()
        conn.close()

        out, _p = self.run_import()
        self.assertEqual(out["written"]["restored"], 4)
        conn = sqlite3.connect(self.db)
        got = dict(conn.execute(
            "SELECT dataset||'/'||metric, origin FROM gd_spot_metric"
            " WHERE dataset IN ('node_price_peak','day_ahead_decl','summary')").fetchall())
        restores = conn.execute(
            "SELECT COUNT(*) FROM gd_spot_metric_audit WHERE operation=?",
            (imp.AUDIT_RESTORE,)).fetchone()[0]
        conn.close()
        self.assertEqual(set(got.values()), {"manual"})
        self.assertEqual(restores, 4)

        # 重跑：已复原的行不再出现在 restore 计划里，审计不重复写
        again, p2 = self.run_import()
        self.assertEqual(again["written"]["restored"], 0)
        self.assertEqual(p2["restores"], [])
        conn = sqlite3.connect(self.db)
        self.assertEqual(conn.execute(
            "SELECT COUNT(*) FROM gd_spot_metric WHERE origin='void'").fetchone()[0], 4)
        self.assertEqual(conn.execute(
            "SELECT COUNT(*) FROM gd_spot_metric_audit WHERE operation=?",
            (imp.AUDIT_RESTORE,)).fetchone()[0], 4)
        conn.close()

    def test_void_row_without_audit_stays_void(self):
        """没有审计可查的老 void 行：不知道原 origin 就不乱改，保持 void。"""
        conn = sqlite3.connect(self.db)
        conn.execute("UPDATE gd_spot_metric SET origin='void' WHERE metric='价格档位'")
        conn.commit()
        conn.close()
        out, p = self.run_import()
        self.assertEqual(p["restores"], [])
        self.assertEqual(out["written"]["restored"], 0)
        conn = sqlite3.connect(self.db)
        self.assertEqual(conn.execute(
            "SELECT origin FROM gd_spot_metric WHERE metric='价格档位'").fetchone()[0], "void")
        conn.close()


if __name__ == "__main__":
    unittest.main()