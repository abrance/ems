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
from scipy.stats import norm
from sklearn.impute import SimpleImputer
from sklearn.linear_model import LassoCV
from sklearn.model_selection import TimeSeriesSplit
from sklearn.pipeline import make_pipeline
from sklearn.preprocessing import StandardScaler

DB_LINK = os.environ.get(
    "EMS_DB_LINK", "postgresql://test:test123456@10.193.0.10:5432/ems-test")
FORECAST_URL = os.environ.get("FORECAST_URL", "https://forecast.xiaoyxq.top/v1/forecast")
QUANTILES = (0.1, 0.5, 0.9)

# 法定节假日（评估窗口 2024-2026）：元旦/春节/清明/五一/端午/中秋/国庆
HOLIDAYS = set()
DAY_CACHE: dict[str, list[float]] = {}   # date -> [ahead_price x24]
DAY_MEAN: dict[str, float] = {}          # date -> 全天均价
DAY_CACHE_RT: dict[str, list[float]] = {}  # 实时价分支：date -> [realtime_price x24]
DAY_MEAN_RT: dict[str, float] = {}         # 实时价分支：date -> 全天均价

def build_caches(days):
    DAY_CACHE.clear(); DAY_MEAN.clear()
    for d, rows in days.items():
        DAY_CACHE[d] = [r[0] for r in rows]
        DAY_MEAN[d] = float(np.mean([r[0] for r in rows]))

def build_caches_rt(days_rt):
    DAY_CACHE_RT.clear(); DAY_MEAN_RT.clear()
    for d, rows in days_rt.items():
        DAY_CACHE_RT[d] = [r[1] for r in rows]  # 下标1 = realtime_price
        DAY_MEAN_RT[d] = float(np.mean([r[1] for r in rows]))
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


def load_days_rt() -> dict[str, list[tuple[float, float, float]]]:
    """实时价方向分支专用：{date: [(ahead_price, realtime_price, power_mw) ×24 按小时升序]}。

    只留完整 24 点日。下标含义：0=ahead, 1=realtime, 2=power。
    原 load_days()（日前价 KPI）不动，两路口径互不污染。
    """
    conn = psycopg.connect(DB_LINK, connect_timeout=10)
    cur = conn.cursor()
    cur.execute("""SELECT price_date, hour, ahead_price, realtime_price, power_mw
                   FROM gd_home_24h ORDER BY price_date, hour""")
    byday: dict[str, dict[int, tuple[float, float, float]]] = {}
    for d, h, a, r, w in cur.fetchall():
        byday.setdefault(d, {})[h] = (float(a), float(r), float(w))
    conn.close()
    return {d: [rows[h] for h in range(24)] for d, rows in byday.items() if len(rows) == 24}


