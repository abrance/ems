#!/usr/bin/env python3
"""广东电力现货**日频**指标 -> VictoriaMetrics。

抓取部分直接复用同目录的 `gd_pm_fetch.py`（端点、索引、限速都不重写），
这里只做两件事：把指标表字段映射成固定指标名 + 标签，然后按 prometheus
文本行协议批量写 vminsert。

指标建模（标签只放低基数维度，指标名不塞 label）：

    gdpm_price_cny_mwh{market, side, kind, src}   电价，元/MWh
    gdpm_volume_mwh{market, side, kind, src}      电量，MWh

    market = dayahead | realtime
    side   = gen | load          （申报均价按发电侧算 gen）
    kind   = weighted_avg | declared_avg | max | min | total
    src    = pm.gd.csg.cn

时间戳统一取当日 00:00:00+08:00 的毫秒值。

用法：
    python3 gdpm_import.py --date 2026-11-10
    python3 gdpm_import.py --from 2026-11-05 --to 2026-11-10 --dry-run
    python3 gdpm_import.py --date 2026-11-10 \
        --vm-insert-url http://127.0.0.1:8480/insert/0/prometheus/api/v1/import/prometheus

幂等：同一序列同一时间戳重复写是覆盖，同一天重跑不会重复。

TODO(月频/年频)：中长期、绿电、零售、Top-m、HHI 这次不进 VM，等确定存储位置与保留期后另接。
"""

import argparse
import datetime as dt
import json
import sys
import urllib.error
import urllib.parse
import urllib.request

import gd_pm_fetch as fetch

SRC = "pm.gd.csg.cn"
PRICE_METRIC = "gdpm_price_cny_mwh"
VOLUME_METRIC = "gdpm_volume_mwh"

DEFAULT_INSERT_URL = "http://10.43.183.106:8480/insert/0/prometheus/api/v1/import/prometheus"
DEFAULT_SELECT_URL = "http://10.43.96.62:8481/select/0/prometheus"
# 源站限速，沿用 gd_pm_fetch.py 的 0.7s；这里不再另外加延迟
TZ = dt.timezone(dt.timedelta(hours=8))


# ---------------------------------------------------------------- 指标映射

def market_of(template_name):
    """指标表名 -> market 标签；不是现货日频表就返回 None（不写 VM）。"""
    name = template_name or ""
    if "现货市场总体运行情况" not in name:
        return None
    if "实时" in name:
        return "realtime"
    if "日前" in name:
        return "dayahead"
    return None


def side_of(field_key):
    if field_key.startswith("consumerSide"):
        return "load"
    # generationSide* 与 dailyDeclarationAvgPrice（发电侧申报）都算发电侧
    return "gen"


def kind_of(field_key):
    """字段名 -> kind 标签；认不出的字段返回 None（跳过，不猜）。"""
    if "MaxPrice" in field_key:
        return "max"
    if "MinPrice" in field_key:
        return "min"
    if "DeclarationAvg" in field_key:
        return "declared_avg"
    if "AvgPrice" in field_key:
        return "weighted_avg"
    if "TotalVolume" in field_key:
        return "total"
    return None


def metric_of(field_key):
    if "Price" in field_key:
        return PRICE_METRIC
    if "Volume" in field_key:
        return VOLUME_METRIC
    return None


def series_of(field_key, market):
    """字段 -> 序列描述；不认识的字段返回 None。"""
    metric, kind = metric_of(field_key), kind_of(field_key)
    if metric is None or kind is None:
        return None
    return {
        "metric": metric,
        "labels": {"market": market, "side": side_of(field_key), "kind": kind, "src": SRC},
    }


def day_ts_ms(date_str):
    """YYYY-MM-DD -> 当日 00:00:00+08:00 的毫秒时间戳。"""
    d = dt.datetime.strptime(date_str, "%Y-%m-%d").replace(tzinfo=TZ)
    return int(d.timestamp() * 1000)


def esc(v):
    return str(v).replace("\\", r"\\").replace('"', r"\"").replace("\n", r"\n")


def format_line(metric, labels, value, ts_ms):
    body = ",".join(f'{k}="{esc(labels[k])}"' for k in sorted(labels))
    return f"{metric}{{{body}}} {value} {ts_ms}"


def rows_to_samples(results):
    """gd_pm_fetch.collect() 的结果 -> (lines, 跳过说明)。"""
    lines, skipped = [], []
    for res in results:
        market = market_of(res.get("templateName"))
        if market is None:
            skipped.append(f"{res.get('templateName')}: 非现货日频表")
            continue
        declared = [f[0] for f in res.get("fields") or []]
        for row in res.get("rows") or []:
            date_str = str(row.get(res.get("dateField") or "date", ""))[:10]
            if not date_str:
                skipped.append(f"{res.get('templateName')}: 行没有日期")
                continue
            ts_ms = day_ts_ms(date_str)
            for key in declared:  # 只取本表声明的字段，实时表夹带的日前字段不会串味
                series = series_of(key, market)
                if series is None:
                    continue
                raw = row.get(key)
                if raw in (None, ""):
                    continue
                try:
                    value = float(raw)
                except (TypeError, ValueError):
                    skipped.append(f"{res.get('templateName')}.{key}: 非数值 {raw!r}")
                    continue
                lines.append(format_line(series["metric"], series["labels"], value, ts_ms))
    return lines, skipped


# ---------------------------------------------------------------- 时间区间

