# 数据源矩阵：官方直采 vs 第三方 CSV

> 2026-11-13 定案。用户规则（硬性）：
> **官方覆盖的能力一律读官方，CSV 路径删掉，不做 fallback；官方没覆盖的能力要明说用不了，
> 不许拿 CSV 假装还在工作。**

## 0. 一句话结论

- **日频市场数据：已全切官方直采**（官方披露接口 → `gd_spot_metric`，`origin=pm-api`）。
- **96 点（15 分钟）分时：官方拿不到。** 匿名访问门户的四个 96 点节点全部返回 0 条；
  要拿只能靠**市场成员账号**在交易平台导出。匿名不可得，且不要尝试绕过鉴权。
- 第三方（电查查）96 点仍在更新（T-7 左右），但**没有免登录、没有自动采集**：
  抓取要 10~15 分钟寿命的凭证。是否恢复由用户拍板，代码里已保留开关与恢复路径。

## 1. 能力 × 改造前源 × 改造后源

| # | 能力 | 改造前（源） | 改造后（源） | 官方字段 / 接口 | 状态 |
|---|---|---|---|---|---|
| 1 | 日均成交价（发电侧日前/实时） | 第三方 CSV（`gd_metric.csv`，sourceId=9/999） | `gd_spot_metric`（`origin=pm-api`） | `generationSideDailyAvgPrice` / `generationSideRealTimeAvgPrice` | ✅ 已切 |
| 2 | 日最高 / 最低电价 | 同上 | 同上 | `generationSideDailyMaxPrice` / `generationSideDailyMinPrice` | ✅ 已切 |
| 3 | 日前申报均价（供给侧） | 同上 | 同上 | `dailyDeclarationAvgPrice` | ✅ 已切 |
| 4 | 日前 / 实时成交量 | 同上 | 同上 | `generationSideDailyTotalVolume` / `consumerSideDailyTotalVolume` / `generationSideRealTimeTotalVolume` | ✅ 已切 |
| 5 | 峰谷价差 / 环比 / 同比 | 页面自算 | `spotmarket.LoadBoard`（后端算好） | 派生字段 `peak/valley/spread/momPct/yoyPct` | ✅ 已切 |
| 6 | 日前-实时价差 | 页面自算 | 同上 | `aheadRealtimeSpread` | ✅ 已切 |
| 7 | 日频电价预测 | 已走官方（`LoadPrice` → `spotMetricSource`） | 不变 | `发电侧日前/实时加权平均电价` | ✅ 早已切 |
| 8 | **日频回测（复盘·当前模型重跑腿）** | 第三方 CSV 96 点真值 | `BacktestPriceDaily`：as-of 预测 + 官方日均价真值 | 同 #1 | ✅ 本次切（新增） |
| 8b | **留痕对账（复盘·默认腿「当时预测」）** | 第三方 CSV 96 点真值（`allowFrozen`） | `LoadDailyActuals`：官方日均价真值，一天一条 1:1 配对 | 同 #1 | ✅ 本次切（2026-11-13 返工） |
| 9 | 96 点电价曲线（日前/实时） | 第三方 CSV | 无（仅冻结历史演示） | — | ⛔ 已停用（守卫） |
| 10 | 96 点负荷曲线 | 第三方 CSV | 无（官方无负荷口径） | — | ⛔ 已停用（守卫） |
| 11 | 相似日（96 点形态） | 第三方 CSV | 无 | — | ⛔ 已停用（守卫） |
| 12 | 供给曲线反演（96 点负荷×电价） | 第三方 CSV | 无 | — | ⛔ 已停用（守卫） |
| 13 | 96 点分时预测 | 第三方 CSV（3 通道：实时价+日前价+负荷） | 无（官方无 96 点） | — | ⛔ 已停用（守卫），恢复路径保留 |
| 14 | 96 点回测真值 | 第三方 CSV | 无（默认腿已换日频） | — | ⛔ 默认不可达，`allowStale` 的历史演示腿保留 |
| 15 | **小时粒度（24 点）电价预测** | 无 | 官网**主页网关** 24 点分时 `gd_home_24h` | `ahead_price` / `realtime_price`（整点） | ✅ 新增（目标日 as-of 滚动） |

