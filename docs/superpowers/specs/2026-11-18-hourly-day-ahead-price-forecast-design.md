# 设计：决策支持页新增小时粒度（24 点）日前电价预测

日期：2026-11-18  
状态：设计已与用户对齐，待用户 review spec

## 1. 背景

「分析预测 → 决策支持 → 日前电价预测」目前只有一张**日均**图：

- 接口：`GET /api/forecast/price`（不传 `grain`，落到 `day`）
- 序列源：`gd_spot_metric` 的官方直采日均成交价，1 点/天
- 前端：`OpenLEMS/web/src/pages/scene/DecisionView.vue`

现状的问题：**数据源靠 `grain` 隐式决定**（`day` → `gd_spot_metric`，`intraday` → `gd_metric.csv`）。
两个数据源的锚点规则、缺失判定、可预测长度、留痕来源都不同，`grain` 一词把
「多细的时间粒度」和「哪份数据」混成了一件事，再加第三路（官网 24 点）就会开始靠约定猜。
所以本设计把数据源提升成**显式请求参数 `source`**，`grain` 只表达采样步长，
两者交叉校验，不一致直接报参数错误。

15 分钟（96 点）分时预测已停用：它依赖第三方 `gd_metric.csv`，实际值止于 2026-10-02，官方信息披露匿名接口没有 96 点出清价。

但库里已有另一路**官方主页 24 点分时**数据 `gd_home_24h`：

- 采集：`application/internal/logic/homemarket/collect.go`，官方主页网关匿名可取
- 字段：`ahead_price`（日前）、`realtime_price`（实时）、`power_mw`（系统负荷）
- 粒度：一天 24 个整点（0~23）
- 滞后：约 T-6，采集器默认回补最近 14 天

用户要求：保留现有日均图，另加一张**小时粒度**预测图，每天 24 点。

## 2. 目标与非目标

### 目标

1. 在决策支持页「日前电价预测」下方新增一张图：**D+1 日 24 个整点**的日前电价预测（含 10%/90% 分位）。
2. 后端扩 `/api/forecast/price`：新增 `grain=hour` 与显式 `source` 参数，复用现有缓存、`forceRefresh`、分位与留痕链路。
3. 图上和文案明确：锚点是库内最新完整 24 点日 D，预测的是 D+1，不是“今天/明天”的实时滚动预测。
4. `source` 与 `grain` 一一对应、交叉校验；三路数据源各有独立 loader，不共用序列构造逻辑。

### 非目标

- 不复活 96 点分时预测，不改 `grain=intraday` 的停用状态。
- 不把日均预测插值成 24 点（那是假粒度）。
- 不加 1/3/5 天切换；`hour` 只出 D+1 的 24 点。
- 不新增独立接口，不新增前端路由。
- 不改 `gd_spot_metric` 的日均预测口径。
- 不把三路数据源合并成一份「统一序列」再喂模型；合并会掩盖各自的缺口语义。
- 不为兼容性删除 `grain` 的隐式推导：不传 `source` 时仍按 `grain` 推导，老调用方不受影响。

## 3. 数据源与口径

| 项 | 取值 |
|---|---|
| 数据表 | `gd_home_24h` |
| 列 | `ahead_price`（`market=ahead`）/ `realtime_price`（`market=real`） |
| 步长 | 60 分钟，24 点/自然日，小时取 0~23 |
| 单位 | 元/MWh（与现有电价图一致） |
| 锚点 D | 区间内**最后一个 24 点齐全**的自然日 |
| 预测目标 | D+1 的 `00:00 … 23:00`，共 24 点 |
| 历史输入 | 结束于 D 的连续 `historyDays` 个完整日，每天 24 点；默认 14 天，最多 30 天，至少 7 天 |
| 数据源参数 | `source=home24h`、`grain=hour`（见 §4.1） |
| 来源角标 | 官网·24点 |

口径约束：

- 比 D 更新的**部分日**忽略，绝不拿它当锚点。
- D 之前的连续窗口里，任一天缺小时或整天缺失 → 报错，提示“最近完整日为 D，窗口内 YYYY-MM-DD 不足 24 点”。
- 不补 0、不插值、不用旧值顶替。

## 4. API 契约

### 4.1 请求参数：数据源 + 粒度

`source` 决定**读哪份数据、用哪套预测逻辑**；`grain` 决定**采样步长**。两者必须自洽。

