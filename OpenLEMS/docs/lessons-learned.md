# 工程经验与排查手册

**文档名称**：OpenLEMS 工程经验与排查手册
**日期**：2026-10-08
**范围**：本轮前端重构（`web/`）与其牵动的后端 / CI / 部署改动

这份文档记录的是**踩过的坑与对应的排查办法**，不是设计文档。每条都有在案的事故，
写下来是为了「下次遇到同样的症状，不用再从零猜一遍」。

## 怎么用

| 你要做的事 | 看哪一节 |
| --- | --- |
| 排查一个「页面/接口不对」的问题 | §1 排查手册（按症状查） |
| 写检查 / 写测试 / 判断「改好了没」 | §2 检查纪律 |
| 改前后端接口或响应拆包 | §3 契约坑 |
| 改构建、发版、部署 | §4 §5 |
| 改认证、权限、默认口令 | §6 |
| 让 agent 或脚本帮你改代码 | §7 操作纪律 |

---

## 1. 排查手册（按症状查）

| 症状 | 先查什么 | 常见根因 |
| --- | --- | --- |
| **页面整片空白**（有顶栏，主区什么都没有） | 浏览器控制台；或在前端路由里跳过去并**提前装 error 钩子** | ① 组件 `setup` 抛异常（如 `null.length`、`undefined.toFixed`）——**页面不会有任何提示**；② 请求被服务端某个静态目录前缀接走 → 404 |
| **接口有数据，但界面是空的** | 分清「数据状态」还是「渲染」：KPI 有数而图空白 = 渲染问题；连 KPI 都是「—」= 数据没到 | ① 图表容器被换掉后实例还挂在旧节点上；② 组件没拿到数据（看请求 URL 的区间参数） |
| **接口报「数据不足（0 点）」** | 请求里的时间窗（`from`/`to`）与数据实际覆盖范围 | **写法把「今天」当锚点**，而数据滞后（CSV 只到某天）→ 窗口整段为空 |
| **某个前缀下的页面全 404**（如 `/config/*`） | `curl -s -o /dev/null -w '%{http_code} %{content_type}'` | 服务端为旧前端保留了静态目录绑定，与新的 SPA 路由撞了 |
| **打错一次密码就被踢回登录页** | 看响应的**body** 里的 `code`，不是 HTTP 状态 | 业务错误被当成 401（会话过期）。GoFrame 中间件恒写 HTTP 200，错误码在 body |
| **401 之后界面一直转圈、不跳登录页** | 前端 401 判定是否只看 `res.status` | 后端把 401 写在 body 里（HTTP 200） |
| **改设置「保存成功但不生效」** | 全仓 `grep` 这个设置 ID 的**读取点** | 设置写了没人读（死设置）。同类历史问题：保留天数、`password_length`、会话时长 |
| **改了源码、测试全绿，但用户看到旧界面** | 产物是否与源码同步 | 交付物（`web.zip`）是构建产物：忘了打包 / 镜像用的是旧产物 |
| **推送 tag 报 `fatal error in commit_refs`** | —— | GitHub 服务端瞬时故障：**别改东西，等一会儿重试** |

查询常用命令：

```bash
# 线上页面是不是 SPA（对比一个已知正常的路径）
curl -s -o /dev/null -w '%{http_code} %{content_type}\n' https://<host>/scene/disclosure
curl -s -o /dev/null -w '%{http_code} %{content_type}\n' https://<host>/config/info

# 容器跑的是哪个镜像 / 起于何时
ssh <host> "docker inspect <container> --format 'image={{.Config.Image}} started={{.State.StartedAt}}'"

# 前端产物里有没有某段代码（判断部署的是不是新构建）
curl -s https://<host>/ | grep -oE '/assets/index-[A-Za-z0-9_-]+\.js'
curl -s https://<host>/assets/<chunk>.js | grep -c '<特征串>'
```

---

## 2. 检查纪律：一个检查必须能证明结论

### 2.1 四个「假通过」（都在案，都是自己骗自己）

| 当时用的检查 | 真实情况 |
| --- | --- |
| 断言「图表数据点数 > 0」→ 认为图渲染了 | 冒烟跑在 happy-dom 里，**没有 canvas**，`echarts.init` 直接抛错 —— 这个断言根本没碰到渲染 |
| 断言「ρ 显示样本不足」 | 看到的是**数据加载前**的占位文案，不是算出来的结果 |
| 等「请求已发出」→ 断言页面文案 | 文案是在请求 **resolve 之后**才赋值的，之前通过纯属运气 |
| 断言「请求体正确」 | Vue 把监听器挂在元素上：**已移出 DOM 的按钮被 click 仍会提交** —— 是错的原因让它通过 |

