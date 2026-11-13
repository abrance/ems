# 静态 Token 鉴权（API 测试用）

线上 API 的自动化测试、巡检脚本、CI 冒烟，**一律用静态 token，不要用账号密码去登录**。
账号密码流程要维护会话 cookie、会被验证码/密码轮换打断、失败时还容易把业务问题误判成登录问题；
静态 token 是一次请求一个头，脚本里没有状态。

> 本文只讲怎么用、怎么轮换、有什么风险。鉴权逻辑本身在
> `application/internal/utils/middleware_auth.go`，改动请走代码评审，不要照本文改实现。

---

## 1. 两种请求头写法

两种等价，任选其一；两者同时出现时命中任一即可。

```bash
# 写法 A：专用头（推荐，语义最清楚）
curl -s -H "X-API-Token: $EMS_API_STATIC_TOKEN" \
  "https://ems.xiaoyxq.top/api/spot-report?page=1"

# 写法 B：Authorization Bearer（给只支持标准头的客户端/网关用）
curl -s -H "Authorization: Bearer $EMS_API_STATIC_TOKEN" \
  "https://ems.xiaoyxq.top/api/spot-report?page=1"
```

参数规则（`matchesAPIStaticToken`）：

| 情况 | 结果 |
|---|---|
| 容器里没有 `EMS_API_STATIC_TOKEN`（未配置/已关闭） | 两种头都被忽略，请求按未登录处理（`code:401`） |
| 头缺失、为空、长度与配置值不同、内容不同 | 不命中；请求继续走会话鉴权，最终通常是 `code:401` |
| 命中 | 按 `admin` 放行，**不做任何接口级权限校验**，且**不签发 session cookie** |

比较是常量时间的（`crypto/subtle.ConstantTimeCompare`，长度不同直接判否），不会因为响应时间泄露前缀。

**判成功要看响应体里的 `code`，不要看 HTTP 状态码**：未登录时服务端返回的是
`HTTP 200` + `{"code":401,"message":"未登录或登录已过期","data":null}`。

## 2. 命中即 admin：这是最大的风险面

- 静态 token 命中后 `userRole=admin`，**全量接口可读写**，包括管理类 `POST/PUT/DELETE`。
  它不是一个「只读测试账号」，没有按接口收窄的能力。
- 因此：token 等同于一把 admin 钥匙。**只放在脚本运行环境/CI secret 里**，
  不要写进仓库、不要打进前端产物（前端 bundle 里出现就等于公开）、不要贴进工单/聊天记录、
  不要放进会归档的日志。
- 请求必须走 https；明文 http 下这个头就是明文密码。
- 排查问题时只打印 token 的前 4~6 位或它的 sha256 前缀，不要打印完整值。

已知的优先级（理解排障时有用）：

1. `AUTH_BYPASS_ROLE`（临时放开鉴权开关）优先于静态 token；该开关开着时，脚本不需要 token 也能通。
   **线上不该有它**，看到能匿名访问先查它，而不是怀疑 token。
2. 静态 token 命中后直接放行，不再看会话。
3. 标注了 `noAuth` 的公共接口（如 `/api/health`）本来就不需要 token。

## 3. Token 的下发链路

token 只在这条链路上流动，任何一环都不要手改：

```
GitHub Actions secret  EMS_API_STATIC_TOKEN
        │  cops/.github/workflows/deploy.yml → 步骤「下发运行期密钥」
        ▼
部署主机文件  <BASE_PATH>/secrets/lems-cloud3.env      （由脚本按 SECRET_ENV 声明写入）
        │  cops/scripts/deploy-k8s.sh：kubectl create secret --from-env-file
        ▼
k8s Secret  cops/lems-cloud3-secrets
        │  cops/apps/lems-cloud3/k8s.yaml：envFrom.secretRef
        ▼
容器环境变量  EMS_API_STATIC_TOKEN  （Deployment lems）
```

关键点：

- `cops/apps/lems-cloud3/app.conf` 的 `SECRET_ENV="... EMS_API_STATIC_TOKEN"` 是这条链路的开关：
  声明了才会下发，删掉就断链。它**故意不进 `REQUIRED_ENV`**——这样关闭这个特性时
  不会变成「缺变量导致部署失败」，pod 重建后只是不再带该变量。
- **emsdevice 不引用这个 Secret**，滚动重建与它无关（`K8S_ROLLOUT` 里只有引用 Secret 的工作负载才会被密钥变化牵动）。

## 4. 轮换步骤

1. 生成新 token（≥32 字节随机，例如 `openssl rand -hex 32`）。
2. 更新 GitHub secret `abrance/cops` → `EMS_API_STATIC_TOKEN`（仓库 Settings → Secrets）。
3. 触发一次部署，任选其一：
   - `workflow_dispatch` 手动运行 Deploy，`app` 填 `lems-cloud3`（只部署这一个单元）；
   - 或让 `apps/**` 的改动合入 main 顺带部署。
4. 部署脚本比对 `secrets/lems-cloud3.env` 的 sha256，写入 `cops.vectorman.cn/secret-checksum`
   注解 → Deployment `lems` 滚动；注解没变说明文件没换，别以为是「没生效」。
5. 验证：新 token 打一次 `./tools/api-test/ems_api.sh GET /api/spot-report?page=1` 返回 `code:0`；
   旧 token 立刻失效（pod 滚动完成后容器里只剩新值）。
6. 在任务系统/工单里记录轮换时间，但**不记录 token 值**。

怀疑泄露时的顺序：先轮换（第 1~4 步），再回头查访问日志。轮换的时间窗只等于一次 pod 滚动的时间。

## 5. 测试脚本

```bash
EMS_TOKEN=<token> ./tools/api-test/ems_api.sh GET "/api/spot-report?page=1"
```

- `EMS_BASE` 覆盖 base（默认 `https://ems.xiaoyxq.top`），`EMS_AUTH=bearer` 改用 `Authorization: Bearer`。
- token 只从环境变量读，脚本里没有默认值，也没有任何硬编码。
- 退出码 = 响应体里的 `code`（`0` 成功；`401` 未登录；`403` 权限不足……）。
  shell 退出码会被截断到 0~255，`401` 会表现为 `$?=145`，所以失败时脚本同时把 `code=… message` 打到 stderr，
  以 stderr 那一行为准。参数错误/网络不通时退出码是 `2`/`3`/curl 自己的码。

## 6. 相关代码与用例

| 位置 | 作用 |
|---|---|
| `application/internal/utils/middleware_auth.go` | 中间件与 `matchesAPIStaticToken` |
| `application/internal/utils/middleware_auth_static_token_test.go` | 单测：未配置时 401、两种头、读写方法均放行、不签 cookie、bypass 优先级、错误 token 回落会话 |
| `tools/api-test/ems_api.sh` | 本文配套脚本，零依赖（sh + curl） |

改鉴权逻辑时必须保持上表单测全绿，且新增行为要有对应用例（见 `docs/testing-standard.md` §2.1「鉴权 / 权限 / 安全边界」）。
