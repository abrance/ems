# EMS 测试规范

适用范围：本仓库（`OpenLEMS` 各 Go 模块、`emsdevice`、`gd-price-collector`、`web`）的全部变更。
目标：把测试投入放在**最容易出错、最难靠人眼验证**的地方，而不是堆行覆盖率。

一句话：**单测少而准，集成测试多而真。**

---

## 1. 总原则

1. 单测证明「这段逻辑算得对」，集成测试证明「这套系统跑得通」。
2. 判断标准是**理解成本和故障代价**，不是函数行数覆盖率。
3. 每个 `test_plan/` 场景都必须能被一条命令跑起来；只有确实无法自动化的场景才用人工步骤记录，并在用例里写明原因。
4. 测试代码和业务代码同等待遇：命名清楚、失败信息能直接定位问题、不写「跑通就行」的空断言。
5. 不为了覆盖率写测试。一个没有断言价值的用例比没有更糟——它让 CI 变慢，还给人虚假的安全感。

---

## 2. 单元测试（`*_test.go`，与被测代码同包）

### 2.1 必须写

| 场景 | 例子 |
|---|---|
| 复杂算法、计算、指标口径 | `application/internal/logic/effect`、`gdmetric`、`s_effect`、`s_forecast` |
| 协议编解码、寄存器映射 | `p_modbus`、`p_canbus`、`s_export_modbus` |
| 状态机、调度、重试、幂等 | `s_automation`、`s_policy` |
| 枚举 ↔ 存储 ↔ 外部接口的**互逆契约** | `s_price` 电价类型（历史事故：`string(枚举)` 落库全变平电） |
| 鉴权 / 权限 / 安全边界 | `application/internal/utils/middleware_auth` |
| 每个已修复 bug 的回归用例 | 命名 `TestXxx_RegressionIssue<编号>` 或在注释里写清事故 |
| 并发、边界值、浮点容差、时区/夏令时 | 涉及金额、电价、SOC、功率的计算 |

### 2.2 不写

- **100 行以内的函数和简单算法函数**，不单独写单测。
- 纯转发、getter/setter、结构体字段映射、DTO 转换。
- GoFrame 生成代码（`api/`、`dao`、`controller` 样板）和框架本身的行为。
- 需要数据库、Redis、MQTT、Modbus 从站、真实端口、真实时间的逻辑——这些归集成测试。

### 2.3 硬约束

- 单测**零外部依赖**：不连库、不连网、不开固定端口、不读生产配置。
- 不 `sleep` 等真实时间：时间、随机数、外部客户端都要可注入。
- 单个模块 `go test ./...` 在无网络、无 DB 的 CI 上必须通过，总时长控制在 60s 以内。
- 允许使用的例外只有一处：`httptest` / `g.Server` 监听随机端口（`:0`）测试 HTTP 中间件，用完必须关闭。

### 2.4 100 行规则的例外

行数不是唯一标准。以下情况即使函数很短，也必须写单测：

- 曾经出过线上/联调事故（回归用例）；
- 两个模块之间的数据契约（枚举、错误码、单位、时区）；
- 鉴权、越权、脱敏、密钥处理；
- 并发访问或共享状态。

---

## 3. 集成测试（`test_plan/`）

`test_plan/` 是**集成测试用例目录**，用来存放典型业务场景，不是单测的补充仓库。

### 3.1 放在哪一层

按「这个场景牵涉到谁」决定位置，就近放置，不要都堆到仓库根：

```
ems/
├── OpenLEMS/
│   ├── test_plan/                     # 项目级：跨模块端到端
│   ├── application/test_plan/         # 应用层：HTTP API + 鉴权 + 配置 + DB
│   ├── services/s_price/test_plan/    # 模块级：本服务 + 真实 DB/外部接口
│   ├── services/s_db/test_plan/
│   └── ...
├── emsdevice/test_plan/               # 设备侧协议/采集链路
└── gd-price-collector/test_plan/      # 抓取链路（真实来源可用录制样本替代）
```