| `source` | 含义 | 数据表 / 源 | 允许的 `grain` | loader |
|---|---|---|---|---|
| `spot_metric` | 官方披露接口直采（pm-api）日均成交价 | `gd_spot_metric` | `day` | `LoadPrice` |
| `home24h` | 官方主页网关 24 点分时 | `gd_home_24h` | `hour` | `loadHourCached`（新增） |
| `gd_metric_csv` | 第三方电网日指标 CSV（已停更） | `gd_metric.csv` | `intraday` | `loadIntradayCached` |

规则：

- `source` 与 `grain` 同时给且不匹配（如 `source=home24h&grain=day`）→ `400 CodeInvalidParameter`，消息列出该 source 允许的 grain。
- 只给 `grain` 不给 `source` → 按上表反推 source（保持现有调用方行为：`day` / `intraday` 不变，`hour` → `home24h`）。
- 只给 `source` 不给 `grain` → 按上表取唯一合法 grain。
- 都不给 → `source=spot_metric`、`grain=day`。
- 未知取值 → `400`，不回落到默认值（静默回退会让人以为在看官方日频数据）。
- 响应里回显 `source`，前端据此决定角标文案，不靠猜 `grain`。

请求示例：

```
GET /api/forecast/price?market=ahead&source=home24h&grain=hour&historyDays=14&horizonDays=1&quantiles=0.1,0.5,0.9&forceRefresh=false
```

### 4.2 响应

响应沿用 `GetPriceForecastRes`，新增 `source` 字段；`hour` 粒度关键字段：

| 字段 | hour 粒度取值 |
|---|---|
| `source` | `home24h` |
| `grain` | `hour` |
| `stepMinutes` | `60` |
| `from` / `to` | 历史首日 / 锚点 D |
| `historyDates` | `YYYY-MM-DD HH:00`，按时间升序 |
| `historyValues` | 对应整点电价 |
| `forecast[].targetTime` | `YYYY-MM-DD HH:00` |
| `forecast[].runDate` | D+1 的日期 |
| `forecast[].atTime` | `HH:00` |
| `dataset` / `metric` | 官网主页 24 点 / 日前电价（整点）或实时电价（整点） |
| `unit` | 元/MWh |
| `contextPoints` | `historyDays * 24` |
| `missingDays` | `0`（窗口已强制完整；缺数据直接报错） |

`horizonDays` 在 `hour` 粒度下固定为 1；传 0 或其他值都按 1 处理，API 注释写明。


## 5. 后端设计

### 5.1 粒度常量与解析

- `services/s_db/s_db_model/s_db_model_price_forecast_s.go`
  - 新增 `ForecastGrainHour = "hour"`。
  - 新增数据源常量：`PriceSourceSpotMetric = "spot_metric"`、`PriceSourceHome24h = "home24h"`、`PriceSourceGdMetricCSV = "gd_metric_csv"`。
- `application/internal/logic/forecast/price.go`
  - `ParseGrain`：接受 `hour` / `hourly` / `24`，归一化为 `ForecastGrainHour`；现有 `day` / `intraday` 行为不变。
  - 新增 `ParsePriceSource(source, grain string) (source, grain string, err error)`：按 §4.1 的对应表做推导与交叉校验，是唯一的校验入口，控制器不复制这套规则。
  - 新增 `priceSourceSpec` 表：`source → { 数据表/数据集名, 允许 grain, loader 分派标记 }`，`LoadPriceAny` 只认这张表。

### 5.2 区间查询

`IHomeMarketService` 新增：

```go
ListHome24hRange(ctx context.Context, from, to string) ([]*s_db_model.SHome24hModel, error)
```

实现放在 `services/s_db/internal/impl/s_db_impl_home_market_service_s.go`：

- `WHERE price_date BETWEEN from AND to`
- `ORDER BY price_date ASC, hour ASC`
- 无数据返回空切片，不报错。

保留原 `ListHome24h(date)` 给现有页面用。

### 5.3 新 loader

新增 `application/internal/logic/forecast/price_hour.go`，照 `price_intraday.go` 的结构写：

1. `resolvePriceSpec(market)` 得到市场口径。
2. `ListHome24hRange(ctx, D-30, today)` 拿原始行。
3. 按日期分组，得到最新完整日 D；从 D 往前取连续 `historyDays` 个完整日。
4. 拼时间序列：按 `(date, hour)` 升序，值取 `AheadPrice` 或 `RealtimePrice`。
5. 调 `s_forecast.Client.Forecast`：`Horizon = 24`，`Quantiles` 用默认 `0.1,0.5,0.9`。
6. 目标时间从 D+1 00:00 起，每小时一点；时区用 `priceLocation()`。
7. 返回 `TPriceForecastResult`，`Grain=hour`、`StepMinutes=60`。

