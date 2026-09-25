# 电力现货趋势图（首页新卡片）

- 日期：2026-09-25
- 状态：已确认（口径与形态已与业务逐条确认）
- 上游：[2026-09-22-spot-report-data-extract/design.md](../2026-09-22-spot-report-data-extract/design.md)

## 目标

把逐日存下来的现货指标聚合成**以周 / 以月为单位**的时间序列，在首页新增一张趋势卡片画出来，
能一眼看出「均价在涨还是在跌、高价档位占比有没有升高」。

## 挂载位置

首页 `.dashboard-landscape` 是 `flex-direction: column`（gap 13，overflow-y auto，实测宽 1568），
现有子节点：`#spot-home-block.spot-home`（现货卡）、`.dashboard-landscape-row1`、`.dashboard-landscape-row2`。

新卡片 `#spot-trend-block.spot-trend` 插在现货卡**后面**（`homeBlock.nextSibling`），同样整行宽。
由现有 `sync()` 与 `MutationObserver` 里的 `ensureHome()` 同位调用 `ensureTrend()`，幂等：
只在卡片不存在时创建；数据只在「参数变了或还没加载过」时重新拉（`sync` 每 1.2s 跑一次）。

## 工具条

```
电力现货趋势   [本周] [本月] [自定义]  2026-09-01 → 2026-09-25   [按日|按周|按月]   已排除未复核 83 项
```

- **本周** = 本周一 → 今天（ISO 周，与「按周」桶同一口径，不会错位）
- **本月** = 本月 1 日 → 今天（默认）
- **自定义** = 两个原生 `<input type="date">`，`max` = 今天，`from > to` 自动纠正，改完即刷新
- 粒度默认按区间跨度推断：`≤62 天 → 按日`、`≤400 天 → 按周`、更长 `→ 按月`；用户可随时手切
- 区间内桶数 < 2（如「本周 + 按周」）时图下给一句提示，不静默改用户选择
- 没有翻页按钮：快捷 + 自定义已覆盖

## 聚合口径（业务确认）

1. **时间轴用 `run_date`（运行日）**，不是报告发布日。分桶：日 `2026-09-20`、周 ISO `2026-W39`、月 `2026-09`。
2. **日前成交只取 `表1(二)`**：同一运行日的 `表2(一)` 是滞后 T-2 的复核值（少新能源、无环比），
   两路同时进来同一天会出现两个点。实时成交只有 `表2(二)` 一路，照用。
3. **只看已确认（`origin='manual'`）**：未复核整行剔除，桶里没有点就不画（不做补洞）。
   卡片角标显示区间内因此被排除的条数。
4. 每桶每序列输出 `avg / max / min` 与参与天数；同键同一天的重复行只算一次。
5. **申报容量结构**不用平均 `share_pct`，而是桶内 `Σ容量 ÷ Σ申报容量` 重算，避免小样本天数拉偏。
6. **地市档位**（`node_price_peak/valley`）是分类值不是价格：输出「各档位城市-天计数」。
7. 缺天（未采集）直接不出现该桶 —— 不补洞、不做回补（业务确认「没有就先置空」）。

## 接口

```
GET /api/spot-report/trend?from=2026-09-01&to=2026-09-25&grain=day|week|month

缺参 / 非法 / from>to → 兜底「本月 1 日→今天」
跨度 > 730 天 → 400「时间范围最多 730 天」

→ { from, to, grain, excludedPending, points: [
     { bucket, from, to, dataset, scope, metric,
       avg, max, min, sum, days, unit, sharePct,
       value, textValue, count }   // 后三个仅地市档位用
   ] }
```

扁平点数组：6 张图共用一种形状，前端按 `dataset/scope/metric` 取数。

## 分层

聚合在 Go（`logic/spotreport/trend.go`），接口只回聚合点。理由：去重、ISO 周桶、占比重算是要写单测的
口径逻辑；放 SQL 里没法测，放前端则要传全量明细。

| 层 | 改动 |
|---|---|
| s_db | `ListMetricsByRunDateRange`（SQL 里就过滤 `origin='manual'`）、`CountUnconfirmedByRunDateRange` |
| logic | 新增 `trend.go`：`NormalizeTrendRange` / `DefaultTrendGrain` / `ParseTrendGrain` / `BucketOf` / `BuildTrend` / `ToTrendPoints` |
| api + controller | `GetSpotReportTrendReq/Res` + handler |
| overlay | `spot-overlay.js` 趋势卡片与自绘 SVG、`spot-overlay.css` |

## 6 张图（卡片内两列）

| # | 图 | 数据 |
|---|---|---|
| ① | 日前成交均价 | `day_ahead_trade` × 发电侧/燃煤/燃气 × 平均电价 → 3 条折线 |
| ② | 实时成交均价 | `realtime_trade` × 同上 |
| ③ | 发电侧极值带 | `day_ahead_trade` 发电侧 平均/最高/最低 → 均值线 + 极值带 |
| ④ | 申报容量结构 | `day_ahead_decl` × 零价及以下 / 报价600以上 × 煤机/气机/新能源 容量占比 → 100% 堆叠柱 |
| ⑤ | 正文摘要 | `summary` × 市场 × 日前加权平均电价/燃煤均价/燃气均价 → 3 条折线 |
| ⑥ | 地市档位分布 | `node_price_peak` / `node_price_valley` × 档位 → 城市-天计数堆叠柱（高峰/低谷各一张，共 7 张卡片） |

图表全部自绘 SVG（`viewBox` 640×190，折线/极值带/堆叠柱），零依赖 —— 宿主页面离线，
公网 CDN 不可靠（详见 2026-09-22 spec 的分层说明）。hover 提示用 SVG `<title>`。

实现中定下的两个细节：

- 档位点**不以城市为维度**（城市是样本，计入 `count`），分组键用「桶 + 数据集 + 档位区间」，
  否则每个城市会各成一个序列；档位计数不做「同一运行日只算一次」的去重（城市-天都是样本）。
- 自定义区间起止颠倒时**对调**两个输入框并回显，不把区间压成一天（看着像查询失败）。

## 测试

- `trend_test.go`：ISO 周桶键、`表2(一)` 被剔除、未复核被剔除、`avg/max/min`、同天重复行只算一次、
  容量占比按 Σ 重算、档位计数、跨度上限与兜底、空数据
- 前端：`node --check` + 本地 Playwright 注入 overlay 实测卡片渲染（沿用 28713b2 的手法）

## 不做（YAGNI）

自动采集 / 定时采集、历史 URL 批量回补、图表库、CSV 导出、容量加权均价、25 城逐城趋势、翻页按钮、日期选择器之外的筛选器。
