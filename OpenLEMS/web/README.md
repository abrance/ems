# LEMS Web

EMS 前端源码。企业后台布局：顶部 Tab（运行监控 / 场景 / 管理 / 配置）+ 左侧栏 + 主界面。

对应设计稿：[`.monkeycode/specs/2026-10-06-ems-frontend-shell/design.md`](../../.monkeycode/specs/2026-10-06-ems-frontend-shell/design.md)

## 开发

```bash
npm install
npm run dev        # http://localhost:5173，/api 反向代理到后端 :15880
```

后端按 dev 配置启动（`manifest/config/config-dev.yaml`，监听 `:15880`）：

```bash
cd ../application && go run . --web=true
```

## 校验

```bash
npm run verify     # typecheck + build + 壳层冒烟
```

`npm run smoke` 在 happy-dom 里加载**构建产物**，逐个点过 nav 里的 29 个页面，
断言标题、占位面板、状态提示与顶栏约束。它依赖 `dist/`，所以要先 build。
不覆盖 ECharts 的实际绘制（happy-dom 无 canvas）。

## 结构

```
src/
  nav/index.ts          导航唯一真源：Tab / 侧栏 / 路由 / 权限码 / 页面状态
  router/routes.ts      路由表由 nav 生成（纯函数，测试复用）
  router/index.ts       web history 实例
  pages/index.ts        页面组件注册表；未登记的 key 落到 PlaceholderView
  api/client.ts         统一请求：Bearer token + {code,message,data} 拆包
  api/gdmetric.ts       /api/gd-metric
  components/           AppBar / TabBar / SideNav / PageHeader / PanelBox / KpiCard / StatusTag
  layouts/AppShell.vue
  composables/useECharts.ts
  styles/tokens.css     间距 / 字号 / 圆角阶梯 + 对比度达标色板
```

加一个页面 = `nav/index.ts` 加一条 + `pages/index.ts` 注册组件 + 把该页 `status` 改成 `ready`。

## 两个刻意的决定

1. **顶栏不放流量 / CPU / 时钟**（设计稿 §2 原则 6）。这些属于「配置 · 系统信息」页。
2. **不注册 Arco 全局组件**（手动控制页验证下来仍然成立）。量过：全量
   `import ArcoVue` + `arco.css` 会让 CSS 从 **8KB 涨到 404KB**、JS 从 **108KB 涨到 872KB**。
   按需引入单组件样式（`es/<comp>/style/index.css`）可以把 CSS 控制在百 KB 内，
   但本项目自己的 tokens + 手写控件已经覆盖了「输入、下拉、开关、卡片」这些形态，
   且对比度是算过的；引入 Arco 会带来视觉二次对齐的成本。

   **留到真正需要复杂组件时再引**：树、日期区间、虚拟滚动表格。
   届时按需引入即可，`styles/arco-override.css` 已把 Arco 变量对齐到本项目色板，
   不会产生视觉漂移。

## 已实现的页面

