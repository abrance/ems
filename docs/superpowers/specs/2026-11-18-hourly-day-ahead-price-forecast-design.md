# 设计：决策支持页新增小时粒度（24 点）日前电价预测

日期：2026-11-18（修订 3）  
状态：待用户 review

## 1. 背景

「分析预测 → 决策支持 → 日前电价预测」目前只有一张**日均**图：

- 接口：`GET /api/forecast/price`（不传 `grain`，落到 `day`）
- 序列源：`gd_spot_metric` 的官方直采日均成交价，1 点/天
- 前端：`OpenLEMS/web/src/pages/scene/DecisionView.vue`

两个既有问题：

1. **数据源靠 `grain` 隐式决定**（`day` → `gd_spot_metric`，`intraday` → `gd_metric.csv`）。
   两路的锚点规则、缺失判定、可预测长度、留痕来源都不同，`grain` 一词把「多细的时间粒度」
   和「哪份数据」混成一件事。再说加第三路，就只能靠约定猜。
2. **24 点数据滞后 T-6**（`homemarket/collect.go` 实测注释：11-13 只能取到 11-07）。
   所以「锚定最新日 D 再预测 D+1」得到的其实是**5 天前的明天**，作为「明日电价」不可用。

本设计的处理：

- 数据源提升成**显式请求参数 `source`**，`grain` 只表达采样步长，两者交叉校验。
- 小时图引入**目标日 `targetDate`**（默认今天，可选历史日）：以目标日之前最新的完整 24 点日
  为锚点 D，用 as-of 滚动预测到目标日，取最后 24 点。这样「默认预测当天」成立，
  而滞后多少天由锚点 D 在图上说清楚。

数据源（`gd_home_24h`，官方主页网关匿名可取）：

- 采集：`application/internal/logic/homemarket/collect.go`
- 字段：`ahead_price`（日前）、`realtime_price`（实时）、`power_mw`（系统负荷）
- 粒度：一天 24 个整点（0~23）
- 滞后：约 T-6，采集器默认回补最近 14 天

## 2. 目标与非目标

### 目标

1. 决策支持页「日前电价预测」下方新增一张**小时（24 点）**预测图。
2. 目标日可选，默认今天；预测终止于目标日 X 的 `00:00 … 23:00`，共 24 点，含 10%/90% 分位。
3. X 在库里已有完整 24 点实际值时，同图叠加一条实际线（直接看准不准）。
4. 后端扩 `/api/forecast/price`：新增 `grain=hour`、显式 `source`、`targetDate`；复用缓存、
   `forceRefresh`、分位与留痕链路。
5. 图上写清「锚点 D」与「目标日 X」；两值不同就是 T-6 滞后的可见证据，不隐藏。

### 非目标

- 不复活 96 点分时预测，不改 `grain=intraday` 的停用状态。
- 不把日均预测插值成 24 点（那是假粒度）。
- 不做逐步滚动（`targetDate` 一步到位，不为中间每天各出 24 点）。
- 不新增独立接口，不新增前端路由。
- 不改 `gd_spot_metric` 的日均预测口径。
- 不把三路数据源合并成一份「统一序列」再喂模型；合并会掩盖各自的缺口语义。
- 不为兼容性删除 `grain` 的隐式推导：不传 `source` 时仍按 `grain` 推导，老调用方不受影响。

## 3. 数据源与口径（hour）

| 项 | 取值 |
|---|---|
| 数据表 | `gd_home_24h` |
| 列 | `ahead_price`（`market=ahead`）/ `realtime_price`（`market=real`） |
| 步长 | 60 分钟，24 点/自然日，小时取 0~23 |
| 单位 | 元/MWh |
| 目标日 X | 请求参数 `targetDate`，默认今天（`priceLocation()` 的今天） |
| 锚点 D | X 之前最新一个 **24 点齐全**的自然日（严格小于 X） |
| 历史输入 | 结束于 D 的连续 `historyDays` 个完整日，每天 24 点；默认 14 天，最多 30 天，至少 7 天 |
| 预测步数 | `(X - D) × 24` 点，取**最后 24 点**作为 X 的 24 个整点 |
| 数据源参数 | `source=home24h`、`grain=hour`（见 §4.1） |
| 来源角标 | 官网·24点 |

口径约束：

- 比 X 更新的日不进历史；X 之前的**部分日**（缺点）不能当锚点，只能继续往前找 D。
- D 往前连续 `historyDays` 天内，任一天缺小时或整天缺失 → 报错，提示
  「最近完整日为 D，窗口内 YYYY-MM-DD 不足 24 点」。不补 0、不插值、不用旧值顶替。
