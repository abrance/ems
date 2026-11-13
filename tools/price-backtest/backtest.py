#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""96 点电价离线回测：TimesFM vs 持久化（昨日同时刻）vs 抄日前价。

口径与线上代码保持一致（见 OpenLEMS/application/internal/logic/forecast/price_intraday.go）：
  - 数据源 gd_metric.csv，typeId 21=日前电价 22=实时电价 23=负荷，sourceId 9=实际值，
    999=电查查预测占位（**一律剔除**，拿占位当基准等于两个模型互比）。
  - 目标日 D 的 96 点 = D 00:15 … D+1 00:00（CSV 把当天最后一点写成 24:00）。
  - 上下文 = D 之前最近 history-days 天的实际值，尾部截断到 4096 点（TimesFM max_context）。
  - market=real 送三通道 [实时价, 日前价, 系统负荷]（priceCovariateKinds["real"]），
    任一通道缺值的时刻从所有通道一起丢弃；market=dayAhead 单变量。
  - 指标 MAE（元/MWh）、WAPE = Σ|e| / Σ|actual|。

用法见同目录 README.md。典型一条命令（在仓库根目录，--csv 可省略）：
  python3 tools/price-backtest/backtest.py \\
      --market real --from 2026-09-08 --to 2026-10-01 --history-days 24