- **项目级**：电价同步 → 落库 → API 返回 → 前端所需字段；设备采集 → 策略计算 → 下发指令。
- **模块级**：本服务对真实中间件的行为（唯一键冲突、事务、批量写入、分页）。
- **分层级**：鉴权中间件 + 真实 session/静态 token、配置加载优先级、日志落盘。

### 3.2 目录内约定

```
test_plan/
├── README.md                       # 场景索引表：ID / 场景 / 依赖 / 命令 / 负责人
├── price-sync-to-api.md            # 场景说明（唯一事实来源）
└── price_sync_to_api_test.go       # 可执行实现
```

- 用例编号：`IT-<模块>-<两位序号>`，例如 `IT-PRICE-01`。
- Go 测试函数按编号命名，`-` 换成 `_`：`func TestIT_PRICE_01_PriceSyncToAPI(t *testing.T)`。
- 每个 Go 文件首行：`//go:build integration`。保证普通 `go test ./...` 不会因为缺中间件而失败。
- 场景 Markdown 必须写：前置数据、执行步骤、期望结果、清理方式、关联需求或事故编号。
- 一个场景一个文件对（`.md` + `_test.go`），不做「大杂烩」测试。

### 3.3 写用例的要求

- 走真实链路：真实 DB、真实 HTTP 路由、真实配置加载。只允许 mock 系统外部第三方（第三方 HTTP 接口、OCR、支付、短信）。
- 不允许 mock 自己的数据库层——SQL、唯一键、事务、时区正是最容易出问题的地方。
- 断言业务结果（落库值、接口字段、状态变化），不断言日志文本。
- 每个场景自带数据隔离：独立库/schema 或表名/主键前缀 `it_<场景ID>_`，结束时清理；失败时保留现场。
- 场景之间不得有顺序依赖，单跑和全跑结果一致。

---

## 4. 运行方式：脚本 + `.env`

集成测试**只连接** `.env` 指定的中间件，**不负责启动/停止**它们。Docker Compose、测试库、MQTT broker 由人或 CI 预先准备好。

### 4.1 命令

```bash
cp .env.integration.example .env.integration   # 首次
$EDITOR .env.integration                       # 填本机中间件地址
./script/test-integration.sh                   # 跑全部场景
./script/test-integration.sh IT-PRICE-01       # 只跑一个场景
```

- `.env.integration` 已在 `.gitignore` 中（`.env` 同规则），凭证绝不入库。
- 脚本在缺变量、必填项为空、连不通、`TEST_ENV=prod` 时**直接失败并打印修复提示**，不静默跳过。

### 4.2 环境变量

模板见仓库根 `.env.integration.example`。

| 变量 | 说明 | 默认 / 必填 |
|---|---|---|
| `TEST_ENV` | 环境标识，只允许 `local` / `dev` / `ci` | `local` |
| `LOG_LEVEL` | 日志级别，同时作用于应用 logger 与测试输出 | `warn` |
| `TEST_DB_DRIVER` | `sqlite` / `mysql` / `postgres` | 必填 |
| `TEST_DB_DSN` | 连接串或 sqlite 文件路径 | 必填 |
| `TEST_DB_SCHEMA` | 独立 schema/库名，供隔离使用 | 必填（mysql/pg） |
| `TEST_REDIS_ADDR` / `TEST_REDIS_DB` | Redis 地址与库号 | 场景需要时必填 |
| `TEST_MQTT_BROKER` | MQTT broker 地址 | 场景需要时必填 |
| `TEST_MODBUS_TCP_ADDR` | Modbus TCP 从站地址 | 场景需要时必填 |
| `TEST_HTTP_BASE_URL` | 端到端场景访问的运行实例 | 端到端场景必填 |
| `TEST_TIMEOUT` | 单次运行超时 | `10m` |
| `EMS_TOKEN` | 线上/预发接口冒烟用的静态 token，**只从环境变量读，不写进任何文件** | 需要打线上接口时必填 |
| `TEST_ARTIFACT_DIR` | 失败现场目录 | `out/test-artifacts` |

鉴权：打线上或预发接口一律用静态 token（`X-API-Token` / `Authorization: Bearer`），不要用账号密码登录换会话；
写法、风险、下发链路与轮换见 [`api-static-token.md`](api-static-token.md)，配套脚本 `tools/api-test/ems_api.sh`。

