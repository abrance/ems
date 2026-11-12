#!/usr/bin/env python3
"""广东电力交易中心官方指标 -> ems 业务库 `gd_spot_metric`。

官方 API（pm.gd.csg.cn）是**唯一权威源**：OCR 日报旧值与第三方 gdmetric CSV 都作废。
本脚本只做「文件 -> 业务表」：读 `gdpm_dataset.py` 产出的长表 CSV，写
`gd_spot_metric`（origin=pm-api）、采集元数据表 `gd_pm_fetch_run`，并把被官方整表取代的
旧行（日前/实时成交电价四类指标）标成 origin=void，逐条写审计；官方不给数的指标
（地市档位、申报容量/台数、燃煤·燃气均价）保持原 origin，上一版误伤的按审计原值放回。

口径（与 OCR 日报现有约定一致，已用生产库核对）：
    日前：report_date = run_date = 数据日（日报日 D 的日前出清落在 D）
    实时：run_date = 数据日（运行日），report_date = 数据日 + 2（T+2 发布）
    电价 unit = 元/兆瓦时；电量 unit = MWh

作废范围就是 `VOID_DATASETS × VOID_METRICS`（见下方常量），不再是一刀切；
改这个范围前先确认官方是否真的提供了该指标，否则作废等于丢数据（2026-11-11 踩过）。

用法：
    python3 gdpm_sqlite.py --db /tmp/db.sqlite3 --report /tmp/report.json          # 只算不写
    python3 gdpm_sqlite.py --db /tmp/db.sqlite3 --report /tmp/report.json --apply
"""

import argparse
import csv
import datetime as dt
import json
import os
import sqlite3
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.path.normpath(os.path.join(HERE, "..", "..", "data", "gdpm"))
DEFAULT_CSV = os.path.join(DATA_DIR, "gdpm_long.csv")

EXTRACTOR = "gdpm-1"
ORIGIN = "pm-api"
SOURCE = "gdpm-api"
OPERATOR = "gdpm-api"
VOID_ORIGIN = "void"
AUDIT_DEPRECATE = "deprecated_by_pm_api"
AUDIT_RESTORE = "restored_by_pm_api_scope_fix"

# 作废范围只到「官方整表取代」的这两类数据集 × 这四个指标。
# 之前是一刀切（origin NOT IN ('pm-api','void') 全作废），把官方根本不给数的行也埋了：
#   地市节点电价档位 232 行、日前申报容量/台数/煤机气机新能源容量 122 行、
#   燃煤·燃气均价 20 行 —— 这些指标 pm-api 没有，作废等于丢数据。
VOID_DATASETS = ("day_ahead_trade", "realtime_trade")
VOID_METRICS = ("平均电价", "最高电价", "最低电价", "电价环比")

# OCR 日报的指标名与官方指标名的对应关系（只有这两个指标两边同义，实测差 < 0.01）。
# 不映射的项目及原因：
#   OCR 最高/最低电价 = 市场限价维度（实测固定 -50 / 1800），官方「加权最高/最低电价」
#     是另一个量（实测 351/515），同名不同义，不能对应；
#   OCR 申报类「平均电价」vs 官方「日前申报均价」口径不同（实测系统性高 2%~4%）。
# 官方表没有「燃煤/燃气/新能源」分电源类型拆分，那部分只能保留 OCR 历史行。
METRIC_MAP = [
    ("day_ahead_trade", "发电侧", "平均电价", "发电侧日前加权平均电价"),
    ("realtime_trade", "发电侧", "平均电价", "发电侧实时加权平均电价"),
]
# 作废依据：用同日官方加权均价行证明「这份日报的成交电价表已被官方取代」
VOID_BASIS = {ds: api_metric for ds, _sc, ocr_metric, api_metric in METRIC_MAP
              if ocr_metric == "平均电价"}

# 同名不同义/口径不同，只在报告里列对照，不当成「同一指标的新值」
METRIC_NOTE = [
    ("day_ahead_trade", "发电侧", "最高电价", "发电侧日前加权最高电价", "限价维度 vs 加权最高价"),
    ("day_ahead_trade", "发电侧", "最低电价", "发电侧日前加权最低电价", "限价维度 vs 加权最低价"),
    ("day_ahead_decl", "发电侧", "平均电价", "日前申报均价", "简单均价 vs 加权均价"),
]