**结论**：写完检查要能回答「为什么它证明了结论」。答不上来就是在给自己发假证书。

### 2.2 现在的四层检查，各挡一类

```bash
cd OpenLEMS/web
npm run typecheck      # 类型 / 引用
npm run smoke          # happy-dom 加载 dist：逐页点过 + 行为断言（逻辑与契约）
npm run browser-check  # 无头 Firefox 真加载：每个 .chart 必须有 canvas（渲染）
npm run verify         # = 上面三个 + build
```

外加 Go 侧一条（挡服务端路由）：

```bash
cd OpenLEMS/application && go test ./internal/cmd/ -run TestSPARoutesServeIndexHTML
```

**为什么非要真浏览器那一层**：happy-dom 没有 canvas，「图画没画出来」在冒烟里根本不存在。
两个只有真浏览器能发现的 bug：

1. 图表在数据到达前挂载 → 只画首帧，之后不重绘
2. 容器被换成新 DOM 节点（面板加载中不渲染 slot）→ 实例还挂在**脱离文档的旧节点**上
   → 容器空、整页没有 canvas、**且不报错**

**检查要自带原因**：browser-check 的注入桩会在应用代码之前装
`error` / `unhandledrejection` / `console.error` 钩子；失败时打印
`h1` / canvas 数 / 页面空态文字 / JS 异常。
否则只知道「0 个图」，而**组件 setup 抛异常的表现就是整页空白且不报错**。

**测边界要写清楚**：browser-check 是本地起 `dist`，**绕过 Go 路由** ——
「服务端把某个前缀接走」这类问题它天生看不到，所以才有上面的 Go 路由测试。

### 2.3 变异验证：改代码看测试是否变红

没做过变异的测试都是猜。本轮对每个关键断言都做了一次：

```
把 PAVA 的合并条件去掉        → 供应链清理测试变红
把 clampAnchor 改回「今天」    → 96 点预测测试复现「数据不足（0 点）」
把 skipped 归一化撤掉         → 预测复盘 3 条断言失败（夹具给了 null）
把 /config/* 静态绑定加回去    → Go 路由测试报 404
把储能柜趋势循环改回 members   → browser-check 报「储能柜 0 个图」
不注册 ScatterChart           → browser-check 报 getProgressive + 0 canvas
```

**有一次变异没抓住**：我把期望值写成硬编码常量（`EXPECTED_PAGES`）而不是真实导航页数，
于是「加了页没打包」这个场景正好测不出来。改成比真实值后立刻抓住 —— 这也是变异验证的价值。

### 2.4 夹具必须与真实后端一致

夹具不一致会让检查**因为错的原因通过**：

| 夹具问题 | 后果 |
| --- | --- |
| 遥测桩只返回 `ess_1`，而设备列表里没有这台设备 | 新页面拿不到遥测（修夹具，不是改断言） |
| `skipped: []`（真实是 `null`） | 掩盖了「读 `null.length` 崩整页」 |
| `/api/forecast/load` 少了 `{ forecast: ... }` 外壳 | 页面取不到 `timeMs` → 崩 |
| 桩把业务错误的 HTTP 状态写成 401（真实是 200 + body code 61） | 页面被踢回登录页，后续断言全在登录页上「通过」 |
| 断言依赖 `NAV[0]` 序号 | Tab 顺序变过（加了「运行监控」），按序号断言会跟着一起错 → 改成按 key 找 |

---

## 3. 前后端契约的坑

### 3.1 Go 的 nil 切片序列化成 `null`，不是 `[]`

```go
type SPriceBacktest struct {
    TargetDates []string   // 没有数据时 → JSON null
    Skipped     []string   // 同上
}
```

前端读 `backtestMeta.skipped.length` → **组件 setup 抛异常 → 整页空白且无提示**。

**修法：在 api 层统一归一化**（`web/src/api/*.ts`），别指望每个页面记得加：

```ts
return api.get<PriceBacktest>('/api/forecast/backtest', params, signal).then((r) => ({
  ...r,
  targetDates: r.targetDates ?? [],
  skipped: r.skipped ?? [],
  points: r.points ?? [],
}))
```

涉及接口：`forecast/backtest`、`forecast/price`、`forecast/recorded`、`forecast/load`、
`gd-metric/series`、`gd-metric/supply-curve`、`effect/backtest`、`gd-metric`。

### 3.2 `?.` 只护住一层

```ts
f?.historyDates.length ?? 0   // historyDates 是 null 时：null.length 照样抛
f?.historyDates?.length ?? 0  // 这样才对；但更好的做法是上面那种 api 层归一化
```

### 3.3 错误码在 body 里

GoFrame 的响应中间件**始终写 HTTP 200**，把错误码放在 `{code, message, data}` 的 `code` 里：

