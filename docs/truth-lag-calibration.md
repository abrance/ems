# 真值发布滞后（T+N）判定手册

> 2026-11-14 定稿。**这份文档只给判断依据，不改任何代码。**
> 阅读者：下一次要动 `firstPairDate.ts` 里那个偏移量的人。
> 先读 §4 的门禁，再决定要不要改。

## 0. 一句话

复盘页「首个可配对交易日」的偏移（日前 `+1`、实时 `+3`）**目前没有任何增量数据支撑**：
`gd_spot_metric` 的日频采集历史上只跑过一次全量回填，之后一次都没增量跑过，
所以「源站到底 T+0 还是 T+1 发布」这件事在库里**无法证伪也无法证实**。
要判定只有一条路：让采集按日跑满 3 天，用 §2 的 SQL 读推进量。

同时必须分清两件互相独立的事，混在一起就永远判不清：

| 现象 | 原因 | 归属 |
| --- | --- | --- |
| 页面上的「首个可配对日」比真实可配对日早/晚 | 偏移量取错（源站节奏） | 本文件 §3 |
| 页面明明过了那个日子还是空的 | **本地没去采**（`tools/gdpm` 是每周手动跑） | `tools/gdpm/README.md:169`「每周更新（手动执行一次）」 |

现在页面卡住的**主因是第二行**，不是第一行。偏移量改对了，采集不跑，页面照样空。

---

## 1. 为什么现在判不了（取证）

### 1.1 采集只跑过一次，没有增量语义

线上库快照 `/tmp/yui-live-db/live.sqlite3`（`max(log.created_at)` = `2026-11-13 22:45:45+08:00`）：

```
$ sqlite3 live.sqlite3 "select count(*) total, count(distinct created_at) batches, \
    min(created_at), max(created_at) from gd_pm_fetch_run;"
1499|1|2026-11-11 16:27:21|2026-11-11 16:27:21
```

1499 行采集元数据、覆盖 10 个 dataset、`target_date` 从 2023-01-01 到 2026-11-11，
**全部在同一秒写入**。这是一次性全量回填，不是按日增量。

`gd_spot_metric` 侧同口径：

```
$ sqlite3 live.sqlite3 "select count(*) rows, count(distinct updated_at) distinct_updated \
    from gd_spot_metric;"
6811|22
```

22 个不同的 `updated_at` 全部落在 `2026-11-11 16:27:2x`，同一批。

**所以 `created_at - report_date` 这个差不能当发布节奏用。** 任何一天的差值都只是
「回填那天离这个数据日多久」，跟源站什么时候发布毫无关系。

### 1.2 已知的、可靠的只有「日期偏移」这一层

`report_date` 与 `run_date` 的关系是采集脚本自己写进去的常量，不受采集时点影响：

```
$ sqlite3 live.sqlite3 "select dataset, cast(julianday(report_date)-julianday(run_date) as integer) offset_days, \
    count(*) n from gd_spot_metric where origin='pm-api' group by dataset, offset_days;"
day_ahead_trade|0|4080
realtime_trade|2|1356
```

实现见 `tools/gdpm/gdpm_sqlite.py:204`：

```python
def report_of(dataset, day):
    """实时是 T+2 发布；日前与日报同日。"""
    return shift(day, 2) if dataset == "realtime_trade" else day
```

这条能证明的只是「源站把 D 日的数据挂在 D 日的报告下（实时挂在 D+2 下）」，
**不能证明「D 日几点能拿到」**。而复盘页要的是后者：留痕 `generated_on = D`、
`target_time = D+1`，页面在 D+1 那天究竟能不能配上对，取决于 D+1 的实际值
什么时候出现在库里。

### 1.3 留痕侧也没有满 3 天

```
$ sqlite3 live.sqlite3 "select id, created_at, substr(content,1,60) from log \
    where content like '%留痕已写入%' order by id;"
15764|2026-11-13 20:41:57|每日电价留痕已写入：ahead 生成 2026-11-13 / 目标 2026-11-14（432.13
15765|2026-11-13 20:41:58|每日电价留痕已写入：real  生成 2026-11-13 / 目标 2026-11-14（472.78
15874|2026-11-13 21:43:19|每日电价留痕已写入：ahead 生成 2026-11-13 / 目标 2026-11-14（432.13
15875|2026-11-13 21:43:20|每日电价留痕已写入：real  生成 2026-11-13 / 目标 2026-11-14（472.78
```