"""

import argparse
import csv
import json
import os
import sys
import urllib.request
from datetime import datetime, timedelta

TYPE_DA, TYPE_RT, TYPE_LOAD = 21, 22, 23
SOURCE_REAL = 9
MAX_CONTEXT = 4096
STEP = timedelta(minutes=15)
KIND_BY_MARKET = {
    "dayAhead": {"all": [TYPE_DA]},
    "real": {"all": [TYPE_RT, TYPE_DA, TYPE_LOAD]},
    "load": {"all": [TYPE_LOAD]},
}

# gd_metric.csv 的两个可能位置：仓库根 out/（部署机）与仓库内 gdmetric 产出目录（当前仓库）
CSV_CANDIDATES = (
    "out/gd_metric.csv",
    "OpenLEMS/application/internal/logic/gdmetric/out/gd_metric.csv",
)
REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def resolve_csv(explicit):
    """--csv 显式 > 环境变量 GD_METRIC_CSV > 候选相对路径（先 cwd 再仓库根）。"""
    if explicit:
        return explicit
    env = os.environ.get("GD_METRIC_CSV")
    if env:
        return env
    for base in (os.getcwd(), REPO_ROOT):
        for rel in CSV_CANDIDATES:
            path = os.path.join(base, rel)
            if os.path.exists(path):
                return path
    return CSV_CANDIDATES[0]


def load_series(path):
    """返回 {typeId: {timestamp: value}}，只保留实际值来源。"""
    out = {TYPE_DA: {}, TYPE_RT: {}, TYPE_LOAD: {}}
    bad_clock = 0
    with open(path, "r", encoding="utf-8-sig", newline="") as f:
        r = csv.DictReader(f)
        for row in r:
            try:
                type_id = int(row["typeId"])
                source_id = int(row["sourceId"] or 0)
                value = float(row["数值"])
            except (KeyError, TypeError, ValueError):
                continue
            if type_id not in out or source_id != SOURCE_REAL:
                continue
            date_s, clock = row["日期"].strip(), row["时间"].strip()
            try:
                d = datetime.strptime(date_s, "%Y-%m-%d")
                if clock == "24:00":
                    ts = d + timedelta(days=1)
                else:
                    hh, mm = clock.split(":")
                    ts = d.replace(hour=int(hh), minute=int(mm))
            except ValueError:
                bad_clock += 1
                continue
            out[type_id][ts] = value
    if bad_clock:
        print("跳过时刻格式异常行 %d 条" % bad_clock, file=sys.stderr)
    return out


def target_points(day):
    """目标日 D 的 96 个时刻：D 00:15 … D+1 00:00。"""
    base = datetime(day.year, day.month, day.day)
    return [base + STEP * (i + 1) for i in range(96)]


def context_matrix(series, target, history_days, channels):
    """目标时刻之前的上下文矩阵；按时间戳对齐，缺任一通道则整点丢弃。"""
    start = datetime(target.year, target.month, target.day) - timedelta(days=history_days)
    ref = series[channels[0]]
    stamps = sorted(ts for ts in ref if start < ts < datetime(target.year, target.month, target.day))
    stamps = [ts for ts in stamps if all(ts in series[ch] for ch in channels)]
    matrix = [[series[ch][ts] for ts in stamps] for ch in channels]
    if len(stamps) > MAX_CONTEXT:
        stamps = stamps[-MAX_CONTEXT:]
        matrix = [ch[-MAX_CONTEXT:] for ch in matrix]
    return stamps, matrix


def call_timesfm(url, matrix, horizon, timeout=120):
    """返回 (point, quantiles)；quantiles 形如 {"0.9": [...]}，目标通道同样是第 0 通道。"""
    values = matrix if len(matrix) > 1 else matrix[0]
    body = json.dumps({"series": [{"id": "bt", "values": values}], "horizon": horizon}).encode()
    req = urllib.request.Request(url.rstrip("/") + "/v1/forecast", data=body,
                                 headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        payload = json.loads(resp.read().decode())
    if not payload.get("success", True):
        raise RuntimeError("预测服务返回失败: %s" % (payload.get("error") or payload))
    item = payload["forecasts"][0]
    point = item["point"]
    quantiles = item.get("quantiles") or {}
    # 多变量输入时服务端把 point / 分位回成 通道×horizon 的矩阵，目标通道是第 0 通道
    if point and isinstance(point[0], list):
        point = point[0]
    quantiles = {k: (v[0] if v and isinstance(v[0], list) else v) for k, v in quantiles.items()}
    return point, quantiles


def wape_stats(errs, actuals):
    n = len(actuals)
    if n == 0:
        return 0.0, 0.0, 0
    mae = sum(abs(e) for e in errs) / n
    denom = sum(abs(a) for a in actuals)
    return mae, (sum(abs(e) for e in errs) / denom * 100 if denom else 0.0), n


def main():
    ap = argparse.ArgumentParser(description="96 点电价回测（TimesFM / 持久化 / 抄日前价）")
    ap.add_argument("--csv", default="", help="gd_metric.csv 路径（默认自动探测 %s）" % " / ".join(CSV_CANDIDATES))
    ap.add_argument("--market", default="real", choices=sorted(KIND_BY_MARKET), help="目标市场")
    ap.add_argument("--from", dest="date_from", required=True, help="首个目标日 YYYY-MM-DD")
    ap.add_argument("--to", dest="date_to", required=True, help="末个目标日 YYYY-MM-DD")
    ap.add_argument("--history-days", type=int, default=30, help="送入模型的上下文天数（默认 30）")
    ap.add_argument("--forecast-url", default=os.environ.get("FORECAST_URL", "https://forecast.xiaoyxq.top"),
                    help="TimesFM 服务地址")
    ap.add_argument("--no-timesfm", action="store_true", help="只算基线，不调用预测服务")
    ap.add_argument("--cache-dir", default="", help="逐日预测缓存目录（可断点续跑）")
    ap.add_argument("--limit", type=int, default=0, help="最多回测多少天（调试用）")
    ap.add_argument("--export-quantiles", default="", metavar="PATH",
                    help="额外导出逐日 96 点分位序列 CSV（timestamp,value），供需量控制回测当 fixture")
    ap.add_argument("--quantile-level", default="0.9", help="--export-quantiles 的分位，默认 0.9")
    args = ap.parse_args()

    args.csv = resolve_csv(args.csv)
    if not os.path.exists(args.csv):
        sys.exit("找不到 gd_metric.csv：显式给了 %s，候选路径 %s 均不存在。用 --csv 或 GD_METRIC_CSV 指定"
                 % (args.csv, " / ".join(CSV_CANDIDATES)))
    print("读数据 %s …" % args.csv, file=sys.stderr)
    series = load_series(args.csv)
    channels = KIND_BY_MARKET[args.market]["all"]
    target_kind = channels[0]
    print("实际值点数：日前 %d / 实时 %d / 负荷 %d" % (
        len(series[TYPE_DA]), len(series[TYPE_RT]), len(series[TYPE_LOAD])), file=sys.stderr)

    # 可回测的目标日：目标市场当天 96 点齐全，且 12 天前起有上下文
    d0 = datetime.strptime(args.date_from, "%Y-%m-%d")
    d1 = datetime.strptime(args.date_to, "%Y-%m-%d")
    days = []
    d = d0
    while d <= d1:
        pts = target_points(d)
        if sum(1 for ts in pts if ts in series[target_kind]) >= 96:
            days.append(d)
        d += timedelta(days=1)
    if args.limit:
        days = days[: args.limit]
    if not days:
        sys.exit("区间内没有 96 点齐全的目标日，检查 --from/--to 与数据覆盖")

    acc = {k: ([], []) for k in ("timesfm", "persistence", "dayahead_as_actual")}
    skipped = []
    export_rows = []
    for i, day in enumerate(days, 1):
        pts = target_points(day)
        actuals = [series[target_kind][ts] for ts in pts if ts in series[target_kind]]

        # 持久化：前一天同一时刻
        prev = [series[target_kind].get(ts - timedelta(days=1)) for ts in pts]
        if all(v is not None for v in prev):
            acc["persistence"][0].extend(p - a for p, a in zip(prev, actuals))
            acc["persistence"][1].extend(actuals)

        # 抄日前价：目标日已发布的日前电价当实时价（只有 real 市场有意义）
        if target_kind == TYPE_RT:
            da = [series[TYPE_DA].get(ts) for ts in pts]
            if all(v is not None for v in da):
                acc["dayahead_as_actual"][0].extend(p - a for p, a in zip(da, actuals))
                acc["dayahead_as_actual"][1].extend(actuals)

        if args.no_timesfm:
            continue
        stamps, matrix = context_matrix(series, day, args.history_days, channels)
        if len(stamps) < 96:
            skipped.append((day.date().isoformat(), "上下文不足 %d 点" % len(stamps)))
            continue
        cache = os.path.join(args.cache_dir, "%s_%s_h%d.json" % (
            args.market, day.date().isoformat(), args.history_days)) if args.cache_dir else ""
        point, quantiles = None, {}
        if cache and os.path.exists(cache):
            cached = json.load(open(cache))
            if isinstance(cached, dict):
                point, quantiles = cached.get("point"), cached.get("quantiles") or {}
            else:  # 旧格式缓存：只有 point
                point = cached
            if point and isinstance(point[0], list):
                point = point[0]
        else:
            try:
                point, quantiles = call_timesfm(args.forecast_url, matrix, len(pts))
            except Exception as exc:  # 预测服务不可用不影响基线
                skipped.append((day.date().isoformat(), str(exc)))
                continue
            if cache:
                os.makedirs(os.path.dirname(cache), exist_ok=True)
                json.dump({"point": point, "quantiles": quantiles}, open(cache, "w"))
        pairs = [(p, series[target_kind][ts]) for p, ts in zip(point, pts)
                 if ts in series[target_kind]]
        acc["timesfm"][0].extend(p - a for p, a in pairs)
        acc["timesfm"][1].extend(a for _, a in pairs)
        if args.export_quantiles:
            level = quantiles.get(args.quantile_level) or []
            for idx, ts in enumerate(pts):
                value = level[idx] if idx < len(level) else (point[idx] if idx < len(point) else None)
                if value is not None:
                    export_rows.append((ts, value))
        print("  %s 完成（%d/%d 天）" % (day.date(), i, len(days)), file=sys.stderr)

    label = {"timesfm": "TimesFM", "persistence": "持久化(昨日同时刻)", "dayahead_as_actual": "抄日前价"}
    if target_kind == TYPE_RT:
        label["timesfm"] = "TimesFM 三通道(实时+日前+负荷)"
    else:
        label["timesfm"] = "TimesFM 单变量"
    print("\n市场=%s 窗口 %s~%s 上下文 %d 天（%d 个目标日）" % (
        args.market, args.date_from, args.date_to, args.history_days, len(days)))
    print("| 方法 | MAE 元/MWh | WAPE | 点数 |")
    print("|---|---|---|---|")
    for key in ("timesfm", "persistence", "dayahead_as_actual"):
        errs, actuals = acc[key]
        if not actuals:
            print("| %s | - | - | 0 |" % label[key])
            continue
        mae, wape, n = wape_stats(errs, actuals)
        print("| %s | %.1f | %.1f%% | %d |" % (label[key], mae, wape, n))
    if skipped:
        print("\n跳过 %d 天：%s" % (len(skipped), "; ".join("%s(%s)" % s for s in skipped[:5])))

    if args.export_quantiles:
        if not export_rows:
            sys.exit("\n--export-quantiles 没有可导出的分位值（服务未返回分位或全部跳过），未写文件")
        path = os.path.abspath(args.export_quantiles)
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        export_rows.sort()
        with open(path, "w", encoding="utf-8", newline="") as f:
            w = csv.writer(f, lineterminator="\n")
            w.writerow(["timestamp", "value"])
            for ts, value in export_rows:
                w.writerow([ts.strftime("%Y-%m-%dT%H:%M:%S"), "%.4f" % value])
        print("\n分位序列已导出 q%s：%s（%d 行，%s ~ %s）" % (
            args.quantile_level, path, len(export_rows),
            export_rows[0][0], export_rows[-1][0]))


if __name__ == "__main__":
    main()