| 场景 | code |
| --- | --- |
| 未登录 / 会话过期（中间件写） | `401` |
| 角色不足（中间件写） | `403` |
| `gcode.CodeValidationFailed` | `51` |
| `gcode.CodeNotAuthorized`（如「当前密码不正确」） | `61` |

前端**只能把 401 当会话过期**；把它和业务错误混在一起，会出现「打错一次密码被踢回登录页」。

### 3.4 接口可能有必填参数

`GET /api/price` 的 `page` / `pageSize` 是 `v:"required"` —— 不带参数探接口会拿到 500。
**探接口前先看 API 定义**，别急着把它当 bug 报。

### 3.5 窗口锚点：数据会滞后于现实日期

gd_metric.csv / 日报都可能滞后一个月以上。任何「取最近 N 天」的默认值都要**锚到数据最新可用日**，
否则窗口整段为空，报出来却像「没数据」：

```
回测失败: 区间 2026-10-01 ~ 2026-10-07 内没有完整（96 点）的日前电价数据   ← 当天是 10-07，数据只到 08-31
电价预测失败: 15 分钟日前电价数据不足（0 点，至少需要一天 96 点）
```

- 后端：`clampAnchor(asOf, latestAvailableDate(ctx))`（`internal/logic/forecast/price_intraday.go`）——
  拿不到 / 格式不对 / 不更早时**原样返回**，兜底不该挡住主流程
- 前端：`src/utils/latestRange.ts`（`anchorEndDate`），可用长窗口（如 90 天）不用改

还有一层容易漏：**后端的缺省区间和前端预设可能不是一套**。
`/spot-report/trend` 的 `from`/`to` 缺省是「本月 1 日 ~ 今天」，
而前端以前**只在自定义模式才传区间** → 预设实际查的是后端缺省。改成一律显式传。

### 3.6 组件 setup 抛异常 = 整页空白且不报错

`<script setup>` 里抛异常（或模板表达式抛），Vue 的默认表现是**什么都不渲染、界面上也没有提示**。
排查办法：在前端路由里跳到该页、提前装错误钩子，读 `err.message`。

---

## 4. 构建与交付

### 4.1 `web.zip` 是**构建产物**，不是源码

Go 侧用 `//go:embed web.zip` 把它编进可执行文件（`application/manifest/web_embed.go`），
静态服务挂在根路径。**Go 侧没有「从目录服务静态文件」的路径。**

历史事故：产物曾入库 → 源码 32 个页面 chunk、zip 里只有 16 个 → **部署出去是旧界面，
而 CI 全绿**（Go CI 用仓库里那份 zip，不重建）。

现在：产物**不入库**，构建时生成。

```
web/src ──npm run build──▶ dist ──npm run pack──▶ application/manifest/web.zip ──//go:embed──▶ 可执行文件
```

**硬约束**：`//go:embed` 要求**编译时文件存在**，缺了直接编译不过：

```
manifest/web_embed.go:12:12: pattern web.zip: no matching files found
```

所以 **Go CI 必须先 `npm ci && npm run pack`**，本地改完后端要编译时也要先跑一次。

### 4.2 打包脚本零外部依赖

`web/scripts/pack.mjs` 自己写 zip（`zlib.crc32` + `deflateRawSync`）。

原因：本机与 CI 都没有 `zip` 命令；一开始用 `python3 -c` + zipfile，
结果在 `node:22-alpine` 里 `spawnSync python3 ENOENT` —— **一个 Node 项目的打包脚本
不该依赖另一种语言运行时**。时间戳固定成 1980-01-01，于是同输入产出逐字节相同的 zip。

### 4.3 CI 覆盖全部工作区模块

Go CI 的 `defaults.run.working-directory` 是 `application`，
所以 `services/`、`plugins/`、`common/` 下约 25 个模块**曾长期没有 vet/test 覆盖** ——
一个「电价类型落库恒为平电」的 bug 就那样藏了很久（`go vet` 本来能直接报出来）。

现在 `workspace-ci` job 读 `go work edit -json` 逐个模块跑（模块清单**不手写**，
手写一份迟早和 `go.work` 不一致，而表现是「新模块静默不受保护」）。

---

## 5. 部署真相

- 生产在 `<prod-host>`（`<ems-host>`），部署目录 `<deploy-root>`
- 由 **部署仓库**（`.github/workflows/deploy.yml`）下发：
  push 到 main 且命中 `apps/**` 自动部署；**每天 03:17 UTC 做一次漂移纠正**
  → **服务器上手改会被覆盖，改动必须进仓库**