# 月频/年频模板 -> gd_spot_metric。期次统一落到**期初第一天**（月 "2026-10" -> 2026-10-01，
# 年 "2025" -> 2025-01-01）：这样能和日频共用 run_date 区间查询，粒度靠 dataset 区分。
# 不能把 "2026-10" 原样写进 run_date —— 字符串比较下 "2026-10" < "2026-10-05"，
# 月频行会漏进日频区间查询（预测取数就是这么被污染的）。
PERIODIC = [
    {
        "file": "metrics_mid_long_term_month.csv",
        "dataset": "mid_long_term_month",
        "date": ["月份"], "grain": "month", "scope": None,
        "metrics": [
            ("净合约成交电量（MWh）", "净合约成交电量", "MWh"),
            ("累计合约成交电量（MWh）", "累计合约成交电量", "MWh"),
            ("净合约成交均价（元/MWh）", "净合约成交均价", "元/兆瓦时"),
            ("累计合约成交均价（元/MWh）", "累计合约成交均价", "元/兆瓦时"),
        ],
    },
    {
        "file": "metrics_mid_long_term_year.csv",
        "dataset": "mid_long_term_year",
        "date": ["年份"], "grain": "year", "scope": None,
        "metrics": [
            ("净合约成交电量（MWh）", "净合约成交电量", "MWh"),
            ("累计合约成交电量（MWh）", "累计合约成交电量", "MWh"),
            ("净合约成交均价（元/MWh）", "净合约成交均价", "元/兆瓦时"),
            ("累计合约成交均价（元/MWh）", "累计合约成交均价", "元/兆瓦时"),
        ],
    },
    {
        "file": "metrics_green_power_month.csv",
        "dataset": "green_power_month",
        # 一条 gd_spot_metric 只能存一个数，绿电按「交易序列」拆行，页面再按需汇总
        "date": ["月份"], "grain": "month", "scope": "序列名称",
        "metrics": [
            ("成交电量（MWh）", "成交电量", "MWh"),
            ("电能量成交均价（元/MWh）", "电能量成交均价", "元/兆瓦时"),
            ("绿色环境价值成交均价（元/MWh）", "绿色环境价值成交均价", "元/兆瓦时"),
        ],
    },
    {
        "file": "metrics_green_power_year.csv",
        "dataset": "green_power_year",
        "date": ["年份"], "grain": "year", "scope": "序列名称",
        "metrics": [
            ("成交电量（MWh）", "成交电量", "MWh"),
            ("电能量成交均价（元/MWh）", "电能量成交均价", "元/兆瓦时"),
            ("绿色环境价值成交均价（元/MWh）", "绿色环境价值成交均价", "元/兆瓦时"),
        ],
    },
    {
        "file": "metrics_green_power_retail.csv",
        "dataset": "green_power_retail",
        "date": ["月份"], "grain": "month", "scope": None,
        "metrics": [
            ("电量（MWh）", "电量", "MWh"),
            ("均价（元/MWh）", "均价", "元/兆瓦时"),
            ("用户数（户）", "用户数", "户"),
        ],
    },
    {
        "file": "metrics_energy_retail.csv",
        "dataset": "energy_retail",
        "date": ["月份"], "grain": "month", "scope": "类型",
        "metrics": [
            ("电量（MWh）", "电量", "MWh"),
            ("均价（元/MWh）", "均价", "元/兆瓦时"),
            ("用户数（户）", "用户数", "户"),
        ],
    },
    {
        "file": "metrics_hhi.csv",
        "dataset": "hhi",
        "date": ["年份"], "grain": "year", "scope": None,
        "metrics": [("HHI指标", "HHI指标", "")],
    },
    {
        "file": "metrics_top_m.csv",
        "dataset": "top_m",
        "date": ["年份", "月份"], "grain": "month", "scope": None,
        "metrics": [("TOP1", "TOP1", "%"), ("TOP4", "TOP4", "%")],
    },
]