为便于测试，抽取 `loadPriceHourWith(ctx, client, source, spec, historyDays, quantiles, loc, now)`，注入风格沿用 `loadPriceWith`。

### 5.4 分派、时间格式与留痕

改 `application/internal/logic/forecast/price_any.go`：

- `LoadPriceAny`：先 `ParsePriceSource` 得到 `(source, grain)`，再按 `source` 分派：
  `spot_metric → loadDaily`、`home24h → loadHourCached`、`gd_metric_csv → loadIntradayCached`。
  分派写在一处，`if grain == ...` 的隐式判断删掉。
- `loadHourCached`：缓存键 `forecast:price:<source>:<market>:<historyDays>:1:<quantiles>`，复用 `withForecastCache` 和 `ForceRefresh`；
  同一次改造把 `loadDaily` / `loadIntradayCached` 的缓存键也补上 `source` 段，避免以后两条源出同一键。
- `TPriceForecastResult` 新增 `Source` 字段，一路带到 DTO。
- `formatTargetTime`：`day` 仍是 `YYYY-MM-DD`；`hour` 与 `intraday` 一样输出 `YYYY-MM-DD HH:MM`，整点即 `HH:00`。
- `targetAfterGeneratedOn`：`hour` 与 `intraday` 同样保留同日后续时段（虽然本次只出 D+1，不会触发）。
- `recordForecastPoints`：`hour` 的留痕来源文案写 `官网主页24点分时`（对应库里 `gd_price_forecast.source` 列），不再落到默认的 `gd_metric.csv`。
  注：留痕的 `source` 列是给人看的来源文案，与请求参数 `source`（`home24h`）不是一个东西，别合并。

`generatedOn` 用锚点 D，不用今天；这样留痕的“当时预测”语义才成立。

### 5.5 DTO 注释

`application/api/forecast/v1/forecast_price.go`：

- 请求新增 `Source string`（`dc:"数据源：spot_metric（默认）/ home24h / gd_metric_csv，与 grain 交叉校验"`）。
- 响应 `PriceForecast` 新增 `Source string`。
- 更新注释：`grain=hour` = 官网主页 24 点、`TargetTime` 为 `YYYY-MM-DD HH:00`、`AtTime` 为 `HH:00`。

## 6. 前端设计

### 6.1 API 层

`OpenLEMS/web/src/api/forecast.ts`：

- `PriceForecast` 新增 `source: string`；`fetchPriceForecast` 参数补 `source?: 'spot_metric' | 'home24h' | 'gd_metric_csv'`，并补 `grain` 的 `hour` 取值。
- `PriceForecastPoint.atTime` 注释补 `hour`。
- 不改其他类型形状，不新增请求函数。

### 6.2 决策支持页

`OpenLEMS/web/src/pages/scene/DecisionView.vue`：

- 新增 `priceHour = ref<PriceForecast | null>(null)`、`loading.priceHour`、`errors.priceHour`。
- `loadForecast(force)` 从 `Promise.allSettled([load, dayPrice])` 扩成三路：
  - `fetchPriceForecast({ source: 'spot_metric', historyDays: 60, horizonDays: 5, ... })`（原日均图，显式带 source）
  - `fetchPriceForecast({ source: 'home24h', grain: 'hour', historyDays: 14, horizonDays: 1, ... })`（新小时图）
- 「刷新预测（跳过缓存）」同时穿透两路缓存。
- 原「日前电价预测」PanelBox 保持不变，只在它下面加一个新 PanelBox：
  - 标题：`日前电价预测 · 小时（24 点）`
  - 副标题：`TimesFM · D+1 00:00~23:00`
  - SourceBadge：`官网·24点`
  - foot：`锚点 D=${to} · 历史 ${contextPoints} 点 · 模型 ${model} · 耗时 ${timeMs}ms`
  - 警示文案：数据在 T-6 左右；预测相对 D，不是今天；不做日均插值。

### 6.3 图表

新 ECharts 只画 48 个点，避免 14 天×24 点刷屏：

- x 轴：`forecast[].atTime`，固定 `00:00 … 23:00`。
- 历史腿：取 `historyValues` 最后 24 个点（即 D 日），实线蓝色。
- 预测腿：`point` 虚线品牌色，`quantiles['0.1']` / `['0.9']` 灰色点线。
- y 轴单位取 `unit`。

## 7. 错误处理