def features(days, X: date, hour: int, H: int, price_col: int = 0, power_col: int = 1) -> list[float] | None:
    """X 目标日、锚点 A = X-H。只用 ≤ A 的数据；缺失 lag 用 nan 让 LightGBM 自己分支。

    price_col=0/power_col=1（默认）：日前价 KPI 口径，配 load_days() 的 (ahead, power)。
    price_col=1/power_col=2：实时价方向分支，配 load_days_rt() 的 (ahead, realtime, power)。
    """
    A = X - timedelta(days=H)
    s = days.get(A.isoformat())
    if s is None:
        return None
    nan = float("nan")
    price = lambda d: days[d.isoformat()][hour][price_col] if d.isoformat() in days else nan
    power = lambda d: days[d.isoformat()][hour][power_col] if d.isoformat() in days else nan

    lags = [price(A - timedelta(days=k)) for k in (0, 1, 2, 7, 14)]
    # 同小时滚动统计（7/30 天，含 A）
    roll = []
    for win in (7, 30):
        seq = [price(A - timedelta(days=k)) for k in range(win)]
        arr = np.array([v for v in seq if v == v], dtype=float)
        roll += [float(arr.mean()) if arr.size else nan, float(arr.std()) if arr.size else nan]
    # 锚点日全天形态（24 点 min/max/mean）与电量
    prices_a = np.array([s[h][price_col] for h in range(24)])
    power_a = np.array([s[h][power_col] for h in range(24)])
    daystat = [prices_a.min(), prices_a.max(), prices_a.mean(),
               float(power_a.mean()), power(A)]
    # 同星期几多周同小时均值（近 4 个，≤ A）——查表 O(1)
    # 实时价分支用独立缓存，避免与日前价 DAY_CACHE/DAY_MEAN 混算。
    cache, mean_cache = (DAY_CACHE_RT, DAY_MEAN_RT) if price_col == 1 else (DAY_CACHE, DAY_MEAN)
    dows = []
    cur, k = A, 0
    while len(dows) < 4 and k < 35:
        if cur.weekday() == X.weekday():
            dc = cache.get(cur.isoformat())
            if dc is not None:
                dows.append(dc[hour])
        cur -= timedelta(days=1)
        k += 1
    dow_mean = float(np.mean(dows)) if dows else nan
    # 日级趋势：近 3 天日均 / 近 10 天日均——查表 O(1)
    short = [mean_cache.get((A - timedelta(days=k)).isoformat(), nan) for k in range(3)]
    long_ = [mean_cache.get((A - timedelta(days=k)).isoformat(), nan) for k in range(10)]
    ls, ll = np.nanmean(short), np.nanmean(long_)
    trend = float(ls / ll) if ll not in (0.0, nan) else nan
    # 形状：锚点日该小时占全天均价比例
    mean_a = prices_a.mean()
    shape = s[hour][price_col] / mean_a if mean_a else nan
    dow = X.weekday()
    cal = [hour, dow, X.month, 1.0 if dow >= 5 else 0.0,
           1.0 if X.isoformat() in HOLIDAYS else 0.0]
    return lags + roll + daystat + cal + [dow_mean, trend, shape]


def build_matrix(days, targets: list[date], H: int, price_col: int = 0, power_col: int = 1):
    Xs, ys = [], []
    have_days = []
    for X in targets:
        rows = days.get(X.isoformat())
        if rows is None:
            continue
        day_rows = 0
        for h in range(24):
            f = features(days, X, h, H, price_col=price_col, power_col=power_col)
            if f is None:
                break
            Xs.append(f)
            ys.append(rows[h][price_col])
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
    by_date = {d: {"median": np.array(results[d]["median"]),
                   "q10": np.array(results[d]["q10"]) if results[d]["q10"] else None,
                   "q90": np.array(results[d]["q90"]) if results[d]["q90"] else None}
               for d in ev_dates}
    return out, by_date


# ---------------------------------------------------------------------------
# LEAR 基线 + 验证集加权融合 + DM 检验 + 实时-日前方向准确率
# ---------------------------------------------------------------------------

def _train_eval_split(days, eval_targets, H, price_col=0, power_col=1):
    """训练窗 = eval0 之前所有天；评估窗 = eval_targets。返回 build_matrix 三件套。"""
    all_days = sorted(date.fromisoformat(d) for d in days)
    eval0 = eval_targets[0]
    train_targets = [d for d in all_days if d < eval0]
    Xtr, ytr, _ = build_matrix(days, train_targets, H, price_col, power_col)
    Xev, yev, ev_dates = build_matrix(days, eval_targets, H, price_col, power_col)
    return Xtr, ytr, Xev, yev, ev_dates


