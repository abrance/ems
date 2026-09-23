(function () {
  const HOME_ID = "spot-home-block";
  const CARD_ID = "spot-info-card";
  const PAGE_ID = "spot-info-page";
  const COL_ID = "spot-info-col";

  let homeState = {
    dates: [], date: "", report: null, page: 0, empty: true,
    mode: "data",            // data=结构化数据（默认） / images=原图
    data: null,              // 当前日期已加载的结构化数据
    dataDate: "",            // data 对应的日期
    dataAttempted: "",       // 已尝试加载过的日期：防止渲染循环里反复重试同一个失败请求
    dataError: "",
    dataLoading: false
  };

  // 数据变更（解析/确认/修订）后让首页重新拉一次
  function invalidateHomeData(date) {
    if (!date || date === homeState.dataAttempted) {
      homeState.dataAttempted = "";
      homeState.data = null;
      homeState.dataDate = "";
    }
  }
  // 地图档位配色（与日报图例一致：浅绿/浅蓝/浅黄/浅橙/浅红）
  const BUCKET_COLORS = ["#b7d6a3", "#adcdea", "#ffff7e", "#ffe07e", "#ff7e7e"];

  // 结构化数据的展示配置：数据集 → 展示名与列（列 = [指标名, 单位]）
  const DATASET_ORDER = ["summary", "day_ahead_trade", "realtime_trade", "day_ahead_decl",
                         "node_price_peak", "node_price_valley"];
  const DATASET_LABELS = {
    summary: "正文摘要指标",
    day_ahead_trade: "日前成交电价",
    realtime_trade: "实时成交电价",
    day_ahead_decl: "日前申报信息",
    node_price_peak: "高峰时刻地市节点电价档位（图2-1）",
    node_price_valley: "低谷时刻地市节点电价档位（图2-2）"
  };
  const BUCKET_LABELS = ["[-inf,200)", "[200,400)", "[400,600)", "[600,800)", "[800,+inf)"];
  const DATASET_COLUMNS = {
    summary: [["日前加权平均电价"], ["燃煤均价"], ["燃气均价"], ["日前成交最高价"], ["日前成交最低价"]],
    day_ahead_trade: [["最高电价"], ["最低电价"], ["平均电价"], ["电价环比"]],
    realtime_trade: [["最高电价"], ["最低电价"], ["平均电价"], ["电价环比"]],
    day_ahead_decl: [["最高电价"], ["最低电价"], ["平均电价"], ["自主申报台数"], ["默认申报台数"],
                     ["申报容量"], ["煤机容量"], ["气机容量"], ["新能源容量"]],
    node_price_peak: [["价格档位"]],
    node_price_valley: [["价格档位"]]
  };
  let listState = [];
  let lastPath = "";

  function token() {
    return localStorage.getItem("ems:user:token") || "";
  }
  function role() {
    return localStorage.getItem("ems:user:role") || "";
  }
  function isAdmin() {
    return role() === "admin";
  }
  function pathName() {
    return location.pathname.replace(/\/+$/, "") || "/";
  }

  async function api(path, options) {
    const headers = Object.assign(
      { "Accept-Language": "zh-CN" },
      options && options.headers
    );
    const t = token();
    if (t) headers.Authorization = "Bearer " + t;
    if (options && options.body && !headers["Content-Type"]) {
      headers["Content-Type"] = "application/json";
    }
    const res = await fetch(path, Object.assign({ credentials: "include" }, options, { headers }));
    const json = await res.json().catch(function () { return {}; });
    if (json.code && json.code !== 0) {
      throw new Error(json.message || "请求失败");
    }
    return json.data;
  }

  function el(tag, attrs, children) {
    const node = document.createElement(tag);
    Object.keys(attrs || {}).forEach(function (key) {
      if (key === "class") node.className = attrs[key];
      else if (key === "text") node.textContent = attrs[key];
      else if (key.slice(0, 2) === "on") node.addEventListener(key.slice(2).toLowerCase(), attrs[key]);
      else if (attrs[key] === false || attrs[key] === "" || attrs[key] == null) return;
      else node.setAttribute(key, attrs[key]);
    });
    (children || []).forEach(function (child) {
      if (child) node.appendChild(child);
    });
    return node;
  }

  function currentIndex() {
    return homeState.dates.indexOf(homeState.date);
  }

  function renderHome() {
    const root = document.getElementById(HOME_ID);
    if (!root) return;
    root.innerHTML = "";
    const idx = currentIndex();
    const pages = (homeState.report && homeState.report.pages) || [];
    const prevDate = el("button", {
      class: "spot-btn",
      text: "前一天",
      disabled: idx <= 0 ? "disabled" : "",
      onClick: function () {
        if (idx > 0) loadHomeDate(homeState.dates[idx - 1]);
      }
    });
    const nextDate = el("button", {
      class: "spot-btn",
      text: "后一天",
      disabled: idx < 0 || idx >= homeState.dates.length - 1 ? "disabled" : "",
      onClick: function () {
        if (idx >= 0 && idx < homeState.dates.length - 1) loadHomeDate(homeState.dates[idx + 1]);
      }
    });
    if (idx <= 0) prevDate.disabled = true;
    if (idx < 0 || idx >= homeState.dates.length - 1) nextDate.disabled = true;

    const report = homeState.report || {};
    const pendingCount = (homeState.data && homeState.data.metrics || []).filter(function (m) {
      return m.origin === "cv";
    }).length;

    const today = new Date();
    const yst = new Date(today.getTime() - 86400000);
    const ystText = yst.getFullYear() + "-" + String(yst.getMonth() + 1).padStart(2, "0") + "-" +
      String(yst.getDate()).padStart(2, "0");
    const isStale = homeState.date && homeState.date !== ystText;

    const tabData = el("button", { class: "spot-tab" + (homeState.mode === "data" ? " active" : ""), text: "数据",
      onClick: function () { switchHomeMode("data"); } });
    const tabImages = el("button", { class: "spot-tab" + (homeState.mode === "images" ? " active" : ""), text: "原图",
      onClick: function () { switchHomeMode("images"); } });
    const headRight = [
      el("div", { class: "spot-tabs" }, [tabData, tabImages])
    ];
    if (isStale) {
      headRight.push(el("span", { class: "spot-stale-badge", text: "非最新：数据截至 " + homeState.date,
        title: "默认展示昨天的运行日；当前不是昨天（可能尚未发布或未采集）" }));
    }
    if (pendingCount) {
      headRight.push(el("span", { class: "spot-pending-badge",
        text: "待确认 " + pendingCount + " 项",
        title: "图像采样预填，尚未人工复核" }));
      if (isAdmin()) {
        headRight.push(el("button", { class: "spot-btn spot-confirm-btn", text: "去确认",
          onClick: gotoInfoManagement }));
      }
    }

    root.appendChild(el("div", { class: "spot-home-head" }, [
      el("div", { class: "spot-home-heading" }, [
        el("div", { class: "spot-home-title", text: "电力现货" }),
        el("div", { class: "spot-home-sub", text: report.title ? report.title : "暂无日报" })
      ]),
      el("div", { class: "spot-date-nav" }, [
        prevDate,
        el("div", { class: "spot-date-label", text: homeState.date || "暂无日报" }),
        nextDate
      ]),
      el("div", { class: "spot-home-tools" }, headRight)
    ]));

    if (homeState.empty || !pages.length) {
      root.appendChild(el("div", { class: "spot-home-empty" }, [
        el("div", { class: "spot-home-empty-icon", text: "讯" }),
        el("div", { class: "spot-empty", text: "暂无现货日报" }),
        el("div", { class: "spot-empty-hint", text: isAdmin() ? "可在 系统管理 → 信息管理 中新增采集" : "等待管理员采集" })
      ]));
      return;
    }

    if (homeState.mode === "images") {
      root.appendChild(buildHomeImages(pages));
      return;
    }
    const dataBody = el("div", { class: "spot-home-data" });
    root.appendChild(dataBody);
    renderHomeData(dataBody, report);
    if (homeState.dataAttempted !== homeState.date) {
      loadHomeData(homeState.date);
    }

  }

  // ---- 首页数据视图：KPI + 图表（当日快照；不把不同运行日混在同一张图里） ----

  function metricMap(metrics) {
    const index = {};
    (metrics || []).forEach(function (m) {
      index[m.dataset + "|" + m.source + "|" + m.scope + "|" + m.metric] = m;
    });
    return index;
  }

  function firstMetric(metrics, dataset, scope, metric, source) {
    for (let i = 0; i < (metrics || []).length; i++) {
      const m = metrics[i];
      if (m.dataset !== dataset || m.scope !== scope || m.metric !== metric) continue;
      if (source && m.source !== source) continue;
      return m;
    }
    return null;
  }

  function buildHomeImages(pages) {
    // 四页并排：图片按原始比例铺满各自列宽，一眼看全 4 页；点击任一张放大查看
    return el("div", { class: "spot-grid" }, pages.map(function (item, index) {
      return el("button", {
        class: "spot-cell",
        type: "button",
        title: item.label,
        onClick: function () { openViewer(index); }
      }, [
        el("img", { src: item.url, alt: item.label, loading: "lazy" }),
        el("div", { class: "spot-cell-tag" }, [
          el("div", { class: "spot-cell-index", text: String(item.page || index + 1) }),
          el("div", { class: "spot-cell-label", text: item.label })
        ])
      ]);
    }));
  }

  function switchHomeMode(mode) {
    homeState.mode = mode;
    renderHome();
  }

  function gotoInfoManagement() {
    history.pushState({}, "", "/infoManagement");
    sync();
  }

  async function loadHomeData(date) {
    // 同一日期只加载一次：失败时也只记一次，避免每轮渲染都重试（曾因此打出无限请求）
    if (!date || homeState.dataLoading || homeState.dataAttempted === date) return;
    homeState.dataLoading = true;
    try {
      homeState.data = await api("/api/spot-report/data?date=" + encodeURIComponent(date));
      homeState.dataDate = date;
      homeState.dataError = "";
    } catch (err) {
      homeState.data = null;
      homeState.dataDate = "";
      homeState.dataError = err.message || "加载失败";
    } finally {
      homeState.dataLoading = false;
      homeState.dataAttempted = date;
      if (homeState.mode === "data") renderHome();
    }
  }

  // KPI 条：正文摘要的 5 个标量 + 对应的环比
  function buildHomeKpi(metrics, pending) {
    const spec = [
      ["日前加权均价", "日前加权平均电价", "发电侧"],
      ["燃煤均价", "燃煤均价", "燃煤"],
      ["燃气均价", "燃气均价", "燃气"],
      ["日前成交最高", "日前成交最高价", null],
      ["日前成交最低", "日前成交最低价", null]
    ];
    const bar = el("div", { class: "spot-kpi-bar" });
    spec.forEach(function (item) {
      const m = firstMetric(metrics, "summary", "市场", item[1]);
      if (!m) return;
      // 环比优先取「电价环比」行，缺失时回落到该类型平均电价自带的 momPct
      let momValue = null;
      if (item[2]) {
        const momRow = firstMetric(metrics, "day_ahead_trade", item[2], "电价环比", "表1(二)");
        if (momRow) {
          momValue = momRow.value;
        } else {
          const avgRow = firstMetric(metrics, "day_ahead_trade", item[2], "平均电价", "表1(二)");
          if (avgRow && avgRow.momPct !== null && avgRow.momPct !== undefined) momValue = avgRow.momPct;
        }
      }
      bar.appendChild(el("div", { class: "spot-kpi" }, [
        el("div", { class: "spot-kpi-label", text: item[0] }),
        el("div", { class: "spot-kpi-value" }, [
          el("span", { text: fmtNum(m.value) }),
          el("span", { class: "spot-kpi-unit", text: m.unit || "" }),
          momBadge(momValue)
        ])
      ]));
    });
    if (pending) bar.className += " spot-pending";
    return bar;
  }

  // 分类型均价柱状（同一运行日、同一来源，避免把 T 与 T-2 混在一起）
  function buildTradeChart(metrics, dataset, source, title) {
    const scopes = ["发电侧", "燃煤", "燃气", "新能源"];
    const rows = [];
    scopes.forEach(function (scope) {
      const avg = firstMetric(metrics, dataset, scope, "平均电价", source);
      const hi = firstMetric(metrics, dataset, scope, "最高电价", source);
      const lo = firstMetric(metrics, dataset, scope, "最低电价", source);
      if (avg) rows.push({ scope: scope, avg: avg.value, hi: hi ? hi.value : null, lo: lo ? lo.value : null });
    });
    if (!rows.length) return null;
    const max = Math.max.apply(null, rows.map(function (r) { return r.hi !== null ? Math.max(r.hi, r.avg) : r.avg; }));
    const min = Math.min.apply(null, rows.map(function (r) { return r.lo !== null ? r.lo : r.avg; }));
    const span = Math.max(1, max - min);
    const chart = el("div", { class: "spot-chart" }, [
      el("div", { class: "spot-chart-title", text: title })
    ]);
    const body = el("div", { class: "spot-bars" });
    rows.forEach(function (r) {
      const height = Math.max(4, Math.round((r.avg - min) / span * 100));
      body.appendChild(el("div", { class: "spot-bar-col" }, [
        el("div", { class: "spot-bar-track" }, [
          el("div", { class: "spot-bar", style: "height:" + height + "%",
            title: r.scope + " 均价 " + fmtNum(r.avg) + " 元/兆瓦时" })
        ]),
        el("div", { class: "spot-bar-value", text: fmtNum(r.avg) }),
        el("div", { class: "spot-bar-label", text: r.scope })
      ]));
    });
    chart.appendChild(body);
    return chart;
  }

  // 申报容量结构：两行（零价及以下 / 报价600以上）× 四类容量占比
  function buildDeclChart(metrics) {
    const rows = ["零价及以下", "报价600以上"];
    const kinds = ["申报容量", "煤机容量", "气机容量", "新能源容量"];
    const colors = { "申报容量": "#c9d7f0", "煤机容量": "#8fb6e8", "气机容量": "#ffd28a", "新能源容量": "#a8dfa0" };
    const has = rows.some(function (row) { return kinds.some(function (k) { return firstMetric(metrics, "day_ahead_decl", row, k); }); });
    if (!has) return null;
    const chart = el("div", { class: "spot-chart" }, [
      el("div", { class: "spot-chart-title", text: "日前申报容量结构（占市场比例）" })
    ]);
    rows.forEach(function (row) {
      const items = kinds.map(function (k) { return firstMetric(metrics, "day_ahead_decl", row, k); }).filter(Boolean);
      if (!items.length) return;
      const track = el("div", { class: "spot-stack" });
      items.forEach(function (m) {
        const pct = m.sharePct === null || m.sharePct === undefined ? 0 : m.sharePct;
        track.appendChild(el("div", { class: "spot-stack-seg",
          style: "width:" + pct + "%;background:" + (colors[m.metric] || "#ccc"),
          title: row + " " + m.metric + " " + fmtNum(m.value) + " MW（占 " + fmtNum(pct) + "%）",
          text: pct >= 6 ? fmtNum(pct) + "%" : "" }));
      });
      chart.appendChild(el("div", { class: "spot-stack-row" }, [
        el("div", { class: "spot-stack-label", text: row }),
        track
      ]));
    });
    const legend = el("div", { class: "spot-legend" });
    kinds.forEach(function (k) {
      legend.appendChild(el("span", { class: "spot-legend-item" }, [
        el("span", { class: "spot-legend-dot", style: "background:" + (colors[k] || "#ccc") }),
        el("span", { text: k })
      ]));
    });
    chart.appendChild(legend);
    return chart;
  }

  // 地市节点电价档位：色块网格（高峰/低谷并排），未判定留空
  function buildBucketGrid(metrics, dataset, title) {
    const rows = (metrics || []).filter(function (m) { return m.dataset === dataset; });
    if (!rows.length) return null;
    const chart = el("div", { class: "spot-chart" }, [
      el("div", { class: "spot-chart-title", text: title })
    ]);
    const grid = el("div", { class: "spot-bucket-grid" });
    rows.slice().sort(function (a, b) { return b.value - a.value || a.scope.localeCompare(b.scope); })
      .forEach(function (m) {
        const color = BUCKET_COLORS[Math.min(BUCKET_COLORS.length, Math.max(1, m.value)) - 1];
        grid.appendChild(el("div", { class: "spot-bucket-cell",
          style: "background:" + color,
          title: m.scope + " " + m.value + " 档 " + (m.textValue || "") + (m.origin === "cv" ? "（待确认）" : ""),
          text: m.scope }));
      });
    chart.appendChild(grid);
    return chart;
  }

  function buildBucketLegend() {
    const legend = el("div", { class: "spot-legend" });
    BUCKET_LABELS.forEach(function (label, i) {
      legend.appendChild(el("span", { class: "spot-legend-item" }, [
        el("span", { class: "spot-legend-dot", style: "background:" + BUCKET_COLORS[i] }),
        el("span", { text: (i + 1) + " 档 " + label })
      ]));
    });
    return legend;
  }

  function renderHomeData(container, report) {
    container.innerHTML = "";
    if (homeState.dataLoading && !homeState.data) {
      container.appendChild(el("div", { class: "spot-empty", text: "加载数据中…" }));
      return;
    }
    if (homeState.dataError && !homeState.data) {
      container.appendChild(el("div", { class: "spot-home-empty" }, [
        el("div", { class: "spot-home-empty-icon", text: "!" }),
        el("div", { class: "spot-empty", text: "数据加载失败：" + homeState.dataError }),
        el("button", { class: "spot-btn", text: "重试", onClick: function () {
          invalidateHomeData(homeState.date);
          renderHome();
        } })
      ]));
      return;
    }
    const data = homeState.data;
    const metrics = (data && data.metrics) || [];
    if (!metrics.length) {
      container.appendChild(el("div", { class: "spot-home-empty" }, [
        el("div", { class: "spot-home-empty-icon", text: "数" }),
        el("div", { class: "spot-empty", text: "该日报还没有结构化数据" }),
        el("div", { class: "spot-empty-hint",
          text: isAdmin() ? "去管理界面执行「解析数据」后，这里会展示 KPI 与图表" : "等待管理员解析" }),
        isAdmin() ? el("button", { class: "spot-btn primary", text: "去解析", onClick: gotoInfoManagement }) : null
      ]));
      return;
    }
    const pending = metrics.filter(function (m) { return m.origin === "cv"; }).length;
    container.appendChild(buildHomeKpi(metrics, pending));

    const charts = el("div", { class: "spot-chart-row" });
    const dayAhead = buildTradeChart(metrics, "day_ahead_trade", "表1(二)", "日前成交均价（运行日 " + report.date + "）");
    if (dayAhead) charts.appendChild(dayAhead);
    const realtime = buildTradeChart(metrics, "realtime_trade", "表2(二)",
      "实时成交均价" + (data.tradeDate ? "（运行日 " + data.tradeDate + "）" : ""));
    if (realtime) charts.appendChild(realtime);
    const decl = buildDeclChart(metrics);
    if (decl) charts.appendChild(decl);
    container.appendChild(charts);

    const buckets = el("div", { class: "spot-chart-row" });
    const peak = buildBucketGrid(metrics, "node_price_peak", "地市节点电价档位 · 高峰");
    const valley = buildBucketGrid(metrics, "node_price_valley", "地市节点电价档位 · 低谷");
    if (peak) buckets.appendChild(peak);
    if (valley) buckets.appendChild(valley);
    if (peak || valley) {
      const holder = el("div", { class: "spot-chart" }, [
        el("div", { class: "spot-chart-title", text: "档位说明" }),
        buildBucketLegend()
      ]);
      buckets.appendChild(holder);
      container.appendChild(buckets);
    }
    if (pending) {
      container.appendChild(el("div", { class: "spot-data-note",
        text: "含 " + pending + " 项图像采样预填（未人工复核），结论性字段仅供参考" }));
    }
  }

  let viewerIndex = -1;

  function viewerKeydown(ev) {
    if (ev.key === "Escape") {
      closeViewer();
    } else if (ev.key === "ArrowLeft") {
      stepViewer(-1);
    } else if (ev.key === "ArrowRight") {
      stepViewer(1);
    }
  }

  function closeViewer() {
    document.querySelectorAll(".spot-modal-mask.spot-viewer-mask").forEach(function (node) { node.remove(); });
    if (viewerIndex >= 0) {
      viewerIndex = -1;
      document.removeEventListener("keydown", viewerKeydown);
    }
  }

  function stepViewer(delta) {
    const pages = (homeState.report && homeState.report.pages) || [];
    if (!pages.length) return;
    viewerIndex = (viewerIndex + delta + pages.length) % pages.length;
    renderViewer();
  }

  function renderViewer() {
    const mask = document.querySelector(".spot-modal-mask.spot-viewer-mask");
    const report = homeState.report || {};
    const pages = report.pages || [];
    if (!mask || viewerIndex < 0 || !pages.length) return;
    const page = pages[viewerIndex];
    mask.innerHTML = "";
    mask.appendChild(el("div", { class: "spot-viewer" }, [
      el("div", { class: "spot-viewer-head" }, [
        el("div", { class: "spot-viewer-title", text: report.title || "现货运行日报" }),
        el("button", { class: "spot-btn", text: "关闭", onClick: closeViewer })
      ]),
      el("div", { class: "spot-viewer-body" }, [
        el("button", { class: "spot-page-btn", text: "<", title: "上一页", onClick: function () { stepViewer(-1); } }),
        el("img", { src: page.url, alt: page.label }),
        el("button", { class: "spot-page-btn", text: ">", title: "下一页", onClick: function () { stepViewer(1); } })
      ]),
      el("div", { class: "spot-viewer-foot" }, [
        el("div", { class: "spot-page-caption", text: "运行日 " + (report.date || "") + " · " + (viewerIndex + 1) + " / " + pages.length + " · " + page.label }),
        el("div", { class: "spot-viewer-hint", text: "← → 翻页，Esc 关闭" })
      ])
    ]));
  }

  function openViewer(index) {
    const pages = (homeState.report && homeState.report.pages) || [];
    if (!pages.length) return;
    closeModal();
    viewerIndex = index;
    const mask = el("div", {
      class: "spot-modal-mask spot-viewer-mask",
      onClick: function (ev) {
        if (ev.target === mask) closeViewer();
      }
    });
    document.body.appendChild(mask);
    renderViewer();
    document.addEventListener("keydown", viewerKeydown);
  }

  async function loadHomeDate(date) {
    closeViewer();
    homeState.dataAttempted = "";
    try {
      const data = await api("/api/spot-report/detail?date=" + encodeURIComponent(date));
      homeState.date = date;
      homeState.report = data && data.report;
      homeState.empty = !homeState.report;
      homeState.page = 0;
      renderHome();
    } catch (err) {
      homeState.empty = true;
      homeState.report = null;
      renderHome();
    }
  }

  async function ensureHome() {
    const landscape = document.querySelector(".dashboard-landscape");
    if (!landscape) return;
    let block = document.getElementById(HOME_ID);
    if (!block) {
      block = el("div", { id: HOME_ID, class: "spot-home" });
      landscape.insertBefore(block, landscape.firstChild);
      try {
        const data = await api("/api/spot-report/home");
        homeState.dates = ((data && data.dates) || []).slice().sort();
        homeState.date = (data && data.date) || "";
        homeState.report = data && data.report;
        homeState.empty = !!(data && data.empty) || !homeState.report;
        homeState.page = 0;
      } catch (err) {
        homeState.empty = true;
      }
    }
    renderHome();
  }

  function openInfoPage() {
    history.pushState({}, "", "/infoManagement");
    sync();
  }

  function ensureCard() {
    if (!isAdmin()) return;
    const cat = document.getElementById("cat-Config");
    if (!cat) return;
    if (document.getElementById(CARD_ID)) return;
    const row = cat.querySelector(".arco-row") || cat.querySelector("[class*='arco-row']") || cat.lastElementChild;
    if (!row) return;
    const col = el("div", { id: COL_ID, class: row.firstElementChild ? row.firstElementChild.className : "" });
    const card = el("div", { id: CARD_ID, class: "spot-info-card", onClick: openInfoPage }, [
      el("div", { class: "spot-info-card-icon", text: "讯" }),
      el("div", { class: "spot-info-card-name", text: "信息管理" }),
      el("div", { class: "spot-info-card-desc", text: "现货运行日报采集与档案" })
    ]);
    col.appendChild(card);
    row.appendChild(col);
  }

  function closeModal() {
    // 只关采集/详情弹窗，不碰放大查看视图（sync 定时重跑会反复调用本函数）
    document.querySelectorAll(".spot-modal-mask:not(.spot-viewer-mask)").forEach(function (node) { node.remove(); });
  }

  function showCollectModal() {
    closeModal();
    const input = el("input", { class: "spot-input", placeholder: "粘贴 mp.weixin.qq.com 文章链接" });
    const error = el("div", { class: "spot-error" });
    const submit = el("button", { class: "spot-btn primary", text: "采集", onClick: async function () {
      error.textContent = "";
      submit.disabled = true;
      submit.textContent = "采集中...";
      try {
        await api("/api/spot-report/collect", {
          method: "POST",
          body: JSON.stringify({ url: input.value })
        });
        closeModal();
        invalidateHomeData("");
        await refreshList();
        window.alert("采集完成。正在后台解析（OCR + 地图采样，约 6-10 秒），稍后可在首页「数据」视图查看并确认。");
      } catch (err) {
        error.textContent = err.message || "采集失败";
      } finally {
        submit.disabled = false;
        submit.textContent = "采集";
      }
    }});
    const mask = el("div", { class: "spot-modal-mask", onClick: function (ev) {
      if (ev.target === mask) closeModal();
    }}, [
      el("div", { class: "spot-modal" }, [
        el("h3", { text: "新增采集" }),
        input,
        error,
        el("div", { class: "spot-modal-actions" }, [
          el("button", { class: "spot-btn", text: "取消", onClick: closeModal }),
          submit
        ])
      ])
    ]);
    document.body.appendChild(mask);
    input.focus();
  }

  function fmtNum(v) {
    if (v === null || v === undefined) return "-";
    const n = Math.round(v * 100) / 100;
    return n.toLocaleString("zh-CN", { maximumFractionDigits: 2 });
  }

  function momBadge(v) {
    if (v === null || v === undefined) return null;
    const up = v >= 0;
    return el("span", { class: "spot-mom " + (up ? "up" : "down"),
      text: (up ? "▲" : "▼") + Math.abs(v).toFixed(2) + "%" });
  }

  // 人工修订单个字段：就地输入 → 保存（origin 变 manual，后台留审计）
  function editMetric(container, report, metric) {
    const isBucket = metric.metric === "价格档位";
    const hint = isBucket
      ? "\n请输入档位序号 1-" + BUCKET_LABELS.length + "：\n" +
        BUCKET_LABELS.map(function (l, i) { return "  " + (i + 1) + " = " + l; }).join("\n")
      : "";
    const next = window.prompt("修订 " + metric.scope + " / " + metric.metric +
      "（运行日 " + metric.runDate + "）\n当前值：" + metric.value + (metric.unit || "") + hint +
      "\n注意：再次执行「解析数据」会以采样/OCR 结果覆盖本次修订（审计表会留痕）", String(metric.value));
    if (next === null) return;
    const value = Number(next);
    if (!isFinite(value)) { window.alert("请输入数字"); return; }
    if (isBucket && (value < 1 || value > BUCKET_LABELS.length || value !== Math.round(value))) {
      window.alert("档位必须是 1-" + BUCKET_LABELS.length + " 的整数");
      return;
    }
    api("/api/spot-report/metric", {
      method: "POST",
      body: JSON.stringify({
        date: report.date, runDate: metric.runDate, dataset: metric.dataset,
        scope: metric.scope, metric: metric.metric, value: value
      })
    }).then(function () {
      metric.value = value;
      metric.origin = "manual";
      if (isBucket) metric.textValue = BUCKET_LABELS[value - 1];
      invalidateHomeData(report.date);
      renderDataTab(container, report);
    }).catch(function (err) {
      window.alert("修订失败：" + (err.message || "未知错误"));
    });
  }

  // 一键确认预填：origin=cv → manual（人工复核，不是逐项重填）
  function confirmPrefill(container, report, pending, dataset) {
    const scopeText = dataset ? "（仅 " + (DATASET_LABELS[dataset] || dataset) + "）" : "（该日报全部数据集）";
    if (!window.confirm("确认将 " + pending + " 项预填值标记为已人工复核" + scopeText + "？\n（数值不变；如需修正请先点击对应字段修改）")) return;
    const payload = { date: report.date };
    if (dataset) payload.dataset = dataset;
    api("/api/spot-report/confirm", {
      method: "POST",
      body: JSON.stringify(payload)
    }).then(function (res) {
      invalidateHomeData(report.date);
      renderDataTab(container, report);
      if (res && res.confirmed) {
        window.alert("已确认 " + res.confirmed + " 项，可在「变更记录」中查看");
      }
    }).catch(function (err) {
      window.alert("确认失败：" + (err.message || "未知错误"));
    });
  }

  // 审计：人工修订与被覆盖记录
  function showAudit(report) {
    api("/api/spot-report/audit?date=" + encodeURIComponent(report.date) + "&limit=50").then(function (data) {
      const list = (data && data.list) || [];
      const mask = el("div", { class: "spot-modal-mask", onClick: function (ev) {
        if (ev.target === mask) closeModal();
      }}, [
        el("div", { class: "spot-modal" }, [
          el("div", { class: "spot-modal-head" }, [
            el("div", {}, [
              el("h3", { text: "指标变更记录" }),
              el("div", { class: "spot-page-caption", text: "日报日 " + report.date })
            ])
          ]),
          list.length ? el("table", { class: "spot-data-table" }, [
            el("thead", {}, [el("tr", {}, [
              el("th", { text: "时间" }), el("th", { text: "运行日" }), el("th", { text: "对象" }),
              el("th", { text: "指标" }), el("th", { text: "原值" }), el("th", { text: "新值" }),
              el("th", { text: "类型" }), el("th", { text: "操作人" })
            ])]),
            el("tbody", {}, list.map(function (a) {
              return el("tr", {}, [
                el("td", { text: (a.createdAt || "").slice(0, 19).replace("T", " ") }),
                el("td", { text: a.runDate }),
                el("td", { text: a.scope }),
                el("td", { text: a.metric }),
                el("td", { text: a.oldValue === null || a.oldValue === undefined ? "-" : String(a.oldValue) }),
                el("td", { text: a.newValue === null || a.newValue === undefined ? "-" : String(a.newValue) }),
                el("td", { text: a.operation === "edit" ? "人工修订" : (a.operation === "confirm" ? "人工确认" : "被解析覆盖") }),
                el("td", { text: a.operator || "-" })
              ]);
            }))
          ]) : el("div", { class: "spot-empty", text: "暂无变更记录" }),
          el("div", { class: "spot-modal-actions" }, [
            el("button", { class: "spot-btn", text: "关闭", onClick: closeModal })
          ])
        ])
      ]);
      document.body.appendChild(mask);
    }).catch(function (err) {
      window.alert("加载审计失败：" + (err.message || "未知错误"));
    });
  }

  // 数据页签：按数据集分组，行=对象，列=指标，环比以角标显示
  function renderDataTab(container, report) {
    container.innerHTML = "";
    container.appendChild(el("div", { class: "spot-empty", text: "解析数据加载中…" }));
    api("/api/spot-report/data?date=" + encodeURIComponent(report.date)).then(function (data) {
      container.innerHTML = "";
      const metrics = (data && data.metrics) || [];
      const info = data && data.extract;
      if (!metrics.length) {
        container.appendChild(el("div", { class: "spot-empty", text: "该日报还没有结构化数据" }));
        container.appendChild(el("div", { class: "spot-empty-hint", text: isAdmin() ? "在「信息管理」里点该行的「解析数据」" : "请联系管理员解析" }));
        return;
      }
      const metaBits = [];
      if (info) {
        metaBits.push("OCR " + info.ocrModel);
        metaBits.push(info.pageCount + " 页");
        metaBits.push(Math.round(info.ocrMs / 1000) + "s");
        if (info.updatedAt) metaBits.push("提取于 " + info.updatedAt.slice(0, 19).replace("T", " "));
      }
      container.appendChild(el("div", { class: "spot-data-meta" }, [
        el("span", { text: metaBits.join(" · ") }),
        isAdmin() ? el("button", { class: "spot-link", text: "变更记录", onClick: function () { showAudit(report); } }) : null
      ]));
      if (data.tradeDate && data.tradeDate !== report.date) {
        container.appendChild(el("div", { class: "spot-data-note",
          text: "注：日报里的表2 承载滞后运行日 " + data.tradeDate + " 的日前/实时成交电价" }));
      }
      const pending = metrics.filter(function (m) { return m.origin === "cv"; }).length;
      if (pending) {
        const note = el("div", { class: "spot-data-note" }, [
          el("span", { text: "其中 " + pending + " 项为图像采样预填（标记「预填」）。可整篇确认，也可只确认某一张表；核对无误后一次点击即可，无需逐项修改。" }),
          isAdmin() ? el("button", { class: "spot-btn primary spot-confirm-btn", text: "确认全部 " + pending + " 项",
            onClick: function () { confirmPrefill(container, report, pending, ""); } }) : null
        ]);
        container.appendChild(note);
      }
      if (info && info.warnings && info.warnings.length) {
        container.appendChild(el("div", { class: "spot-data-warn", text: "校验告警：" + info.warnings.join("；") }));
      }

      DATASET_ORDER.forEach(function (dataset) {
        const rows = metrics.filter(function (m) { return m.dataset === dataset; });
        if (!rows.length) return;
        const columns = DATASET_COLUMNS[dataset] || [];
        const scopes = [];
        rows.forEach(function (m) { if (scopes.indexOf(m.scope) < 0) scopes.push(m.scope); });
        const title = DATASET_LABELS[dataset] || dataset;
        const runDates = rows.map(function (m) { return m.runDate; }).filter(function (v, i, a) { return v && a.indexOf(v) === i; });
        const datasetPending = rows.filter(function (m) { return m.origin === "cv"; }).length;
        const titleRow = el("div", { class: "spot-data-title-row" }, [
          el("div", { class: "spot-data-title",
            text: title + (runDates.length === 1 && runDates[0] !== report.date ? "（运行日 " + runDates[0] + "）" : "") }),
          isAdmin() && datasetPending
            ? el("button", { class: "spot-btn spot-confirm-btn", text: "确认本表 " + datasetPending + " 项",
                onClick: function () { confirmPrefill(container, report, datasetPending, dataset); } })
            : null
        ]);
        container.appendChild(titleRow);

        const table = el("table", { class: "spot-data-table" });
        const head = el("tr", {}, [el("th", { text: "对象" })]);
        columns.forEach(function (c) { head.appendChild(el("th", { text: c[0] })); });
        table.appendChild(el("thead", {}, [head]));
        const tbody = el("tbody");
        scopes.forEach(function (scope) {
          const tr = el("tr", {}, [el("td", { class: "spot-data-scope", text: scope })]);
          columns.forEach(function (col) {
            const name = col[0];
            const hit = rows.filter(function (m) { return m.scope === scope && m.metric === name; })[0];
            if (!hit) {
              tr.appendChild(el("td", { class: "spot-data-missing", text: "-" }));
              return;
            }
            const cellChildren = [];
            const isBucket = hit.metric === "价格档位";
            const valueText = isBucket
              ? (fmtNum(hit.value) + " 档" + (hit.textValue ? " · " + hit.textValue : ""))
              : fmtNum(hit.value) + (hit.unit === "%" ? "%" : "");
            if (isAdmin()) {
              cellChildren.push(el("button", {
                class: "spot-data-value spot-data-editable",
                text: valueText,
                title: "点击修订该字段",
                onClick: function () { editMetric(container, report, hit); }
              }));
            } else {
              cellChildren.push(el("div", { class: "spot-data-value", text: valueText }));
            }
            if (hit.origin === "manual") {
              cellChildren.push(el("span", { class: "spot-origin-manual",
                text: "人工" + (hit.editedBy ? "·" + hit.editedBy : ""),
                title: "人工修订" + (hit.editedAt ? "于 " + hit.editedAt.slice(0, 19).replace("T", " ") : "") }));
            } else if (hit.origin === "cv") {
              cellChildren.push(el("span", { class: "spot-origin-cv", text: "预填",
                title: "图像颜色采样预填，请人工确认" }));
            }
            if (hit.atTime) cellChildren.push(el("div", { class: "spot-data-time", text: "@" + hit.atTime }));
            const badge = momBadge(hit.momPct);
            if (badge) cellChildren.push(badge);
            if (hit.sharePct !== null && hit.sharePct !== undefined) {
              cellChildren.push(el("div", { class: "spot-data-share", text: "占市场 " + fmtNum(hit.sharePct) + "%" }));
            }
            tr.appendChild(el("td", {}, cellChildren));
          });
          tbody.appendChild(tr);
        });
        table.appendChild(tbody);
        container.appendChild(table);
      });
    }).catch(function (err) {
      container.innerHTML = "";
      container.appendChild(el("div", { class: "spot-error", text: err.message || "加载数据失败" }));
    });
  }

  function showDetail(report) {
    closeModal();
    let active = "pages";
    const body = el("div", { class: "spot-detail-body" });
    const tabPages = el("button", { class: "spot-tab active", text: "原图", onClick: function () { switchTab("pages"); } });
    const today = new Date();
    const yst = new Date(today.getTime() - 86400000);
    const ystText = yst.getFullYear() + "-" + String(yst.getMonth() + 1).padStart(2, "0") + "-" +
      String(yst.getDate()).padStart(2, "0");
    const isStale = homeState.date && homeState.date !== ystText;

    const tabData = el("button", { class: "spot-tab", text: "数据", onClick: function () { switchTab("data"); } });

    const pagesView = el("div", { class: "spot-detail-pages" }, (report.pages || []).map(function (page) {
      return el("figure", {}, [
        el("img", { src: page.url, alt: page.label }),
        el("figcaption", { text: page.page + ". " + page.label })
      ]);
    }));

    function switchTab(tab) {
      active = tab;
      tabPages.className = "spot-tab" + (tab === "pages" ? " active" : "");
      tabData.className = "spot-tab" + (tab === "data" ? " active" : "");
      body.innerHTML = "";
      if (tab === "pages") {
        body.appendChild(pagesView);
      } else {
        renderDataTab(body, report);
      }
    }

    const mask = el("div", { class: "spot-modal-mask", onClick: function (ev) {
      if (ev.target === mask) closeModal();
    }}, [
      el("div", { class: "spot-modal" }, [
        el("div", { class: "spot-modal-head" }, [
          el("div", {}, [
            el("h3", { text: report.title || "现货运行日报" }),
            el("div", { class: "spot-page-caption", text: "运行日 " + report.date })
          ]),
          el("div", { class: "spot-tabs" }, [tabPages, tabData])
        ]),
        body,
        el("div", { class: "spot-modal-actions" }, [
          el("button", { class: "spot-btn", text: "关闭", onClick: closeModal })
        ])
      ])
    ]);
    document.body.appendChild(mask);
    switchTab("pages");
  }

  // 批量重跑解析：解析器升级后回填历史日报（后台任务 + 进度轮询）
  function showBatchExtract() {
    const dates = listState.map(function (item) { return item.date; }).sort();
    if (!dates.length) { window.alert("还没有已采集的日报"); return; }
    const defFrom = dates[0];
    const defTo = dates[dates.length - 1];
    const from = window.prompt("批量重跑解析：起始日报日（YYYY-MM-DD，留空表示最早 " + defFrom + "）", defFrom);
    if (from === null) return;
    const to = window.prompt("结束日报日（留空表示最新 " + defTo + "）", defTo);
    if (to === null) return;
    if (!window.confirm("将对 " + (from || defFrom) + " ~ " + (to || defTo) + " 的日报逐篇重跑解析。\n" +
      "每篇约 6-10 秒，会串行执行；本次重跑以 OCR/采样结果为准，" +
      "已人工确认或修订的值会被覆盖（覆盖前写审计，结束时给出条数）。确定继续？")) return;
    api("/api/spot-report/extract/batch", {
      method: "POST",
      body: JSON.stringify({ from: from || "", to: to || "" })
    }).then(function (res) {
      window.alert("已启动批量任务 " + res.id + "，共 " + res.total + " 篇。\n进度会显示在「信息管理」标题旁，请勿关闭页面。");
      pollBatch(res.id);
    }).catch(function (err) {
      window.alert("启动失败：" + (err.message || "未知错误"));
    });
  }

  function pollBatch(id) {
    const tick = async function () {
      try {
        const st = await api("/api/spot-report/extract/batch/status?id=" + encodeURIComponent(id));
        renderBatchProgress(st);
        if (st.status === "running") {
          setTimeout(tick, 2500);
          return;
        }
        invalidateHomeData("");
        await refreshList();
        const lines = [
          "批量解析完成：" + st.succeeded + "/" + st.total + " 成功" + (st.failed ? "，" + st.failed + " 失败" : ""),
          st.overwritten ? "覆盖人工值 " + st.overwritten + " 条（已写审计，可在「变更记录」查看）" : "未覆盖任何人工值"
        ];
        if (st.errors && st.errors.length) lines.push("错误：" + st.errors.slice(0, 5).join("；"));
        window.alert(lines.join("\n"));
      } catch (err) {
        renderBatchProgress(null);
      }
    };
    tick();
  }

  function renderBatchProgress(st) {
    const host = document.querySelector(".spot-info-actions");
    if (!host) return;
    let box = document.getElementById("spot-batch-progress");
    if (!st) {
      if (box) box.remove();
      return;
    }
    if (!box) {
      box = el("span", { id: "spot-batch-progress", class: "spot-batch-progress" });
      host.insertBefore(box, host.firstChild);
    }
    box.textContent = st.status === "running"
      ? "重跑 " + st.done + "/" + st.total + (st.current ? "（" + st.current + "）" : "")
      : "重跑完成 " + st.succeeded + "/" + st.total + (st.overwritten ? "，覆盖人工值 " + st.overwritten + " 条" : "");
  }

  async function extractData(item) {
    if (!item || !item.date) return;
    const confirmed = window.confirm("对 " + item.date + " 的日报执行 OCR 解析？4 页识别约需 6-10 秒。");
    if (!confirmed) return;
    try {
      const res = await api("/api/spot-report/extract", {
        method: "POST",
        body: JSON.stringify({ date: item.date })
      });
      const warnings = (res && res.warnings) || [];
      const lines = [
        "解析完成：" + item.date,
        "状态：" + (res && res.status),
        "指标数：" + (res && res.metricCount),
        res && res.tradeDate && res.tradeDate !== item.date ? "表2 运行日：" + res.tradeDate : null,
        warnings.length ? "告警：" + warnings.join("；") : "校验无告警"
      ].filter(Boolean);
      window.alert(lines.join("\n"));
      invalidateHomeData(item.date);
      await refreshList();
    } catch (err) {
      window.alert("解析失败：" + (err.message || "未知错误"));
    }
  }

  async function refreshList() {
    const data = await api("/api/spot-report");
    listState = (data && data.list) || [];
    renderInfoPage();
  }

  function renderInfoPage() {
    const root = document.getElementById(PAGE_ID);
    if (!root) return;
    root.innerHTML = "";
    root.appendChild(el("div", { class: "spot-info-head" }, [
      el("div", { class: "spot-info-title", text: "信息管理" }),
      el("div", { class: "spot-info-actions" }, [
        isAdmin() ? el("button", { class: "spot-btn", text: "批量重跑解析", onClick: showBatchExtract }) : null,
        isAdmin() ? el("button", { class: "spot-btn primary", text: "新增采集", onClick: showCollectModal }) : null
      ])
    ]));
    const table = el("table", { class: "spot-info-table" });
    table.appendChild(el("thead", {}, [
      el("tr", {}, [
        el("th", { text: "运行日" }),
        el("th", { text: "标题" }),
        el("th", { text: "页数" }),
        el("th", { text: "操作" })
      ])
    ]));
    const tbody = el("tbody");
    if (!listState.length) {
      tbody.appendChild(el("tr", {}, [
        el("td", { colspan: "4", text: "暂无日报" })
      ]));
    } else {
      listState.forEach(function (item) {
        tbody.appendChild(el("tr", {}, [
          el("td", { text: item.date }),
          el("td", { text: item.title }),
          el("td", { text: String(item.pageCount || 0) }),
          el("td", {}, [
            el("button", { class: "spot-link", text: "查看", onClick: function () { showDetail(item); } }),
            isAdmin() ? el("button", { class: "spot-link", text: "解析数据", onClick: function () { extractData(item); } }) : null
          ])
        ]));
      });
    }
    table.appendChild(tbody);
    root.appendChild(table);
  }

  async function ensureInfoPage() {
    if (!isAdmin()) {
      history.replaceState({}, "", "/programs");
      return;
    }
    let page = document.getElementById(PAGE_ID);
    if (!page) {
      page = el("div", { id: PAGE_ID, class: "spot-info-page" });
      document.body.appendChild(page);
    }
    page.style.display = "block";
    try {
      await refreshList();
    } catch (err) {
      page.innerHTML = "";
      page.appendChild(el("div", { class: "spot-empty", text: err.message || "加载失败" }));
    }
  }

  function hideInfoPage() {
    const page = document.getElementById(PAGE_ID);
    if (page) page.style.display = "none";
    closeModal();
  }

  function sync() {
    const path = pathName();
    if (path === "/") {
      hideInfoPage();
      ensureHome();
    } else if (path === "/programs") {
      hideInfoPage();
      ensureCard();
    } else if (path === "/infoManagement") {
      ensureInfoPage();
    } else {
      hideInfoPage();
    }
    lastPath = path;
  }

  const push = history.pushState;
  history.pushState = function () {
    const ret = push.apply(this, arguments);
    setTimeout(sync, 30);
    return ret;
  };
  const replace = history.replaceState;
  history.replaceState = function () {
    const ret = replace.apply(this, arguments);
    setTimeout(sync, 30);
    return ret;
  };
  window.addEventListener("popstate", function () { setTimeout(sync, 30); });

  const observer = new MutationObserver(function () {
    if (pathName() !== lastPath) {
      sync();
      return;
    }
    if (pathName() === "/" && !document.getElementById(HOME_ID)) ensureHome();
    if (pathName() === "/programs" && isAdmin() && !document.getElementById(CARD_ID)) ensureCard();
  });
  observer.observe(document.documentElement, { childList: true, subtree: true });
  setInterval(sync, 1200);
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", sync);
  } else {
    sync();
  }
})();
