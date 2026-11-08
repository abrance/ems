# IT-PRICE-01 电价写入 → 落库 → 缓存 → 按时段取价

关联：`s_price`、`s_db`（`price` 表）。实现：`price_sync_to_api_test.go`。

## 为什么要这个场景

电价是全站计费与策略的输入，而它的落库路径出过一次静默事故：`priceTypeLabel` 写成 `string(枚举)`，
把整数当码点转成控制字符，消费侧 `ParsePriceType` 匹配不上任何分支就一律取「平电」——
**所有落库的电价类型都变成平电，且没有任何报错**。这个场景锁的就是
「DB 里的字符串 ↔ 枚举」以及「按时间取到正确时段」这条链路。

## 依赖

- 无需外部中间件：用 sqlite（与生产 `config.yaml` / `config-prod.yaml` 同款内嵌库）。
- 库文件来自 `TEST_DB_DSN`；相对路径按测试进程工作目录（即 `services/s_price`）解析，建议写绝对路径。
  不填则落在 `TEST_ARTIFACT_DIR` 下的临时库。

## 步骤

1. 建库建表：走生产同一条 `s_db.Init()`。
2. 清空 `price` 表，保证场景可重复（场景之间不允许有顺序依赖）。
3. 用 `s_db.GetPriceService().CreatePrice` 写入一条长期有效、覆盖五个电价类型的电价。
4. 读回原文，断言 `price_segments` 里没有 `\x01` 一类控制字符。
5. `s_price.RefreshPriceCache`（生产同一条 DB → 缓存路径）。
6. 在 01:00 / 09:00 / 14:00 / 19:00 / 22:00 分别取时段，断言类型与电价。
7. `GetCurrentActivePrice` 断言优先级与描述。

## 期望

| 检查点 | 期望 |
|---|---|
| 写入 | 返回自增 ID > 0 |
| 落库原文 | 枚举名（`Valley`/`Peak`/…），不含控制字符 |
| 01:00 / 09:00 / 14:00 / 19:00 / 22:00 | 依次 Valley 0.32、Flat 0.68、Peak 1.12、Sharp 1.45、DeepValley 0.19 |
| 激活电价 | 优先级 1、描述与写入一致 |

## 清理

用例结束删除本场景写入的电价；库文件由 CI 的临时工作目录丢弃，本机可手工删 `out/it-ci.sqlite3`。

## 运行

```bash
./script/test-integration.sh IT-PRICE-01
```

CI：`.github/workflows/integration-test.yml`，PR / push main / 每日夜间 / 手动触发。