- `(X - D)` 可能为 1（数据新鲜时）到 7（T-6 时）：两者都合法，图上必须显示真实 D。
- 预测步数上限 512（TimesFM `max_horizon`，约 21 天）。`(X-D)×24 > 512` → 报错并提示把目标日往前调。
- **口径警告（必须上图注）**：`gd_home_24h.ahead_price`（主页日前价）与日均图的
  `gd_spot_metric`「发电侧日前加权平均电价」**定义不同**，两条曲线不可对齐、不可加总、不可互相换算。

## 4. API 契约

### 4.1 请求参数：数据源 + 粒度 + 目标日

`source` 决定**读哪份数据、用哪套预测逻辑**；`grain` 决定**采样步长**。两者必须自洽。

| `source` | 含义 | 数据表 / 源 | 允许的 `grain` | loader |
|---|---|---|---|---|
| `spot_metric` | 官方披露接口直采（pm-api）日均成交价 | `gd_spot_metric` | `day` | `loadDaily` |
| `home24h` | 官方主页网关 24 点分时 | `gd_home_24h` | `hour` | `loadHourCached`（新增） |
| `gd_metric_csv` | 第三方电网日指标 CSV（已停更） | `gd_metric.csv` | `intraday` | `loadIntradayCached` |

规则：

- `source` 与 `grain` 同时给且不匹配（如 `source=home24h&grain=day`）→ `400 CodeInvalidParameter`，
  消息列出该 source 允许的 grain。
- 只给 `grain` 不给 `source` → 按上表反推 source（`day` / `intraday` 行为不变，`hour` → `home24h`）。
- 只给 `source` 不给 `grain` → 按上表取唯一合法 grain。
- 都不给 → `source=spot_metric`、`grain=day`。
- 未知取值 → `400`，不静默回落到默认（回退会让人以为在看官方日频数据）。
- `targetDate` 只对 `grain=hour` 生效：
  - 缺省 / 空 → 今天；
  - 格式必须是 `YYYY-MM-DD`，否则 `400`；
  - 晚于今天 → `400`（没有未来 24 点数据可锚）。
- `horizonDays` 在 `hour` 粒度下被忽略（步数由 `(X-D)×24` 推出）；day / intraday 行为不变。
- 响应回显 `source` 与 `targetDate`，前端据此出角标与标题，不靠猜。

请求示例：

```
GET /api/forecast/price?market=ahead&source=home24h&grain=hour&targetDate=2026-11-18&historyDays=14&quantiles=0.1,0.5,0.9&forceRefresh=false
```

### 4.2 响应

响应沿用 `GetPriceForecastRes`，新增 `source`、`targetDate`、`actual`；`hour` 粒度关键字段：

| 字段 | hour 粒度取值 |
|---|---|
| `source` | `home24h` |
| `grain` | `hour` |
| `targetDate` | 目标日 X |
| `stepMinutes` | `60` |
| `from` / `to` | 历史首日 / 锚点 D（`to` 就是 D） |
| `historyDates` | `YYYY-MM-DD HH:00`，升序，长度 `historyDays × 24` |
| `historyValues` | 对应整点电价 |
| `forecast[].targetTime` | X 的 `YYYY-MM-DD HH:00`，共 24 条 |
| `forecast[].runDate` | X |
| `forecast[].atTime` | `HH:00` |
| `actual` | X 的 24 个整点实际值；该日不满 24 点时为**空数组**（不是补 0） |
| `dataset` / `metric` | `官网·24点` / `日前电价（整点）` 或 `实时电价（整点）` |
| `unit` | 元/MWh |
| `contextPoints` | `historyDays × 24` |
| `missingDays` | `0`（窗口已强制完整；缺数据直接报错） |

`actual` 与 `forecast` 都按 0~23 点顺序排列，前端按下标对齐即可（两数组等长或 `actual` 为空）。

## 5. 后端设计

### 5.1 常量与解析

- `services/s_db/s_db_model/s_db_model_price_forecast_s.go`
  - 新增 `ForecastGrainHour = "hour"`，并把 `ForecastGrain*` 注释补成 `day / hour / intraday`。
  - 新增数据源常量：`PriceSourceSpotMetric = "spot_metric"`、`PriceSourceHome24h = "home24h"`、
    `PriceSourceGdMetricCSV = "gd_metric_csv"`。
  - `SPriceForecastModel.Grain` 注释补 `hour`；`s_db_init_f.go` 里 `gd_price_forecast` 的建表注释同上（列不变，不迁移）。