- 容器环境变量来自 `<deploy-repo>/apps/<app>/.env`：`LEMS_IMAGE_TAG` 钉镜像版本
- 发版流程：PR 合并 → 打 tag `vX.Y.Z` → 触发 `docker-publish-ghcr.yml` 构建推镜像 → 改 `<deploy-repo>` 里对应单元的 tag → 部署
- 部署后**浏览器硬刷新**（资源文件名变了，缓存的旧 `index.html` 会指向不存在的 chunk）

---

## 6. 安全

1. **默认口令**：`admin` 的出厂默认值在 `s_db_basic` 常量里（当前 `admin`；
   早期是 `100086`）。部署环境可用 `EMS_ADMIN_PASSWORD` / `EMS_USER_PASSWORD` 指定初始口令 ——
   **只影响新建的库**，已有库保留原密码（刻意如此：升级时重置用户改过的密码更危险）。
   存量库要换口令走登录后的改密接口，不是改环境变量。部署后第一件事就是改掉弱口令，**别把管理端放公网**。
2. **登录没有失败次数限制**，密码是每个角色一个共享口令。默认口令 + 公网 = 风险叠加。
3. **`AUTH_BYPASS_ROLE` 是逃生门，不是常规配置**：非空时中间件不做任何鉴权与角色校验，
   前端也会被注入 `auth-bypass` 会话。**公网环境开启等于免密管理员**。默认不要设。
4. **改密码接口的权限判定必须在后端**：目标账号**不能取自请求体**（曾经因此可以
   由普通用户改掉管理员密码），并要求校验当前密码。参见
   `internal/controller/auth/auth_change_password_rules.go`（有矩阵测试）。
5. **`/api/setting/update` 是全量覆盖**：没给的字段会被写成默认值
   （`isPublic` → false、`remark` → ""）。改值前要先 `POST /api/setting/detail`
   读回详情再合并，否则那条设置会变成非公开、之后再也读不到。

---

## 7. 操作纪律（给 agent / 脚本）

这轮我在这上面栽的次数最多，每条都是实际损失：

| 纪律 | 事故 |
| --- | --- |
| **脚本替换必须带断言**（`assert count == 1`） | 锚点写错 → 替换静默没生效，我还接着排查别的原因 |
| **别用下标切片改脚本** | `end` 匹配到错的 `})` → 删掉 150 行（连 `clickTo` 都删了） |
| **`pkill -f <串>` 会匹配到自己的命令行** | 自己把自己杀掉（三次）。只按 `argv[0]` 匹配，或扫 `/proc` |
| **别往「用户即将合并的 PR」追提交** | 用户合的是旧那版 → 修复没进 main，白坏一版。后续工作另开分支/PR |
| **`if cmd \| tail` 取的是 `tail` 的退出码** | 以为推送成功了，其实没有。要用 `$?` 或事后 `ls-remote` 确认 |
| **长跑的 python 用 `-u` + 重定向到文件** | 块缓冲 + 被 kill → 输出全丢，看着像「什么都没发生」 |

---

## 8. 产品规矩（这几条反复用到）

1. **不假装有能力**：没有数据源的块标「待数据源 / 待口径」，不画假图、不编数字。
2. **不做假控件**：改了不生效的东西宁可**标出来**也别做成控件 ——
   曾经存在的假控件：场景切换器、保留天数、`password_length`、会话时长设置。
3. **同一件事只留一个入口**：数据保留并入存储管理；设备监控并入实时数据。
4. **界面不做权限隐藏**（权威判定在后端），但要意识到这会让「点了才 403」变成体验问题。
5. **主动全量扫描**，别等用户逐页发现 —— 配置 Tab 那 7 个 404 就是这么找出来的。

---

## 9. 已知缺口（诚实清单）

| 缺口 | 说明 |
| --- | --- |
| 多场景（山西现货 / 储能套利） | **没有数据源**，页面上已写明各缺什么 |
| 按机组的策略反演 | 需机组台账 + 出清机组明细 |
| 报表导出 | 只是基础版（前端拼 CSV），无异步任务 / 历史文件 / 保留期 |
| 多用户 / 自定义角色 / 权限点 | 后端只有 `admin` / `user` 两个内置账号 |
| 长周期投资评估（年 / 15 年） | 属另一条产品线 |
| 会话时长设置 | 读取方式有 bug（`g.Cfg().MustGet(ctx, "int:"+值)` 是无效配置键），
且服务端会话上限是全局的 `SetSessionMaxAge` → per-role 做不到 |
| ~~`emsdevice` 模拟点表~~ | 已补（2026-10-08）：`emsdevice.json` 的 `devices` 段成为点表，加了光伏 / 负载 / 关口电表三张表，并给储能柜补齐可写点位；四类设备共用一个从站（地址不重叠），运行监控不再只有一台柜子 |
