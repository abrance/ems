#!/usr/bin/env python3
"""官网主页 24 点分时历史回补（一次性深回补，独立于 ems 进程内采集）。

背景：本地库 gd_home_24h 只有 13 天（2026-10-31 起），而源站网关
tradeSituation?date=D 能取到 2023-09 起的历史（2026-11-20 实测：2023-09-10 起
大体有数，更早为空）。进程内 backfill API 上限 30 天，补不了两年，
所以用本脚本按日期区间直打官网网关批量回补。

口径与 Go 采集器（application/internal/logic/homemarket/collect.go）一致：
  - GET {gateway}/sso-portal/portal/home/trade/tradeSituation?date=D&type=1
    （必须走 /portal/xhr/ 网关，直连 /sso-portal 会 400；Referer=站点根 + /portal/）
  - hourOf：dateTime "YYYY-MM-DD HH:00:00" → 整点小时
  - guardBatch：返回行但一条解不出 / 全 0 价 → 拒绝写库（疑似字段改名）
  - Upsert：INSERT ... ON CONFLICT(price_date, hour) DO UPDATE（覆盖写，
    部分小时断线不丢上一轮已采到的小时）
  - fetch_run 记录 gd_home_market_fetch_run（dataset=home24h，status ok/empty/failed）
    供 /api/gd-metric/home-market/status 页面口径一致

库：EMS_DB_LINK 为 postgres URI → psycopg；否则视为 sqlite 路径（本地调试）。
用法：
  /usr/local/python3/bin/python3.12 tools/home24h-backfill.py \
      --from 2023-09-01 --to 2026-11-12 [--workers 4] [--dry-run]
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from datetime import date, timedelta

GATEWAY_DEFAULT = "https://pm.gd.csg.cn/portal/xhr"
SOURCE = "官网·24点"
UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

UPSERT = """
INSERT INTO gd_home_24h (price_date, hour, power_mw, ahead_price, realtime_price, source, updated_at)
VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(price_date, hour) DO UPDATE SET
    power_mw = excluded.power_mw,
    ahead_price = excluded.ahead_price,
    realtime_price = excluded.realtime_price,
    source = excluded.source,
    updated_at = CURRENT_TIMESTAMP
"""

UPSERT_PG = UPSERT.replace("?", "%s")

FETCH_RUN = """
INSERT INTO gd_home_market_fetch_run
    (target_date, dataset, status, error_kind, rows_written, rows_expected,
     attempt_count, consecutive_failures, consecutive_empties, last_error,
     first_seen_at, last_ok_at, updated_at)
VALUES (?, 'home24h', ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP)
ON CONFLICT(target_date, dataset) DO UPDATE SET
    status = excluded.status,
    error_kind = excluded.error_kind,
    rows_written = excluded.rows_written,
    rows_expected = excluded.rows_expected,
    attempt_count = excluded.attempt_count,
    consecutive_failures = excluded.consecutive_failures,
    consecutive_empties = excluded.consecutive_empties,
    last_error = excluded.last_error,
    last_ok_at = excluded.last_ok_at,
    updated_at = CURRENT_TIMESTAMP
