# 设计：小时电价预测 LEAR 基线 + 验证集加权融合（离线验证阶段）

日期：2026-11-22
状态：待用户 review

## 1. 背景

`tools/hour-price-benchmark/benchmark.py` 已能 as-of 重放对比
naive_d1 / naive_weekly / timesfm-3.0 / lightgbm，指标 MAE/RMSE/MAPE + q10-q90 覆盖率。
生产侧（Go）另有 price-gbm 服务 + TimesFM + 简单均值融合 + 留痕回测体系。

缺口：
1. **LEAR/LASSO 电价强基线**缺失。Lago et al. 2021（Applied Energy）证明规范统计模型
   常不输复杂深度学习，是电价预测必须有的对照。
2. 融合仅为生产侧「两者点预测简单均值」，**无体系化加权、无显著性检验**。
3. 无「实时-日前价差方向准确率」指标（业务核心，README 已示「抄日前价」能赢 TimesFM）。

本设计所有工作**先在离线工具完成验证**，不动 Go 生产代码/数据库/外部服务；生产化另行立项。

## 2. 目标与非目标

目标：
1. benchmark.py 新增 LEAR/LASSO 基线（第三强基线）。
2. 新增验证集加权融合：留出验证窗按近期误差学权重，点预测与分位带均加权，按 H 分别定权。
3. 新增评估：Diebold–Mariano 显著性检验 + 严格的价差方向准确率（实时-日前，ref=目标日日前价）。
4. 产出验证报告，为生产化决策提供依据。

非目标：
- 不改 Go 生产代码 / algo_review / 数据库留痕 / 外部服务。
- 不做 stacking 等非线性元学习器。
- 不改 H=1 / H=8 口径与 as-of 特征（只用 ≤ 锚点数据）。
- 不改变现有日前价 KPI（MAE/RMSE/MAPE/覆盖率）的定义。

## 3. 数据与口径

`gd_home_24h`：2023-09-02 ~ 2026-11-15，26688 行，ahead_price / realtime_price / power_mw 全量齐全。
近一年 9048 点统计：实时-日前价差 中位 -8.9、标准差 137，41% 时段实时高于日前
——方向有信号、非一边倒，适合作为评估目标。

新增 `load_days_rt()`：返回 `{date: [(ahead_price, realtime_price, power_mw) ×24]}`。
保留原 `load_days()` 不动，日前价 KPI 口径不受影响。

## 4. 新增算法：LEAR

- LEAR = LASSO Estimated AutoRegression（Lago et al. 2021）。
- 复用 `features()` 产出的特征矩阵（滞后价/滚动统计/日形态/日历/星期/趋势/形状）。
- sklearn 适配：① `nan` 用同小时中位数/前向值填充（LASSO 不接受 nan）；
  ② `StandardScaler` 标准化（LASSO 对尺度敏感）。
- 训练窗口与 lightgbm 一致（eval0 之前所有天），`LassoCV`（时间交叉验证选 alpha）。
- 分位带：LEAR 只给点预测，用**验证子集残差的经验 0.1/0.9 分位数**作偏移，以计入覆盖率。
- 输出结构与 `run_lightgbm` 同构，直接进 KPI/覆盖率口径。

## 5. 验证集加权融合

- eval_targets 切分为：验证段（前 `valid_days` 天，默认 30）+ 测试段（其余）。
  权重只在验证段学，测试段评估，避免用全评估窗学权重造成信息泄漏。
- 权重 = 各模型验证段 MAE 的**逆误差归一化**（稳健、可解释）；同时打印**等权平均**对照。
- 点预测 `median = Σ wᵢ·medianᵢ`；分位带 `q10 = Σ wᵢ·q10ᵢ`、`q90 = Σ wᵢ·q90ᵢ`。
- 各模型评估日期可能不齐 → 对齐到公共日期交集后再融合。
- 输出融合 KPI + 权重明细；按 H 分别定权、分别报告。

## 6. 评估增强

- `dm_test(e1, e2)`：Diebold–Mariano 检验，比较融合 vs 最优单模型损失差是否显著，
  返回 DM 统计量与 p 值（正态近似）。
- `directional_accuracy(pred_rt, actual_rt, actual_da)`：严格复现用户公式——
  `D_i = sign(pred_rt_i − actual_da_i)`（目标日日前价为 ref），
  `score = Σ D_i·(actual_rt_i − actual_da_i) / Σ |actual_rt_i − actual_da_i|`。
- 实时价预测分支：新增 `run_realtime_direction()`，对 eval_targets 各模型（LEAR / lightgbm /
  timesfm，按 `--skip-*` 开关取可用者）以**实时价为目标**、复用同一套 `features()` 重新训练，
  产出目标日实时价点预测 `pred_rt`；再按验证段权重融合出融合模型的 `pred_rt`。
  **方向准确率按模型分别报告**（LEAR / lightgbm / timesfm / 融合各一行），以便横向比较谁更能判断
  实时-日前价差方向——这是与「多模型对比」目标一致的解法。
  日前价 KPI 与实时方向准确率分开呈现，不混算。
  阈值 `D=0`（pred 与 da 相等）的判定：用 `|pred−da| < eps`（eps 默认 0.5 元）判 0。

## 7. 改动文件

- `tools/hour-price-benchmark/benchmark.py`（主要）：新增 `load_days_rt` / `run_lear` /
  `run_fusion` / `dm_test` / `directional_accuracy` / `run_realtime_direction`；
  扩展 main 编排与 argparse：`--skip-lear` / `--skip-fusion` / `--valid-days` / `--export-report`。
- 新增 `tools/hour-price-benchmark/README.md`：记录用法、口径、指标语义（方向准确率为严格实时-日前口径）。

## 8. 验证方式

```bash
# 快速（不调 TimesFM，验证 naive/lgb/lear/fusion 链路）
python3 tools/hour-price-benchmark/benchmark.py --eval-days 90 --skip-timesfm \
  --export-report /tmp/hour-bench-report.json

# 完整（含 TimesFM，两口径）
python3 tools/hour-price-benchmark/benchmark.py --eval-days 90 --horizons 1,8 --workers 4
```

验收：无脚本异常；报告含各算法 KPI、融合权重、DM 检验、方向准确率；
LEAR 与 lightgbm/timesfm 同台可比；权重仅在验证段学习。

## 9. 风险与回滚

- 风险：验证段/测试段切分对权重敏感（不同 valid_days 结果可能不同）→ 报告同时给等权对照与多
  valid_days 敏感性可后续补。
- 风险：实时价预测方向难、值域波动大（std 137）→ 严格指标可能偏低，但这是真实业务难度，
  不通过放宽口径美化。
- 风险：LassoCV 时间开销（每日逐点预测）→ 用训练矩阵一次性 fit + predict，避免逐点训练。
- 回滚：新增函数与 argparse 开关均为增量；`--skip-lear --skip-fusion` 即回到原行为。