def run_lear(days, eval_targets, H):
    """LEAR/LASSO 强基线（Lago et al. 2021）。点预测 + 残差经验分位带。

    与 run_lightgbm 同特征、同训练窗、同 KPI 口径，可同台对比。
    分位带非模型原生：取训练残差 0.1/0.9 经验分位作固定偏移（已在 README 标注）。
    """
    Xtr, ytr, Xev, yev, ev_dates = _train_eval_split(days, eval_targets, H)
    per_day = np.array([np.mean([days[d.isoformat()][h][0] for h in range(24)]) for d in ev_dates])
    pipe = make_pipeline(
        SimpleImputer(strategy="median"),
        StandardScaler(),
        LassoCV(cv=TimeSeriesSplit(n_splits=3), max_iter=5000, random_state=0),
    )
    pipe.fit(Xtr, ytr)
    pred = pipe.predict(Xev)
    resid = ytr - pipe.predict(Xtr)
    off_lo, off_hi = np.quantile(resid, [0.1, 0.9])
    preds = {0.5: pred, 0.1: pred + off_lo, 0.9: pred + off_hi}
    res = {"lear": kpi(pred, yev, ev_dates, per_day)}
    res["lear"]["覆盖%"] = coverage(preds[0.1], preds[0.9], yev)
    return res, preds, yev, ev_dates, per_day


def by_date_from_preds(preds, ev_dates):
    """把 {q: 扁平数组} 转成 {date: {median,q10,q90}}，供融合按日对齐。"""
    out = {}
    med = preds.get(0.5, preds.get("0.5"))
    lo = preds.get(0.1, preds.get("0.1"))
    hi = preds.get(0.9, preds.get("0.9"))
    for i, d in enumerate(ev_dates):
        s, e = i * 24, (i + 1) * 24
        out[d] = {
            "median": np.array(med[s:e]),
            "q10": np.array(lo[s:e]) if lo is not None else None,
            "q90": np.array(hi[s:e]) if hi is not None else None,
        }
    return out


def run_fusion(model_by_date, days, valid_days):
    """验证集加权融合。权重 = 验证段各模型 MAE 逆误差归一化；测试段评估。

    model_by_date: {name: {date: {median,q10,q90}}}
    只学权重于验证段（前 valid_days 天），测试段（其余）评估，避免信息泄漏。
    """
    all_dates = sorted(d for m in model_by_date.values() for d in m)
    valid = set(all_dates[:valid_days])
    test = all_dates[valid_days:]
    if not test:
        return None
    names = list(model_by_date)
    common_valid = sorted(set.intersection(*[set(m) for m in model_by_date.values()]) & valid)

    def flatten(values):
        return np.array([v for arr in values for v in arr], dtype=float)

    # 验证段 MAE → 权重
    maes = {}
    for name in names:
        bd = model_by_date[name]
        pv = flatten([bd[d]["median"] for d in common_valid])
        av = flatten([[days[d.isoformat()][h][0] for h in range(24)] for d in common_valid])
        maes[name] = float(np.mean(np.abs(pv - av))) if len(pv) else float("inf")
    inv = {n: 1.0 / max(v, 1e-9) for n, v in maes.items()}
    tot = sum(inv.values())
    weights = {n: inv[n] / tot for n in names}

    # 测试段：各模型日期交集
    common_test = sorted(set.intersection(*[set(m) for m in model_by_date.values()]) & set(test))

    def blend(w):
        fused = {}
        for d in common_test:
            med = sum(w[n] * np.asarray(model_by_date[n][d]["median"], float) for n in names)
            lo_parts = [w[n] * np.asarray(model_by_date[n][d]["q10"], float)
                        for n in names if model_by_date[n][d].get("q10") is not None]
            hi_parts = [w[n] * np.asarray(model_by_date[n][d]["q90"], float)
                        for n in names if model_by_date[n][d].get("q90") is not None]
            fused[d] = {
                "median": med,
                "q10": sum(lo_parts) if lo_parts else None,
                "q90": sum(hi_parts) if hi_parts else None,
            }
        return fused

    fused = blend(weights)
    eq = {n: 1.0 / len(names) for n in names}
    fused_eq = blend(eq)

    def kpi_of(f):
        pred = np.concatenate([f[d]["median"] for d in common_test])
        act = np.concatenate([[days[d.isoformat()][h][0] for h in range(24)] for d in common_test])
        per = np.array([np.mean([days[d.isoformat()][h][0] for h in range(24)]) for d in common_test])
        return kpi(pred, act, common_test, per)

    res = kpi_of(fused)
    if all(fused[d]["q10"] is not None and fused[d]["q90"] is not None for d in common_test):
        q10 = np.concatenate([fused[d]["q10"] for d in common_test])
        q90 = np.concatenate([fused[d]["q90"] for d in common_test])
        act = np.concatenate([[days[d.isoformat()][h][0] for h in range(24)] for d in common_test])
        res["覆盖%"] = coverage(q10, q90, act)
    return {
        "weights": weights, "equal_weights": eq, "maes": maes,
        "kpi": res, "equal_kpi": kpi_of(fused_eq),
        "fused_by_date": fused, "valid_days": len(common_valid),
        "test_days": len(common_test), "abs_err": (
            np.abs(np.concatenate([fused[d]["median"] for d in common_test]) -
                   np.concatenate([[days[d.isoformat()][h][0] for h in range(24)] for d in common_test])),
            common_test,
        ),
    }


