#!/usr/bin/env python3
"""地市节点电价档位回灌：从 gd_spot_extract_run.payload 里把档位行写回 gd_spot_metric。

为什么从 payload 取：档位只存在于日报图片（图2-1 高峰 / 图2-2 低谷），pm-api 没有这个指标。
11-11 换数据卷时新库只迁了官方 6126 行，档位行整段丢失；原始识别结果还完整躺在
每份日报的 gd_spot_extract_run.payload 里（9-16~9-25 共 232 行），零外部依赖可重建。

origin 取 manual，不是 cv：
  - 页面（市场出清 → 档位面板）只过滤 origin='void'，cv 行也能显示，但页脚会标
    「图像采样预填（origin=cv）」且计入「待确认」；
  - 这 232 行在 2026-09-22 已经人工确认过（gd_spot_metric_audit 有 232 条
    operation=confirm、old_origin=cv → manual 的记录），回灌成 cv 等于把已确认的数据
    降级、要人再确认一遍，还会从「历史复盘」趋势里消失（趋势只认 manual / pm-api）。
  所以按确认后的状态回灌 manual。没确认过的日期则由 payload 里的 Origin（cv）决定。

幂等：ON CONFLICT(report_date,run_date,dataset,scope,metric) DO UPDATE，重复跑只是把同样的值
再写一遍；pm-api 行永远不覆盖。

用法：
    python3 gdpm_tier_backfill.py --db /tmp/db.sqlite3 --report /tmp/tier.json          # 只算不写
    python3 gdpm_tier_backfill.py --db /tmp/db.sqlite3 --report /tmp/tier.json --apply
"""

import argparse
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import gdpm_db as dbc
import gdpm_sqlite as base  # 复用 DDL 常量，避免两处表结构漂移

TIER_DATASETS = ("node_price_peak", "node_price_valley")
TIER_METRIC = "价格档位"
DEFAULT_ORIGIN = "manual"
EXTRACTOR = "gdpm-tier-1"
SOURCE_KEEP = True


def load_payload_rows(conn, day_from=None, day_to=None):
    """gd_spot_extract_run.payload -> 档位行（按唯一键去重，后一份日报覆盖前一份）。"""
    rows = {}
    for run in conn.execute(
            "SELECT report_date, payload FROM gd_spot_extract_run"
            " WHERE payload IS NOT NULL AND payload <> '' ORDER BY report_date"):
        day = run["report_date"]
        if day_from and day < day_from:
            continue
        if day_to and day > day_to:
            continue
        try:
            payload = json.loads(run["payload"])
        except ValueError:
            continue
        for m in payload.get("metrics") or []:
            if m.get("Dataset") not in TIER_DATASETS or m.get("Metric") != TIER_METRIC:
                continue
            run_date = str(m.get("RunDate") or day)[:10]
            key = (run_date, run_date, m["Dataset"], str(m.get("Scope") or ""), TIER_METRIC)
            rows[key] = {
                "report_date": run_date, "run_date": run_date,
                "dataset": m["Dataset"], "scope": key[3], "metric": TIER_METRIC,
                "value": float(m.get("Value") or 0),
                "unit": str(m.get("Unit") or ""),
                "text_value": str(m.get("TextValue") or ""),
                "source": str(m.get("Source") or ""),
                "conf": float(m.get("Conf") or 0),
                "origin": str(m.get("Origin") or "") or DEFAULT_ORIGIN,
            }
    return [rows[k] for k in sorted(rows)]


def existing_rows(conn):
    """库里同键的现状：origin 与值，用来判断是新增、回灌还是覆盖。"""
    return {(r["report_date"], r["run_date"], r["dataset"], r["scope"], r["metric"]):
            {"origin": r["origin"] or "", "value": r["value"], "text_value": r["text_value"]}
            for r in conn.execute(
                "SELECT report_date,run_date,dataset,scope,metric,value,text_value,origin"
                " FROM gd_spot_metric WHERE dataset IN (?,?)", TIER_DATASETS)}


def confirmed_keys(conn):
    """审计里人工确认过的键（operation=confirm）：这些行回灌时按 manual，其余按 payload 的 origin。"""
    try:
        return {(r[0], r[1], r[2], r[3], r[4]) for r in conn.execute(
            "SELECT report_date,run_date,dataset,scope,metric FROM gd_spot_metric_audit"
            " WHERE operation='confirm' AND dataset IN (?,?)", TIER_DATASETS)}
    except Exception as e:  # sqlite: no such table / PG: relation "x" does not exist
        msg = str(e).lower()
        if "no such table" not in msg and "does not exist" not in msg:
            raise
        return set()