# 日频数据集：只有这几个能按「天」算缺日
DAILY_DATASETS = ("day_ahead_trade", "realtime_trade")

DDL_METRIC = """
CREATE TABLE IF NOT EXISTS gd_spot_metric (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_date VARCHAR(10) NOT NULL,
    run_date VARCHAR(10) NOT NULL,
    dataset VARCHAR(32) NOT NULL,
    scope VARCHAR(32) NOT NULL DEFAULT '',
    metric VARCHAR(32) NOT NULL,
    value REAL NOT NULL DEFAULT 0,
    unit VARCHAR(16) NOT NULL DEFAULT '',
    at_time VARCHAR(8) NOT NULL DEFAULT '',
    text_value VARCHAR(32) NOT NULL DEFAULT '',
    mom_pct REAL,
    share_pct REAL,
    source VARCHAR(32) NOT NULL DEFAULT '',
    conf REAL NOT NULL DEFAULT 0,
    extractor_version VARCHAR(16) NOT NULL DEFAULT '',
    origin VARCHAR(8) NOT NULL DEFAULT 'ocr',
    edited_by VARCHAR(64) NOT NULL DEFAULT '',
    edited_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (report_date, run_date, dataset, scope, metric))
"""

DDL_FETCH_RUN = """
CREATE TABLE IF NOT EXISTS gd_pm_fetch_run (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_date VARCHAR(10) NOT NULL,
    dataset VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT '',
    rows_written INTEGER NOT NULL DEFAULT 0,
    source_url VARCHAR(255) NOT NULL DEFAULT '',
    payload TEXT,
    warnings TEXT,
    extractor_version VARCHAR(16) NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (target_date, dataset, extractor_version))
"""

DATE_FMT = "%Y-%m-%d"


def shift(day, days):
    return (dt.datetime.strptime(day, DATE_FMT) + dt.timedelta(days=days)).strftime(DATE_FMT)


def report_of(dataset, day):
    """实时是 T+2 发布；日前与日报同日。"""
    return shift(day, 2) if dataset == "realtime_trade" else day


def load_long(path):
    """长表 CSV -> [(date,dataset,scope,metric,value,unit)]，同键去重（求和）。"""
    agg = {}
    with open(path, encoding="utf-8-sig") as f:
        rows = csv.DictReader(f)
        for row in rows:
            key = (row["date"], row["dataset"], row["scope"], row["metric"], row["unit"])
            agg[key] = agg.get(key, 0.0) + float(row["value"])
    return [(k[0], k[1], k[2], k[3], v, k[4]) for k, v in sorted(agg.items())]


def period_start(row, cols, grain):
    """期次列 -> 期初第一天；缺列或写不成日期返回 None。"""
    parts = [str(row.get(c) or "").strip() for c in cols]
    if not parts or not all(parts):
        return None
    if grain == "year":
        return "%04d-01-01" % int(parts[0])
    if len(parts) >= 2:
        year, month = parts[0], parts[1]
    else:
        try:
            year, month = parts[0].split("-")
        except ValueError:
            return None
    return "%04d-%02d-01" % (int(year), int(month))


def num(raw):
    """源站数值 -> float；空值/占位符/非数字返回 None。"""
    text = str(raw if raw is not None else "").strip().replace(",", "")
    if text.endswith("%"):
        text = text[:-1]
    if text in ("", "-", "--", "—", "null", "None"):
        return None
    try:
        return float(text)
    except ValueError:
        return None


def load_periodic(data_dir):
    """月频/年频 CSV -> 与日频同形状的记录。

    文件不存在就跳过（只要日频也能跑）；返回 (records, 载入的文件名, 跳过的文件名)。
    """
    records, loaded, skipped = [], [], []
    for spec in PERIODIC:
        path = os.path.join(data_dir, spec["file"])
        if not os.path.exists(path):
            skipped.append(spec["file"])
            continue
        loaded.append(spec["file"])
        with open(path, encoding="utf-8-sig") as f:
            for row in csv.DictReader(f):
                day = period_start(row, spec["date"], spec["grain"])
                if day is None:
                    continue
                scope = str(row.get(spec["scope"]) or "").strip() if spec["scope"] else ""
                # 2024 年绿电行没有序列名称，回落到序列编号，否则 scope 全塌成 ""
                # 同键互相覆盖，一笔交易悄悄吃掉另一笔
                if spec["scope"] and not scope:
                    scope = str(row.get("序列编号") or "").strip()
                for column, metric, unit in spec["metrics"]:
                    value = num(row.get(column))
                    if value is None:
                        continue
                    records.append((day, spec["dataset"], scope, metric, value, unit))
    return records, loaded, skipped


