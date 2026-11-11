# gdpm：广东电力交易指标（日频）-> VictoriaMetrics

`gdpm_import.py` 把广东电力交易中心（`pm.gd.csg.cn`）**现货日前 / 实时**两张日频
指标表写进 VictoriaMetrics，供 ems 的存储接口按日期查回。

抓取部分直接 `import gd_pm_fetch`（同目录，来自抓取脚本原件，端点/索引/限速都不重写）：
每天 2 次请求，无登录、无凭据。限速沿用 0.7s。

## 指标命名

指标名是**数值本身**，维度只进标签（不要把一个指标的中文名塞进 label，那是高基数）。

| 指标名 | 含义 | 单位 |
| --- | --- | --- |
| `gdpm_price_cny_mwh` | 电价 | 元/MWh |
| `gdpm_volume_mwh` | 成交电量 | MWh |

标签（全部低基数）：

| 标签 | 取值 |
| --- | --- |
| `market` | `dayahead`（日前） / `realtime`（实时） |
| `side` | `gen`（发电侧） / `load`（用电侧） |
| `kind` | `weighted_avg` / `declared_avg` / `max` / `min` / `total` |
| `src` | `pm.gd.csg.cn` |

字段映射（源站字段 -> 指标 + 标签），规则在 `gdpm_import.py` 的 `series_of()`：

| 源字段 | 指标 | market | side | kind |
| --- | --- | --- | --- | --- |
| `generationSideDailyTotalVolume` | `gdpm_volume_mwh` | dayahead | gen | total |
| `consumerSideDailyTotalVolume` | `gdpm_volume_mwh` | dayahead | load | total |
| `generationSideDailyAvgPrice` | `gdpm_price_cny_mwh` | dayahead | gen | weighted_avg |
| `generationSideDailyMaxPrice` | `gdpm_price_cny_mwh` | dayahead | gen | max |
| `generationSideDailyMinPrice` | `gdpm_price_cny_mwh` | dayahead | gen | min |
| `dailyDeclarationAvgPrice` | `gdpm_price_cny_mwh` | dayahead | gen | declared_avg |
| `generationSideRealTimeTotalVolume` | `gdpm_volume_mwh` | realtime | gen | total |
| `generationSideRealTimeAvgPrice` | `gdpm_price_cny_mwh` | realtime | gen | weighted_avg |

时间戳统一取**当日 00:00:00+08:00** 的毫秒值。实时表的行里会夹带日前字段
（反之亦然），所以只写**本表声明过的字段**。

## 用法

```bash
cd tools/gdpm

# 看要写什么（不联网写库）
python3 gdpm_import.py --date 2026-11-10 --dry-run

# 写一天，写完用 vmselect 回查
python3 gdpm_import.py --date 2026-11-10 \
  --vm-insert-url http://127.0.0.1:8480/insert/0/prometheus/api/v1/import/prometheus \
  --vm-select-url  http://127.0.0.1:8481/select/0/prometheus

# 区间（幂等：同序列同时间戳重写是覆盖）
python3 gdpm_import.py --from 2026-11-05 --to 2026-11-10
```

默认地址是 cloud3 的 ClusterIP（`10.43.183.106:8480` 写 / `10.43.96.62:8481` 查），
在集群外跑时先 `kubectl -n victoria-metrics port-forward svc/...-vminsert 8480:8480`。

单测（纯函数，不联网）：`python3 test_gdpm_import.py`

## 坑

1. **`query_range` 只按 `start + k*step` 的网格返回点**。日频数据必须把 `start`
   对齐到 00:00+08:00、`step` 给 86400s，否则取不到点。
2. **instant query（`/api/v1/query`）有回看窗口**，查一天前的点会返回空 —— 校验历史点要用
   `query_range` 把窗口卡在目标时间戳上（`gdpm_import.py --verify` 就是这么做的）。
3. 源站只发布了部分日期（如 2026-11-10 实时表当天未发布）；`--from/--to` 区间里没数据的
   日期会跳过并在 stderr 说明。

## 未做（有意）

月频（中长期 / 绿电 / 零售 / Top-m）和年频（HHI）这次不进 VM：保留期还没定，
先只做日频。代码里留了 TODO，等存储位置与 retention 定了再接。