def dm_test(e1, e2):
    """Diebold–Mariano 检验（平方损失差的正态近似，无 HAC 修正）。e 为绝对误差数组。"""
    d = np.asarray(e1, float) ** 2 - np.asarray(e2, float) ** 2
    n = len(d)
    if n < 2:
        return {"dm": None, "p": None}
    var = d.var(ddof=1)
    if var == 0:
        return {"dm": 0.0, "p": 1.0}
    dm = float(d.mean() / np.sqrt(var / n))
    return {"dm": round(dm, 3), "p": round(float(2 * (1 - norm.cdf(abs(dm)))), 4)}


def directional_accuracy(pred_rt, actual_rt, actual_da, eps=0.5):
    """严格实时-日前价差方向准确率（用户公式）。

    D_i = sign(pred_rt_i − actual_da_i)（|差|<eps 判 0）；
    score = Σ D_i·(actual_rt_i − actual_da_i) / Σ |actual_rt_i − actual_da_i|。
    """
    pred_rt = np.asarray(pred_rt, float)
    actual_rt = np.asarray(actual_rt, float)
    actual_da = np.asarray(actual_da, float)
    spread = actual_rt - actual_da
    diff = pred_rt - actual_da
    D = np.where(diff > eps, 1, np.where(diff < -eps, -1, 0))
    denom = float(np.abs(spread).sum())
    if denom == 0:
        return float("nan")
    return float((D * spread).sum() / denom)


def predict_realtime_by_model(days_rt, eval_targets, H, kind):
    """以实时价为目标训练并预测，返回 {date: 实时价点预测数组(24)}。

    kind='lightgbm'（分位回归中位数）或 'lear'（LASSO）。复用同一套 as-of 特征，
    price_col=1/realtime, power_col=2（load_days_rt 三列结构）。
    """
    Xtr, ytr, Xev, yev, ev_dates = _train_eval_split(days_rt, eval_targets, H, price_col=1, power_col=2)
    if kind == "lightgbm":
        m = lgb.train({"objective": "regression", "verbosity": -1, "num_leaves": 63,
                       "learning_rate": 0.05, "min_data_in_leaf": 50, "num_threads": 1},
                      lgb.Dataset(Xtr, ytr), num_boost_round=500)
        pred = m.predict(Xev)
    elif kind == "lear":
        pipe = make_pipeline(SimpleImputer(strategy="median"), StandardScaler(),
                             LassoCV(cv=TimeSeriesSplit(n_splits=3), max_iter=5000, random_state=0))
        pipe.fit(Xtr, ytr)
        pred = pipe.predict(Xev)
    else:
        raise ValueError("unknown kind: " + kind)
    return {d: np.array(pred[i * 24:(i + 1) * 24]) for i, d in enumerate(ev_dates)}