| 页面 | 数据来源 |
| --- | --- |
| 场景 · 信息披露 | `GET /api/gd-metric`（负荷 / 日前价 / 实时价，15min） |
| 管理 · 手动控制 | `GET /api/device/name-list` `GET /api/control/custom-services` `GET /api/device/telemetry` `POST /api/device/history` `POST /api/control/device` |
| 配置 · 储能参数 | `GET /api/policy/config-fields` `GET /api/policy/config` `POST /api/setting/update` |
| 管理 · 策略管理 | `GET/POST /api/strategy/energy-storage` `PUT/DELETE .../{id}` `POST .../activate` `GET /api/effect/backtest` |
| 管理 · 电价管理 | `GET /api/price` `GET /api/price/current` `POST /api/price` `PUT/DELETE /api/price/{id}` `PUT /api/price/{id}/toggle` |
| 管理 · 采集管理 | `GET /api/spot-report` `GET .../data` `GET .../audit` `POST .../collect` `.../extract` `.../extract/batch` `.../metric` `.../confirm` |
| 场景 · 历史信息 | `GET /api/spot-report/trend` |
| 场景 · 决策支持 | `GET /api/effect/summary` `GET /api/effect/backtest` `GET /api/forecast/load` `GET /api/forecast/price` |
| 场景 · 市场出清 | `GET /api/spot-report/data` |
| 配置 · 系统信息 | `GET /api/system/summary` `GET /api/system/net/traffic` `POST /api/system/resource/chart` |
| 配置 · 存储管理 | `GET /api/system/storage/stats` `POST /api/system/storage-time` |
| 配置 · 系统操作 | `POST /api/system/hostname/update` `POST /api/system/time/update` `POST /api/system/reboot/apply` `.../execute` |
| 管理 · 设备管理 | `GET /api/device/detailed-list` `GET /api/device/{id}/points/definitions` `POST /api/device/enable` `.../disable` |
| 管理 · 协议与驱动 | `GET /api/protocol/list` `GET /api/driver/list` `GET /api/driver/get` |
| 管理 · 远程调度 | `GET /api/remote/modbus/status` `GET /api/remote/mqtt/status` `PUT .../enabled/{enabled}` `POST .../reload` |
| 管理 · 网络配置 | `GET /api/network/interface/list` `POST .../interface/update` `.../interface/state` `.../ping` |
| 管理 · 统计分析 | `POST /api/device/compare` `POST /api/protocol/monitor/{overview,distribution,trend,metrics}` `POST /api/system/resource/chart` |
| 管理 · 审计 | `GET /api/log/biz` `.../biz/info` `.../biz/statistics` `DELETE /api/log/biz` |
| 管理 · 自动化联动 | `GET /api/automation/page` `POST /api/automation` `PUT/DELETE /api/automation/{id}` `POST .../toggle` |
| 配置 · 采集与凭据 | `GET /api/system/credentials`（只读，密钥不回显） |
| 配置 · 界面与语言 | 纯前端（localStorage），无接口 |
| 场景 · 长周期电价预测 | `GET /api/forecast/price`（`grain=day\|intraday`） |
| 场景 · 预测复盘 | `GET /api/forecast/recorded`（留痕对账）`GET /api/forecast/backtest`（1A 回测）`GET /api/gd-metric/series`（96 点实际值） |
| 场景 · 策略反演 | `GET /api/gd-metric/supply-curve`（统计反演：由负荷-出清价反推聚合报价曲线） |
| 管理 · 报表导出 | 不新增接口：复用上面这些接口在前端拼 CSV（见 `src/exports/datasets.ts`） |
| 配置 · 日志级别 | `GET /api/setting`（公开设置）、`POST /api/setting/detail`、`POST /api/setting/update`（**全量覆盖**，改值前先读详情再合并） |
| 配置 · 存储管理 | `GET /api/system/retention`（保留策略状态）、`POST /api/system/retention/clean`（立即清理） |
| 运行监控 · 四页 | 复用既有接口：`/device/{detailed-list,telemetry,history,*\/points/definitions}`、`/alarm/{current,history,ignore}`、`/effect/summary`、`/forecast/load`（**零后端改动**） |
| 登录 / 退出 / 改密 | `POST /api/auth/login`（`{role, password}`，**没有用户名字段**）、`POST /api/auth/logout`、`POST /api/auth/change-password`（带 `oldPassword` 校验）、`GET /api/auth/current` |

手动控制页的两条硬规则：

1. **服务与参数全部来自驱动声明**，前端不写死「充电 / 放电」。驱动换一套，页面跟着变。
2. `parameters` 是**位置参数数组**，顺序必须与驱动声明一致 —— 表单按声明顺序渲染，
   下发前把订单再展示一遍让人确认。冒烟用例专门断言了顺序与类型转换。

储能参数页的字段表**不写在前端**：后端反射 `SPolicyMircogridConfig` 的 struct tag
（`name`/`ct`/`vt`/`min`/`max`/`unit`/`group`）返回字段与分组，加字段只改 Go struct。
保存前会弹出旧值 → 新值的 diff，确认后才 `POST /api/setting/update`（值是 JSON 字符串）。

手动控制页叠加两层保护：驱动声明的 `min/max`，以及从储能参数推导的
「功率上限 = 设备额定 × 配置比例」。被拦时会说清是哪一条拦的。

策略管理页刻意复用 `/api/effect/backtest` 做「试运行」，与效果评估同一套成本模型 ——
另写一套估算会让同一个策略在两个页面给出两个收益。

电价管理页配置的峰平谷时段是**全站唯一来源**（策略套利、预测复盘的分时段指标都读它）。
`priceType` 走字符串（后端 `EPriceType.MarshalJSON`）。校验分两级：重叠/负价/起止非法阻断保存，
未铺满 24 小时只提醒。

