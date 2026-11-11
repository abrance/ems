# gdpm：广东电力交易指标（官方 API -> ems 业务库）

广东电力交易中心门户 `pm.gd.csg.cn` 的公开披露接口，无登录、无凭据、限速 0.7s。

**2026-11-11 起，官方 API 是广东电力市场数据的唯一权威源。** 原先两条数据源作废：

| 旧数据源 | 处置 | 说明 |
| --- | --- | --- |
| OCR 日报（`gd_spot_metric` 里 `origin=manual` 的 804 行，2026-09-14~09-25） | **标记作废但保留**：`origin='void'`，逐条写 `gd_spot_metric_audit`（`operation=deprecated_by_pm_api`） | 可查、可追溯、不删行 |
| 第三方 CSV（`gdmetric`，`https://file.xiaoyxq.top/gd_metric.csv`） | **已停更**：`gd_metric.enabled=false`，只读本地缓存、不再向第三方站点拉取 | 见文末「怎么恢复/停用 gdmetric」 |

## 三段式

```
① 抓取落文件   python3 gdpm_dataset.py            -> data/gdpm/*.csv
② 入业务库     python3 gdpm_sqlite.py --db ... --apply
③ 读路径       forecast/price.go、spotreport/trend.go（阶段 B：复用现有接口，只换源）
```

抓取复用 `gd_pm_fetch.py`（端点、索引、限速、树缓存都在那边，不重写）。
`gd_pm_fetch.py --date` 仍可用于单日排查；`gdpm_import.py` 是历史上写 VictoriaMetrics 的
脚本，**不再是权威路径**，保留仅作参考。

## ① gdpm_dataset.py：抓成能直接打开的文件

```
python3 gdpm_dataset.py                 # 默认输出 ../../data/gdpm/
python3 gdpm_dataset.py --only 现货      # 只拉名字含子串的模板（调试）
```

16 次请求拉完索引里所有指标模板（每表整表一次请求）。产物：

| 文件 | 内容 |
| --- | --- |
| `raw/<templateUuid>.json` | 源站原始返回 + 字段定义（事后可追溯） |
| `spot_day_ahead_<年>.csv` | 日前日频宽表，一天一行，**列名保持源站中文原样**，按年分文件 |
| `spot_realtime_<年>.csv` | 实时日频宽表，同上 |
| `metrics_<slug>.csv` | 月频/年频/静态模板（中长期、绿电、零售、HHI、Top-m、企业信息） |

月频/年频里进业务库的是这 8 个（`metrics_*.csv` 与 dataset 的对应写在 `gdpm_sqlite.py: PERIODIC`）：

| 文件 | dataset | 粒度 |
| --- | --- | --- |
| `metrics_mid_long_term_month.csv` | `mid_long_term_month` | 月 |
| `metrics_mid_long_term_year.csv` | `mid_long_term_year` | 年 |
| `metrics_green_power_month.csv` | `green_power_month` | 月（按交易序列拆行） |
| `metrics_green_power_year.csv` | `green_power_year` | 年（按交易序列拆行） |
| `metrics_green_power_retail.csv` | `green_power_retail` | 月 |
| `metrics_energy_retail.csv` | `energy_retail` | 月（按交易类型拆行） |
| `metrics_hhi.csv` | `hhi` | 年 |
| `metrics_top_m.csv` | `top_m` | 月 |

`metrics_grid_company*.csv` / `metrics_exchange_*.csv` 是**工商登记信息**（企业全称、统一社会信用代码、
股东持股），没有时间维度，不是时序指标，因此不入 `gd_spot_metric`；要展示时直接读 CSV。
| `gdpm_long.csv` | 日频长表 `date,dataset,scope,metric,value,unit,source`，②的输入 |

CSV 用 `utf-8-sig`，Excel 双击打开不乱码。

**不抓**：`强制或自愿退出且公示生效后的市场经营主体名单`（售电公司 8.5k 行、电力用户
78 万行）。它是主体名单不是指标，`gdpm_dataset.py` 里 `SKIP_NAME` / `MAX_ROWS` 明确跳过。

## ② gdpm_sqlite.py：入 `gd_spot_metric`

```
python3 gdpm_sqlite.py --db /path/db.sqlite3 --report /tmp/report.json          # 只算不写
python3 gdpm_sqlite.py --db /path/db.sqlite3 --report /tmp/report.json --apply
```

写库口径（与 OCR 日报既有约定一致，已用生产库核对）：

| 项 | 规则 |
| --- | --- |
| `origin` | `pm-api`（新值） / `void`（作废的旧值） |
| `source` | `gdpm-api` |
| `extractor_version` | `gdpm-1` |
| 日前 | `report_date = run_date = 数据日`（日报日 D 的日前出清落在 D） |
| 实时 | `run_date = 数据日`，`report_date = 数据日 + 2`（T+2 发布） |
| 电价 `unit` | `元/兆瓦时`（源站表头写 `元/MWh`，入库统一成中文单位） |
| 电量 `unit` | `MWh` |
| `dataset` | 日前 `day_ahead_trade` / 实时 `realtime_trade`；月频年频见上表 |
| `scope` | 日频 `发电侧` / `用电侧`；绿电与零售用交易序列名 / 交易类型；中长期/HHI/Top-m 为空 |
| 月频年频期次 | `run_date` 落**期初第一天**（`2026-10` → `2026-10-01`，`2025` → `2025-01-01`）。不能把 `2026-10` 原样写进 `run_date`：字符串比较下它小于 `2026-10-05`，月频行会漏进日频区间查询 |
| `metric` | 源站中文指标名去掉单位括号，如 `发电侧日前加权平均电价` |
| `conf` | 1.0（官方直采，非提取） |

