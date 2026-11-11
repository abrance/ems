#!/usr/bin/env python3
"""广东电力交易中心门户（pm.gd.csg.cn）信息披露指标抓取。

按日期取「电力交易指标」：遍历披露节点树 -> 每个节点的 excelData 拿到指标表
（templateUuid + 字段定义）-> 用 excelData/page 按日期区间取数。

无需登录，无需任何凭据。只读接口。

用法:
  python3 gd_pm_fetch.py --list-nodes
  python3 gd_pm_fetch.py --date 2026-11-10
  python3 gd_pm_fetch.py --date 2026-11-10 --node 现货
  python3 gd_pm_fetch.py --date 2026-11-10 --json > out.json
  python3 gd_pm_fetch.py --build-index          # 建/续建指标索引（分批可重复执行）
"""

import argparse
import json
import os
import re
import sys
import time
import urllib.request
import urllib.error

BASE = "https://pm.gd.csg.cn/portal/xhr/sso-info-disclosure"
UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
HERE = os.path.dirname(os.path.abspath(__file__))
CACHE_TREE = os.path.join(HERE, "gd_pm_tree.json")
CACHE_INDEX = os.path.join(HERE, "gd_pm_index.json")

DELAY = 0.7          # 请求间隔（秒），别把人家站点打挂
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}")
DAY_RE = re.compile(r"^\d{4}-\d{2}-\d{2}(?:[ T]|$)")


class Budget:
    def __init__(self, limit):
        self.limit = limit
        self.used = 0
        self._last = 0.0

    def take(self):
        if self.used >= self.limit:
            raise BudgetExceeded()
        wait = DELAY - (time.time() - self._last)
        if wait > 0:
            time.sleep(wait)
        self.used += 1
        self._last = time.time()


class BudgetExceeded(Exception):
    pass


def post(path, payload, budget):
    budget.take()
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(payload).encode(),
        headers={
            "User-Agent": UA,
            "Content-Type": "application/json",
            "Referer": "https://pm.gd.csg.cn/portal/",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=40) as r:
        body = json.loads(r.read().decode())
    if body.get("code") != 200:
        raise RuntimeError(f"{path} -> {body.get('code')} {body.get('msg')}")
    return body.get("data")


def get(path, budget):
    budget.take()
    req = urllib.request.Request(
        BASE + path,
        headers={"User-Agent": UA, "Referer": "https://pm.gd.csg.cn/portal/"},
    )
    with urllib.request.urlopen(req, timeout=40) as r:
        body = json.loads(r.read().decode())
    if body.get("code") != 200:
        raise RuntimeError(f"{path} -> {body.get('code')} {body.get('msg')}")
    return body.get("data")


# ---------------------------------------------------------------- tree

def flatten_tree(root):
    """-> [{nodeCode, nodeName, path, depth, configs}]  只保留有披露配置的叶子节点"""
    out = []

    def walk(node, parents):
        name = (node.get("nodeName") or "").strip()
        path = parents + [name]
        kids = node.get("children") or []
        cfgs = node.get("disclosureConfigUuids") or []
        if not kids and cfgs:
            out.append({
                "nodeCode": node["nodeCode"],
                "nodeName": name,
                "path": " / ".join(path),
                "depth": node.get("level"),
                "configs": cfgs,
            })
        for k in kids:
            walk(k, path)

    walk(root, [])
    return out


def load_tree(budget, refresh=False):
    if os.path.exists(CACHE_TREE) and not refresh:
        with open(CACHE_TREE) as f:
            return json.load(f)
    root = get("/api/disclosure/nav/tree", budget)
    leaves = flatten_tree(root)
    with open(CACHE_TREE, "w") as f:
        json.dump(leaves, f, ensure_ascii=False, indent=1)
    return leaves


# ---------------------------------------------------------------- index

DAY_KEYS = {"date", "tradeDate", "dataDate", "statDate", "tradeDay", "日期"}


def is_day_field(key, label):
    """日期列必须是「业务日期」而不是「工商注册时间」这类普通日期字段。"""
    return key in DAY_KEYS or "日期" in (label or "")


def detect_date_field(defs, rows):
    """从字段定义 + 样例行判断哪一列是日期列（YYYY-MM-DD）。"""
    by_key = {d.get("fieldKey"): d.get("fieldLabel") for d in defs}
    for k, label in by_key.items():
        if not is_day_field(k, label):
            continue
        vals = [r.get(k) for r in rows if isinstance(r.get(k), str)]
        if vals and all(DAY_RE.match(v.strip()) for v in vals):
            return k
    for k, label in by_key.items():
        if not is_day_field(k, label):
            continue
        if any(isinstance(r.get(k), str) and DAY_RE.match(r[k].strip()) for r in rows):
            return k
    return None