- `application/internal/logic/forecast/price.go`
  - `ParseGrain`：接受 `hour` / `hourly` / `24` → `ForecastGrainHour`；day / intraday 行为不变。
  - 新增 `ParsePriceSource(source, grain string) (source, grain string, err error)`：§4.1 唯一的推导与校验入口。
    别名归一化复用 `ParseGrain`，不另写一份别名表。
  - 新增 `priceSourceSpec` 表：`source → { 允许 grain, 数据集名, 指标名 }`，`LoadPriceAny` 只认这张表。
- `application/internal/logic/forecast/price_any.go`
  - `PriceRequest` 新增 `Source string` 与 `TargetDate string` 字段。
  - `TPriceForecastResult` 新增 `Source`、`TargetDate`、`Actual []float64`。

### 5.2 区间查询

`IHomeMarketService` 新增：

```go
// ListHome24hRange 取 [from, to] 区间内的 24 点行（按 price_date、hour 升序）。
// 没有数据返回空切片，不报错。
ListHome24hRange(ctx context.Context, from, to string) ([]*s_db_model.SHome24hModel, error)
```

实现（`services/s_db/internal/impl/s_db_impl_home_market_service_s.go`）：

- `WHERE price_date BETWEEN from AND to`
- `ORDER BY price_date ASC, hour ASC`
- 保留原 `ListHome24h(date)` 给现有页面用。

### 5.3 新 loader

新增 `application/internal/logic/forecast/price_hour.go`，结构照 `price_intraday.go`：

1. `resolvePriceSpec(market)` **只取 `Market` 字段**（其余字段是现货日频口径，不能用；
   dataset/metric 由本 loader 自己给）。
2. 目标日 X：`targetDate` 为空取 `now` 的今天；与 `now` 同日之外不做特殊处理。
3. 查询 `ListHome24hRange(ctx, X-30天, X-1天)`（下界含 30 天覆盖 `historyDays` 上限）。
4. 按日期分组，取 `X` 之前最新的 24 点齐全日作为 D；再往前取连续 `historyDays` 个完整日。
5. 历史序列按 `(date, hour)` 升序，值取 `AheadPrice` 或 `RealtimePrice`。
6. `steps := (X - D) 的天数 × 24`；`steps > 512` → 报错提示「目标日距锚点 N 天，超过模型单次上限 21 天，请把目标日往前调」。
7. 调 `s_forecast.Client.Forecast`：`Horizon = steps`，`Quantiles` 默认 `0.1,0.5,0.9`。
8. 取返回点预测的**最后 24 点**作为 X 的 24 个整点；`TargetTime` 从 X 00:00 起逐小时生成（不依赖返回值下标推算日期）。
9. 实际值：若 X 在库里 24 点齐全，取这 24 点按 hour 升序填 `Actual`；否则 `Actual = []float64{}`。
10. 返回 `TPriceForecastResult`：`Source=home24h`、`Grain=hour`、`StepMinutes=60`、
    `Dataset=homemarket.SourceHome24h`、`Metric` 按市场给「日前电价（整点）」/「实时电价（整点）」、
    `Unit="元/MWh"`、`From`/`To(=D)`、`TargetDate=X`。

为便于测试，抽取
`loadPriceHourWith(ctx, client, source, market, targetDate, historyDays, quantiles, loc, now)`，
注入风格沿用 `loadPriceWith`。

### 5.4 分派、时间格式与留痕

改 `application/internal/logic/forecast/price_any.go`：

- `LoadPriceAny`：先 `ParsePriceSource` 得到 `(source, grain)`，再经**包级可替换的 loader 表**分派：

  ```go
  var priceLoaders = map[string]func(context.Context, PriceRequest) (*TPriceForecastResult, error){...}
  ```

  三路分别指向 `loadDaily` / `loadHourCached` / `loadIntradayCached`。测试替换该表即可断言分派命中，
  不必起 HTTP。`if grain == ...` 的隐式判断删掉。
- 三个 loader 都要显式设置 `Source`（`loadDaily` → `spot_metric`，`loadHourCached` → `home24h`，
  `loadIntradayCached` → `gd_metric_csv`），否则 day 路径回显空。
- `loadHourCached`：缓存键 `forecast:price:<source>:<market>:<targetDate>:<historyDays>:<quantiles>`，
  复用 `withForecastCache` 与 `ForceRefresh`。同一次改造给 `loadDaily` / `loadIntradayCached` 的键补 `source` 段。
