#!/usr/bin/env python3
"""日报采集缺口检查 / 补采：交易中心「广东电力现货市场运行日报」。

日报来源（唯一）：微信公众号「广东电力交易中心」当天推文 → 正文 4 张图片
（page-1 市场概况与日前申报 / page-2 日前成交与高峰节点电价 / page-3 低谷节点电价 /
page-4 实时交易电价）。没有可枚举的文章列表接口，链接只能人工从公众号取，所以本脚本
**不自带搜索**：给 links 文件（每行 `YYYY-MM-DD<TAB>公众号文章链接`），
由脚本调 ems 自己的采集/解析接口（/api/spot-report/collect、/api/spot-report/extract）。

拿不到链接的日期不是静默跳过，而是写一条 status=partial 的采集记录并留 warnings，
页面上「该日没有日报」与「采过但失败」就不再混在一起。

与周更入库链路的关系：周更（gdpm_sqlite.py）只写官方 pm-api 指标，作废范围已收窄到
「日前/实时成交电价」四类，档位行不在其中（见 gdpm_sqlite.VOID_DATASETS/VOID_METRICS），
所以日报补采与周更互不覆盖 —— 不需要额外的加锁或先后顺序。

用法：
    python3 gdpm_report_gap.py --db /tmp/db.sqlite3 --report /tmp/gap.json              # 只看缺口
    python3 gdpm_report_gap.py --db /tmp/db.sqlite3 --links links.tsv --apply \\
        --api http://127.0.0.1:80                                                      # 真采
"""

import argparse
import datetime as dt
import json
import os
import sys
import urllib.error
import urllib.request

import gdpm_db as dbc

DEFAULT_FROM = "2026-09-26"
DEFAULT_EXTRACTOR = "gap-1"
PARTIAL_WARNING = ("未取得该日日报文章链接（微信公众号「广东电力交易中心」），"
                   "节点电价档位等图片指标缺失")


def daterange(day_from, day_to):
    start = dt.date.fromisoformat(day_from)
    end = dt.date.fromisoformat(day_to)
    days = []
    while start <= end:
        days.append(start.isoformat())
        start += dt.timedelta(days=1)
    return days


def load_state(conn):
    """每个日期：有没有日报、采集/解析状态。"""
    state = {}
    try:
        for r in conn.execute("SELECT run_date, source_url FROM gd_spot_report"):
            state.setdefault(r[0], {})["source_url"] = r[1]
    except Exception as e:  # sqlite: no such table / PG: relation "x" does not exist
        msg = str(e).lower()
        if "no such table" not in msg and "does not exist" not in msg:
            raise
    try:
        for r in conn.execute("SELECT report_date, status, warnings FROM gd_spot_extract_run"):
            state.setdefault(r[0], {}).update(status=r[1] or "", warnings=r[2] or "")
    except Exception as e:  # sqlite: no such table / PG: relation "x" does not exist
        msg = str(e).lower()
        if "no such table" not in msg and "does not exist" not in msg:
            raise
    return state


def read_links(path):
    links = {}
    if not path:
        return links
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            day, _, url = line.partition("\t")
            if url:
                links[day.strip()] = url.strip()
    return links


def plan(conn, day_from, day_to, links):
    state = load_state(conn)
    rows = []
    for day in daterange(day_from, day_to):
        cur = state.get(day) or {}
        status = cur.get("status", "")
        if cur.get("source_url"):
            action = "have_report" if status in ("ok", "partial") else "recollect"
        elif day in links:
            action = "collect"
        elif status == "partial" and PARTIAL_WARNING in cur.get("warnings", ""):
            action = "already_marked_partial"
        else:
            action = "mark_partial"
        rows.append({"date": day, "action": action, "status": status,
                     "source_url": cur.get("source_url", ""), "link": links.get(day, "")})
    return rows


def post_json(base, path, body):
    req = urllib.request.Request(base.rstrip("/") + path, data=json.dumps(body).encode(),
                                 method="POST", headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=180) as r:
        return r.status, json.loads(r.read().decode() or "{}")


