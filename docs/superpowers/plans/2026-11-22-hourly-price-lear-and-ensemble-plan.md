# 实现计划：小时电价 LEAR 基线 + 验证集加权融合

日期：2026-11-22
对应 spec：`docs/superpowers/specs/2026-11-22-hourly-price-lear-and-ensemble-design.md`

## 环境与前提（已验证）

- Python：`/usr/local/python3/bin/python3.12`（3.12.3）
- 依赖已齐：numpy 2.2.3、lightgbm 4.7.0、scipy 1.18.0、psycopg、scikit-learn 1.9.1（本次新装）
- DB：`postgresql://test:test123456@10.193.0.10:5432/ems-test`，表 `gd_home_24h` 有
  ahead_price / realtime_price / power_mw，2023-09-02 ~ 2026-11-15 全量齐全。

## 改动范围

仅 `tools/hour-price-benchmark/benchmark.py`（+ 新增同目录 README.md）。不动 Go 生产代码、
不动数据库、不动外部服务。

## 实施步骤

### Step 1 — 数据加载扩展

1. 新增 `load_days_rt()`：SQL 改 `SELECT price_date, hour, ahead_price, realtime_price, power_mw`，
   返回 `{date: [(ahead_price, realtime_price, power_mw) ×24 按 hour 升序]}`，只留完整 24 点日。
2. 保留 `load_days()` 原样（日前价 KPI 口径不受影响）。
3. 为兼容：把现有 `features/build_matrix` 里对 `days[d][h][0]` 的取值保持不变
   （rt 元组多一列不影响下标 0/1 的语义；`load_days_rt` 单独供实时价训练用）。
   - 注意 `features()` 里的 `power=lambda d: days[d][h][1]` 在 rt 结构下取到的是 realtime_price，
     会破坏特征语义。**处理方式**：实时价训练时不用 `features()`，而是用新的
     `build_matrix_rt()`（Step 4）；`features()` 保持只配 `load_days()` 的旧结构。

### Step 2 — LEAR 基线 `run_lear(days, eval_targets, H)`

1. 复用 `features()` 特征矩阵（与 lightgbm 同特征）。
2. sklearn 管道：`SimpleImputer(strategy="median")` → `StandardScaler()` → `LassoCV(cv=TimeSeriesSplit 或 5, max_iter=...)`。
   - nan 填充：sklearn `SimpleImputer` 按列中位数；LASSO 不接受 nan。
   - 训练窗 = eval0 之前所有天，与 `run_lightgbm` 一致（`build_matrix`）。
3. 点预测：`pipe.predict(Xev)`。
4. 分位带：取训练矩阵 `ytr` 上模型残差（用训练集自身预测，避免额外切分）的
   0.1 / 0.9 经验分位数作固定偏移 `q10=pred+off_lo, q90=pred+off_hi`（off_lo 通常为负）。
5. KPI：`kpi(pred, yev, ev_dates, per_day)` + `coverage(q10,q90,yev)`。
6. 返回同 `run_lightgbm` 的 `(res, preds, yev, ev_dates, per_day)` 风格，preds 用键
   `0.1/0.5/0.9`，供融合使用。

### Step 3 — 验证集加权融合 `run_fusion(model_preds, model_kpis, eval_dates, ...)`

输入：每个可用模型的 `{date: {median,q10,q90}}`、各自验证段 MAE、验证段/测试段日期切分。

1. `valid_days`（默认 30）切分：验证段 = 前 N 天，测试段 = 其余。
2. 权重：`w_i = (1/MAE_i) / Σ(1/MAE_j)`（MAE 来自验证段）；同时算等权 `1/n`。
3. 日期对齐：`common = ∩ 各模型日期`（交集），按 date 排序。
4. 融合：`median = Σ w_i·median_i`，`q10 = Σ w_i·q10_i`，`q90 = Σ w_i·q90_i`（逐点）。
5. KPI 用 `kpi()`，在测试段上评估；返回融合 KPI + 权重表（每个模型名称→权重）。
6. 等权对照同样算一套 KPI。

### Step 4 — 实时价方向分支 `run_realtime_direction(days_rt, eval_targets, H, models)`

1. 新增 `features_rt()`：与 `features()` 结构一致但值取 realtime_price（下标 1）。
   最简做法——**复制 `features()` 函数体，把 `price`/`power` lambda 的下标对调**
   （目标从日前价改为实时价，同时保留日前价作为外生通道可选；spec 未要求外生通道，
   先保持纯实时价单列，避免与现有特征语义混淆）。
   更稳妥做法：把 `features()` 重构为 `features(days, X, hour, H, price_col=0, power_col=1)`，
   默认参数保持旧行为；`run_realtime_direction` 调用 `price_col=1`。**采用重构方案**（兼容且单一真相源）。