### 15 的口径（别和第 9~14 行混谈）

- 官网**信息披露接口**没有分时是真的（§2 实测：四个 96 点节点全 0 条）；
  官网**主页网关**另有 24 整点分时，是**另一路**数据、另一套端点，匿名可取。
- 这路数据**滞后约 T-6**，所以小时预测不是「明日电价」：以目标日 X 之前最新的完整 24 点日为锚点 D，
  预测 `(X-D)×24` 点后取最后 24 点作为 X。响应里 `to` 是锚点 D、`targetDate` 是 X，两者不同是常态。
- 口径与日均不同：`gd_home_24h.ahead_price`（主页日前价）≠ `gd_spot_metric` 的
  「发电侧日前加权平均电价」。两条曲线不可对齐、不可加总、不可互相换算。
- 不复活 96 点，不替代日频回测真值，也不参与复盘的真值配对。
- 接口：`GET /api/forecast/price?source=home24h&grain=hour&targetDate=YYYY-MM-DD`；
  数据源与粒度交叉校验（`spot_metric+day` / `home24h+hour` / `gd_metric_csv+intraday`）。

## 2. 官方披露到底有什么（实测）

用 `tools/gdpm/gd_pm_fetch.py` 的匿名接口
`POST /portal/xhr/sso-info-disclosure/api/disclosure/nav/market/excelData` 逐节点打：

| 节点 | nodeCode | 返回 |
|---|---|---|
| 交易时间单元所有节点的节点边际电价 | `79411bd76a9a4f5e8e6df4325699f9d4` | `code=200`，blocks=**0** |
| 各节点边际电价的电能量、阻塞各分量价格 | `8bd8ec3ae86f4373aa21c5cf1af1f577` | 200，blocks=**0** |
| 现货市场申报，出清信息 | `e2ad3e9ab756442b8033fab7a229c72b` | 200，blocks=**0** |
| 日前、实时市场各时段出清的断面约束及阻塞情况 | `bcc25842253d43e98a24e752cc2bf133` | 200，blocks=**0** |

`gd_pm_index.json` 里 120 个叶子节点只索引出 20 张表，**按日有数据的只有两张**：

- 现货市场总体运行情况（日前）7 字段，680 行：
  `date`、`generationSideDailyTotalVolume`、`consumerSideDailyTotalVolume`、
  `generationSideDailyAvgPrice`、`generationSideDailyMaxPrice`、`generationSideDailyMinPrice`、
  `dailyDeclarationAvgPrice`
- 现货市场总体运行情况（实时）3 字段，678 行：
  `date`、`generationSideRealTimeTotalVolume`、`generationSideRealTimeAvgPrice`

**没有时间维度、没有 96 点、没有负荷。** 这就是「96 点官方拿不到」的全部依据。

## 3. CSV 代码路径清单（改前 → 改后）

CSV 引擎：`application/internal/logic/gdmetric/`（`file.go:ingestFile` 读 96 点 CSV）。

