# 小时电价预测算法离线对比（hour-price-benchmark）

选优工具，不碰线上：用 as-of 重放在历史数据上对比多种预测算法，只读 `gd_home_24h`，
不写库、不改 Go 生产代码。

## 对比对象

日前价 KPI（同台可比，共用 `kpi()` / `coverage()`）：
- `naive_d1` / `naive_weekly`：同小时、锚点日 / 最近同星期几价格直接抄。
- `lightgbm`：分位回归（0.1/0.5/0.9），特征 = 滞后价 / 滚动统计 / 日形态 / 电量 / 日历。
- `timesfm-3.0`：直打 `FORECAST_URL`，输入锚点及以前全部点（服务端 4096 点截断），滚动取最后 24 点。
- `lear`：LASSO Estimated AutoRegression（Lago et al. 2021），复用同一套特征 + 中位数填 nan + 标准化 + LassoCV。
- `fusion`：验证段学权重的加权融合（见下）+ 等权对照。

实时-日前价差方向准确率（独立口径，与日前价 KPI 分开呈现，见下）。

## 用法

```bash
PY=/usr/local/python3/bin/python3.12

# 快速（不调 TimesFM）：naive / lightgbm / lear / fusion + 方向分支
$PY tools/hour-price-benchmark/benchmark.py --eval-days 90 --skip-timesfm \
  --export-report /tmp/hour-bench-report.json

# 完整（含 TimesFM，两口径 H=1/H=8）
$PY tools/hour-price-benchmark/benchmark.py --eval-days 90 --horizons 1,8 --workers 4

# 回到改动前的原生行为（不加 LEAR / 融合）
$PY tools/hour-price-benchmark/benchmark.py --eval-days 90 --skip-timesfm --skip-lear --skip-fusion
```

环境：依赖 numpy / lightgbm / scipy / psycopg / scikit-learn。
DB 默认 `postgresql://test:test123456@10.193.0.10:5432/ems-test`，可用环境变量 `EMS_DB_LINK` 覆盖。
TimesFM 地址用 `FORECAST_URL` 覆盖。

参数：`--eval-days`（评估窗天数，默认 90）、`--horizons`（默认 `1,8`）、
`--workers`（TimesFM 并发，默认 4）、`--skip-timesfm` / `--skip-lear` / `--skip-fusion`、
`--valid-days`（融合验证段天数，默认 30）、`--export-report PATH`、
`--skip-direction-timesfm`（方向分支不跑 TimesFM，省 HTTP）。

## 口径

- 锚点 A = 目标日 X − H。所有特征**只用 ≤ A 的数据**（as-of，无未来泄漏）。
- H=1：数据齐口径（与页面统计一致）；H=8：生产口径（官网 T-6 滞后 → 目标日距锚点约 8 天）。
- 日前价 KPI：MAE（元/MWh）、RMSE、MAPE（按日 = 当日平均绝对误差 ÷ 当日实际均价）、
  q10-q90 覆盖率。
- `gd_home_24h` 下标：本工具 `load_days()` 用 `(ahead_price, power_mw)`；
  实时价分支 `load_days_rt()` 用 `(ahead_price, realtime_price, power_mw)`。

## 融合（`run_fusion`）

- 评估窗切两段：前 `--valid-days` 天为验证段（只在这里学权重），其余为测试段（评估）。
- 权重 = 各模型验证段 MAE 的逆误差归一化（`w_i = (1/MAE_i) / Σ(1/MAE_j)`）；
  同时打印等权平均作对照。日期按各模型交集对齐。
- 点预测与分位带均按权重线性组合。DM 检验（Diebold–Mariano，平方损失差正态近似，
  **无 HAC 修正**）比较融合 vs 验证 MAE 最低的单模型，报告 DM 统计量与 p 值。

## 实时-日前价差方向准确率（严格口径，独立呈现）

- 预测目标是**实时价**，参照物是**目标日已发布日前价**（不是被预测对象）：
  - `D_i = sign(pred_rt_i − actual_da_i)`（`|pred_rt−da| < eps=0.5 元` 判 0）；
  - `score = Σ D_i·(actual_rt_i − actual_da_i) / Σ |actual_rt_i − actual_da_i|`，范围 `[-1,1]`。
- 各模型（LEAR / lightgbm / timesfm）分别报告一行，融合模型用日 KPI 学出的权重融合
  `pred_rt` 后再算一行。
- 语义：衡量「能否预见实时价相对日前价的偏离方向，且在大价差时段判对」。与 MAE 等
  数值精度指标**不可换算、不混算**——日前价预测最准的模型，方向判断可能全错。

## 三处口径差异提示（避免误读）

1. **LEAR 的分位带不是模型原生**：LEAR 本体只给点预测，q10/q90 是训练残差的经验分位数
   作固定偏移。覆盖率可与 LightGBM 对照参考，但统计含义弱于 TimesFM/GBM 的原生分位带。
2. **LEAR 每次预测都要重新 fit LASSO**（截至锚点的历史重训），与「加载即用」的
   TimesFM / GBM 服务不同。本次仅在离线工具验证；生产化需另立服务形态。
3. **方向准确率是独立口径**：与日前价 KPI 分开呈现，不进融合权重学习，不混成单一「准确率」。

## 数据核查（2026-11 实取）

`gd_home_24h` 共 26688 行（2023-09-02 ~ 2026-11-15），ahead/realtime/power 全量齐全。
近一年 9048 点实时-日前价差：中位 −8.9、标准差 137，41% 时段实时高于日前 —— 方向有信号、
非一边倒，适合作为评估目标。

## 风险与局限

- 验证/测试切分对 `valid_days` 敏感；报告同时给等权对照，多 `valid_days` 敏感性可后续补。
- 实时价波动大（std 137），严格方向分可能偏低，这是真实业务难度，不放宽口径美化。
- TimesFM HTTP 串行受服务端并发 2 限制；方向分支可用 `--skip-direction-timesfm` 跳过以加速。
