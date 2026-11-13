# P1 改动点（文件级）

目标：实时价 96 点主预测从「TimesFM 三通道」改为「已发布日前价为主，TimesFM 只做残差修正/回退」，去掉两路推理。
验收：2026-08-01~10-01 回测，实时价 WAPE 23.2%（或同窗口复现值）→ **<18%**，推理调用量降 2/3。
验收用 `tools/price-backtest/backtest.py`（见同目录 README），**先有数再改代码**。

所有行号对应 `main @ 6e5feb4`。

## 1. `OpenLEMS/application/internal/logic/forecast/price_intraday.go`（主改动，唯一业务文件）

| 位置 | 现状 | 改动 |
|---|---|---|
| L242-L256 `priceCovariateKinds` | `"real": {KindDayAhead, KindLoad}`，日前价当**历史区间内**协变量送模型，注释自认「目标日日前价需服务端支持未来协变量」 | 删掉 `"real"` 这条（map 只留空/删变量），`buildPriceInputChannels` L264 因此对 real 直接返回单通道。三通道推理停用 → 调用量降 |
| L103-L130 `loadPriceIntradayAsOf` | 只读「目标日之前」的实时价历史，窗口 `[today-historyDays+1, today]` | 新增一段：**另读一次** `gdmetric.GetSeries(from, to+horizonDays, KindDayAhead)`，作为目标时段的日前价（这就是「未来协变量」，不需要服务端支持，`series()` 只是按日期取已存在的行）。拿不到就整段为空，走回退 |
| L211-L219 点预测装配 | 每点直接取 `item.Point[i]`，分位取 `quantileAt(item.Quantiles, …)` | 每点按优先级取值：① 目标时刻日前价（存在即用）② 日前价缺失时用 TimesFM 点预测 ③ TimesFM 也失败时用持久化（上一实际点同时段值）。分位：①路径用 `日前价 + 残差分位`（见下）；②沿用服务端分位 |
| L33-L46 `TPriceForecastPoint` | 只有 `Point/Q10/Q50/Q90` | 加 `Source string`（`"dayahead"` / `"tsfm"` / `"persist"`）。看板要统计回退率，否则 WAPE 掉了也说不清是改对了还是恰好那几天好 |
| L49-L70 `SPriceForecastIntraday` | `InputChannels` 语义 = 送模型的通道数 | 保留字段，real 市场被删协变量后恒为 1；`Model` 写 `"dayahead-primary+tsfm-fallback"`，便于留痕区分改前改后 |
| L306 `alignChannels` | 多通道对齐 | 保留不动（dayAhead 市场仍在用；实时不再走多通道，但函数不删，避免影响单测） |

**残差修正接法（阶段一先留钩子，默认关闭）**：新增

```go
// residualAdjust 日前价残差修正：返回给日前价加上的偏移（分位用偏移分位）。
// 阶段一返回 0，主预测就是纯日前价；阶段二换成模型/分位回归，只改这一个函数。
func residualAdjust(ctx context.Context, market string, loc *time.Location, base []float64, target []time.Time) (median, q10, q90 []float64, err error)
```

实现先取「最近 14 天同时段 (实时价 − 日前价)」的中位数/q10/q90（`gdmetric.GetSeries(..., KindRealtime)` 已有实际值就够，实时实际值有 352 天）；任一阶段出错就返回全 0 并 `c_log.Warningf`，**不许让残差修正挡住主预测**。

**回退链不能断（硬约束）**：TimesFM 服务不可用 / 超时 / 返回空时，只要日前价在，就必须仍出结果（L207-L210 现在是「预测服务失败直接 return error」，改后这个 error 只在日前价也缺时才抛）。三档全缺 → 报错文案沿用「15 分钟实时电价数据不足」。

## 2. `OpenLEMS/application/internal/logic/forecast/price_realdata_test.go`

现测「real 走三通道」。改后断言：real 市场 `InputChannels==1`、`Model` 含 `dayahead-primary`、`Points[i].Source` 非空；补一条「日前价缺失时 Source==tsfm」的用例（可用 testdata 裁剪，别打网络）。

## 3. 受影响但只读确认（不改逻辑）

- `backtest.go` L195：走同一个 `loadPriceIntradayAsOf`，改完自动生效；注意它的 `maxBacktestPoints=6` 抽样，别用它出验收数（验收用 `tools/price-backtest`）。
- `price_any.go` / `price.go` 的 `defaultQuantiles{0.1,0.5,0.9}` 不动。

## 4. 顺带确认的 P2 事实（给 pit，不影响 P1 排期）

`plugins/plug_policy/p_policy_mircogrid/internal/p_policy_mircogrid_main_s_f.go:215` 现在是
`ForecastLoad(ctx, client, id, 0, Horizon, []float64{0.5})` 然后 `s.forecastPowers = item.Point`——
**`item.Point` 是点预测，不是分位**。所以 P2 不是「0.5 改 0.9」一行：要改成送 `0.9` 并读 `item.Quantiles["0.9"]`（键是字符串，`s_forecast/internal/s_forecast_types_s.go:83-90`），取不到时回退 `item.Point`。
另：`ForecastHorizon` 默认 4（`p_policy_mircogrid_config_s.go:26`）= 未来 **4 点 ≈ 1 小时**，用 1 小时的 q0.9 卡需量上限，对「今天下午会不会顶超」基本无信息——P2 的验收里除了超限次数，建议加「提前削峰的有效提前量」，否则改分位可能只改了个安慰。

## 边界

- 业务文件（`price_intraday.go`、policy 插件）改动权归 pit；本文件只列改动点，我不提交这些文件。
- 本目录（`tools/price-backtest/`）与指标口径归 sor，pit 不自行改口径；要改口径先改 README 的口径表并说明验收数怎么变。