def parse_days(args):
    if args.date:
        days = [args.date]
    elif args.from_date and args.to_date:
        start = dt.date.fromisoformat(args.from_date)
        end = dt.date.fromisoformat(args.to_date)
        if end < start:
            raise SystemExit("--to 早于 --from")
        days = [(start + dt.timedelta(days=i)).isoformat() for i in range((end - start).days + 1)]
    else:
        raise SystemExit("需要 --date 或 --from/--to")
    for d in days:
        dt.date.fromisoformat(d)  # 校验格式
    return days


# ---------------------------------------------------------------- 写入 / 校验

def post_lines(url, lines):
    body = ("\n".join(lines) + "\n").encode()
    req = urllib.request.Request(url, data=body, method="POST",
                                headers={"Content-Type": "text/plain"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.status


def query_day(select_url, promql, date_str):
    """用 export 精确取某一天的样本。

    不用 query_range：没有 rollup 函数时它每个网格点的取值窗口是 [ts-step, ts]，
    step 一大就会把前一天的值冒充成当天（当天没数据也返回旧值）。export 返回真实
    样本时间戳，没有样本就是空。
    """
    day_start = day_ts_ms(date_str) // 1000
    q = urllib.parse.urlencode({
        "match[]": promql,
        "start": day_start,
        "end": day_start + 86400 - 1,
    })
    with urllib.request.urlopen(f"{select_url}/api/v1/export?{q}", timeout=60) as r:
        body = r.read().decode()
    return parse_export_day(body, date_str)


def parse_export_day(body, date_str):
    """解析 export 的 NDJSON，只保留落在 date_str 当天的样本，返回最后一个值（没有则 None）。"""
    day_start_ms = day_ts_ms(date_str)
    last = None
    for line in body.splitlines():
        line = line.strip()
        if not line:
            continue
        row = json.loads(line)
        if row.get("status") == "error" or row.get("error"):
            raise RuntimeError(row.get("error") or "export 失败")
        ts_list = row.get("timestamps") or []
        vals = row.get("values") or []
        for ts_ms, val in zip(ts_list, vals):
            # 只认落在当天的样本，杜绝邻近日期的值混进来
            if day_start_ms <= ts_ms <= day_start_ms + 86400 * 1000 - 1:
                last = val
    return last


def verify_day(select_url, date_str, log=print):
    """回查某一天写的序列：打印每个序列的值（没有则打印空）。"""
    found = 0
    for metric, labels in (
        (PRICE_METRIC, {"market": "dayahead", "side": "gen", "kind": "weighted_avg"}),
        (PRICE_METRIC, {"market": "dayahead", "side": "gen", "kind": "declared_avg"}),
        (VOLUME_METRIC, {"market": "dayahead", "side": "gen", "kind": "total"}),
        (VOLUME_METRIC, {"market": "dayahead", "side": "load", "kind": "total"}),
    ):
        sel = ",".join(f'{k}="{v}"' for k, v in labels.items())
        val = query_day(select_url, f"{metric}{{{sel}}}", date_str)
        if val is not None:
            found += 1
        else:
            val = "（无）"
        log(f"  {metric}{{{sel}}} = {val}")
    log(f"  {date_str}: {found} 条序列查到值（@ts={day_ts_ms(date_str) // 1000}）")
    return found


# ---------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser(description="广东现货日频指标 -> VictoriaMetrics")
    g = ap.add_mutually_exclusive_group(required=True)
    g.add_argument("--date", help="单日 YYYY-MM-DD")
    g.add_argument("--from", dest="from_date", help="区间起始 YYYY-MM-DD")
    ap.add_argument("--to", dest="to_date", help="区间结束 YYYY-MM-DD（含）")
    ap.add_argument("--vm-insert-url", default=DEFAULT_INSERT_URL,
                    help="vminsert 导入地址")
    ap.add_argument("--vm-select-url", default=None,
                    help="vmselect 前缀，给了就在写完后回查（--verify 的地址）")
    ap.add_argument("--verify", action="store_true", help="写完立刻按日期回查")
    ap.add_argument("--dry-run", action="store_true", help="只打印要写的行，不发请求")
    ap.add_argument("--max-requests", type=int, default=90,
                    help="每轮抓取的请求上限（沿用 gd_pm_fetch 的预算机制）")
    args = ap.parse_args()

    days = parse_days(args)
    index = fetch.load_index()
    log = lambda msg: print(msg, file=sys.stderr)

    total_lines = 0
    for day in days:
        budget = fetch.Budget(args.max_requests)
        results = fetch.collect(index, day, budget=budget, log=log)
        lines, skipped = rows_to_samples(results)
        for note in skipped:
            log(f"  · {note}")
        if not lines:
            log(f"{day}: 没有可写的日频指标（可能当日未披露）")
            continue
        if args.dry_run:
            print("\n".join(lines))
            total_lines += len(lines)
            continue
        post_lines(args.vm_insert_url, lines)
        total_lines += len(lines)
        log(f"{day}: 写入 {len(lines)} 条样本，请求 {budget.used} 次")
        if args.verify or args.vm_select_url:
            verify_day(args.vm_select_url or DEFAULT_SELECT_URL, day, log=log)

    print(f"完成：{len(days)} 天，{total_lines} 条样本"
          f"{'（dry-run 未写入）' if args.dry_run else ''}")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except urllib.error.HTTPError as e:
        print(f"HTTP {e.code}: {e.read()[:200]!r}", file=sys.stderr)
        sys.exit(1)
    except urllib.error.URLError as e:
        print(f"网络错误: {e.reason}", file=sys.stderr)
        sys.exit(1)