留痕任务 11-13 才第一次成功写入，而且那天写了两次（重启补跑，唯一键去重后不堆行）。
**11-11 起的 3 个生成日里，没有一个走完过「生成 → 目标日 → 真值可用」的完整链路。**

### 1.4 结论

| 问题 | 现在能不能答 |
| --- | --- |
| 源站的 `report_date` 与 `run_date` 什么关系 | 能，见 §1.2，是常量 |
| 日前实际值在目标日当天几点入库 | **不能**，没有增量样本 |
| 实时实际值是不是真的等到 T+2 | **不能**，同上 |
| 当前偏移 `+1` / `+3` 对不对 | **不能判定**。`+1`/`+3` 是照 §1.2 的常量推的，未经实测 |

---

## 2. 判定 SQL：连续 3 天记推进量

### 2.1 前置：先把采集按日跑起来

判定需要**至少 3 个不同的采集日**，且第一天的全量回填必须排除。

```bash
# 每天固定时刻跑一次（不要在一天内跑多次：同一天多次只会覆盖，不产生新的 created_at）
cd <repo> && python3 OpenLEMS/tools/gdpm/gdpm_dataset.py --only 现货 \
  && python3 OpenLEMS/tools/gdpm/gdpm_sqlite.py --db <ems 的 out/db.sqlite3> --apply \
     --report /tmp/gdpm-daily.json
```

调度落在常驻容器里（该机没有可用 crontab，见 `deploy/docker-compose/README.md:64`），
和备份容器 `ems-backup` 同一套做法。

### 2.2 判定 SQL（日前）

```sql
-- 每个采集日「最新拿到哪个运行日」，以及推进量 L
-- L = 采集日 - 最新运行日
--   0 → 目标日 D+1 当天就有值 → 偏移应为 +1
--   1 → 目标日 D+1 要等到 D+2  → 偏移应为 +2
--   2 → 要等到 D+3             → 偏移应为 +3
WITH first_seen AS (
  SELECT run_date, MIN(date(created_at)) AS fetch_day
  FROM gd_spot_metric
  WHERE dataset = 'day_ahead_trade'
    AND metric  = '发电侧日前加权平均电价'
    AND origin  = 'pm-api'
  GROUP BY run_date
)
SELECT fetch_day,
       MAX(run_date)                                   AS newest_run_date,
       CAST(julianday(fetch_day) - julianday(MAX(run_date)) AS INTEGER) AS advance_days
FROM first_seen
GROUP BY fetch_day
ORDER BY fetch_day;
```

用 `created_at` 不用 `updated_at`：`gdpm_sqlite.py:453` 的
`ON CONFLICT ... DO UPDATE` 只改 `updated_at`，`created_at` 保留首次入库时刻 ——
这正是「某数据日第一次被看到」的定义。

**排除回填日**：输出的第一行就是 2026-11-11 那次全量回填，它的 `advance_days`
会是 0（因为回填把当天数据一起拉了），**不能当样本**。判据：只认第 2 行起、
且相邻两行 `fetch_day` 相差 1 天的连续 3 行。

### 2.3 判定 SQL（实时）

同样的 SQL，把 `dataset` 换成 `realtime_trade`、`metric` 换成
`发电侧实时加权平均电价`。预期 `advance_days = 2`（与 `report_of()` 的 T+2 一致），
若实测是 2，则实时偏移 `+3` 成立，不必再改。

### 2.4 读法

以 3 个连续采集日为例（采集日 D1 < D2 < D3，最新运行日 R1 < R2 < R3）：

| 观测 | 判定 | 偏移 |
| --- | --- | --- |
| `R3 = D3`（当天数据当天就有） | 源站 T+0，本地采集当天完成 | **+1** |
| `R3 = D3 - 1` | 源站 T+1（或当天发布太晚，采集赶不上） | **+2** |
| `R3 = D3 - 2` | 源站 T+2 | **+3** |