2. 对每个可用模型（lear / lightgbm / timesfm），以实时价为目标、用同一套训练窗与超参重新训练/
   推理，产出 `pred_rt[X][h]`（与日前价分支平行，互不覆盖）。
   - timesfm 实时价：复用 `timesfm_one()` 思路，但输入序列取 realtime_price 列；为避免重复
     大量 HTTP，若 `--skip-timesfm` 或方向分支显式跳过则不做（argparse 加 `--skip-direction-timesfm`）。
3. 对每个模型算方向分：`directional_accuracy(pred_rt, actual_rt, actual_da)`。
4. 融合模型方向分：用 Step 3 学出的权重融合 `pred_rt` 后再算。

### Step 5 — 评估增强函数

1. `dm_test(e1, e2)`：Diebold–Mariano 统计量。
   - `d_t = e1^2 - e2^2`（平方损失差），`dm = mean(d)/sqrt(var(d)/n)`（无 hAC 修正的正态近似，
     spec 允许），p 值 = `2*(1-Φ(|dm|))`，scipy.stats.norm。
   - 比较：融合 vs 最优单模型（测试段）。
2. `directional_accuracy(pred_rt, actual_rt, actual_da, eps=0.5)`：
   - `spread = actual_rt - actual_da`
   - `D = where(pred_rt - actual_da > eps, 1, where(pred_rt - actual_da < -eps, -1, 0))`
   - `score = Σ(D*spread) / Σ|spread|`（分母为 0 时返回 nan）
   - 注意：分母按全部时刻；对 `D=0` 时刻贡献 0，与公式一致。

### Step 6 — main 编排与 argparse

1. 新增开关：`--skip-lear`、`--skip-fusion`、`--valid-days`(默认30)、
   `--export-report PATH`、`--skip-direction-timesfm`。
2. main 循环每个 H：
   - 跑 naives / lightgbm / timesfm（原逻辑）
   - 若未 skip：`run_lear` → 打印 KPI
   - 若未 skip fusion：收集各模型 preds + KPI → `run_fusion` → 打印 KPI + 权重
   - 方向分支：按可用模型产出 pred_rt → 各模型方向分 + 融合方向分；DM 检验
   - 所有指标汇总进 report dict，`--export-report` 时 json.dump（含参数、H、各 KPI、权重、
     DM 结果、方向分）
3. 打印分区标题：`=== 日前价 KPI ===` 与 `=== 实时-日前价差方向准确率 ===`，明确分开呈现。

### Step 7 — README.md

新建 `tools/hour-price-benchmark/README.md`，记录：
- 用法（含新命令）
- 指标语义与口径：日前价 KPI / 覆盖率 / DM / 方向准确率（严格实时-日前）
- **三处口径差异提示**（LEAR 残差分位 vs 原生分位、每次重 fit、方向准确率独立口径）
- 数据来源与 as-of 约束

## 验证（完成定义）

```bash
PY=/usr/local/python3/bin/python3.12
# 1) 快速链路（不调 TimesFM）：naive/lgb/lear/fusion + 方向分支
$PY tools/hour-price-benchmark/benchmark.py --eval-days 90 --skip-timesfm \
  --export-report /tmp/hour-bench-report.json

# 2) 语法/静态
$PY -m py_compile tools/hour-price-benchmark/benchmark.py
```

验收标准：
- 脚本无异常；report.json 含各算法 KPI、融合权重、DM 结果、各模型方向分。
- LEAR 与 lightgbm 同台可比；权重只在验证段学（用断言或代码审查确认）。
- `--skip-lear --skip-fusion` 回到原行为（与改动前输出一致）。
- 可选完整跑（含 TimesFM）：`--eval-days 90 --horizons 1,8 --workers 4`（耗时较长，服务端并发2）。

## 风险与应对

- LassoCV 在 ~数千行×几十特征上拟合，单次秒级；一次性 fit+predict，不逐点训练。
- 方向分支需实时价作为目标 → `load_days_rt` + `features(price_col=1)`；timesfm 实时分支 HTTP 多，
  提供 `--skip-direction-timesfm` 以便快速验证，默认仍跑以完整对比。
- 日期交集对齐后样本减少 → 报告里打印各模型覆盖天数，避免样本不一致误读。
- DM 为无 HAC 的正态近似（spec 允许），README 注明局限。