"""

FETCH_RUN_PG = FETCH_RUN.replace("?", "%s")


class DB:
    """sqlite / PG 双方言的极窄写入面（只用到 execute + executemany 语义）。"""

    def __init__(self, link: str):
        self.is_pg = link.startswith(("postgres://", "postgresql://"))
        if self.is_pg:
            import psycopg  # noqa: 延迟导入，本地 sqlite 调试不要求装
            self.conn = psycopg.connect(link, connect_timeout=10)
        else:
            import sqlite3
            self.conn = sqlite3.connect(link, timeout=30)
        self.ph = "%s" if self.is_pg else "?"
        self.batch: list[tuple] = []

    def exec(self, sql: str, args: tuple):
        cur = self.conn.cursor()
        cur.execute(sql.replace("?", self.ph) if self.is_pg else sql, args)
        cur.close()

    def execmany(self, sql: str, rows: list[tuple]):
        cur = self.conn.cursor()
        if self.is_pg:
            for r in rows:
                cur.execute(sql.replace("?", self.ph), r)
        else:
            cur.executemany(sql, rows)
        cur.close()

    def commit(self):
        self.conn.commit()

    def close(self):
        self.conn.close()


def referer(gateway: str) -> str:
    # 与 Go gatewayReferer 一致：scheme://host/portal/
    p = urllib.parse.urlparse(gateway)
    if not p.scheme or not p.netloc:
        return ""
    return f"{p.scheme}://{p.netloc}/portal/"


def fetch_day(gateway: str, d: str) -> tuple[list[tuple], str]:
    """取一天，返回 (rows, status)。status: ok / empty / failed。守卫语义同 Go。"""
    url = f"{gateway}/sso-portal/portal/home/trade/tradeSituation?date={d}&type=1"
    req = urllib.request.Request(url, headers={
        "User-Agent": UA,
        "Referer": referer(gateway),
        "Accept": "application/json, text/plain, */*",
    })
    with urllib.request.urlopen(req, timeout=30) as res:
        if res.status != 200:
            raise RuntimeError(f"HTTP {res.status}")
        body = json.loads(res.read().decode("utf-8", "replace"))
    if body.get("code") != 200:
        raise RuntimeError(f"code={body.get('code')} msg={body.get('msg')}")
    details = (body.get("data") or {}).get("details") or []
    rows: list[tuple] = []
    for x in details:
        dt = (x.get("dateTime") or "").strip()
        if len(dt) < 13 or not dt[11:13].isdigit():
            continue  # 时间格式不认识，跳过该条（同 Go 的 hourOf 容错）
        rows.append((d, int(dt[11:13]), float(x.get("powerNum") or 0),
                     float(x.get("currentPrice") or 0), float(x.get("realTimePrice") or 0), SOURCE))
    if details and not rows:
        raise ValueError(f"返回 {len(details)} 行但一条也解析不了，疑似字段改名")
    if rows and all(r[3] == 0 for r in rows):
        raise ValueError(f"返回 {len(rows)} 行但日前价全为 0，疑似字段改名或改版（拒绝写库）")
    if rows and len(rows) < 24:
        # 部分天：写入会让「连续完整日」检查报错（price_hour 对不满 24 点的天点名报错），整日弃用
        return [], "empty"
    return rows, ("ok" if rows else "empty")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--from", dest="from_", required=True, help="起始日 YYYY-MM-DD")
    ap.add_argument("--to", required=True, help="结束日 YYYY-MM-DD（含）")
    ap.add_argument("--gateway", default=os.environ.get("HOME_MARKET_GATEWAY_BASE", GATEWAY_DEFAULT))
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--retries", type=int, default=3)
    ap.add_argument("--dry-run", action="store_true", help="只拉不写")
    args = ap.parse_args()

    link = os.environ.get("EMS_DB_LINK", "")
    if not link and not args.dry_run:
        print("没有 EMS_DB_LINK，也没有 --dry-run：不知道写哪个库", file=sys.stderr)
        return 2

    d0 = date.fromisoformat(args.from_)
    d1 = date.fromisoformat(args.to)
    days = [(d0 + timedelta(days=k)).isoformat() for k in range((d1 - d0).days + 1)]
    print(f"回补 {days[0]} ~ {days[-1]} 共 {len(days)} 天，workers={args.workers}，dry_run={args.dry_run}")

    db = None if args.dry_run else DB(link)
    if db:
        db.execmany(UPSERT, [])  # 预热方言检查（空跑不写行）

    stats = {"ok": 0, "empty": 0, "failed": 0, "rows": 0}
    lock_print = __import__("threading").Lock()

    def work(d: str):
        for attempt in range(1, args.retries + 1):
            try:
                rows, status = fetch_day(args.gateway, d)
                with lock_print:
                    stats[status] += 1
                    stats["rows"] += len(rows)
                    tag = "✓" if status == "ok" else "·"
                    print(f"{tag} {d} {status} rows={len(rows)}" if status == "ok"
                          else f"· {d} {status}")
                if not args.dry_run and rows:
                    db.execmany(UPSERT, rows)
                if not args.dry_run:
                    # PG 传字符串时间；sqlite 传 None 让 DEFAULT CURRENT_TIMESTAMP 生效
                    now = time.strftime("%Y-%m-%d %H:%M:%S") if db.is_pg else None
                    db.exec(FETCH_RUN, (d, status, "", len(rows), len(rows), attempt, 0,
                                        1 if status == "empty" else 0, "", now))
                    db.commit()  # 逐日提交：中断不丢已补天，重跑幂等覆盖
                return
            except Exception as exc:  # noqa: 网络抖动重试
                if attempt == args.retries:
                    with lock_print:
                        stats["failed"] += 1
                        print(f"✗ {d} failed after {attempt} 次: {exc}")
                    if not args.dry_run:
                        db.exec(FETCH_RUN, (d, "failed", "network", 0, 24, attempt, 1, 0, str(exc)[:200], None))
                    return
                time.sleep(1.5 * attempt)

    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        for d in days:
            pool.submit(work, d)
    pool.shutdown(wait=True)  # noqa: 上面的 with 已等待；显式调用为了可读

    if db:
        db.commit()
        db.close()
    print(f"汇总: ok={stats['ok']} empty={stats['empty']} failed={stats['failed']} rows={stats['rows']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