def build_index(budget, leaves, log=print):
    """逐节点调 excelData，记录指标表。可重复执行：已收录的节点跳过。"""
    index = {}
    if os.path.exists(CACHE_INDEX):
        with open(CACHE_INDEX) as f:
            index = json.load(f)
    todo = [n for n in leaves if n["nodeCode"] not in index]
    log(f"节点共 {len(leaves)}，已索引 {len(index)}，本次待扫 {len(todo)}")
    done = 0
    for node in todo:
        try:
            blocks = post("/api/disclosure/nav/market/excelData",
                          {"nodeCode": node["nodeCode"]}, budget)
        except BudgetExceeded:
            log(f"请求预算用尽（{budget.used}），索引已保存，下次继续")
            break
        except Exception as e:  # 单节点失败不影响整体
            log(f"  ! {node['nodeName']}: {e}")
            index[node["nodeCode"]] = []
            done += 1
            continue
        entries = []
        for blk in blocks or []:
            tpl = blk.get("templateContentVO") or {}
            defs = tpl.get("fieldDefinitions") or []
            rows = blk.get("data") or []
            date_field = detect_date_field(defs, rows) if rows else None
            entries.append({
                "templateUuid": tpl.get("templateUuid"),
                "templateName": tpl.get("templateName"),
                "dateField": date_field,
                "total": blk.get("total"),
                "fields": [[d.get("fieldKey"), d.get("fieldLabel")]
                           for d in defs],
                "nodes": [node["path"]],
                "nodeCode": node["nodeCode"],
            })
        index[node["nodeCode"]] = entries
        done += 1
        dated = sum(1 for e in entries if e["dateField"])
        log(f"  [{done}/{len(todo)}] {node['path']}: {len(entries)} 表, 按日 {dated}")
        with open(CACHE_INDEX, "w") as f:
            json.dump(index, f, ensure_ascii=False, indent=1)
    with open(CACHE_INDEX, "w") as f:
        json.dump(index, f, ensure_ascii=False, indent=1)
    return index


def load_index():
    if not os.path.exists(CACHE_INDEX):
        raise SystemExit("还没有指标索引，先跑一次: gd_pm_fetch.py --build-index")
    with open(CACHE_INDEX) as f:
        return json.load(f)


# ---------------------------------------------------------------- fetch by date

def fetch_day(entry, date, budget):
    """按单日区间取一张指标表，返回行列表（无数据则空）。"""
    data = post("/api/disclosure/nav/market/excelData/page", {
        "templateUuid": entry["templateUuid"],
        "pageNum": 1,
        "pageSize": 50,
        "startTime": f"{date} 00:00:00",
        "endTime": f"{date} 23:59:59",
    }, budget)
    if isinstance(data, dict):
        rows = data.get("data") or []
    elif isinstance(data, list):
        rows = data
    else:
        rows = []
    want = entry["dateField"]
    return [r for r in rows if str(r.get(want, ""))[:10] == date]


def collect(index, date, node_filter=None, budget=None, log=print):
    results = []
    seen = set()
    candidates = []
    for node_entries in index.values():
        for e in node_entries:
            if not e.get("templateUuid") or not e.get("dateField"):
                continue
            labels = dict(e.get("fields") or [])
            if not is_day_field(e["dateField"], labels.get(e["dateField"])):
                continue
            if e["templateUuid"] in seen:
                continue
            if node_filter:
                hay = (e.get("templateName") or "") + " ".join(e.get("nodes") or [])
                if node_filter.lower() not in hay.lower():
                    continue
            seen.add(e["templateUuid"])
            candidates.append(e)
    log(f"候选日频指标表 {len(candidates)} 张，按 {date} 取数…")
    for e in candidates:
        try:
            rows = fetch_day(e, date, budget)
        except BudgetExceeded:
            log(f"请求预算用尽（{budget.used}），已取 {len(results)} 张")
            break
        except Exception as ex:
            log(f"  ! {e['templateName']}: {ex}")
            continue
        if rows:
            results.append({
                "node": (e.get("nodes") or [""])[0],
                "nodeCode": e.get("nodeCode"),
                "templateName": e.get("templateName"),
                "templateUuid": e["templateUuid"],
                "dateField": e["dateField"],
                "fields": e["fields"],
                "rows": rows,
            })
    return results


