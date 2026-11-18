# gd-spot：广东现货 15 分钟数据补齐

`fetch_gd_metric.py` 从第三方现货站（`DIANCHACHA_BASE_URL`）拉广东现货 15 分钟数据，
按 **`gd_metric.csv` 的原格式**导出 —— EMS 首页「电网日指标」读的就是那份文件
（`config.yaml` 的 `gd_metric.url`，由 `GD_METRIC_URL` 注入）。

## 为什么需要它

那份 CSV 的上游就是这个接口。逐点核对过：同一 `日期/时间/typeId` 的数值与 CSV **完全一致**
（2026-08 整月 3360 行字符串级对拍、0 处不一致），所以用它补缺口是**无缝**的，
不是另接一套数据。

上游停更时（例如 2026-09-01 起 CSV 卡在 08-31）、或需要把某段历史重新导出时用它。

## 用法

```bash
cd tools/gd-spot
export DIANCHACHA_BASE_URL=https://<host>
cp creds.json.example creds.json      # 填 authorization / jwt / cookie / aesKey / aesIv

# 先看服务端最新到哪天（免登录，不消耗凭证）
python3 fetch_gd_metric.py --check

# 补 09-01 ~ 10-06 三类，只留实际值（sourceId 9）
python3 fetch_gd_metric.py --from 2026-09-01 --to 2026-10-06 --source 9 --out patch.csv

# 合并进已有 CSV：跳过里头已有的点，输出全量
python3 fetch_gd_metric.py --from 2026-09-01 --to 2026-10-06 --append-to gd_metric.csv --out gd_metric_new.csv
```

凭证怎么取：浏览器打开 `$DIANCHACHA_BASE_URL/transaction-new/trade-data`，
DevTools → Network → 找任意 `trade/power/data/spot` 请求，复制请求头的
`Authorization`、`JWTHeaderName`，以及 Cookie。

## 输出

列与 `gd_metric.csv` 完全一致：

```
时间戳,日期,时间,typeId,类型,单位,数值,sourceId
```

- `typeId` 21 日前电价 / 22 实时电价（元/兆瓦时）、23 负荷（MW）
- `sourceId` 9 = 广东电力交易中心（实际值）、999 = 电查查（预测值）
- 时间轴 96 点/天（00:15 ~ 24:00），`24:00` 的 `时间戳` 归到次日 `00:00:00`
- 逐点只保留一行：同一 `(日期,时间,typeId)` 优先实际值，否则预测值 —— 与原 CSV 约定一致
- 默认写 UTF-8 BOM（EMS 的解析器 `gdmetric/file.go` 会 strip），`--no-bom` 可关

## 注意

- **凭证只活十来分钟 / 十几个请求**：401/403 会被识别成凭证失效，脚本**立刻停手不重试**
  （只对 5xx / 网络错误重试 2 次）。换 `creds.json` 再跑。
- **一次请求取整段区间，不要逐天发**：接口单次能返回整月。逐天发（31 请求/月/类型）会被风控 403。
  脚本按 31 天分块，块间默认停 3 秒（`--delay` 可调）。
- 请求头里的 AES key/iv（`w*qYPqDydSIEdc#*` / `mLPeKAH6psn9dnS2`）来自该站前端公开的 JS chunk，
  响应体是 AES-128-CBC + PKCS7 的 base64。对方接口非公开 API，仅内部工具使用。
- `creds.json` 已在仓库根 `.gitignore` 里，不要提交。
