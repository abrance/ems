import "./styles.css";

const app = document.querySelector("#app");
const state = {
  url: "https://mp.weixin.qq.com/s/pxdJ0CyMva1evGOKsCpE0w",
  status: "",
  statusType: "",
  loading: false,
  config: { qiniu: false, storage: "local" },
  records: [],
  current: null,
  notes: "",
};

boot();

async function boot() {
  render();
  await Promise.all([loadConfig(), loadRecords()]);
  render();
}

async function loadConfig() {
  const res = await fetch("/api/config");
  state.config = await res.json();
}

async function loadRecords() {
  const res = await fetch("/api/records");
  const data = await res.json();
  state.records = data.list || [];
}

async function collect() {
  const url = state.url.trim();
  if (!url) {
    state.status = "请先粘贴微信文章链接";
    state.statusType = "error";
    render();
    return;
  }
  state.loading = true;
  state.status = "正在拉取文章、拆图并归档...";
  state.statusType = "";
  render();
  try {
    const res = await fetch("/api/collect", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "采集失败");
    state.status = `已归档 ${data.date}，共 ${data.images.length} 页`;
    state.statusType = "ok";
    await loadRecords();
    state.current = data;
    state.notes = data.notes || "";
  } catch (err) {
    state.status = err.message;
    state.statusType = "error";
  } finally {
    state.loading = false;
    render();
  }
}

async function openRecord(date) {
  const res = await fetch(`/api/records/${date}`);
  const data = await res.json();
  if (!res.ok) {
    state.status = data.error;
    state.statusType = "error";
    render();
    return;
  }
  state.current = data;
  state.notes = data.notes || "";
  render();
}

async function saveNotes() {
  if (!state.current) return;
  const res = await fetch(`/api/records/${state.current.date}/notes`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ notes: state.notes }),
  });
  const data = await res.json();
  if (!res.ok) {
    state.status = data.error;
    state.statusType = "error";
    render();
    return;
  }
  state.current = data;
  state.status = "批注已写入档案";
  state.statusType = "ok";
  render();
}

function closeReader() {
  state.current = null;
  render();
}

function render() {
  app.innerHTML = `
    <header class="masthead">
      <div>
        <p class="kicker">GUANGDONG SPOT MARKET</p>
        <h1>现货运行日报采集</h1>
        <p class="hint">把广东电力交易中心公众号日报链接贴进来，抽取标题、年月日和固定版式图片，格式化后入档案柜。</p>
      </div>
      <div class="seal">已<br />归档<small>LEDGER</small></div>
    </header>
    <div class="layout">
      <aside class="panel">
        <h2>采集台</h2>
        <label>微信文章链接</label>
        <input class="url-box" id="url" value="${escapeHtml(state.url)}" placeholder="https://mp.weixin.qq.com/s/..." />
        <div class="row">
          <button class="btn" id="collect" ${state.loading ? "disabled" : ""}>${state.loading ? "采集中" : "采集入库"}</button>
        </div>
        <p class="hint">
          <span class="chip">${state.config.qiniu ? "七牛云" : "本地存储"}</span>
          当前未配置七牛时，图片会先落到本机 /uploads，配好密钥后同一流程会改传到七牛。
        </p>
        <p class="status ${state.statusType}">${escapeHtml(state.status)}</p>
      </aside>
      <section>
        <div class="sheet" style="padding:22px;margin-bottom:16px;">
          <h2>档案柜 · ${state.records.length} 份</h2>
          <p class="meta">按运行日存放。点开后可阅读四页原图，并补写当日批注。</p>
        </div>
        ${renderArchive()}
      </section>
    </div>
    ${state.current ? renderReader(state.current) : ""}
  `;

  app.querySelector("#url")?.addEventListener("input", (e) => {
    state.url = e.target.value;
  });
  app.querySelector("#url")?.addEventListener("keydown", (e) => {
    if (e.key === "Enter") collect();
  });
  app.querySelector("#collect")?.addEventListener("click", collect);
  app.querySelectorAll("[data-date]").forEach((el) => {
    el.addEventListener("click", () => openRecord(el.dataset.date));
  });
  app.querySelector("#close")?.addEventListener("click", closeReader);
  app.querySelector("#notes")?.addEventListener("input", (e) => {
    state.notes = e.target.value;
  });
  app.querySelector("#save-notes")?.addEventListener("click", saveNotes);
}

function renderArchive() {
  if (!state.records.length) {
    return `<div class="empty">档案柜还是空的。先采集一篇 09.21 这样的运行日报。</div>`;
  }
  return `<div class="archive">${state.records
    .map(
      (item) => `
      <article class="card" data-date="${item.date}">
        <p class="date">${pad(item.month)}.${pad(item.day)}</p>
        <p class="title">${escapeHtml(item.title)}</p>
        <p class="meta">${item.year} · ${item.pageCount} 页</p>
      </article>`
    )
    .join("")}</div>`;
}

function renderReader(record) {
  return `
    <div class="reader">
      <article class="reader-sheet">
        <div class="reader-head">
          <div>
            <p class="kicker">${escapeHtml(record.author || "广东电力交易中心")}</p>
            <p class="big-date">${record.year}.${pad(record.month)}.${pad(record.day)}</p>
            <p>${escapeHtml(record.title)}</p>
          </div>
          <button class="btn ghost" id="close">关闭</button>
        </div>
        <div class="facts">
          <div class="fact"><span>年</span><b>${record.year}</b></div>
          <div class="fact"><span>月</span><b>${pad(record.month)}</b></div>
          <div class="fact"><span>日</span><b>${pad(record.day)}</b></div>
          <div class="fact"><span>页数</span><b>${record.images.length}</b></div>
        </div>
        <p class="meta">来源：<a href="${escapeHtml(record.sourceUrl)}" target="_blank" rel="noreferrer">${escapeHtml(record.sourceUrl)}</a></p>
        <div class="pages">
          ${record.images
            .map(
              (img) => `
            <figure class="page">
              <img src="${escapeHtml(img.url)}" alt="${escapeHtml(img.label)}" />
              <figcaption><span>${escapeHtml(img.label)}</span><span>第 ${img.page} 页 · ${img.storage}</span></figcaption>
            </figure>`
            )
            .join("")}
        </div>
        <h2 style="margin-top:28px;">当日批注</h2>
        <textarea id="notes">${escapeHtml(state.notes)}</textarea>
        <div class="row">
          <button class="btn" id="save-notes">写入档案</button>
        </div>
      </article>
    </div>
  `;
}

function pad(n) {
  return String(n).padStart(2, "0");
}

function escapeHtml(value) {
  return String(value || "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}