- `formatTargetTime`：`day` 仍 `YYYY-MM-DD`；`hour` 与 `intraday` 一样 `YYYY-MM-DD HH:MM`（整点即 `HH:00`）。
- `targetAfterGeneratedOn`：`hour` 与 `intraday` 同样保留同日后续时段。
- `recordForecastPoints`：留痕 `Source` 文案按 source 给（`官网主页24点分时` / `官方直采日均成交价` /
  `gd_metric.csv`）。注意 `gd_price_forecast.source` 列是**给人看的文案**，与请求参数 `source` 同名不同义，不合并。
- `generatedOn` = **锚点 D**（不是今天，也不是 X）：留痕语义是「以 D 为锚点时对 X 的预测」。
  唯一键 `(generated_on, market, grain, target_time)` 意味着同一 D 下重复刷新会覆盖同一行，
  即**只保留最后一次**；这是有意为之（同锚点不存在多个 as-of 版本），写进注释。

### 5.5 DTO

`application/api/forecast/v1/forecast_price.go`：

- 请求新增 `Source string`（`dc:"数据源：spot_metric（默认）/ home24h / gd_metric_csv，与 grain 交叉校验"`）、
  `TargetDate string`（`dc:"目标日 YYYY-MM-DD，仅 grain=hour 生效，默认今天；晚于今天报错"`）。
- 响应 `PriceForecast` 新增 `Source string`、`TargetDate string`、`Actual []float64`
  （`dc:"目标日 24 点实际值；不满 24 点为空数组"`）。
- 控制器 `application/internal/controller/forecast/forecast_v1_price.go`：把 `req.Source`、`req.TargetDate`
  传进 `PriceRequest`，并回填 `Source` / `TargetDate` / `Actual`。
- 注释：`grain=hour` = 官网主页 24 点，`TargetTime` 为 `YYYY-MM-DD HH:00`，`AtTime` 为 `HH:00`，
  `horizonDays` 在该粒度下被忽略。

## 6. 前端设计

### 6.1 API 层

`OpenLEMS/web/src/api/forecast.ts`：

- `PriceForecast` 新增 `source: string`、`targetDate?: string`、`actual?: number[]`；
  `fetchPriceForecast` 参数补 `source?: 'spot_metric' | 'home24h' | 'gd_metric_csv'`、`targetDate?: string`，
  并补 `grain` 的 `hour`。
- `PriceForecastPoint.atTime` 注释补 `hour`。
- `normalizeForecast` 里把 `actual` 归一成 `[]`（Go 的 nil 切片是 `null`）。
- 不新增请求函数。

### 6.2 决策支持页

`OpenLEMS/web/src/pages/scene/DecisionView.vue`：

- 新增 `priceHour = ref<PriceForecast | null>(null)`、`hourDate = ref(today)`、`loading.priceHour`、`errors.priceHour`。
- `loadForecast(force)` 从两路扩成三路：
  - `fetchPriceForecast({ source: 'spot_metric', historyDays: 60, horizonDays: 5, ... })`（原日均图）
  - `fetchPriceForecast({ source: 'home24h', grain: 'hour', targetDate: hourDate.value, historyDays: 14, ... })`
- 「刷新预测（跳过缓存）」同时穿透三路缓存。
- 原「日前电价预测」PanelBox 不动，下面新增 PanelBox：
  - 标题：`日前电价预测 · 小时（24 点）`
  - 副标题：`目标日 ${targetDate} · 锚点 ${to}`
  - 工具栏：`目标日 <input type="date" :max="today">`，改日期只重取小时这一路
  - SourceBadge：`官网·24点`
  - foot：`锚点 D=${to} · 历史 ${contextPoints} 点 · 模型 ${model} · 耗时 ${timeMs}ms`
  - 图注两段：
    1. 「数据滞后约 T-6：锚点 D 与目标日 X 不同是正常的；预测以 D 为起点滚动到 X。」
    2. 「本图口径为主页日前价，与上方日均图的『发电侧日前加权平均电价』定义不同，不可对齐、不可加总。」

### 6.3 图表

只画 24 个 x 点（0~23），最多 4 条线：

- x 轴：`00:00 … 23:00`（按 `forecast[].atTime`）。
- 实际腿：`actual` 非空时画实线（灰色），与预测同 x 轴。
- 预测腿：`point` 虚线品牌色，`quantiles['0.1']` / `['0.9']` 灰色点线。
- 预测与实际都按数组下标对齐，**不按长度推算**缺失点。