def run_realtime_direction(days_rt, eval_targets, H, kinds, fusion_weights=None, skip_timesfm=True):
    """各模型（+融合）的实时-日前价差方向准确率。ref = 目标日已发布日前价。"""
    scores = {}
    rt_by_model = {}
    for kind in kinds:
        bd = predict_realtime_by_model(days_rt, eval_targets, H, kind)
        rt_by_model[kind] = bd
        preds, acts_rt, acts_da = [], [], []
        for d in sorted(bd):
            if d.isoformat() not in days_rt:
                continue
            preds.extend(bd[d])
            acts_rt.extend([days_rt[d.isoformat()][h][1] for h in range(24)])
            acts_da.extend([days_rt[d.isoformat()][h][0] for h in range(24)])
        scores[kind] = directional_accuracy(preds, acts_rt, acts_da) if preds else float("nan")

    # 融合方向分：用日 KPI 学出的权重融合各模型实时价预测
    if fusion_weights and len(kinds) > 1:
        common = set.intersection(*[set(rt_by_model[k]) for k in kinds])
        preds, acts_rt, acts_da = [], [], []
        for d in sorted(common):
            if d.isoformat() not in days_rt:
                continue
            fused_rt = sum(fusion_weights.get(k, 0) * rt_by_model[k][d] for k in kinds
                           if k in fusion_weights)
            preds.extend(fused_rt)
            acts_rt.extend([days_rt[d.isoformat()][h][1] for h in range(24)])
            acts_da.extend([days_rt[d.isoformat()][h][0] for h in range(24)])
        if preds:
            scores["fusion"] = directional_accuracy(preds, acts_rt, acts_da)
    return scores


