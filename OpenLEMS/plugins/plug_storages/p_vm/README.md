# p_vm：VictoriaMetrics 存储插件

实现 `common/c_base.IStorage`，给 ems 接 VictoriaMetrics（集群版：vminsert 写、
vmselect 查）。配置切换方式与 `p_tsdb` 相同，在 `s_storage.NewStorageManagerWithConfig`
里按 `SStorageConfig.Type` 选后端。

## 接入

| 环境变量 | 说明 |
| --- | --- |
| `EMS_STORAGE_TYPE=vm` | 切到 VictoriaMetrics；不设仍是本地 TSDB |
| `EMS_VM_INSERT_URL` | 例如 `http://vminsert:8480`（也可直接给完整导入路径） |
| `EMS_VM_SELECT_URL` | 例如 `http://vmselect:8481`（也可直接给 `/select/0/prometheus`） |
| `EMS_VM_BASE_URL` | 可选兜底地址（`/delete_series` 用） |

也可以不经环境变量，直接构造 `SStorageConfig{Type: "vm", Params: {"insert_url":…, "select_url":…}}`。

## 写

`/insert/0/prometheus/api/v1/import/prometheus`（prometheus 文本行协议，批量 5000 行/请求）。
同一序列同一时间戳重复写是覆盖，天然幂等。

`SaveSystemMetrics` 两种命名：

- 不带 `tags`：`ems_metric{type,id,field}` —— 与 `p_tsdb` 一致，ems 已有图表不用改；
- 带 `tags`：**字段名即指标名，tags 原样做标签** —— 外部采集指标（如 `gdpm_*`）用它，
  避免把指标名塞进 label 造成高基数。

## 读

`/select/0/prometheus/api/v1/query_range`（集群版只有 PromQL，没有 InfluxQL），
返回 `c_chart.ChartData`，上层查询代码不用改。

`GetStorageData` 的 `pointKey` 每一项可以是：

- 完整选择器，例如 `gdpm_price_cny_mwh{market="dayahead",side="gen",kind="weighted_avg"}`
  （**原样**下发给 vmselect）；
- 纯字段名，例如 `cpu` -> `ems_metric{type=…,id=…,field="cpu"}` 兼容选择器。

`step` 单位毫秒；不传按日频取（86400s）。

## 注意

1. `query_range` 按 `start + k*step` 的网格返回点：日频数据要把 `start` 对齐到
   当日 00:00+08:00，否则查不到点。
2. `DeleteBefore` 是空实现：保留期由 vmstorage 的 `--retentionPeriod` 决定，客户端不删数据。
3. `GetStorageStats` 取 vmselect 的 `status/tsdb`；取不到时返回零值而不报错，免得
   存储管理页整页失败。

## 测试

```bash
go test ./...                    # 纯单测：写入 payload 格式、query_range 解析
EMS_VM_IT=1 EMS_VM_INSERT_URL=… EMS_VM_SELECT_URL=… go test ./internal -run TestVMIntegration -v
```

集成测试会真写一条探针样本再按日期读回，默认跳过。