| 场景 | 行为 |
|---|---|
| `source` 与 `grain` 不匹配 / 取值未知 | `400`，消息列出 source 允许的 grain；不静默回落到日频 |
| `gd_home_24h` 没有完整 24 点日 | 后端报“24 点电价数据不足：没有完整的 24 点运行日”，前端面板显示空态 |
| D 前连续窗口缺某天/某小时 | 后端报具体日期，前端显示该错误；不静默跳过 |
| 预测服务不可用 | 沿用现有 `预测服务失败` 错误；日均图不受影响 |
| 留痕写失败 | 沿用现有 best-effort：告警但不挡预测结果 |
| `market=real` | 用 `realtime_price`，图表标题/指标名随市场切换；决策支持页默认只请求 `ahead` |

## 8. 文档

`docs/data-source-matrix.md` 加一行并补一句口径说明：

- 官网**信息披露接口**没有分时是真的；官网**主页网关**有 24 整点是另一路，不能混谈。
- 24 点只用于本次新增的小时预测，不复活 96 点、不替代日频回测真值。

## 9. 测试

### Go 单测

`application/internal/logic/forecast/price_hour_test.go`（新建）：

1. `ParseGrain("hour"/"hourly"/"24")` → `ForecastGrainHour`。
2. 最新完整日选择：最新部分是 17 点 → 锚点取前一完整日 D。
3. 连续窗口缺一天 → 报错且消息含日期。
4. `market=ahead` 取 `AheadPrice`，`market=real` 取 `RealtimePrice`。
5. 输出 24 点，`targetTime` 为 D+1 `00:00…23:00`，`stepMinutes=60`。
6. `formatTargetTime(hour, ts)` → `YYYY-MM-DD HH:00`。
7. `recordForecastPoints` 的 `hour` 留痕来源文案不是 `gd_metric.csv`（是 `官网主页24点分时`）。

`price_any_test.go` 补 `hour` 解析与格式断言；现有 day/intraday 断言不改。

`price_source_test.go`（新建）覆盖参数校验：

1. 只给 `grain=hour` → `source=home24h`；只给 `source=home24h` → `grain=hour`。
2. `source=home24h&grain=day` → 报错，消息含允许的 grain。
3. 都不给 → `spot_metric` + `day`。
4. 未知 source / 未知 grain → 报错，不回落默认。
5. 三路 source 各自分派到对应 loader（用注入的假 loader 断言命中哪一个）。

### 前端检查

- `OpenLEMS/web/scripts/browser-check.mjs`：决策支持页最小图数 `3 → 4`，新增文本断言 `小时（24 点）`。
- `OpenLEMS/web/scripts/smoke.mjs`：决策支持页断言新增小时图标题。

### 验证命令

在 `OpenLEMS/` 下：

```bash
go test ./application/internal/logic/forecast/... ./services/s_db/internal/impl/...
npm --prefix web run build
node web/scripts/browser-check.mjs
```

## 10. 改动文件清单

后端：

- `OpenLEMS/services/s_db/s_db_basic/s_db_basic_interface.go`
- `OpenLEMS/services/s_db/internal/impl/s_db_impl_home_market_service_s.go`
- `OpenLEMS/services/s_db/s_db_model/s_db_model_price_forecast_s.go`
- `OpenLEMS/application/api/forecast/v1/forecast_price.go`
- `OpenLEMS/application/internal/logic/forecast/price.go`
- `OpenLEMS/application/internal/logic/forecast/price_any.go`
- `OpenLEMS/application/internal/logic/forecast/price_hour.go`（新增）
- `OpenLEMS/application/internal/logic/forecast/price_hour_test.go`（新增）
- `OpenLEMS/application/internal/logic/forecast/price_source_test.go`（新增）
- `OpenLEMS/application/internal/logic/forecast/price_any_test.go`

前端：

- `OpenLEMS/web/src/api/forecast.ts`
- `OpenLEMS/web/src/pages/scene/DecisionView.vue`
- `OpenLEMS/web/scripts/browser-check.mjs`
- `OpenLEMS/web/scripts/smoke.mjs`

文档：

- `docs/data-source-matrix.md`
- 本 spec：`docs/superpowers/specs/2026-11-18-hourly-day-ahead-price-forecast-design.md`

## 11. 风险与回滚

- **风险：24 点源滞后 T-6。** 图上必须写清锚点 D；不能把它叫“明日预测”。
- **风险：源站改版/停采。** 页面只报该面板错误，日均图与其他页面不受影响。
- **风险：留痕混入复盘。** `grain=hour` 独立粒度，`target_time` 带小时；复盘页按 `grain` 过滤，不会被日频腿读到。
- **回滚：** 前端删新 PanelBox 与三路请求中的 hour 调用；后端删 `grain=hour` 分派与 loader。数据表 `gd_home_24h` 与采集任务保留，不动。