| 层 | 位置 | 改前 | 改后 |
|---|---|---|---|
| 引擎 | `internal/logic/gdmetric/` | 公开入口直接读 CSV | 公开入口 `GetSeries/GetRealSeries/GetSnapshot/SimilarDays/GetSupplyCurve` 一律先过 `guard()`（`channel.go`）；只有名字带 `Frozen` 的实现体读 CSV |
| 配置 | `manifest/config/config.yaml` | `enabled: false`（只读缓存） | 增加 `serve_frozen: false`（默认入口的总闸，见 §5） |
| 接口 | `GET /api/gd-metric` | 返回 CSV 快照 | 默认返回停更错误（带「实际值止于哪天」）；`allowFrozen=true` 才给冻结演示数据 |
| 接口 | `GET /api/gd-metric/series` | 返回 96 点 | 同上（`allowFrozen`） |
| 接口 | `GET /api/gd-metric/supply-curve` | 反演 | 停更错误（无 opt-in：能力不可迁移） |
| 接口 | `GET /api/gd-metric/similar-days` | 相似日 | 停更错误（同上） |
| 逻辑 | `logic/forecast/price_intraday.go` | 96 点分时预测 | 停更错误（能力保留，靠 §5 恢复） |
| 逻辑 | `logic/forecast/backtest.go` | 96 点回测真值 | 只有 `allowStale=true` 的历史演示腿走 `GetFrozenRealSeries` |
| 逻辑 | `logic/forecast/backtest_daily.go` | **新增** | 日频回测：`spotMetricSource` 真值 + as-of 预测 |

改完后 `rg 'gdmetric\.' --type go`：**55 处 / 15 个文件**，其中 13 处是显式 `Frozen` 入口，
其余是类型、`NormalizeKind`、常量与守卫本身 —— 没有任何一处用 gdmetric 算日频数字。

**fallback 删除证据**：`GetFrozenSeries` 里 `ensureFresh` 失败仍沿用本地缓存（冻结通道本来就
不会再有新数据，这是既定语义）；但**默认入口在 `ensureFresh` 之前就返回停更错误**，
不存在「拉取失败 → 悄悄拿缓存当日频结果」这条路。日频数字一律来自 `gd_spot_metric`。

## 4. 门禁测试

`internal/logic/gdmetric/channel_guard_test.go`：

- `TestNoNewGdmetricImporters`：白名单外的文件 import `gdmetric` → 红。
  白名单里每一项都写了「为什么只能 96 点」。
- `TestRetiredEntryPointsFailClosed`：五个默认入口必须返回 `ChannelRetiredError`，
  且错误里必须带具体日期（页面要能说「数据止于哪天」）。

红/绿各一次：

```
# 红：新增一个只 import 不使用的文件
$ cat > internal/logic/forecast/zz_gate_red_demo.go <<'EOF'
package forecast
import _ "application/internal/logic/gdmetric"
EOF
$ go test ./internal/logic/gdmetric/ -run TestNoNewGdmetricImporters -v
--- FAIL: TestNoNewGdmetricImporters (0.00s)
    channel_guard_test.go:104: 这些文件 import 了已停更的 gdmetric（第三方 CSV 通道）：
          internal/logic/forecast/zz_gate_red_demo.go

# 绿：删掉后
$ go test ./internal/logic/gdmetric/ -run 'TestNoNewGdmetricImporters|TestRetiredEntryPointsFailClosed' -v
--- PASS: TestNoNewGdmetricImporters (0.00s)
--- PASS: TestRetiredEntryPointsFailClosed (0.00s)
```

## 5. 96 点过渡态（不是删掉，是关闸）

96 点是实时电价预测的核心输入（3 通道：实时价+日前价+负荷，实测 MAE 96.4 → 89.5 元/MWh），
所以代码、数据、测试全部保留，只把**默认入口**关掉。

**开关**：`gd_metric.serve_frozen`（或环境变量 `GD_METRIC_SERVE_FROZEN=true`），默认 `false`。

- `false`：所有默认入口报
  `96 点序列不可用：第三方分时通道（gd_metric.csv）已停更：…（实际值止于 2026-10-02）`。
- `true`：默认入口读本地缓存（**不联网**），页面必须显示「第三方 · 非官方」角标。

**恢复采集（需用户拍板，别自己打开）**：

1. `cp tools/gd-spot/creds.json.example tools/gd-spot/creds.json`，从浏览器 DevTools 填
   `Authorization` / `JWTHeaderName` / Cookie（寿命 10~15 分钟）。