采集管理是 `spot-overlay.js` 能力的迁入点（采集台 / 档案 / 复核 / 重跑），
也是下线 overlay 的前置条件。未复核判定集中在 `api/spotreport.ts` 的 `isPending()`，
页面里不各写一遍。

历史信息是 overlay 趋势卡的迁入点。被排除的未复核条数（`excludedPending`）必须在页面上显示，
否则用户会以为数据少了 —— 这个数字是后端在 SQL 层排除时统计出来的。

决策支持是 `effect-overlay` 的迁入点。预测接口区分两个动作：普通查询不穿透后端缓存，
只有「刷新预测」才带 `forceRefresh=true` —— TimesFM 是 CPU 推理，切页就打模型会排队。

市场出清页的节点电价档位是**图像采样预填**（`origin=cv`）——数值只存在于色块里，
页面显著标注「需人工确认」。档位顺序从区间文本下界推导，日报换档位划分不用改代码。

「数据保留」不单开页：后端接口挂在 `/system/storage-time`，已并入「存储管理」，
避免同一功能两个入口（导航因此从 29 项变 28 项，冒烟里有显式常量盯着）。

31 页全部 ready，没有占位页了（占位兜底组件 `PlaceholderView` 保留，用于将来新加的页）。

默认首页 = `NAV[0].pages[0]`，现在是「运行监控 · 能源总览」（可在「配置 · 界面与语言」里改）。

## 与线上产物的关系（构建流程）

前端**源码**是 `src/`（Vue3 + Vite + TS）。交付走的是 `application/manifest/web.zip` ——
Go 用 `//go:embed` 把它编进可执行文件，静态服务挂在根路径
（`application/manifest/web_embed.go` + `internal/cmd/web.go`；Go 侧没有「从目录服务静态文件」的路径）。

```bash
npm run pack        # = vite build → 打包到 ../application/manifest/web.zip
# 也可以指定输出：npm run pack -- --out /out/web.zip（Dockerfile 就是这么用的）
```

**zip 不再入库**（见 `.gitignore`）。它由构建流程就地生成：

- **构建镜像**（`script/Dockerfile.dev`）：先跑一个 `node:22-alpine` 阶段 `npm ci && npm run pack`，
  再由 Go 阶段消费它 → 镜像里的前端**永远**是本次源码构建出来的
- **Go CI**：`go vet/test/build` 之前也会先构建前端 —— 因为 `//go:embed`
  **要求编译时 `web.zip` 存在**，缺了直接编译不过：

  ```
  manifest/web_embed.go:12:12: pattern web.zip: no matching files found
  ```

所以本地改完后端要编译时，先跑一次 `npm run pack`（或者干脆 `npm run verify`，它顺带检查构建产物完整性）。

这么改的原因：产物提交进仓库时，**「改了源码忘了打包」= 部署旧界面**，
而且 CI 全绿、只有用户看得见 —— 最难查的一类问题。现在产物只由构建产生，这个类别从根上没有了。
冒烟也不再检查 zip，而是检查**刚构建出来的 `dist`**：页面 chunk 数必须等于导航页数 + 非导航页数，
少了就说明有页面没登记到 `pages/index.ts`。

## 数据来源（都来自广东）

侧栏 7 个模块名借用参考系统（山西）的信息架构，但**数据全部是广东的**：`gd_metric.csv`
的 96 点日前/实时电价与负荷、广东现货日报（OCR 提取）、EMS 现场设备、TimesFM 预测。
页头没有「场景切换器」—— 只有一支场景，下拉是假控件（切换不改变任何数据），
现在是静态标签 `数据场景：广东现货日报`，悬停列出规划中的场景各缺什么数据源。

## 登录

后端登录接口只有 `role` + `password` —— `admin` / `user` 两个角色**本身就是账号**，
密码存在系统设置里。登录页的「账号」填这两个词之一，填错的在本地就拦下来了。

- 会话键：`ems:user:token` / `ems:user:role` / `ems:user:expireAt`（与后端
  `authBypassScript` 同一套，不要另起一套）
- 令牌以 `Authorization: Bearer <token>` 发送，同时带 Cookie
- 初始密码：**初次部署** admin = `admin`、user = `111111`；
  **已升级的库保持原密码**（刻意不重置）。忘了密码就删掉 `setting` 表里
  `admin_password` 那一行再重启，会按当前默认值重建
- 出厂默认密码是弱口令：部署后第一件事就是改掉它
- `AUTH_BYPASS_ROLE` 是内网调试/演示用的逃生门，默认不要设
