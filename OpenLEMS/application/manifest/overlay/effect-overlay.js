/*
 * 储能效果评估悬浮层
 *
 * 与 spot-overlay.js 同一套路：不改 web.zip，运行时注入。
 *   - 在「系统管理 → 配置」卡片区插入一张「效果评估」卡片；
 *   - 自建路由 /effect，整页展示：
 *       实测效果（套利 + 需量节省）
 *       算法回测对比（idle / tou-rule / tou-rule+demand）
 *       负荷预测曲线（TimesFM，实际 vs 预测 + 分位带）
 *
 * 依赖后端接口：
 *   GET /api/effect/summary   GET /api/effect/backtest   GET /api/forecast/load
 */
(function () {
  const CARD_ID = "effect-cfg-card";
  const CARD_COL_ID = "effect-cfg-card-col";
  const PAGE_ID = "effect-page";

  const state = {
    start: "",
    end: "",
    summary: null, summaryError: "",
    backtest: null, backtestError: "",
    forecast: null, forecastError: "",
    loadedKey: ""
  };

  // ---------- 基础工具 ----------
  function token() {
    return localStorage.getItem("ems:user:token") || "";
  }
  function pathName() {
    return location.pathname.replace(/\/+$/, "") || "/";
  }

  async function api(path) {
    const headers = { "Accept-Language": "zh-CN" };
    const t = token();
    if (t) headers.Authorization = "Bearer " + t;
    const res = await fetch(path, { credentials: "include", headers: headers });
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

  function fmtNum(v, digits) {
    if (typeof v !== "number" || !isFinite(v)) return "-";
    return v.toFixed(digits == null ? 1 : digits);
  }
  function fmtMoney(v) {
    if (typeof v !== "number" || !isFinite(v)) return "-";
    return "¥" + v.toFixed(2);
  }
  function pad2(n) { return ("0" + n).slice(-2); }
  function fmtDate(d) {
    return d.getFullYear() + "-" + pad2(d.getMonth() + 1) + "-" + pad2(d.getDate());
  }
  function fmtClock(ms) {
    const d = new Date(ms);
    return pad2(d.getMonth() + 1) + "-" + pad2(d.getDate()) + " " + pad2(d.getHours()) + ":" + pad2(d.getMinutes());
  }
  function fmtClockShort(ms) {
    const d = new Date(ms);
    return pad2(d.getHours()) + ":" + pad2(d.getMinutes());
  }

  function setMsg(id, text) {
    const node = document.getElementById(id);
    if (!node) return;
    node.textContent = text || "";
    node.style.display = text ? "" : "none";
  }

  // ---------- SVG 基础 ----------
  const SVG_NS = "http://www.w3.org/2000/svg";
  function svgNode(tag, attrs) {
    const node = document.createElementNS(SVG_NS, tag);
    Object.keys(attrs || {}).forEach(function (k) { node.setAttribute(k, attrs[k]); });
    return node;
  }

  // ---------- 卡片入口 ----------
  // 插到「系统管理」的配置卡片行，与其它卡片等宽
  function ensureCard() {
    const cat = document.getElementById("cat-Config");
    if (!cat) return;
    const row = cat.querySelector(".arco-row") || cat.querySelector("[class*='arco-row']") || cat.lastElementChild;
    if (!row) return;

    // 兄弟卡片宽度由内联样式决定，且 Vue 首帧后才补上：创建时抄一次，之后每次同步
    const sibling = row.firstElementChild;
    const widthStyle = sibling && sibling.getAttribute("style");
    const col = document.getElementById(CARD_COL_ID) || el("div", {
      id: CARD_COL_ID,
      class: sibling ? sibling.className : ""
    });
    if (widthStyle && sibling !== col) col.setAttribute("style", widthStyle);
    if (document.getElementById(CARD_ID)) return;

    const card = el("div", {
      id: CARD_ID,
      class: "effect-card",
      onClick: openPage
    }, [
      el("div", { class: "effect-card-icon", text: "效" }),
      el("div", { class: "effect-card-name", text: "效果评估" }),
      el("div", { class: "effect-card-desc", text: "省电费测算与算法对比" })
    ]);
    col.appendChild(card);
    row.appendChild(col);
  }

  // ---------- 页面 ----------
  function openPage() {
    history.pushState({}, "", "/effect");
    sync();
  }
  function closePage() {
    history.pushState({}, "", "/programs");
    sync();
  }

  function buildPage(page) {
    const range = defaultRange();
    state.start = range.start;
    state.end = range.end;

    const startInput = el("input", { type: "date", id: "effect-start", class: "effect-input", value: state.start });
    const endInput = el("input", { type: "date", id: "effect-end", class: "effect-input", value: state.end });
    startInput.addEventListener("change", function () { state.start = startInput.value; refresh(true); });
    endInput.addEventListener("change", function () { state.end = endInput.value; refresh(true); });

    page.appendChild(el("div", { class: "effect-head" }, [
      el("div", { class: "effect-heading" }, [
        el("div", { class: "effect-title", text: "储能效果评估" }),
        el("div", { class: "effect-sub", text: "峰谷套利 + 需量节省 · 实测与算法回测" })
      ]),
      el("div", { class: "effect-controls" }, [
        el("span", { class: "effect-controls-label", text: "开始" }), startInput,
        el("span", { class: "effect-controls-label", text: "结束" }), endInput,
        el("button", { class: "effect-btn", text: "查询", onClick: function () { refresh(true); } }),
        el("button", { class: "effect-btn", text: "返回", onClick: closePage })
      ])
    ]));

    page.appendChild(el("div", { class: "effect-section" }, [
      el("div", { class: "effect-section-title", text: "实测效果" }),
      el("div", { class: "effect-msg", id: "effect-summary-msg" }),
      el("div", { class: "effect-stats", id: "effect-stats" })
    ]));

    page.appendChild(el("div", { class: "effect-section" }, [
      el("div", { class: "effect-section-title", text: "算法回测对比" }),
      el("div", { class: "effect-sub", text: "同一段历史负荷上模拟各算法，用同一成本模型结算" }),
      el("div", { class: "effect-msg", id: "effect-backtest-msg" }),
      el("div", { id: "effect-backtest-table" })
    ]));

    page.appendChild(el("div", { class: "effect-section" }, [
      el("div", { class: "effect-section-title", text: "负荷预测（TimesFM）" }),
      el("div", { class: "effect-msg", id: "effect-forecast-msg" }),
      el("div", { id: "effect-forecast-chart" })
    ]));
  }

  function defaultRange() {
    const end = new Date();
    const start = new Date(end.getTime() - 6 * 86400000);
    return { start: fmtDate(start), end: fmtDate(end) };
  }

  function ensurePage() {
    let page = document.getElementById(PAGE_ID);
    if (!page) {
      page = el("div", { id: PAGE_ID, class: "effect-page" });
      document.body.appendChild(page);
      buildPage(page);
    }
    page.style.display = "block";
    refresh(false);
  }

  function hidePage() {
    const page = document.getElementById(PAGE_ID);
    if (page) page.style.display = "none";
  }

  // ---------- 数据加载 ----------
  function refresh(force) {
    if (!state.start || !state.end) return;
    const key = state.start + "~" + state.end;
    if (!force && state.loadedKey === key) return;
    state.loadedKey = key;

    loadSummary();
    loadBacktest();
    loadForecast();
  }

  async function loadSummary() {
    setMsg("effect-summary-msg", "加载中…");
    try {
      const data = await api("/api/effect/summary?start=" + state.start + "&end=" + state.end);
      state.summary = (data && data.summary) || null;
      state.summaryError = "";
    } catch (err) {
      state.summary = null;
      state.summaryError = err.message || "加载失败";
    }
    renderSummary();
  }

  function renderSummary() {
    const stats = document.getElementById("effect-stats");
    if (!stats) return;
    stats.innerHTML = "";
    if (state.summaryError) {
      setMsg("effect-summary-msg", state.summaryError);
      return;
    }
    const s = state.summary;
    if (!s) {
      setMsg("effect-summary-msg", "无数据");
      return;
    }
    setMsg("effect-summary-msg", "当前算法：" + (s.algorithm || "-") + " · 关口 " + (s.gridDeviceId || "-"));

    const items = [
      ["峰谷套利净收益", fmtMoney(s.arbitrageProfit),
        "放电收益 " + fmtMoney(s.dischargeRevenue) + " − 充电电费 " + fmtMoney(s.chargeCost)],
      ["需量节省", fmtMoney(s.demandSaving),
        "需量 " + fmtNum(s.baselineMaxDemand, 1) + " → " + fmtNum(s.actualMaxDemand, 1) + " kW"],
      ["总节省", fmtMoney(s.totalSaving), "套利 + 需量"],
      ["充 / 放电量", fmtNum(s.chargeEnergy, 1) + " / " + fmtNum(s.dischargeEnergy, 1) + " kWh",
        "采样点 " + (s.points || 0)]
    ];
    items.forEach(function (it) {
      stats.appendChild(el("div", { class: "effect-stat" }, [
        el("div", { class: "effect-stat-label", text: it[0] }),
        el("div", { class: "effect-stat-value", text: it[1] }),
        el("div", { class: "effect-stat-desc", text: it[2] })
      ]));
    });
  }

  async function loadBacktest() {
    setMsg("effect-backtest-msg", "加载中…");
    try {
      const data = await api("/api/effect/backtest?start=" + state.start + "&end=" + state.end);
      state.backtest = (data && data.backtest) || null;
      state.backtestError = "";
    } catch (err) {
      state.backtest = null;
      state.backtestError = err.message || "加载失败";
    }
    renderBacktest();
  }

  function renderBacktest() {
    const wrap = document.getElementById("effect-backtest-table");
    if (!wrap) return;
    wrap.innerHTML = "";
    if (state.backtestError) {
      setMsg("effect-backtest-msg", state.backtestError);
      return;
    }
    const b = state.backtest;
    if (!b || !b.results || !b.results.length) {
      setMsg("effect-backtest-msg", "无回测结果");
      return;
    }
    setMsg("effect-backtest-msg",
      "等效电池 " + fmtNum(b.ratedPowerKw, 0) + " kW / " + fmtNum(b.capacityKwh, 0) + " kWh" +
      " · 需量目标 " + fmtNum(b.demandTarget, 0) + " kW" +
      " · 需量电价 " + fmtNum(b.demandPrice, 0) + " 元/kW" +
      " · 采样点 " + (b.points || 0));

    const cols = ["算法", "充电量 kWh", "放电量 kWh", "套利净收益", "需量节省", "总节省", "终止SOC %"];
    const head = el("tr", {}, cols.map(function (c) { return el("th", { text: c }); }));
    const tbody = el("tbody", {}, b.results.map(function (r) {
      const isIdle = r.algorithm === "idle";
      return el("tr", { class: isIdle ? "effect-row-baseline" : "" }, [
        el("td", {}, [el("span", { class: "effect-algo", text: r.algorithm + (isIdle ? "（基线）" : "") })]),
        el("td", { class: "effect-num", text: fmtNum(r.chargeEnergy, 1) }),
        el("td", { class: "effect-num", text: fmtNum(r.dischargeEnergy, 1) }),
        el("td", { class: "effect-num", text: fmtMoney(r.arbitrageProfit) }),
        el("td", { class: "effect-num", text: fmtMoney(r.demandSaving) }),
        el("td", { class: "effect-num effect-strong", text: fmtMoney(r.totalSaving) }),
        el("td", { class: "effect-num", text: fmtNum(r.finalSocPct, 1) })
      ]);
    }));
    wrap.appendChild(el("table", { class: "effect-table" }, [el("thead", {}, [head]), tbody]));
  }

  async function loadForecast() {
    setMsg("effect-forecast-msg", "加载中…");
    try {
      const data = await api("/api/forecast/load?historyHours=24");
      state.forecast = (data && data.forecast) || null;
      state.forecastError = "";
    } catch (err) {
      state.forecast = null;
      state.forecastError = err.message || "加载失败";
    }
    renderForecast();
  }

  function renderForecast() {
    const wrap = document.getElementById("effect-forecast-chart");
    if (!wrap) return;
    wrap.innerHTML = "";
    if (state.forecastError) {
      setMsg("effect-forecast-msg", state.forecastError);
      return;
    }
    const f = state.forecast;
    if (!f || !f.forecastTimes || !f.forecastTimes.length) {
      setMsg("effect-forecast-msg", "无预测结果");
      return;
    }
    setMsg("effect-forecast-msg",
      "设备 " + (f.deviceId || "-") + " · 模型 " + (f.model || "-") +
      " · 步长 " + fmtNum(f.stepMinutes, 0) + " 分钟" +
      " · 送入 " + (f.lookbackPoints || 0) + " 点" + (f.inputTruncated ? "（已截断至模型上限）" : "") +
      " · 耗时 " + fmtNum(f.timeMs, 0) + " ms");

    const histT = f.historyTimes || [], histV = f.historyValues || [];
    const fcT = f.forecastTimes || [], fcV = f.forecastPoint || [];
    const q = f.forecastQuantiles || {};
    const loV = q["0.1"] || [], hiV = q["0.9"] || [];

    // 时间轴：历史 + 预测（步长一致，直接拼接即可）
    const times = histT.concat(fcT);
    const nH = histT.length, nF = fcT.length, n = times.length;
    if (n < 2) {
      setMsg("effect-forecast-msg", "数据点不足，无法绘图");
      return;
    }

    let min = Infinity, max = -Infinity;
    [histV, fcV, loV, hiV].forEach(function (arr) {
      arr.forEach(function (v) {
        if (typeof v === "number" && isFinite(v)) {
          if (v < min) min = v;
          if (v > max) max = v;
        }
      });
    });
    if (!isFinite(min) || !isFinite(max)) {
      setMsg("effect-forecast-msg", "预测数据为空");
      return;
    }
    if (min === max) { min -= 1; max += 1; }
    const padY = (max - min) * 0.12;
    min -= padY; max += padY;

    const W = 760, H = 230, L = 62, R = 16, T = 14, B = 30;
    const xOf = function (i) { return L + i * (W - L - R) / (n - 1); };
    const yOf = function (v) { return T + (max - v) * (H - T - B) / (max - min); };

    const svg = svgNode("svg", { viewBox: "0 0 " + W + " " + H, class: "effect-svg" });

    // Y 轴网格
    for (let i = 0; i <= 4; i++) {
      const value = min + (max - min) * i / 4;
      const y = yOf(value);
      svg.appendChild(svgNode("line", { x1: L, y1: y, x2: W - R, y2: y, class: "effect-svg-grid" }));
      const label = svgNode("text", { x: L - 6, y: y + 4, class: "effect-svg-axis", "text-anchor": "end" });
      label.textContent = fmtNum(value, 0);
      svg.appendChild(label);
    }

    // 历史与预测分界
    if (nH > 0 && nF > 0) {
      const xb = xOf(nH - 1);
      svg.appendChild(svgNode("line", { x1: xb, y1: T, x2: xb, y2: H - B, class: "effect-svg-divider" }));
    }

    // 预测分位带（0.1 ~ 0.9）
    if (loV.length === nF && hiV.length === nF && nF > 0) {
      const upper = [], lower = [];
      for (let i = 0; i < nF; i++) {
        const x = xOf(nH + i);
        upper.push(x + "," + yOf(hiV[i]));
      }
      for (let i = nF - 1; i >= 0; i--) {
        lower.push(xOf(nH + i) + "," + yOf(loV[i]));
      }
      svg.appendChild(svgNode("polygon", { points: upper.concat(lower).join(" "), class: "effect-svg-band" }));
    }

    // 历史折线
    if (nH > 0) {
      svg.appendChild(svgNode("polyline", {
        points: histV.map(function (v, i) { return xOf(i) + "," + yOf(v); }).join(" "),
        class: "effect-svg-line effect-svg-history"
      }));
    }
    // 预测折线：从历史末点起画，视觉连续
    if (nF > 0) {
      const pts = [];
      if (nH > 0) pts.push(xOf(nH - 1) + "," + yOf(histV[nH - 1]));
      fcV.forEach(function (v, i) { pts.push(xOf(nH + i) + "," + yOf(v)); });
      svg.appendChild(svgNode("polyline", { points: pts.join(" "), class: "effect-svg-line effect-svg-forecast" }));
    }

    // X 轴刻度
    [0, Math.floor((n - 1) / 2), n - 1].forEach(function (i, idx, arr) {
      if (idx > 0 && i === arr[idx - 1]) return;
      const label = svgNode("text", {
        x: xOf(i), y: H - B + 18, class: "effect-svg-axis",
        "text-anchor": i === 0 ? "start" : (i === n - 1 ? "end" : "middle")
      });
      label.textContent = fmtClockShort(times[i]);
      svg.appendChild(label);
    });

    wrap.appendChild(svg);
    wrap.appendChild(el("div", { class: "effect-legend" }, [
      el("span", { class: "effect-legend-item" }, [el("i", { class: "effect-legend-dot effect-legend-actual" }), el("span", { text: "实际" })]),
      el("span", { class: "effect-legend-item" }, [el("i", { class: "effect-legend-dot effect-legend-pred" }), el("span", { text: "预测" })]),
      el("span", { class: "effect-legend-item" }, [el("i", { class: "effect-legend-dot effect-legend-band" }), el("span", { text: "预测 10%~90% 分位" })]),
      el("span", { class: "effect-legend-item" }, [el("span", { class: "effect-legend-hint", text: "分界 " + (nH ? fmtClock(times[nH - 1]) : "-") })]),
      el("span", { class: "effect-legend-item" }, [el("span", { class: "effect-legend-hint", text: "预测起 " + (nF ? fmtClock(fcT[0]) : "-") })]),
    ]));
  }

  // ---------- 路由同步 ----------
  function sync() {
    const path = pathName();
    if (path === "/programs") {
      hidePage();
      ensureCard();
    } else if (path === "/effect") {
      ensurePage();
    } else {
      hidePage();
    }
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
    if (pathName() === "/programs" && !document.getElementById(CARD_ID)) ensureCard();
  });
  observer.observe(document.documentElement, { childList: true, subtree: true });
  setInterval(sync, 1200);

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", sync);
  } else {
    sync();
  }
})();