def render(results, date):
    if not results:
        print(f"{date} 没有取到任何日频指标数据（可能该日未披露）。")
        return
    print(f"# 广东电力交易指标 {date}  （{len(results)} 张指标表）\n")
    for r in results:
        labels = {k: v for k, v in r["fields"]}
        print(f"## {r['templateName']}   [{r['node']}]")
        for row in r["rows"]:
            print("   " + "  ".join(
                f"{labels.get(k, k)}={v}" for k, v in row.items()
                if k in labels and v not in (None, "")
            ))
        print()


# ---------------------------------------------------------------- fetch by month

def month_capable(entry):
    return any("月份" in (label or "") for _, label in entry.get("fields") or [])


def collect_month(index, month, node_filter=None, budget=None, log=print):
    """月频指标（中长期/绿电/零售/HHI/Top-m 等）。"""
    results, seen, candidates = [], set(), []
    for node_entries in index.values():
        for e in node_entries:
            if not e.get("templateUuid") or not month_capable(e):
                continue
            if e["templateUuid"] in seen:
                continue
            if node_filter:
                hay = (e.get("templateName") or "") + " ".join(e.get("nodes") or [])
                if node_filter.lower() not in hay.lower():
                    continue
            seen.add(e["templateUuid"])
            candidates.append(e)
    log(f"候选月频指标表 {len(candidates)} 张，按 {month} 取数…")
    for e in candidates:
        try:
            data = post("/api/disclosure/nav/market/excelData/page", {
                "templateUuid": e["templateUuid"], "pageNum": 1, "pageSize": 200,
            }, budget)
        except BudgetExceeded:
            log(f"请求预算用尽（{budget.used}），已取 {len(results)} 张")
            break
        except Exception as ex:
            log(f"  ! {e['templateName']}: {ex}")
            continue
        rows = (data.get("data") if isinstance(data, dict) else data) or []
        hit = [r for r in rows
               if any(isinstance(v, str) and v == month for v in r.values())]
        if hit:
            results.append({
                "node": (e.get("nodes") or [""])[0],
                "nodeCode": e.get("nodeCode"),
                "templateName": e.get("templateName"),
                "templateUuid": e["templateUuid"],
                "fields": e["fields"],
                "rows": hit,
            })
    return results


def main():
    ap = argparse.ArgumentParser(description="广东电力交易中心 电力交易指标 抓取")
    ap.add_argument("--date", help="日期 YYYY-MM-DD")
    ap.add_argument("--month", help="月份 YYYY-MM（中长期/绿电/零售等月频指标）")
    ap.add_argument("--node", help="只取匹配的节点/指标表（子串匹配）")
    ap.add_argument("--list-nodes", action="store_true", help="打印披露节点树")
    ap.add_argument("--build-index", action="store_true",
                    help="建/续建指标索引（分批跑，已索引节点跳过）")
    ap.add_argument("--refresh-tree", action="store_true", help="重新拉节点树")
    ap.add_argument("--json", action="store_true", help="输出 JSON")
    ap.add_argument("--max-requests", type=int, default=90,
                    help="单次运行请求上限（默认 90）")
    args = ap.parse_args()

    if not (args.list_nodes or args.build_index):
        if not (re.match(r"^\d{4}-\d{2}-\d{2}$", args.date or "")
                or re.match(r"^\d{4}-\d{2}$", args.month or "")):
            ap.error("需要 --date YYYY-MM-DD、--month YYYY-MM，或用 --list-nodes / --build-index")

    budget = Budget(args.max_requests)
    leaves = load_tree(budget, refresh=args.refresh_tree)

    if args.list_nodes:
        for n in leaves:
            print(f"{'  ' * (int(n.get('depth') or 1) - 1)}{n['nodeName']}  {n['nodeCode']}")
        return

    if args.build_index:
        build_index(budget, leaves)
        print(f"索引完成，本次请求 {budget.used} 次")
        return

    index = load_index()
    log = (lambda m: print(m, file=sys.stderr)) if args.json else print
    if args.month:
        results = collect_month(index, args.month, args.node, budget, log=log)
        label = args.month
    else:
        results = collect(index, args.date, args.node, budget, log=log)
        label = args.date
    if args.json:
        json.dump({"date": label, "count": len(results),
                   "indicators": results}, sys.stdout,
                  ensure_ascii=False, indent=1)
        print()
    else:
        render(results, label)
    if not args.json:
        print(f"(请求 {budget.used} 次)")


if __name__ == "__main__":
    main()