def collect_one(api, day, url, log):
    """先采集（图片入库 + 镜像），再解析。任何一步失败都落到 partial + warnings。"""
    try:
        status, res = post_json(api, "/api/spot-report/collect", {"url": url})
        if status >= 300:
            raise RuntimeError(f"collect HTTP {status}")
        status, res = post_json(api, "/api/spot-report/extract", {"date": day, "force": False})
        if status >= 300:
            raise RuntimeError(f"extract HTTP {status}")
        got = (res.get("data") or res).get("status", "")
        log(f"  {day} 采集完成：status={got}")
        return ("ok" if got in ("ok", "partial") else "failed"), got
    except (urllib.error.URLError, RuntimeError, ValueError) as e:
        log(f"  {day} 采集失败：{e}")
        return "failed", str(e)


def mark_partial(conn, days, now):
    n = 0
    for day in days:
        cur = conn.execute(
            "INSERT INTO gd_spot_extract_run (report_date,status,warnings,extractor_version,"
            "created_at,updated_at) VALUES (?,?,?,?,?,?) "
            "ON CONFLICT (report_date) DO UPDATE SET status='partial', warnings=excluded.warnings,"
            "updated_at=excluded.updated_at WHERE gd_spot_extract_run.status NOT IN ('ok')",
            (day, "partial", json.dumps([PARTIAL_WARNING], ensure_ascii=False),
             DEFAULT_EXTRACTOR, now, now))
        n += cur.rowcount
    return n


def main():
    ap = argparse.ArgumentParser(description="日报采集缺口检查 / 补采")
    ap.add_argument("--db", required=True, help="sqlite 数据库路径")
    ap.add_argument("--from", dest="day_from", default=DEFAULT_FROM)
    ap.add_argument("--to", dest="day_to", default=dt.date.today().isoformat())
    ap.add_argument("--links", help="链接文件，每行 `日期<TAB>文章链接`")
    ap.add_argument("--api", default="http://127.0.0.1:80", help="ems 服务地址")
    ap.add_argument("--report", help="把校验报告写成 JSON")
    ap.add_argument("--apply", action="store_true", help="真去采集 / 写 partial 留痕")
    args = ap.parse_args()

    log = lambda m: print(m, file=sys.stderr)
    # GDPM_DB_LINK 非空时读 PG；--db 只是 sqlite 兜底路径
    conn, _pg = dbc.connect(os.environ.get("GDPM_DB_LINK"), args.db)
    links = read_links(args.links)
    rows = plan(conn, args.day_from, args.day_to, links)

    for r in rows:
        log(f"{r['date']}  {r['action']}")

    if args.apply:
        now = dt.datetime.now().astimezone().isoformat(sep=" ")
        for r in rows:
            if r["action"] == "collect":
                r["result"], r["result_status"] = collect_one(args.api, r["date"],
                                                              r["link"], log)
            elif r["action"] == "recollect":
                r["result"], r["result_status"] = collect_one(args.api, r["date"],
                                                              r["source_url"], log)
        marked = mark_partial(conn, [r["date"] for r in rows
                                     if r["action"] in ("mark_partial", "collect")
                                     and r.get("result") != "ok"], now)
        conn.commit()
        log(f"标 partial 留痕 {marked} 天")
    else:
        marked = 0
        log("dry-run：未采集、未写库")

    report = {
        "from": args.day_from, "to": args.day_to,
        "gaps": [r for r in rows if r["action"] not in ("have_report",)],
        "by_action": {a: sum(1 for r in rows if r["action"] == a)
                      for a in sorted({r["action"] for r in rows})},
        "marked_partial": marked,
        "rows": rows,
    }
    conn.close()
    if args.report:
        with open(args.report, "w", encoding="utf-8") as f:
            json.dump(report, f, ensure_ascii=False, indent=1)
        log(f"报告: {args.report}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