2. `python3 tools/gd-spot/fetch_gd_metric.py --check` 确认服务端最新时间（免登录）。
3. `python3 tools/gd-spot/fetch_gd_metric.py --from <D1> --to <D2> --source 9 --append-to <csv> --out <csv>`。
4. 把新 CSV 传到 `GD_METRIC_URL` 指向的位置（对象存储），
   容器内 `out/gd_metric.csv` 由服务按 `refresh_hours` 刷新。
5. `gd_metric.enabled=true` + `serve_frozen=true`，重启 ems。页面自动带「第三方 · 非官方」。

### 5.1 补数路径：人工导入（C6，2026-11-13）

分时研究页只读冻结快照，快照停在 2026-10-02 就永远是博物馆展品。官方 96 点匿名拿不到，
所以补数只有一条合规路径：**人工从电查查导出 → 校验 → 导入**。

```bash
cd OpenLEMS/application
go run ./cmd/gdmetric-import --in 导出.csv --check                 # 只校验，不写盘
go run ./cmd/gdmetric-import --in 导出.csv --out out/gd_metric.csv # 校验通过才写盘（默认合并）
```

- **不联网、不要 token、不进 API、不进 cron**：命令只在人手跑一次时执行。
- 校验器（`internal/logic/gdmetric/frozen_import.go`）同时收长表与宽表：电查查将来的导出形状未知，
  约定只规定「哪些列/哪些点算数」，不规定导出按钮长什么样。
- 校验项：日期格式、15 分钟整点（96 点/天）、区间内不缺天、`typeId ∈ {21,22}`、
  单位只收 `元/兆瓦时`、重复行、空单元格占位。**任何一项失败都不动目标文件**。
- 时间标签两种都收：`00:15 … 24:00`（交易中心口径）与 `00:00 … 23:45`（导出工具口径），
  按整份文件判定基准后统一成前者 —— 逐格判定会在 `00:15` 撞车，等于把曲线挪错一格。
- 数值 `999` 是合法价格，**不是**占位符：占位只看 `sourceId`（999）与空单元格。
- 写盘成功后删掉 `gd_metric.meta.json`（旧文件的行偏移缓存），否则页面会按旧偏移读错位行。

示例文件：`docs/examples/gdmetric-import/{long,wide}.csv`（单测会拿它们跑一遍，README 抄废了会红）。

**官方替代（推荐）**：向广东电力交易中心申请**市场成员账号**，在交易平台导出
「交易时间单元所有节点的节点边际电价」，那是真正合规的 96 点；拿到后把
`price_intraday.go` / `backtest.go` 的取数换成该来源，再把 `guard` 摘掉。

## 6. 受影响的页面

| 页面 | 文件 | 行为 |
|---|---|---|
| 场景 · 预测复盘 | `web/src/pages/scene/ReviewView.vue` | 默认腿 = **当时预测（留痕对账）**，真值与重跑腿同一口径（`gd_spot_metric` 官方日均价）；「当前模型重跑」是显式切换的第二条腿，图上有常驻水印「非当时预测」。96 点改为显式「96 点 · 冻结」入口，停更档（≥15 天）默认不出数、只给「查看历史表现」；永远显示「实际值截至 X（滞后 N 天）」三档与来源角标 |
| 场景 · 相似日分析 | `web/src/pages/scene/DecisionView.vue` | 运行时明确报停更（不返回旧形态） |
| 场景 · 供给曲线 | `web/src/pages/scene/InferenceView.vue` | 同上 |
| 数据导出 | `web/src/exports/datasets.ts` | 「电网日指标（96 点）」标「已停用」并指向「现货市场日指标」 |
| 信息披露 | `web/src/pages/scene/DisclosureView.vue` | 未改（本来就只读官方 `LoadBoard`） |

## 7. 日频回测实测（真库 + 真预测服务，2026-11-13）

命令：

```bash
GDPM_DB=/tmp/lems-db.sqlite3 go test ./internal/logic/forecast/ \
  -run TestDailyBacktestAgainstRealDB -v -timeout 270s
```