def _fmt_kpi(v):
    cov = f" 覆盖={v['覆盖%']}%" if "覆盖%" in v else ""
    return f"MAE={v['MAE']:7.1f} RMSE={v['RMSE']:7.1f} MAPE={v['MAPE%']:6.2f}%{cov}"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--eval-days", type=int, default=90)
    ap.add_argument("--horizons", default="1,8")
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--skip-timesfm", action="store_true")
    ap.add_argument("--skip-lear", action="store_true")
    ap.add_argument("--skip-fusion", action="store_true")
    ap.add_argument("--valid-days", type=int, default=30,
                    help="融合权重学习用的验证段天数（评估窗前 N 天），其余为测试段")
    ap.add_argument("--export-report", default=None, help="导出 JSON 报告路径")
    ap.add_argument("--skip-direction-timesfm", action="store_true",
                    help="方向准确率分支不跑 TimesFM（少打很多 HTTP；默认跑）")
    args = ap.parse_args()

    days = load_days()
    build_caches(days)
    need_rt = not (args.skip_lear and args.skip_fusion)  # 方向分支始终需要
    days_rt = load_days_rt() if need_rt else None
    if days_rt is not None:
        build_caches_rt(days_rt)
    all_days = sorted(date.fromisoformat(d) for d in days)
    eval_targets = all_days[-args.eval_days:]
    print(f"完整日 {len(all_days)}（{all_days[0]} ~ {all_days[-1]}），评估窗口 "
          f"{eval_targets[0]} ~ {eval_targets[-1]} 共 {len(eval_targets)} 天")

    report = {"eval_days": args.eval_days, "valid_days": args.valid_days,
              "horizons": {}, "note": "方向准确率为严格实时-日前口径，与日前价KPI分开呈现"}

    for H in [int(x) for x in args.horizons.split(",")]:
        print(f"\n=== H={H}（锚点 = 目标日前 {H} 天）===")
        hrep = {"day_kpi": {}, "weights": {}, "dm": {}, "direction": {}}
        report["horizons"][str(H)] = hrep

        # ---- 日前价 KPI（分区一）----
        print("  --- 日前价预测精度 KPI ---")
        naives, _, _, _ = run_naives(days, eval_targets, H)
        for k, v in naives.items():
            print(f"  {k:14s} {_fmt_kpi(v)}")
            hrep["day_kpi"][k] = v
        lgb_res, lgb_preds, lgb_yev, lgb_dates, lgb_per = run_lightgbm(days, eval_targets, H)
        for k, v in lgb_res.items():
            print(f"  {k:14s} {_fmt_kpi(v)}")
            hrep["day_kpi"][k] = v
        tf_res, tf_by_date = None, None
        if not args.skip_timesfm:
            tf_res, tf_by_date = run_timesfm(days, eval_targets, H, args.workers)
            for k, v in tf_res.items():
                print(f"  {k:14s} {_fmt_kpi(v)}")
                hrep["day_kpi"][k] = v

        model_by_date = {"lightgbm": by_date_from_preds(lgb_preds, lgb_dates)}
        if tf_by_date is not None:
            model_by_date["timesfm-3.0"] = tf_by_date

        if not args.skip_lear:
            ler_res, ler_preds, ler_yev, ler_dates, ler_per = run_lear(days, eval_targets, H)
            for k, v in ler_res.items():
                print(f"  {k:14s} {_fmt_kpi(v)}")
                hrep["day_kpi"][k] = v
            model_by_date["lear"] = by_date_from_preds(ler_preds, ler_dates)

        fusion = None
        if not args.skip_fusion and len(model_by_date) >= 2:
            fusion = run_fusion(model_by_date, days, args.valid_days)
            if fusion:
                print(f"  fusion        {_fmt_kpi(fusion['kpi'])}  (验证 {fusion['valid_days']}天/测试 {fusion['test_days']}天)")
                print(f"  等权对照      {_fmt_kpi(fusion['equal_kpi'])}")
                print(f"  融合权重: " + ", ".join(f"{n}={w:.3f}" for n, w in fusion["weights"].items()))
                hrep["day_kpi"]["fusion"] = fusion["kpi"]
                hrep["day_kpi"]["fusion_equal"] = fusion["equal_kpi"]
                hrep["weights"] = fusion["weights"]
                hrep["maes"] = fusion["maes"]

                # DM 检验：融合 vs 单模型中验证 MAE 最低者（测试段）
                best = min(fusion["maes"], key=fusion["maes"].get)
                best_by_date = model_by_date[best]
                common = fusion["abs_err"][1]
                e2 = np.concatenate([
                    np.abs(best_by_date[d]["median"] - np.array(
                        [days[d.isoformat()][hh][0] for hh in range(24)]))
                    for d in common
                ])
                dm = dm_test(fusion["abs_err"][0], e2)
                hrep["dm"] = {"vs": best, **dm}
                print(f"  DM 检验(融合 vs {best}): DM={dm['dm']} p={dm['p']}")

        # ---- 实时-日前价差方向准确率（分区二）----
        if days_rt is not None:
            print("  --- 实时-日前价差方向准确率（ref=目标日日前价）---")
            kinds = []
            if not args.skip_lear:
                kinds.append("lear")
            kinds.append("lightgbm")
            # 注：方向分支的实时价预测目前只实现 lightgbm/lear（predict_realtime_by_model
            # 不支持 timesfm），故不把 timesfm 加入 kinds，避免 ValueError: unknown kind。
            # --skip-direction-timesfm 保留为兼容开关（现已默认不跑 timesfm 方向分）。
            fuse_w = fusion["weights"] if fusion else None
            if fuse_w is not None:
                # 权重键与方向分支 kind 对齐（方向分支只用非-timesfm 模型时键一致）
                fuse_w = {k: v for k, v in fuse_w.items() if k in kinds}
                tot = sum(fuse_w.values()) if fuse_w else 0
                fuse_w = {k: v / tot for k, v in fuse_w.items()} if tot else None
            scores = run_realtime_direction(days_rt, eval_targets, H, kinds,
                                            fusion_weights=fuse_w)
            for k, v in scores.items():
                print(f"  {k:14s} 方向准确率={v:+.3f}")
                hrep["direction"][k] = v

    print("\n注：lightgbm/lear/naive 特征只用 ≤ 锚点数据；timesfm 输入锚点及以前全部点（4096 点内）。")
    print("    方向准确率为严格实时-日前口径，与日前价 KPI 分开呈现，不混算。")
    if args.export_report:
        with open(args.export_report, "w", encoding="utf-8") as f:
            json.dump(report, f, ensure_ascii=False, indent=2, default=str)
        print(f"报告已导出: {args.export_report}")


if __name__ == "__main__":
    sys.exit(main())
