#!/usr/bin/env python3
"""小时电价预测算法离线对比（选优，不碰线上）。

对比对象（as-of 重放，特征只允许 ≤ 锚点 A = X - H 的数据）：
  1. naive_d1      同小时、锚点日价格（昨天/8天前直接抄）
  2. naive_weekly  同小时、锚点往前最近同星期几的价格
  3. timesfm-3.0   现行算法：直打 forecast.xiaoyxq.top，输入 = A 及以前全部点
                   （服务端 4096 点截断），滚动 H*24 步取最后 24 点
  4. lightgbm      特征回归：同小时滞后价、滚动统计、锚点日全天形态、同小时电量、日历

两个口径（H = 目标日 X 距锚点天数）：
  H=1  数据齐口径（与统计按钮一致）
  H=8  生产口径（官网 T+6 滞后 → 线上目标日距锚点 8 天）

评估：最近 EVAL_DAYS 个完整日的 24 点。指标 MAE/RMSE/元，MAPE ÷ 当日实际均价，q10-q90 覆盖率。

用法：
  /usr/local/python3/bin/python3.12 tools/hour-price-benchmark/benchmark.py \
      --eval-days 90 [--horizons 1,8] [--skip-timesfm] [--workers 4]
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from datetime import date, timedelta

import numpy as np
import lightgbm as lgb
import psycopg

DB_LINK = os.environ.get(
    "EMS_DB_LINK", "postgresql://test:test123456@10.193.0.10:5432/ems-test")
FORECAST_URL = os.environ.get("FORECAST_URL", "https://forecast.xiaoyxq.top/v1/forecast")
QUANTILES = (0.1, 0.5, 0.9)

# 法定节假日（评估窗口 2024-2026）：元旦/春节/清明/五一/端午/中秋/国庆
HOLIDAYS = set()
DAY_CACHE: dict[str, list[float]] = {}   # date -> [ahead_price x24]
DAY_MEAN: dict[str, float] = {}          # date -> 全天均价
def build_caches(days):
    DAY_CACHE.clear(); DAY_MEAN.clear()
    for d, rows in days.items():
        DAY_CACHE[d] = [r[0] for r in rows]
        DAY_MEAN[d] = float(np.mean([r[0] for r in rows]))
def _rng(a, b):
    d0, d1 = date.fromisoformat(a), date.fromisoformat(b)
    while d0 <= d1:
        HOLIDAYS.add(d0.isoformat())
        d0 += timedelta(days=1)
for y in (2024, 2025, 2026):
    HOLIDAYS.add(f"{y}-01-01")
for a, b in (("2024-02-10","2024-02-17"),("2025-01-28","2025-02-04"),("2026-02-15","2026-02-22"),
             ("2024-04-04","2024-04-06"),("2025-04-04","2025-04-06"),("2026-04-04","2026-04-06"),
             ("2024-05-01","2024-05-05"),("2025-05-01","2025-05-05"),("2026-05-01","2026-05-05"),
             ("2024-06-08","2024-06-10"),("2025-05-31","2025-06-02"),("2026-06-19","2026-06-21"),
             ("2024-09-15","2024-09-17"),("2025-10-01","2025-10-08"),("2026-09-25","2026-09-27"),
             ("2024-10-01","2024-10-07"),("2026-10-01","2026-10-07")):
    _rng(a, b)



def load_days() -> dict[str, list[tuple[float, float]]]:
    """{date: [(ahead_price, power_mw) × 24 按小时升序]}，只留完整日。"""
    conn = psycopg.connect(DB_LINK, connect_timeout=10)
    cur = conn.cursor()
    cur.execute("""SELECT price_date, hour, ahead_price, power_mw FROM gd_home_24h
                   ORDER BY price_date, hour""")
    byday: dict[str, dict[int, tuple[float, float]]] = {}
    for d, h, p, w in cur.fetchall():
        byday.setdefault(d, {})[h] = (float(p), float(w))
    conn.close()
    return {d: [rows[h] for h in range(24)] for d, rows in byday.items() if len(rows) == 24}


def features(days: dict[str, list[tuple[float, float]]], X: date, hour: int, H: int) -> list[float] | None:
    """X 目标日、锚点 A = X-H。只用 ≤ A 的数据；缺失 lag 用 nan 让 LightGBM 自己分支。"""
    A = X - timedelta(days=H)
    s = days.get(A.isoformat())
    if s is None:
        return None
    nan = float("nan")
    price = lambda d: days[d.isoformat()][hour][0] if d.isoformat() in days else nan
    power = lambda d: days[d.isoformat()][hour][1] if d.isoformat() in days else nan

    lags = [price(A - timedelta(days=k)) for k in (0, 1, 2, 7, 14)]
    # 同小时滚动统计（7/30 天，含 A）
    roll = []
    for win in (7, 30):
        seq = [price(A - timedelta(days=k)) for k in range(win)]
        arr = np.array([v for v in seq if v == v], dtype=float)
        roll += [float(arr.mean()) if arr.size else nan, float(arr.std()) if arr.size else nan]
    # 锚点日全天形态（24 点 min/max/mean）与电量
    prices_a = np.array([s[h][0] for h in range(24)])
    power_a = np.array([s[h][1] for h in range(24)])
    daystat = [prices_a.min(), prices_a.max(), prices_a.mean(),
               float(power_a.mean()), power(A)]
    # 同星期几多周同小时均值（近 4 个，≤ A）——查表 O(1)
    dows = []
    cur, k = A, 0
    while len(dows) < 4 and k < 35:
        if cur.weekday() == X.weekday():
            dc = DAY_CACHE.get(cur.isoformat())
            if dc is not None:
                dows.append(dc[hour])
        cur -= timedelta(days=1)
        k += 1
    dow_mean = float(np.mean(dows)) if dows else nan
    # 日级趋势：近 3 天日均 / 近 10 天日均——查表 O(1)
    short = [DAY_MEAN.get((A - timedelta(days=k)).isoformat(), nan) for k in range(3)]
    long_ = [DAY_MEAN.get((A - timedelta(days=k)).isoformat(), nan) for k in range(10)]
    ls, ll = np.nanmean(short), np.nanmean(long_)
    trend = float(ls / ll) if ll not in (0.0, nan) else nan
    # 形状：锚点日该小时占全天均价比例
    mean_a = prices_a.mean()
    shape = s[hour][0] / mean_a if mean_a else nan
    dow = X.weekday()
    cal = [hour, dow, X.month, 1.0 if dow >= 5 else 0.0,
           1.0 if X.isoformat() in HOLIDAYS else 0.0]
    return lags + roll + daystat + cal + [dow_mean, trend, shape]


def build_matrix(days: dict[str, list[tuple[float, float]]], targets: list[date], H: int):
    Xs, ys = [], []
    have_days = []
    for X in targets:
        rows = days.get(X.isoformat())
        if rows is None:
            continue
        day_rows = 0
        for h in range(24):
            f = features(days, X, h, H)
            if f is None:
                break
            Xs.append(f)
            ys.append(rows[h][0])
            day_rows += 1
        if day_rows == 24:
            have_days.append(X)
    return np.array(Xs, dtype=float), np.array(ys, dtype=float), have_days


def kpi(pred: np.ndarray, actual: np.ndarray, dates: list[date], per_day: np.ndarray) -> dict:
    err = np.abs(pred - actual)
    mae, rmse = float(err.mean()), float(np.sqrt(((pred - actual) ** 2).mean()))
    # MAPE 按日聚合：当日平均绝对误差 ÷ 当日实际均价（与前端统计口径一致）
    mapes = []
    for i, d in enumerate(dates):
        day_err = err[i * 24:(i + 1) * 24].mean()
        avg = abs(per_day[i])
        if avg > 1:
            mapes.append(day_err / avg)
    return {"MAE": round(mae, 1), "RMSE": round(rmse, 1),
            "MAPE%": round(100 * float(np.mean(mapes)), 2), "days": len(dates)}


def coverage(pred_q10: np.ndarray, pred_q90: np.ndarray, actual: np.ndarray) -> float:
    return round(float(((actual >= pred_q10) & (actual <= pred_q90)).mean()) * 100)


def run_lightgbm(days, eval_targets, H):
    all_days = sorted(date.fromisoformat(d) for d in days)
    eval0 = eval_targets[0]
    train_targets = [d for d in all_days if d < eval0]
    Xtr, ytr, _ = build_matrix(days, train_targets, H)
    Xev, yev, ev_dates = build_matrix(days, eval_targets, H)
    # 按日实际均价（MAPE 分母）
    per_day = np.array([np.mean([days[d.isoformat()][h][0] for h in range(24)]) for d in ev_dates])

    preds = {}
    for q in QUANTILES:
        m = lgb.train({"objective": "quantile", "alpha": q, "verbosity": -1,
                       "num_leaves": 63, "learning_rate": 0.05,
                       "min_data_in_leaf": 50, "num_threads": 1},
                      lgb.Dataset(Xtr, ytr), num_boost_round=500)
        preds[q] = m.predict(Xev)
    res = {"lightgbm": kpi(preds[0.5], yev, ev_dates, per_day)}
    res["lightgbm"]["覆盖%"] = coverage(preds[0.1], preds[0.9], yev)
    return res, preds, yev, ev_dates, per_day


def run_naives(days, eval_targets, H):
    out = {}
    preds_d1, preds_wk = [], []
    yev, ev_dates, per_day, wk_dates = [], [], [], []
    for X in eval_targets:
        rows = days.get(X.isoformat())
        A = X - timedelta(days=H)
        if rows is None or A.isoformat() not in days:
            continue
        # 最近同星期几且 ≤ A 的完整日
        wk = A
        for _ in range(21):
            if wk.weekday() == X.weekday() and wk.isoformat() in days:
                break
            wk -= timedelta(days=1)
        wk_ok = wk.weekday() == X.weekday() and wk.isoformat() in days
        for h in range(24):
            preds_d1.append(days[A.isoformat()][h][0])
            if wk_ok:
                preds_wk.append(days[wk.isoformat()][h][0])
            yev.append(rows[h][0])
        ev_dates.append(X)
        if wk_ok:
            wk_dates.append(X)
        per_day.append(np.mean([r[0] for r in rows]))
    yev = np.array(yev); per_day = np.array(per_day)
    out["naive_d1"] = kpi(np.array(preds_d1), yev, ev_dates, per_day)
    # weekly 基线只统计同星期日存在的天，口径单独算，不与 d1 混
    if wk_dates:
        sel = [i for i, d in enumerate(ev_dates) if d in wk_dates]
        yw = np.concatenate([yev[i*24:(i+1)*24] for i in sel])
        pw = np.array(preds_wk)
        pw_day = np.array([per_day[i] for i in sel])
        out["naive_weekly"] = kpi(pw, yw, [ev_dates[i] for i in sel], pw_day)
    return out, yev, ev_dates, per_day


def timesfm_one(days_sorted: list[date], days: dict, X: date, H: int) -> dict | None:
    A = X - timedelta(days=H)
    if A.isoformat() not in days or X.isoformat() not in days:
        return None
    # 输入 = A 及以前全部点（服务端自截 4096）
    vals = []
    for d in days_sorted:
        if d > A:
            break
        vals += [r[0] for r in days[d.isoformat()]]
    body = {"series": [{"values": vals}], "horizon": H * 24,
            "quantiles": ["0.1", "0.5", "0.9"]}
    req = urllib.request.Request(FORECAST_URL, data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=300) as r:
        resp = json.loads(r.read())
    f = resp["forecasts"][0]
    q = f.get("quantiles") or {}
    def last24(key):
        arr = q.get(key) or []
        return arr[-24:] if len(arr) >= 24 else None
    med = last24("0.5") or f.get("point", [])[-24:]
    q10, q90 = last24("0.1"), last24("0.9")
    if len(med) < 24:
        return None
    return {"median": med, "q10": q10, "q90": q90}


def run_timesfm(days, eval_targets, H, workers):
    days_sorted = sorted(date.fromisoformat(d) for d in days)
    results: dict[date, dict] = {}

    def work(X):
        for _ in range(3):
            try:
                r = timesfm_one(days_sorted, days, X, H)
                if r:
                    results[X] = r
                return
            except Exception:
                time.sleep(2)

    with ThreadPoolExecutor(max_workers=workers) as pool:
        list(pool.map(work, eval_targets))
    ev_dates = sorted(results)
    med = np.array([v for d in ev_dates for v in results[d]["median"]])
    yev = np.array([days[d.isoformat()][h][0] for d in ev_dates for h in range(24)])
    per_day = np.array([np.mean([r[0] for r in days[d.isoformat()]]) for d in ev_dates])
    np.savez("/tmp/timesfm-preds-H%d.npz" % H, dates=np.array([d.isoformat() for d in ev_dates]),
             med=med, yev=yev, per_day=per_day,
             q10=np.array([v for d in ev_dates for v in (results[d]["q10"] or [0]*24)]),
             q90=np.array([v for d in ev_dates for v in (results[d]["q90"] or [0]*24)]))
    out = {"timesfm-3.0": kpi(med, yev, ev_dates, per_day)}
    if all(results[d]["q10"] and results[d]["q90"] for d in ev_dates):
        q10 = np.array([v for d in ev_dates for v in results[d]["q10"]])
        q90 = np.array([v for d in ev_dates for v in results[d]["q90"]])
        out["timesfm-3.0"]["覆盖%"] = coverage(q10, q90, yev)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--eval-days", type=int, default=90)
    ap.add_argument("--horizons", default="1,8")
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--skip-timesfm", action="store_true")
    args = ap.parse_args()

    days = load_days()
    build_caches(days)
    all_days = sorted(date.fromisoformat(d) for d in days)
    eval_targets = all_days[-args.eval_days:]
    print(f"完整日 {len(all_days)}（{all_days[0]} ~ {all_days[-1]}），评估窗口 {eval_targets[0]} ~ {eval_targets[-1]} 共 {len(eval_targets)} 天")

    for H in [int(x) for x in args.horizons.split(",")]:
        print(f"\n=== H={H}（锚点 = 目标日前 {H} 天）===")
        naives, _, _, _ = run_naives(days, eval_targets, H)
        for k, v in naives.items():
            print(f"  {k:14s} MAE={v['MAE']:7.1f} RMSE={v['RMSE']:7.1f} MAPE={v['MAPE%']:6.2f}%")
        lgb_res, _, _, _, _ = run_lightgbm(days, eval_targets, H)
        for k, v in lgb_res.items():
            print(f"  {k:14s} MAE={v['MAE']:7.1f} RMSE={v['RMSE']:7.1f} MAPE={v['MAPE%']:6.2f}% 覆盖={v['覆盖%']}%")
        if not args.skip_timesfm:
            tf = run_timesfm(days, eval_targets, H, args.workers)
            for k, v in tf.items():
                cov = f" 覆盖={v.get('覆盖%', '-')}%" if "覆盖%" in v else ""
                print(f"  {k:14s} MAE={v['MAE']:7.1f} RMSE={v['RMSE']:7.1f} MAPE={v['MAPE%']:6.2f}%{cov}")
    print("\n注：lightgbm/naive 特征只用 ≤ 锚点数据；timesfm 输入锚点及以前全部点（4096 点内）。")


if __name__ == "__main__":
    sys.exit(main())