三条样本必须**同向**才下结论。若 3 天里出现两种推进量（例如第一天 0、第二三天 1），
说明发布时刻不稳定 —— 那是「源站当天发但发得晚」，偏移要按**最坏值**取，不是平均值。

> 补充：`advance_days` 只测「日期粒度」。真实能不能在目标日 00:05 就配上对，还取决于
> 源站当天几点发布。若实测 `advance_days = 0` 但发布时刻常在下午，页面在目标日上午仍会是空的 ——
> 这不是偏移错了，是 `daily_record.go` 的执行时刻（00:05）早于源站发布时刻。
> 那种情况下**不要改偏移**，改的是「页面该说『今天的数据还没发』而不是『首个可配对日已过』」。

---

## 3. 结论怎么落地

### 3.1 若实测 `advance_days = 0`（日前 T+0，即现有 `+1` 正确）

**不要改偏移。** 但要改的是「把原因说清楚」，因为页面卡住的真实原因是本地没采：

| 文件:行 | 现状 | 要改成 |
| --- | --- | --- |
| `OpenLEMS/web/src/utils/firstPairDate.ts:17-19` | 「配对点目前最多每周前进一次」 | 采集若已改为按日跑，这句过期，改成按日的口径；若仍是手动跑，保留并补一句「这是页面卡住的主因」 |
| `OpenLEMS/web/src/pages/scene/ReviewView.vue:197` `truthLagNote` | 日前分支 `'目标日数据待入库'` | 正确，不改 |
| `OpenLEMS/web/src/pages/scene/ReviewView.vue:193` `truthLagText` | 日前分支 `'日前市场的官方实际值当天就出'` | 若实测发布时刻偏晚，改成 `'日前市场的官方实际值当天发布'`（去掉「就出」的时刻暗示） |
| `OpenLEMS/web/src/utils/dailyLag.ts:20` | `if (lag <= 2) return 't2'` | **需要单独决策**：这条阈值是给「官方正常 T+2」设的，而它判的 `board.date` 走的是**日前**序列（`spotmarket/board.go:317` `latestDate` 只取 `DatasetAhead`）。若日前确认是 T+0，阈值 2 会把「停采 2 天」误判成正常。改成 `lag <= 0`（或按 dataset 分档）是另一件事，**不要和偏移量一起改** |

### 3.2 若实测 `advance_days = 1`（日前 T+1，`+1` 偏乐观）

| 文件:行 | 现状 | 改成 |
| --- | --- | --- |
| `OpenLEMS/web/src/utils/firstPairDate.ts:26` | `return String(market ?? '').trim() === 'real' ? 3 : 1` | `... ? 3 : 2` |
| `OpenLEMS/web/src/utils/firstPairDate.ts:8` | `- ahead（日前，默认）：T+0 ⇒ 目标日 D+1 当天就能配对，偏移 +1` | `- ahead（日前，默认）：T+1 ⇒ 目标日 D+1 的实际值要到 D+2，偏移 +2` |
| `OpenLEMS/web/src/utils/firstPairDate.ts:5-6` | 「日前与日报同日」推导 | 补一句实测样本日期与 `advance_days` |
| `OpenLEMS/web/src/pages/scene/ReviewView.vue:186` | 注释 `日前 T+0 → +1` | `日前 T+1 → +2` |
| `OpenLEMS/web/src/pages/scene/ReviewView.vue:193` `truthLagText` | 日前分支 `'日前市场的官方实际值当天就出'` | `'日前市场的官方实际值 T+1 才发布'` |
| `OpenLEMS/web/src/pages/scene/ReviewView.vue:197` `truthLagNote` | 日前分支 `'目标日数据待入库'` | `'官方 T+1'`（与实时分支的写法对齐） |
| `OpenLEMS/web/scripts/smoke.mjs:1802` | `firstPairDate('2026-11-13','ahead') === '2026-11-14'` | `=== '2026-11-15'`，标签改「日前 = 启用日 + 2」 |
| `OpenLEMS/web/scripts/smoke.mjs:1810` | `'2026-11-29' → '2026-11-30'`（日前跨月） | `→ '2026-12-01'` |
| `OpenLEMS/web/scripts/smoke.mjs:1812` | `'2026-12-30' → '2026-12-31'`（日前跨年） | `→ '2027-01-01'` |
| `OpenLEMS/web/scripts/smoke.mjs:1823` | `'2026-10-31' → '2026-11-01'`（月末） | `→ '2026-11-02'` |
| `OpenLEMS/web/scripts/smoke.mjs:3400` | `shiftDay(1)`（日前首个可配对日） | `shiftDay(2)` |
| `OpenLEMS/web/scripts/smoke.mjs:3405` | 断言「日前市场的官方实际值当天就出」 | 改断言文本为新的 `truthLagText` |
| `OpenLEMS/web/scripts/smoke.mjs:3409` | 断言「（目标日数据待入库）」 | 改断言文本为「（官方 T+1）」 |
| `OpenLEMS/web/scripts/smoke.mjs:3431` | 切回日前断言 `shiftDay(1)` | `shiftDay(2)` |
| `OpenLEMS/web/scripts/smoke.mjs:3569` | 首个可用生成日 → `shiftDay(1)` | `shiftDay(2)` |