def fetch_run_rows(records):
    """按 (数据日, dataset) 汇总一条采集元数据；payload 存源站原始数值。"""
    by_day = {}
    for day, dataset, scope, metric, value, unit in records:
        by_day.setdefault((day, dataset), []).append(
            {"scope": scope, "metric": metric, "value": value, "unit": unit})
    out = []
    for (day, dataset), items in sorted(by_day.items()):
        out.append({
            "target_date": day, "dataset": dataset, "status": "ok",
            "rows_written": len(items),
            "source_url": "https://pm.gd.csg.cn/portal/xhr/sso-info-disclosure",
            "payload": json.dumps(items, ensure_ascii=False, separators=(",", ":")),
            "warnings": "", "extractor_version": EXTRACTOR,
        })
    return out


def existing_ocr(conn):
    return conn.execute(
        "SELECT id,report_date,run_date,dataset,scope,metric,value,origin,source,unit "
        "FROM gd_spot_metric WHERE origin IS NULL OR origin NOT IN (?,?)",
        (ORIGIN, VOID_ORIGIN)).fetchall()


def existing_void(conn):
    """已作废行 + 上一次作废时记下的原 origin（用来把误伤的行放回去）。

    审计表在老库上可能不存在（gdpm 之前只写不建），读不到就当作没有可复原的依据。
    """
    try:
        return conn.execute(
            "SELECT m.id, m.report_date, m.run_date, m.dataset, m.scope, m.metric, m.value,"
            " (SELECT a.old_origin FROM gd_spot_metric_audit a"
            "   WHERE a.report_date=m.report_date AND a.run_date=m.run_date"
            "     AND a.dataset=m.dataset AND a.scope=m.scope AND a.metric=m.metric"
            "     AND a.operation=? ORDER BY a.id DESC LIMIT 1) AS old_origin"
            " FROM gd_spot_metric m WHERE m.origin=?",
            (AUDIT_DEPRECATE, VOID_ORIGIN)).fetchall()
    except sqlite3.OperationalError as e:
        if "no such table" not in str(e):
            raise
        return []


def replaced_by(row, api):
    """该行是否已被官方同日数据取代：返回 (官方指标名, 官方值)，否则 None。"""
    if row["dataset"] not in VOID_DATASETS or row["metric"] not in VOID_METRICS:
        return None
    basis = VOID_BASIS[row["dataset"]]
    got = api.get((row["run_date"], row["dataset"], "发电侧", basis))
    return (basis, got[0]) if got else None


def plan_voids(api, ocr):
    """只有「官方同日给了这几类指标」的行才作废；依据逐条记在 replaced_by 里。"""
    voids = []
    for r in ocr:
        basis = replaced_by(r, api)
        if not basis:
            continue
        voids.append({"id": r["id"], "report_date": r["report_date"],
                      "run_date": r["run_date"], "dataset": r["dataset"],
                      "scope": r["scope"], "metric": r["metric"], "value": r["value"],
                      "origin": r["origin"] or "",
                      "replaced_by": basis[0], "api_value": basis[1]})
    return voids


def plan_restores(voided, api):
    """已在 void 但不在作废范围内的行：按审计里的原 origin 放回去。

    判断用「是否仍属官方取代范围」，不用「本次新作废了哪些」—— 后者在重跑时为空，
    会把本来就该 void 的行全部复原。
    """
    restores = []
    for r in voided:
        if replaced_by(r, api):
            continue
        to = (r["old_origin"] or "").strip()
        if not to or to == VOID_ORIGIN:
            continue
        restores.append({"id": r["id"], "report_date": r["report_date"],
                         "run_date": r["run_date"], "dataset": r["dataset"],
                         "scope": r["scope"], "metric": r["metric"],
                         "value": r["value"], "restore_to": to})
    return restores