真值：`gd_spot_metric` 官方日前加权均价。**下表是 2026-11-13 取数的快照：当时库里最新到 2026-11-11，
相对查询日滞后 2 天。** 这 2 天**不是**「在等实时腿的 T+2」：那一列是日前腿，源站按
`report_of()` 把日前与日报挂在同一天（`report_date = run_date`），实时腿才是 T+2。
所以这里用「采集滞后」描述，不用「T+2」。

腿与滞后上限以 `docs/truth-lag-calibration.md` 的实测结论与
`OpenLEMS/web/src/utils/dailyLag.ts` 的 `NORMAL_LAG`（日前 1 / 实时 2）为准，本条不另立一套。
注意那份手册同时写明：日前是不是真的 T+0 发布时刻上**尚未跑满 3 天增量样本验证**，
能确认的只是 `report_date = run_date` 这个常量。

| 目标日 | 预测 | 实际 | 误差 |
|---|---|---|---|
| 2026-10-27 | 500.18 | 432.56 | +67.63 |
| 2026-10-30 | 466.01 | 483.49 | -17.49 |
| 2026-11-02 | 533.58 | 528.78 | +4.80 |
| 2026-11-05 | 592.22 | 643.64 | -51.42 |
| 2026-11-08 | 458.88 | 405.97 | +52.91 |
| 2026-11-11 | 355.34 | 362.69 | -7.35 |

MAE = **33.60 元/MWh**，MAPE = **7.20%**，6 个样本日（`sampleDays=3` 间隔抽样，窗口 2026-10-01~11-13），推理总耗时 5.5s。
（样本少、单市场；这是「能不能用」，不是「准到什么程度」的结论。**MAE 依赖样本窗口与抽样步长，不可跨口径比对**，见 §9。）

## 8. 复盘页口径返工（2026-11-13，默认腿与真值一致性）

上一版把「当前模型重跑」放在了默认位置，这是反的：页面叫**预测复盘**，
能回答「当时发出去的预测准不准」的只有**当时的那条预测**。同时两条腿各读各的真值 ——
重跑腿用官方日频（截至 2026-11-11），留痕腿用冻结 CSV（截至 2026-10-02），
同一个页面上出现两个「实际值截至」。

改法：

| 项 | 改前 | 改后 |
|---|---|---|
| 默认腿 | 当前模型重跑 | **当时预测（留痕对账）**；留痕为空也不自动切换，只给「先看当前模型重跑」按钮 |
| 留痕腿真值 | `gdmetric.GetSeries(allowFrozen)` 冻结 CSV | `forecast.LoadDailyActuals` → `spotMetricSource` → `ListMetricsByRunDateRange`（`gd_spot_metric`，与重跑腿同一口径） |
| 留痕粒度 | 96 点（15 分钟） | 日频（一天一条，与官方日均价 1:1） |
| 96 点真值 | 留痕腿也读 | 只在「当前模型重跑 · 96 点 · 冻结」里出现，带「第三方 · 非官方」角标 |
| 陈旧处理 | 只显示「滞后 N 天」 | 三档：0~2 天绿点 / 3~14 天黄条 / ≥15 天红条且**默认不出数**（阻断卡 + 「查看 X 之前的历史表现」，展开后置灰 + 水印 + KPI 角标「历史」；≥30 天额外提示数据源可能停更） |
| 事后回填 | 混在留痕里当「当时预测」 | 服务端剔除（生成日晚于目标日），并回报剔除条数 |
| 越界日期 | 会算出区间外的数 | 日期选择器 `max = 实际值截止日`，越界自动截断并提示「已截到实际值最新日 X」 |

**真库实测（本地后端 + 真库副本，2026-11-13）**：