## 7. 错误处理

| 场景 | 行为 |
|---|---|
| `source` 与 `grain` 不匹配 / 取值未知 | `400`，消息列出 source 允许的 grain |
| `source=gd_metric_csv` | 合法参数，但停更守卫会返回「96 点序列不可用…实际值止于 2026-10-02」；这不代表源可用 |
| `targetDate` 格式错 / 晚于今天 | `400` |
| X 之前没有完整 24 点日 | 报「24 点电价数据不足：目标日 YYYY-MM-DD 之前没有完整的 24 点运行日」 |
| 锚点前连续窗口缺某天/某小时 | 报具体日期；不静默跳过 |
| `(X-D)×24 > 512` | 报「目标日距锚点 N 天，超过模型单次上限 21 天，请把目标日往前调」 |
| X 的 24 点不全 | 不报错，`actual` 返回空数组，图只画预测 |
| 预测服务不可用 | 沿用「预测服务失败」；日均图与其他面板不受影响 |
| 留痕写失败 | 沿用 best-effort：告警但不挡预测结果 |
| `market=real` | 取 `realtime_price`，指标名随市场切换；页默认只请求 `ahead` |

## 8. 文档

`docs/data-source-matrix.md`：

- 加一行「# 15 小时粒度电价预测（官网主页 24 点）」。
- 补口径：官网**信息披露接口**没有分时是真的；官网**主页网关**有 24 整点，是另一路。
- 写明 24 点滞后 T-6、口径与日均不同、不复活 96 点、不替代日频回测真值。

## 9. 测试

### Go 单测

`price_hour_test.go`（新建）：

1. 锚点选择：X 之前最近完整日；X 之前最近一天只有 17 点 → 锚点再往前一天。
2. `(X-D)=1` 与 `(X-D)=6` 两种锚点距离都出 24 点，且 `targetTime` 落在 X。
3. 窗口缺一天 → 报错且消息含日期。
4. `(X-D)×24 > 512` → 报错，消息含天数与上限。
5. `market=ahead` 取 `AheadPrice`，`market=real` 取 `RealtimePrice`。
6. `actual`：X 满 24 点 → 24 个值；X 不足 → 空数组（不是 24 个 0）。
7. `formatTargetTime(hour, ts)` → `YYYY-MM-DD HH:00`。
8. 留痕：`generatedOn` = 锚点 D；`Source` 文案为 `官网主页24点分时`。

`price_any_test.go` 补 hour 解析与格式断言；现有 day/intraday 断言不改。

`price_source_test.go`（新建）：

1. `grain=hour` → `source=home24h`；`source=home24h` → `grain=hour`。
2. `source=home24h&grain=day` → 报错，消息含允许的 grain。
3. 都不给 → `spot_metric` + `day`。
4. 未知 source / 未知 grain → 报错，不回落默认。
5. 替换 `priceLoaders` 假实现，断言三路 source 各命中自己的 loader。

### 前端检查

- `web/scripts/browser-check.mjs`：决策支持页 `min: 3 → 4`，新增文本断言 `小时（24 点）`。
- `web/scripts/smoke.mjs`：决策支持页断言新增小时图标题与「锚点」字样。
- 手动验证：目标日选今天 → 有预测；目标日选一个已有数据的过去日 → 有预测 + 实际线。

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
- `OpenLEMS/services/s_db/internal/s_db_init_f.go`（仅注释）
- `OpenLEMS/application/api/forecast/v1/forecast_price.go`
- `OpenLEMS/application/internal/controller/forecast/forecast_v1_price.go`
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
- 本 spec

## 11. 风险与回滚

- **风险：T-6 滞后导致「目标日今天」实际是 6 步滚动预测**，精度低于真正的日前预测。
  图上必须同时显示 D 与 X，且文案不承诺精度。
- **风险：口径混用**。两条电价曲线定义不同，图注必须写明不可对比/不可换算。
- **风险：源站改版/停采**。只影响本面板，日均图与其他页面不受影响。
- **风险：留痕被覆盖**。同锚点 D 反复刷新只留最后一次；若将来要保留多版本，唯一键需加 as-of 维度
  （届时另开改造，不在本次）。
- **回滚**：前端删新 PanelBox 与第三路请求；后端删 `grain=hour` 分派与 loader、`targetDate` 参数。
  数据表 `gd_home_24h` 与采集任务保留，不动。