### 3.3 若实测 `advance_days = 2`（日前 T+2）

与 §3.2 同一张表，但所有 `+2` 换成 `+3`（即日前与实时同偏移）。这种情况下
`firstPairDate.ts:26` 的整个 `lagDays` 分叉可以删掉，直接 `return 3`；
`ReviewView.vue:192-197` 的两组文案也可以合并 —— 但那属于重构，**单独一个 PR 做**。

### 3.4 实时市场

若 §2.3 实测 `advance_days = 2`，`+3` 正确，不动。若实测是 1，则实时改 `+2`，
改动点与 §3.2 同表（把 `real` 分支的 `3` 换成 `2`，以及对应文案与 smoke 断言）。

---

## 4. 门禁：拿到 3 天增量样本之前，不要再改这个偏移量

**硬性。** 理由：

1. 偏移量是**用户可见承诺**（「首个可配对交易日约在 X」）。改错的方向是双向的：
   说早了 → 用户以为系统坏了；说晚了 → 用户以为数据齐了其实没有。
2. 现在改任何值都是拿 §1.2 的常量推导猜的，猜对了也没有证据留在仓库里，
   下一次有人质疑还是得重推一遍。
3. `+3 → +1` 那次（PR #170）已经有代价：偏移改对了，页面还是空的，因为
   采集停着。**先修采集，再校准偏移**，顺序反了就是白改。

改动前的检查清单：

- [ ] `gd_pm_fetch_run` 有 ≥ 4 个不同的 `date(created_at)`（1 个回填 + 3 个增量）
- [ ] §2.2 的 SQL 输出里，排除回填日后有 3 行连续 `fetch_day`
- [ ] 这 3 行的 `advance_days` 一致（或按最坏值取）
- [ ] 把该 SQL 的原始输出贴进 PR 描述（不是口头结论）
- [ ] `npm run build && npm run smoke` 全绿（smoke 读 `dist/`，必须先 build）

---

## 5. 附：偏移量与采集间隔的合成关系

页面能配上对的那一天 = `generated_on + 采集间隔 + 源站滞后`。
两个量互相独立，但用户只看到一个日期：

| 采集方式 | 源站滞后 | 用户实际等多久（相对生成日 D） |
| --- | --- | --- |
| 每周手动（现状） | 任意 | **最多 +7 + 偏移**，且不可预测（取决于人什么时候想起来跑） |
| 按日跑（目标） | T+0（+1） | +1 |
| 按日跑 | T+2（+3） | +3 |

**这也是为什么 `firstPairDate.ts:17-19` 那句注释必须留着的反面教材**：
它写的是「配对点目前最多每周前进一次」—— 这句是对现状的诚实描述，
一旦采集改成按日跑就过期了，届时要连同 §3.1 的表一起更新。

---

## 本次没做的事

- 没有改任何代码（本文件是唯一新增文件）
- 没有触发采集（源站可达性与调度落点未确认，见 `deploy/docker-compose/README.md:64`）
- 没有现场抓 `pm.gd.csg.cn`：本次未验证源站当前可达性
