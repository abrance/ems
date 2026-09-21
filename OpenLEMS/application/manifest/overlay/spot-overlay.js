(function () {
  const HOME_ID = "spot-home-block";
  const CARD_ID = "spot-info-card";
  const PAGE_ID = "spot-info-page";
  const COL_ID = "spot-info-col";

  let homeState = { dates: [], date: "", report: null, page: 0, empty: true };
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
    const page = pages[homeState.page] || null;
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

    root.appendChild(el("div", { class: "spot-home-head" }, [
      el("div", { class: "spot-home-title", text: "电力现货" }),
      el("div", { class: "spot-date-nav" }, [
        prevDate,
        el("div", { class: "spot-date-label", text: homeState.date || "暂无日报" }),
        nextDate
      ])
    ]));

    const body = el("div", { class: "spot-home-body" });
    if (homeState.empty || !page) {
      body.appendChild(el("div", { class: "spot-empty", text: "暂无日报" }));
    } else {
      const prevPage = el("button", { class: "spot-page-btn", text: "<", onClick: function () {
        homeState.page = (homeState.page + pages.length - 1) % pages.length;
        renderHome();
      }});
      const nextPage = el("button", { class: "spot-page-btn", text: ">", onClick: function () {
        homeState.page = (homeState.page + 1) % pages.length;
        renderHome();
      }});
      const stage = el("div", { class: "spot-page-stage" }, [
        el("img", { src: page.url, alt: page.label }),
        el("div", { class: "spot-page-caption", text: (homeState.page + 1) + " / 4  " + page.label })
      ]);
      body.appendChild(prevPage);
      body.appendChild(stage);
      body.appendChild(nextPage);
    }
    root.appendChild(body);
  }

  async function loadHomeDate(date) {
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
    document.querySelectorAll(".spot-modal-mask").forEach(function (node) { node.remove(); });
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
        await refreshList();
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

  function showDetail(report) {
    closeModal();
    const pages = (report.pages || []).map(function (page) {
      return el("figure", {}, [
        el("img", { src: page.url, alt: page.label }),
        el("figcaption", { text: page.page + ". " + page.label })
      ]);
    });
    const mask = el("div", { class: "spot-modal-mask", onClick: function (ev) {
      if (ev.target === mask) closeModal();
    }}, [
      el("div", { class: "spot-modal" }, [
        el("h3", { text: report.title || "现货运行日报" }),
        el("div", { class: "spot-page-caption", text: "运行日 " + report.date }),
        el("div", { class: "spot-detail-pages" }, pages),
        el("div", { class: "spot-modal-actions" }, [
          el("button", { class: "spot-btn", text: "关闭", onClick: closeModal })
        ])
      ])
    ]);
    document.body.appendChild(mask);
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
      isAdmin() ? el("button", { class: "spot-btn primary", text: "新增采集", onClick: showCollectModal }) : null
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
          el("td", {}, [el("button", { class: "spot-link", text: "查看", onClick: function () { showDetail(item); } })])
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
