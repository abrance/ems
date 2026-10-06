# LEMS Web

EMS 前端源码。企业后台布局：顶部 Tab（场景 / 管理 / 配置）+ 左侧栏 + 主界面。

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
2. **不注册 Arco 全局组件**。量过：`import ArcoVue` + `arco.css` 会让 CSS 从
   **8KB 涨到 404KB**、JS 从 **108KB 涨到 872KB**，而 P0 一个 Arco 组件都没用到。
   依赖仍声明在 `package.json`（下一批表单 / 表格页会用到），
   `styles/arco-override.css` 已经把 Arco 变量对齐到本项目色板，届时只需在用到组件的地方按需引入。

   若要一次性全量注册，改 `src/main.ts` 加上：

   ```ts
   import ArcoVue from '@arco-design/web-vue'
   import '@arco-design/web-vue/dist/arco.css'
   app.use(ArcoVue)
   ```

## 与线上产物的关系

线上是 `application/manifest/web.zip`（打进 Go 可执行文件，静态服务在根路径）。
本仓库 `npm run build` 的 `dist/` 就是同构产物：

```bash
cd dist && zip -r ../../application/manifest/web.zip . -x '.*'
```