def plan(conn, records):
    """算出要写什么、要作废什么、重叠日谁被谁取代；不落库。"""
    api = {(day, ds, sc, mt): (v, unit) for day, ds, sc, mt, v, unit in records}
    ocr = existing_ocr(conn)

    diff, seen = [], set()
    for ds, sc, ocr_metric, api_metric in METRIC_MAP:
        for r in ocr:
            if r["dataset"] != ds or r["scope"] != sc or r["metric"] != ocr_metric:
                continue
            key = (r["run_date"], ds, sc, api_metric)
            if key not in api:
                continue
            new, old = api[key][0], r["value"]
            # 同一运行日有多份日报产出同一个值，去重后再比
            if (r["run_date"], ds, ocr_metric, old, new) in seen:
                continue
            seen.add((r["run_date"], ds, ocr_metric, old, new))
            diff.append({
                "dataset": ds, "run_date": r["run_date"],
                "metric_ocr": ocr_metric, "metric_api": api_metric,
                "ocr": old, "api": new, "delta": round(new - old, 6),
                "pct": round((new - old) / old * 100, 4) if old else None,
            })
    diff.sort(key=lambda d: (d["run_date"], d["dataset"], d["metric_ocr"]))

    notes = []
    for ds, sc, ocr_metric, api_metric, why in METRIC_NOTE:
        for r in ocr:
            if r["dataset"] != ds or r["scope"] != sc or r["metric"] != ocr_metric:
                continue
            got = api.get((r["run_date"], ds, sc, api_metric))
            if not got:
                continue
            notes.append({"dataset": ds, "run_date": r["run_date"],
                          "metric_ocr": ocr_metric, "metric_api": api_metric,
                          "ocr": r["value"], "api": got[0], "why": why})
    voids = plan_voids(api, ocr)
    restores = plan_restores(existing_void(conn), api)
    return {"api": api, "ocr": ocr, "diff": diff, "notes": notes,
            "voids": voids, "restores": restores}


def apply(conn, records, p, log=print):
    # 时间带时区偏移写入（如 2026-11-11 16:27:21.123456+08:00）。不带偏移的本地时间会被
    # Go 侧按 UTC 解析，页面上就多 8 小时（历史行由读侧兜底，见 application/internal/logic/
    # spotreport/extract_run.go 的 auditCreatedAt）。
    now = dt.datetime.now().astimezone().isoformat(sep=" ")
    conn.execute(DDL_METRIC)
    conn.execute(DDL_FETCH_RUN)
    voids, restores = p["voids"], p["restores"]

    def write_audit(entries, operation, old_origin_of):
        conn.executemany(
            "INSERT INTO gd_spot_metric_audit (report_date,run_date,dataset,scope,metric,"
            "old_value,new_value,old_origin,operation,operator,created_at)"
            " VALUES (?,?,?,?,?,?,?,?,?,?,?)",
            [(r["report_date"], r["run_date"], r["dataset"], r["scope"], r["metric"],
              r["value"], None, old_origin_of(r), operation, OPERATOR, now) for r in entries])

    # 1. 先把上一版误伤的行按审计原 origin 放回去（只动还在 void 的行，重跑不会重复写审计）
    restored = 0
    for r in restores:
        restored += conn.execute(
            "UPDATE gd_spot_metric SET origin=?, updated_at=? WHERE id=? AND origin=?",
            (r["restore_to"], now, r["id"], VOID_ORIGIN)).rowcount
    if restores:
        write_audit(restores, AUDIT_RESTORE, lambda _r: VOID_ORIGIN)

    # 2. 再作废官方确实取代的那几类（逐行按 id 更新，不再一刀切）
    voided = 0
    for r in voids:
        voided += conn.execute(
            "UPDATE gd_spot_metric SET origin=?, updated_at=? WHERE id=?"
            " AND (origin IS NULL OR origin NOT IN (?,?))",
            (VOID_ORIGIN, now, r["id"], ORIGIN, VOID_ORIGIN)).rowcount
    if voids:
        write_audit(voids, AUDIT_DEPRECATE, lambda r: r["origin"])

    rows = []
    for day, ds, sc, mt, value, unit in records:
        rows.append((report_of(ds, day), day, ds, sc, mt, value, unit, SOURCE, 1.0,
                     ORIGIN, EXTRACTOR, now, now))
    conn.executemany(
        "INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,unit,"
        "source,conf,origin,extractor_version,created_at,updated_at) "
        "VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) "
        "ON CONFLICT (report_date,run_date,dataset,scope,metric) DO UPDATE SET "
        "value=excluded.value, unit=excluded.unit, source=excluded.source, conf=excluded.conf,"
        "origin=excluded.origin, extractor_version=excluded.extractor_version,"
        "updated_at=excluded.updated_at",
        rows)

    runs = fetch_run_rows(records)
    conn.executemany(
        "INSERT INTO gd_pm_fetch_run (target_date,dataset,status,rows_written,source_url,"
        "payload,warnings,extractor_version,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?) "
        "ON CONFLICT (target_date,dataset,extractor_version) DO UPDATE SET "
        "status=excluded.status, rows_written=excluded.rows_written, payload=excluded.payload,"
        "warnings=excluded.warnings, updated_at=excluded.updated_at",
        [(r["target_date"], r["dataset"], r["status"], r["rows_written"], r["source_url"],
          r["payload"], r["warnings"], r["extractor_version"], now, now) for r in runs])
    conn.commit()
    log(f"复原误伤行 {restored} 条，作废旧行 {voided} 条（逐条写审计），"
        f"写入 {len(rows)} 条 pm-api 指标，采集元数据 {len(runs)} 条")
    return {"restored": restored, "deprecated": voided,
            "metrics": len(rows), "fetch_runs": len(runs)}