```
GET /api/forecast/recorded?market=ahead&grain=day&from=2026-11-05&to=2026-11-11
→ dataAsOf=2026-11-11  lagDays=2  backfilled=0   points=0
GET /api/forecast/recorded?market=ahead&grain=day&from=2026-09-20&to=2026-11-11
→ dataAsOf=2026-11-11  lagDays=2  backfilled=30  points=0
GET /api/forecast/recorded?market=real &grain=day&from=2026-11-01&to=2026-11-11
→ dataAsOf=2026-11-09  lagDays=4  （实时 T+2，走黄色档）
```

页面（真实浏览器）读数：留痕腿「实际值截至 2026-11-11 · 滞后 2 天」，
重跑腿同样是「2026-11-11 · 滞后 2 天」（6 个对比点，MAE 56.4 元/MWh / MAPE 13.41%，近 7 天档 ——
页面 `sampleDays=1` 取的是 **11-06~11-11 连续 6 天**，与 §7 的间隔抽样不是同一批日子，见 §9）。
96 点冻结腿：「实际值截至 2026-10-02 · 已停更 42 天」+ 阻断卡，不点开一个数都不出。

**留痕目前为什么是空的（数据事实，不是代码问题）**：库里 `grain=day, market=ahead` 的 37 条留痕中，
30 条是**生成日晚于目标日**的回填（10-07~11-08 生成、目标 09-26~09-30），
7 条目标日在未来（11-12~11-18，实际值还没出）。真正的 96 点留痕（08-31、10-06 生成）
没有官方分时真值可配对。所以「当时预测」会从**日频留痕开始按日积累**后才有样本。

## 9. 两次 MAE 为何不同（33.60 vs 56.4）：样本日不同，不是配对 bug

同一个真库、同一个 `BacktestPriceDaily`，只换 `from/to/sampleDays` 连跑三次（2026-11-13 实测）：

| 请求 | 样本日 | MAE | MAPE |
|---|---|---|---|
| `from=2026-10-01 to=2026-11-13 sampleDays=3` | 10-27 / 10-30 / 11-02 / 11-05 / 11-08 / 11-11 | **33.60** | **7.20%** |
| `from=2026-11-05 to=2026-11-11 sampleDays=1`（复盘页「近 7 天」档） | 11-06 / 11-07 / 11-08 / 11-09 / 11-10 / 11-11 | **56.36** | **13.41%** |
| `from=2026-11-05 to=2026-11-11 sampleDays=3` | 11-05 / 11-08 / 11-11 | 37.23 | 7.68% |

结论：

- **配对方式没变**：`b437aa4..ca84f5a` 的 diff 里没有 `backtest_daily.go`。两次差异 100% 来自样本窗口
  与抽样步长 —— §7 是 42 天窗口里的每 3 天抽样（点稀、跨峰谷平），§8 是页面「近 7 天」档的
  连续 6 天（`sampleDays=1`，`maxBacktestPoints=6`）。11-06~11-10 这 5 天预测普遍高估（误差 +36~+101），
  所以连续窗口的 MAE 更大。
- **两个数各自内部正确**：三次运行里共同的样本日（11-08 `+52.91`、11-11 `-7.35`）逐点误差完全一致，
  同一天同一 as-of 的预测值也完全一致（500.18 / 466.01 / …），说明 as-of 推理与聚合可复现；
  换库副本（11-12 与 11-13 两份 `lems-db`）结果逐位相同。
- **正确口径**：报 MAE 时必须同时报「窗口 + 抽样步长 + 点数」。页面展示的就是用户窗口内的真实数字
  （近 7 天档 = 最近 6 天连续），所以页面上 **56.4 / 13.41% 是对该窗口的正确值**；
  33.60 / 7.20% 只能标成「10-01~11-13 每 3 天抽样的参考值」，不能拿来当同口径对比。
- 顺带修：`ReviewView` 里「抽满 7 天」的注释与后端 `maxBacktestPoints=6` 不符，
  已改为「最多 6 个点 —— 近 7 天档实际是最近 6 天」。
