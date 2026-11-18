# 96 点电价离线回测（price-backtest）

一条命令复现「TimesFM / 持久化 / 抄日前价」三行 MAE·WAPE。**不依赖 Go 编译，不起服务**，只读 `gd_metric.csv` + 可选调用预测服务。

## 一条重跑命令（仓库根目录，`--csv` 可省）

```bash
# 实时电价，24 天窗口（2026-09-08 ~ 10-01），上下文 24 天
python3 tools/price-backtest/backtest.py \
    --market real --from 2026-09-08 --to 2026-10-01 --history-days 24

# 日前电价，30 天窗口，上下文 30 天
python3 tools/price-backtest/backtest.py \
    --market dayAhead --from 2026-09-02 --to 2026-10-01 --history-days 30

# 只算基线，不调预测服务（2 秒出数，用于口径自检）
python3 tools/price-backtest/backtest.py \
    --market real --from 2026-09-08 --to 2026-10-01 --history-days 24 --no-timesfm

# 断点续跑 / 省重复推理（多变量一次约 5s，24 天约 2.5 分钟）
... --cache-dir /tmp/bt-cache
```

`--csv` 解析顺序：**显式 `--csv` > 环境变量 `GD_METRIC_CSV` > 自动探测**（先当前目录、后仓库根；候选 `out/gd_metric.csv`、`OpenLEMS/application/internal/logic/gdmetric/out/gd_metric.csv`），都没命中才报错并列出候选。当前仓库命中的是后一个（35.7MB）。预测服务地址不内置，用 `--forecast-url` 或 `FORECAST_URL` 指定；只算基线时加 `--no-timesfm`。

参数速查：`--from/--to` 目标日闭区间、`--history-days` 上下文天数、`--market dayAhead|real|load`、`--no-timesfm`、`--cache-dir`、`--limit N`（调试取前 N 天）、`--export-quantiles PATH` + `--quantile-level`（默认 0.9）。

## 导出分位序列（给需量控制回测当 fixture）

```bash
# 2026-09 逐日 96 点负荷预测 q0.9，输出 timestamp,value
python3 tools/price-backtest/backtest.py --market load \
    --from 2026-09-01 --to 2026-09-30 --history-days 30 \
    --cache-dir /tmp/bt-cache-q9 \
    --export-quantiles tools/price-backtest/fixtures/load_q09_2026-09.csv --quantile-level 0.9
```

已生成：`tools/price-backtest/fixtures/load_q09_2026-09.csv`（2880 行 = 30 天 × 96 点，2026-09-01 00:15 ~ 2026-10-01 00:00，92.8KB）。

```
timestamp,value
2026-09-01T00:15:00,114267.3516
2026-09-01T00:30:00,126182.2969
2026-09-01T00:45:00,124780.9531
```

口径与限制：

- 时间戳是 ISO（`D 00:15 … D+1 00:00`），要跟 `gd_metric.csv` 的 `日期/时间` 对行时，把 `24:00` 那点映射成次日 `00:00`，与脚本内部口径一致。
- 单变量（`typeId=23` 负荷）30 天上下文，不是微网 `ForecastedGridPower` 本身；相位差在 1 小时内的需量判定上够用，真要接真机得再用微网侧序列重算。
- 服务端分位来自 TimesFM 的 0.1~0.9 输出，未做校准：2026-09 实测 **actual ≤ q0.9 覆盖率 86.0%**（2477/2880），漏报中位超出 3474、最大 13585（与 90% 名义值差 4 点，偏乐观方向，改 P2 前要知道）。

## 预测服务版本（基线可比性）

`GET $FORECAST_URL/version`（2026-11-12 实取）：

```json
{"name":"modelman-forecast","version":"0.1.0","git_commit":"de7e751","build_time":"2026-09-30T12:36:32Z",
 "tier":"timesfm-3.0","device":"cpu","infer_threads":4,"max_context":4096,"max_horizon":512}
```

`/health` 同时报 `models_loaded:["timesfm-3.0"]`、`uptime_secs:220098`（约 2.5 天）。即**线上服务 2026-09-30 之后没有重启/换权重**，下面两组基线就是它算的，同源可比。


## 口径（与线上一致，改动要同步改这里和 `price_intraday.go`）

| 项 | 口径 |
|---|---|
| 数据 | `gd_metric.csv`，typeId 21=日前 22=实时 23=负荷；**只用 sourceId=9（实际值）**，999（电查查预测占位）剔除，0 视为未知来源也剔除 |
| 目标日 96 点 | D 00:15 … D+1 00:00（CSV 当天最后一点写作 `24:00`，归到次日 00:00） |
| 上下文 | 目标日之前最近 `history-days` 天的实际值；尾部截断到 4096 点（TimesFM `max_context`） |
| 通道 | `real`：`[实时价, 日前价, 系统负荷]`（对应 `priceCovariateKinds["real"]`），任一通道缺值的时刻整点丢弃；`dayAhead`/`load`：单变量 |
| 基线 | **持久化** = 前一天同一时刻实时价；**抄日前价** = 目标日已发布的日前电价，直接当实时价（仅 `real` 有） |
| 指标 | MAE = Σ\|pred−actual\|/n（元/MWh）；WAPE = Σ\|pred−actual\|/Σ\|actual\| |
| 剔除 | 实际值不足 96 点的目标日整日跳过；预测失败的日进 `skipped` 打印，不静默丢 |

## 实测（2026-11-12，`gd_metric.csv` 2021-05~2026-10，sourceId=9）

实时电价，24 天窗口 2026-09-08~10-01，上下文 24 天：

| 方法 | MAE 元/MWh | WAPE |
|---|---|---|
| TimesFM 三通道 | 92.0 | 17.8% |
| 持久化（昨日同时刻） | 114.3 | 22.2% |
| 抄日前价 | **80.3** | **15.6%** |

日前电价，30 天窗口 2026-09-02~10-01，上下文 30 天：

| 方法 | MAE 元/MWh | WAPE |
|---|---|---|
| TimesFM 单变量 | 51.0 | 10.9% |
| 持久化（昨日同时刻） | 55.0 | 11.8% |

## 与最初群里那张表的差异（诚实说明）

- WAPE 可复现：实时 24 天 17.8%（原 17.7%）、抄日前价 15.6%（原 15.6%）、30 天窗口抄日前价 17.2%（原 17.1%）。
- MAE 有 3~13% 差异：最初那版是按日 MAE 再平均，本脚本是池化 Σ\|e\|/n，两者不等价。**结论不变**：TimesFM 胜持久化 7%~20%，输给抄日前价 9%~19%。
- 日前 30 天窗口 MAE 51.0 与最初 102.2 差一倍，是**窗口不同**（最初那版窗口含更多尖峰日）。要看原始窗口请给 `--from/--to`，不要跨窗口比绝对值。

## 已知限制

- 单进程串行调预测服务（服务端并发 2），`real` 3 通道一次约 5s、单变量约 8s（长上下文），`--cache-dir` 可续跑。
- 单位是元/MWh；`--market load` 是负荷口径，与价格不可比。
- 未覆盖概率指标（pinball loss / 覆盖率）；分位校准验收要等阶段二，届时在 `wape_stats` 旁边加。