def anomalies(records):
    """异常值：负价、0 值、量价倒挂、电价日环比跳变 >50%。"""
    prices = {}
    out = {"negative_price": [], "zero_value": [], "price_volume_mismatch": [],
           "jump_over_50pct": []}
    for day, ds, sc, metric, value, unit in records:
        tag = {"date": day, "dataset": ds, "scope": sc, "metric": metric, "value": value}
        if unit == "元/兆瓦时":
            if value < 0:
                out["negative_price"].append(tag)
            if "加权平均电价" in metric or metric == "日前申报均价":
                prices[(ds, sc, metric, day)] = value
        if value == 0:
            out["zero_value"].append(tag)
    # 有价无量 / 有量无价（同一天同一口径下）
    vol_by = {}
    for day, ds, sc, metric, value, unit in records:
        if unit == "MWh":
            side = "发电侧" if sc == "发电侧" else "用电侧"
            vol_by[(ds, side, day)] = value
    for (ds, sc, metric, day), value in prices.items():
        side = "发电侧" if "发电侧" in metric or metric == "日前申报均价" else "用电侧"
        vol = vol_by.get((ds, side, day))
        if value > 0 and vol == 0:
            out["price_volume_mismatch"].append(
                {"date": day, "dataset": ds, "scope": sc, "metric": metric,
                 "value": value, "reason": "有价无量"})
    # 日环比跳变
    series = {}
    for (ds, sc, metric, day), value in prices.items():
        series.setdefault((ds, sc, metric), []).append((day, value))
    for (ds, sc, metric), pts in series.items():
        pts.sort()
        for (d0, v0), (d1, v1) in zip(pts, pts[1:]):
            if shift(d0, 1) != d1 or not v0:
                continue
            pct = (v1 - v0) / v0 * 100
            if abs(pct) > 50:
                out["jump_over_50pct"].append(
                    {"dataset": ds, "scope": sc, "metric": metric, "date": d1,
                     "prev_date": d0, "prev": v0, "value": v1, "pct": round(pct, 2)})
    return out