def plan(conn, rows):
    have = existing_rows(conn)
    confirmed = confirmed_keys(conn)
    out = []
    for r in rows:
        key = (r["report_date"], r["run_date"], r["dataset"], r["scope"], r["metric"])
        cur = have.get(key)
        covered = DEFAULT_ORIGIN if key in confirmed else r["origin"]
        if cur is None:
            action = "insert"
            origin = covered
        elif cur["origin"] == base.ORIGIN:
            action = "skip_pm_api"           # 官方行不覆盖（档位理论上不会有，防御）
            origin = cur["origin"]
        elif cur["origin"] == base.VOID_ORIGIN:
            action = "restore"               # 被上一版一刀切作废的行
            origin = covered
        else:
            action = "update"
            origin = cur["origin"] or covered
        out.append(dict(r, origin=origin, action=action, before=cur))
    return out


def apply(conn, plan_rows, now, pg=False):
    written = 0
    conn.execute(dbc.ddl_for(base.DDL_METRIC, pg))
    for r in plan_rows:
        if r["action"] == "skip_pm_api":
            continue
        cur = conn.execute(
            "INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,unit,"
            "text_value,source,conf,origin,extractor_version,created_at,updated_at) "
            "VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) "
            "ON CONFLICT (report_date,run_date,dataset,scope,metric) DO UPDATE SET "
            "value=excluded.value, unit=excluded.unit, text_value=excluded.text_value,"
            "conf=excluded.conf, origin=excluded.origin,"
            "extractor_version=excluded.extractor_version, updated_at=excluded.updated_at "
            "WHERE gd_spot_metric.origin IS NULL OR gd_spot_metric.origin <> ?",
            (r["report_date"], r["run_date"], r["dataset"], r["scope"], r["metric"],
             r["value"], r["unit"], r["text_value"], r["source"], r["conf"], r["origin"],
             EXTRACTOR, now, now, base.ORIGIN))
        written += cur.rowcount
    return written


def build_report(plan_rows, written):
    by_action, by_dataset, by_day = {}, {}, {}
    for r in plan_rows:
        by_action[r["action"]] = by_action.get(r["action"], 0) + 1
        by_dataset[r["dataset"]] = by_dataset.get(r["dataset"], 0) + 1
        by_day[r["run_date"]] = by_day.get(r["run_date"], 0) + 1
    return {
        "rows_in_payload": len(plan_rows),
        "by_action": by_action,
        "by_dataset": by_dataset,
        "by_run_date": {d: by_day[d] for d in sorted(by_day)},
        "origins": {o: sum(1 for r in plan_rows if r["origin"] == o)
                    for o in sorted({r["origin"] for r in plan_rows})},
        "written": written,
        "rows": plan_rows,
    }


def main():
    ap = argparse.ArgumentParser(description="从 gd_spot_extract_run.payload 回灌地市节点电价档位")
    ap.add_argument("--db", required=True, help="sqlite 数据库路径（生产库请先拷副本）")
    ap.add_argument("--from", dest="day_from", help="起始运行日 YYYY-MM-DD")
    ap.add_argument("--to", dest="day_to", help="结束运行日 YYYY-MM-DD")
    ap.add_argument("--report", help="把校验报告写成 JSON")
    ap.add_argument("--apply", action="store_true", help="真正写库（缺省只算不写）")
    args = ap.parse_args()

    # GDPM_DB_LINK 非空时写 PG；--db 只是 sqlite 兜底路径
    conn, pg = dbc.connect(os.environ.get("GDPM_DB_LINK"), args.db)
    rows = load_payload_rows(conn, args.day_from, args.day_to)
    plan_rows = plan(conn, rows)

    written = 0
    if args.apply:
        import datetime as dt
        written = apply(conn, plan_rows, dt.datetime.now().astimezone().isoformat(sep=" "), pg=pg)
        conn.commit()
    else:
        print("dry-run：未写库", file=sys.stderr)

    report = build_report(plan_rows, written)
    conn.close()

    print(f"payload 档位行 {len(plan_rows)} 条：" +
          "，".join(f"{k} {v}" for k, v in sorted(report["by_action"].items())) +
          f"；写入 {written} 条", file=sys.stderr)
    for ds, n in sorted(report["by_dataset"].items()):
        print(f"  {ds:20s} {n:4d} 行", file=sys.stderr)
    if args.report:
        with open(args.report, "w", encoding="utf-8") as f:
            json.dump(report, f, ensure_ascii=False, indent=1)
        print(f"报告: {args.report}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