变量到运行时配置的映射集中在测试工具包里完成，**不要在每个用例里各写一套**：

| `.env` 变量 | 对应 `application/manifest/config/config.yaml` |
|---|---|
| `TEST_DB_DSN` | `database.default.link` |
| `LOG_LEVEL` | `logger.level` |
| `TEST_HTTP_BASE_URL` | 端到端场景的 `server.address` 目标 |

### 4.3 隔离与清理

- 每次运行生成独立命名空间（`it_<场景ID>_<runid>`），运行结束删除。
- 成功后清理产物；失败保留日志、SQL、响应体到 `TEST_ARTIFACT_DIR/<场景ID>-<时间戳>/`。
- 禁止在 `.env.integration` 里指向任何线上地址；脚本会拒绝 `prod`、线上域名和默认 80/443 的公网目标。

---

## 5. CI 策略

| 时机 | 跑什么 | 要求 |
|---|---|---|
| PR / push | `go-ci`（vet + 单测 + 构建）、`web-ci`（typecheck + build + 冒烟 + 浏览器检查）、`integration-test`（`test_plan` 中不依赖外部中间件的场景） | 必须绿，才允许合入 |
| PR 涉及 DB / 接口 / 配置 / 部署 | 对应 `test_plan` 场景由提交者在本地按脚本跑通，PR 描述贴命令与结果 | 由 reviewer 核对 |
| 每日夜间 | 托管 runner 上跑全部 `integration` 场景（`.github/workflows/integration-test.yml`） | 失败开 issue，不阻塞他人 PR |
| 发版前 | 全量集成场景 + 关键链路手工验收 | 必须通过 |

PR 上只跑「不依赖外部中间件」的场景（如 IT-PRICE-01 用 sqlite，与生产同款内嵌库），这样托管 runner 上不会出现假失败。
需要真中间件的场景（MySQL/Redis/MQTT/Modbus）不进 PR 门禁，只在夜间或手动触发时跑：连接参数放仓库 secret
（`TEST_DB_DRIVER` / `TEST_DB_DSN` / `TEST_DB_SCHEMA` / `TEST_REDIS_ADDR` / `TEST_MQTT_BROKER` / `TEST_MODBUS_TCP_ADDR`），
流水线会自动把它们写进 `.env.integration`；secret 缺失时退回 sqlite。
新增中间件依赖时，同步更新 `.env.integration.example` 和 `.github/workflows/integration-test.yml`。

---

## 6. Review 检查清单

提交者自查，reviewer 按此核对：

- [ ] 新增/修改的复杂逻辑有单测；简单函数没有被硬凑单测。
- [ ] 修 bug 的 PR 带回归用例（哪怕只改 3 行）。
- [ ] 涉及 DB 写入、事务、唯一键、时区、外部接口、配置加载的改动，有对应 `test_plan/` 场景。
- [ ] 新增场景带 `.md` 说明，编号在 `test_plan/README.md` 索引里登记。
- [ ] 集成测试用 `.env.integration`，没有硬编码地址、账号、库名。
- [ ] 没有把单测写成「必须连本地库才能过」。
- [ ] 没有为了通过测试而放宽断言或跳过失败用例（`t.Skip` 必须写原因和关联 issue）。

---

## 7. 存量迁移计划

当前仓库约 78 个 `*_test.go`，绝大多数是有价值的单测（算法、协议映射、鉴权、前端冒烟），不需要重写。按以下顺序补齐：

1. **P0（本次）**：本规范、`test_plan/` 目录约定、`.env` 模板、运行脚本。
2. **P1**：补 3 条主链路场景——电价同步落库、设备采集到指标展示、鉴权与配置加载。
3. **P2**：夜间集成流水线；把 `s_db` 中依赖真实 SQL 行为的用例迁入 `services/s_db/test_plan/`。
4. **P3**：按季度清理低价值单测（无断言、重复覆盖、只测框架行为）。

迁移原则：先加不删。确认新场景覆盖了旧用例的保护目标后，再删除旧用例。