def build_report(conn, records, p, anomalies_, written):
    api, diff, notes = p["api"], p["diff"], p["notes"]
    days = {}
    for day, ds, sc, metric, value, unit in records:
        days.setdefault(ds, set()).add(day)
    span = {}
    for ds, dset in days.items():
        ordered = sorted(dset)
        # 只有日频才追「缺哪一天」；月频/年频的期初日之间本来就有间隔，逐日追会生成上千条假缺失
        gaps = []
        if ds in DAILY_DATASETS:
            cur = dt.datetime.strptime(ordered[0], DATE_FMT)
            end = dt.datetime.strptime(ordered[-1], DATE_FMT)
            while cur <= end:
                d = cur.strftime(DATE_FMT)
                if d not in dset:
                    gaps.append(d)
                cur += dt.timedelta(days=1)
        span[ds] = {"periods": len(dset), "from": ordered[0], "to": ordered[-1],
                    "missing": gaps}

    per_day = {}
    for day, ds, sc, metric, value, unit in records:
        if ds not in DAILY_DATASETS:
            continue
        per_day.setdefault(day, {}).setdefault(ds, 0)
        per_day[day][ds] += 1

    db_after = [dict(r) for r in conn.execute(
        "SELECT dataset, origin, COUNT(*) AS rows, COUNT(DISTINCT run_date) AS days,"
        " MIN(run_date) AS first_day, MAX(run_date) AS last_day"
        " FROM gd_spot_metric GROUP BY dataset, origin ORDER BY dataset, origin")]

    voids = p["voids"]
    void_summary = {}
    for v in voids:
        key = f"{v['dataset']}/{v['metric']}"
        void_summary[key] = void_summary.get(key, 0) + 1
    restore_summary = {}
    for r in p["restores"]:
        key = f"{r['dataset']}/{r['metric']}"
        restore_summary[key] = restore_summary.get(key, 0) + 1

    csvs = []
    return {
        "csv": csvs,
        "long_rows": len(records),
        "per_day_series": {d: per_day[d] for d in sorted(per_day, reverse=True)},
        "coverage": span,
        "overlap_diff": diff,
        "overlap_notes": notes,
        "void_scope": {"datasets": list(VOID_DATASETS), "metrics": list(VOID_METRICS)},
        "void_plan": voids,
        "void_summary": void_summary,
        "restore_plan": p["restores"],
        "restore_summary": restore_summary,
        "ocr_rows_to_void": len(voids),
        "anomalies": anomalies_,
        "db_after_import": db_after,
        "written": written,
    }


def main():
    ap = argparse.ArgumentParser(description="官方广东电力指标 -> ems gd_spot_metric")
    ap.add_argument("--db", required=True, help="sqlite 数据库路径")
    ap.add_argument("--csv", default=DEFAULT_CSV, help=f"日频长表 CSV（默认 {DEFAULT_CSV}）")
    ap.add_argument("--data-dir", help="月频/年频 CSV 所在目录（默认取 --csv 同目录）")
    ap.add_argument("--report", help="把校验报告写成 JSON")
    ap.add_argument("--apply", action="store_true", help="真正写库（缺省只算不写）")
    args = ap.parse_args()

    data_dir = args.data_dir or os.path.dirname(os.path.abspath(args.csv))
    records = load_long(args.csv)
    periodic, loaded, skipped = load_periodic(data_dir)
    records += periodic
    conn = sqlite3.connect(args.db)
    conn.row_factory = sqlite3.Row
    p = plan(conn, records)

    written = {"restored": 0, "deprecated": 0, "metrics": 0, "fetch_runs": 0}
    if args.apply:
        written = apply(conn, records, p)
    else:
        print("dry-run：未写库", file=sys.stderr)

    report = build_report(conn, records, p, anomalies(records), written)
    report["periodic_files"] = {"loaded": loaded, "skipped": skipped}
    report["daily_records"] = len(records) - len(periodic)
    report["periodic_records"] = len(periodic)
    conn.close()

    for ds, info in report["coverage"].items():
        unit = "天" if ds in DAILY_DATASETS else "期"
        print(f"{ds:20s} {info['periods']:4d} {unit}  {info['from']} → {info['to']}  "
              f"缺失 {len(info['missing'])} 天", file=sys.stderr)
    if skipped:
        print(f"未找到月频/年频文件: {', '.join(skipped)}", file=sys.stderr)
    if args.report:
        with open(args.report, "w", encoding="utf-8") as f:
            json.dump(report, f, ensure_ascii=False, indent=1)
        print(f"报告: {args.report}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