重跑幂等：`INSERT ... ON CONFLICT(report_date,run_date,dataset,scope,metric) DO UPDATE`，
作废只对 `origin NOT IN ('pm-api','void')` 的行生效，所以第二次跑不会重复写审计。

采集元数据落 `gd_pm_fetch_run`（`UNIQUE(target_date,dataset,extractor_version)`），
`payload` 存当天源站数值，`status` 记 ok。

### 与 OCR 旧值的关系

只有这两个指标两边同义（实测差 < 0.01，OCR 只是四舍五入），写进报告的 `overlap_diff`：

- OCR `day_ahead_trade/发电侧/平均电价` ↔ `发电侧日前加权平均电价`
- OCR `realtime_trade/发电侧/平均电价` ↔ `发电侧实时加权平均电价`

**同名不同义，不要当新值**（报告里单列 `overlap_notes`）：

| OCR | 官方 | 差异 |
| --- | --- | --- |
| `最高电价` / `最低电价` | `发电侧日前加权最高/最低电价` | OCR 是市场限价维度（实测固定 -50 / 1800），官方是加权极值（实测 351 / 515） |
| `day_ahead_decl/平均电价` | `日前申报均价` | 简单均价 vs 加权均价，OCR 系统性高 2%~4% |

官方表**没有**「燃煤/燃气/新能源」分电源类型拆分，也没有节点电价、申报容量、地市档位。
这些指标切换后不再有官方更新，历史 OCR 行保留为 `void` 供追溯。

## ③ 读路径（已接）

`ListMetricsByRunDateRange` 的 origin 过滤放开到 `manual + pm-api`（`void` 排除），
`spotreport.IsConfirmed` 同步把 `pm-api` 算作可信值 —— 否则切源后历史复盘页会整片空白。

| 消费方 | 取数 |
| --- | --- |
| `GET /api/spot-market/board`（信息披露页） | 日频全套 + 月频年频全套 + 环比/同比/峰谷价差/日前-实时价差，`internal/logic/spotmarket` |
| `forecast/price.go`（日均电价预测，未来 7 天） | 同一份 `gd_spot_metric`，指标名同时认 `平均电价`（OCR）与源站原名 `发电侧日前加权平均电价`/`发电侧实时加权平均电价` |
| `spotreport/trend.go`（历史复盘） | 同上，`origin in (manual, pm-api)` |
| 信息披露页前端 | **只读** `/api/spot-market/board`，不再调用 `/api/gd-metric` |

`/api/gd-metric*`（第三方 CSV）仍在，但只服务「96 点分时序列」相关的页面
（复盘 / 策略反演 / 分时预测）；官方披露接口没有分时曲线，这条读路径暂时无法替代。

## 每周更新（手动执行一次）

用户每周手动跑一次，**没有定时任务**：

```bash
cd <repo> && python3 OpenLEMS/tools/gdpm/gdpm_dataset.py \
  && python3 OpenLEMS/tools/gdpm/gdpm_sqlite.py --db <ems 的 out/db.sqlite3> --apply \
     --report /tmp/gdpm-weekly.json
```

注意：

- 抓取（`gdpm_dataset.py`）会整表重拉所有模板，约 16 次请求、几十秒；`--only 现货` 只拉日频。
- 入库是**幂等**的：`ON CONFLICT ... DO UPDATE`，重复跑不会多出行、不会重复写审计。
- 实时（`realtime_trade`）按 **T+2** 发布，所以库里实时序列天然比日前少两天，不是缺数据；
  第二周再跑一次，上周的价格会补齐。
- `--apply` 之前想先看会写什么：去掉 `--apply` 加 `--report`，就是 dry-run（只算不写）。
- 生产库务必先拷副本验证（`kubectl cp` 出来跑 dry-run + apply），确认行数再动真库。

## 怎么停用 / 恢复 gdmetric 第三方 CSV

开关是配置 `gd_metric.enabled`（`application/manifest/config/config.yaml`，默认 `true`），
代码在 `application/internal/logic/gdmetric/`：

- `false`（当前值）：不向第三方站点发请求，只读本地 `out/gd_metric.csv` 缓存。
  本地没缓存时接口直接报「通道已停用，且本地无缓存」，不会静默返回空图。
- `true`：行为回到历史（每 `refresh_hours` 拉一次，ETag 协商）。
- 也支持环境变量 `GD_METRIC_ENABLED=false` 覆盖，便于容器里临时切换。

## 保留的旧路径：VictoriaMetrics

`gdpm_import.py` 把日频指标写成 prometheus 行协议推给 vminsert（`p_vm` 插件）。
它依赖服务端 30d retention，且绕过 ems 服务层，没有 unit/source/审计，**不是权威落点**。
VM 里的 `gdpm_*` 若要留作镜像就双写，查询一律走 `gd_spot_metric`。

## 测试

```bash
cd tools/gdpm
python3 -m unittest test_gdpm_import test_gdpm_sqlite -v

# 官方数据驱动的 7 天电价预测（需要可达的 TimesFM 服务）
cd ../../OpenLEMS/application
GDPM_CSV=../../data/gdpm/gdpm_long.csv FORECAST_BASE_URL=http://<predictor> \
  go test -run TestLoadPricePMApiForecastSevenDays -v ./internal/logic/forecast/

# 看板（区间过滤 / void 排除 / 价差配对）—— 用临时 sqlite，CI 里也跑
go test -v ./internal/logic/spotmarket/
```
