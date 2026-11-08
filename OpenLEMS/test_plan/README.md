# 项目级集成测试场景

跨模块的端到端场景放这里。规范见 `docs/testing-standard.md`。

## 运行

```bash
cp .env.integration.example .env.integration   # 首次，填本机中间件
./script/test-integration.sh                   # 全部
./script/test-integration.sh IT-PRICE-01       # 单个场景
```

脚本只连接 `.env.integration` 里配置的中间件，不负责启停。缺依赖会直接失败并提示。

## 场景索引

| ID | 场景 | 依赖 | 关联 | 状态 |
|---|---|---|---|---|
| IT-PRICE-01 | 电价写入 → 落库 → 缓存 → 按时段取价（含电价类型枚举互逆） | 无（sqlite） | `s_price`、`s_db` | 已实现：`services/s_price/test_plan/` |
| IT-DEVICE-01 | 设备采集 → 指标计算 → 页面所需数据结构 | DB、Modbus 从站 | `s_driver`、`gdmetric` | 待编写 |
| IT-AUTH-01 | 会话登录 / 静态 token → 角色判定 → 受保护接口 | DB | `middleware_auth` | 待编写 |
| IT-CONFIG-01 | `APP_PROFILE` + 环境变量优先级 → 生效配置 | 无 | `manifest` | 待编写 |

新增场景：在本目录建 `<scenario>.md` + `<scenario>_test.go`（首行 `//go:build integration`），然后登记到上表。

## 文件约定

```
test_plan/
├── README.md                  # 本索引
├── price-sync-to-api.md       # 场景说明：前置、步骤、期望、清理、关联
└── price_sync_to_api_test.go  # 可执行实现
```

- 编号：`IT-<模块>-<两位序号>`。
- 数据隔离：库/schema 或主键前缀 `it_<场景ID>_`，结束清理，失败保留到 `out/test-artifacts/`。
- 场景之间不允许有顺序依赖。
