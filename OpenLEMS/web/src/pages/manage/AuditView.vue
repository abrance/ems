<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  deleteLogs,
  fetchLogInfo,
  fetchLogs,
  fetchLogStatistics,
  LOG_LEVELS,
  LOG_TYPES,
  type LogInfo,
  type LogLine,
  type LogStatistics,
} from '@/api/logs'

/**
 * 管理 · 审计。
 *
 * 现在只有**运行日志**（谁在跑什么）。设计稿 §11.4 规矩 5 想要的「谁在什么时候把哪个
 * 配置从 A 改成 B」需要一张变更审计表，后端还没有 —— 这一块在页面上明确标出，不拿
 * 运行日志冒充。
 *
 * 页内两个视角：
 *   运行日志 —— 按日期/类型/级别查，分页
 *   变更审计 —— 目前只汇总「配置类」日志（policy / settings 相关）供粗略追踪，
 *               并写清它不是真正的变更审计
 */
const tab = ref<'logs' | 'audit'>('logs')

const date = ref(new Date(Date.now() - new Date().getTimezoneOffset() * 60000).toISOString().slice(0, 10))
const type = ref('')
const level = ref('')
const page = ref(1)
const pageSize = 100

const lines = ref<LogLine[]>([])
const total = ref(0)
const info = ref<LogInfo | null>(null)
const stats = ref<LogStatistics | null>(null)
const loading = ref(false)
const error = ref('')
const detail = ref<LogLine | null>(null)

const deleteScope = ref<{ type: string; level: string } | null>(null)
const deleteConfirmText = ref('')

const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined
function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [p, i, s] = await Promise.all([
      fetchLogs({ date: date.value, type: type.value || undefined, level: level.value || undefined, page: page.value, pageSize }),
      fetchLogInfo().catch(() => null),
      fetchLogStatistics().catch(() => null),
    ])
    lines.value = p.lines
    total.value = p.total
    info.value = i
    stats.value = s
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
    lines.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function changeFilter() {
  page.value = 1
  void load()
}

function levelTone(l: string): 'gray' | 'blue' | 'brand' | 'red' {
  if (l === 'ERROR') return 'red'
  if (l === 'WARN') return 'brand'
  if (l === 'INFO') return 'blue'
  return 'gray'
}

function askDelete() {
  deleteScope.value = { type: type.value, level: level.value }
  deleteConfirmText.value = ''
}

async function confirmDelete() {
  const scope = deleteScope.value
  if (!scope) return
  if (deleteConfirmText.value.trim() !== 'DELETE') return
  try {
    const n = await deleteLogs({ type: scope.type || undefined, level: scope.level || undefined })
    deleteScope.value = null
    showToast('green', `已删除 ${n} 条日志`)
    void load()
  } catch (err) {
    showToast('red', `删除失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

/** 配置类日志：目前只能靠 type 粗筛，不是真正的变更审计 */
const configLikeLines = computed(() =>
  lines.value.filter((l) => l.type === 'policy' || l.type === 'ems'),
)

function fmtTime(t: string | null): string {
  return t ? t.replace('T', ' ').slice(0, 19) : '—'
}

onMounted(load)
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <div class="grid">
    <PanelBox
      title="审计"
      :sub="`共 ${total} 条`"
      :span="12"
      :loading="loading"
      :foot="info ? `日志总量 ${info.total} · 最早 ${info.firstLogTime} · 最新 ${info.latestLogTime}` : '数据来源：/api/log/biz'"
    >
      <template #actions>
        <label class="field">日期<input v-model="date" type="date" @change="changeFilter" /></label>
        <label class="field">
          类型
          <select v-model="type" @change="changeFilter">
            <option v-for="t in LOG_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
          </select>
        </label>
        <label class="field">
          级别
          <select v-model="level" @change="changeFilter">
            <option v-for="l in LOG_LEVELS" :key="l.value" :value="l.value">{{ l.label }}</option>
          </select>
        </label>
        <button class="btn sm" type="button" @click="load">刷新</button>
        <button class="btn sm danger" type="button" @click="askDelete">删除</button>
      </template>

      <div class="tabs-inline">
        <button type="button" :class="{ on: tab === 'logs' }" @click="tab = 'logs'">运行日志</button>
        <button type="button" :class="{ on: tab === 'audit' }" @click="tab = 'audit'">变更审计</button>
      </div>

      <div v-if="stats" class="kpis">
        <KpiCard label="ERROR" :value="stats.level.ERROR" :dir="stats.level.ERROR ? 'up' : undefined" />
        <KpiCard label="WARN" :value="stats.level.WARN" />
        <KpiCard label="INFO" :value="stats.level.INFO" />
        <KpiCard label="DEBUG" :value="stats.level.DEBUG" />
        <KpiCard label="device" :value="stats.type.device" />
        <KpiCard label="protocol" :value="stats.type.protocol" hint="policy / ems 见下方筛选" />
      </div>

      <!-- 运行日志 -->
      <template v-if="tab === 'logs'">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>级别</th>
              <th>类型</th>
              <th>内容</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="l in lines" :key="l.id" class="log-row" @click="detail = l">
              <td class="mono">{{ fmtTime(l.createdAt) }}</td>
              <td><StatusTag :tone="levelTone(l.level)">{{ l.level }}</StatusTag></td>
              <td>{{ l.type }}</td>
              <td class="content">{{ l.content }}</td>
            </tr>
            <tr v-if="!lines.length && !loading">
              <td colspan="4" class="empty">该日没有符合条件的日志。</td>
            </tr>
          </tbody>
        </table>

        <div class="pager">
          <button class="btn sm" type="button" :disabled="page <= 1" @click="page -= 1; load()">上一页</button>
          <span class="muted">第 {{ page }} / {{ totalPages }} 页</span>
          <button class="btn sm" type="button" :disabled="page >= totalPages" @click="page += 1; load()">下一页</button>
        </div>
      </template>

      <!-- 变更审计 -->
      <template v-else>
        <div class="notice">
          <strong>这不是真正的变更审计</strong>
          <span>
            　后端只有按类型/级别的运行日志，没有「谁把哪个配置从 A 改成 B」的变更记录表。
            下面只是把 policy / ems 两类日志挑出来做粗略追踪，不能当作合规意义的审计。
          </span>
        </div>
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>级别</th>
              <th>类型</th>
              <th>内容</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="l in configLikeLines" :key="l.id" class="log-row" @click="detail = l">
              <td class="mono">{{ fmtTime(l.createdAt) }}</td>
              <td><StatusTag :tone="levelTone(l.level)">{{ l.level }}</StatusTag>
              </td>
              <td>{{ l.type }}</td>
              <td class="content">{{ l.content }}</td>
            </tr>
            <tr v-if="!configLikeLines.length && !loading">
              <td colspan="4" class="empty">该日没有 policy / ems 类日志。</td>
            </tr>
          </tbody>
        </table>
      </template>
    </PanelBox>
  </div>

  <div v-if="detail" class="drawer-mask" @click.self="detail = null">
    <aside class="drawer">
      <header class="drawer-head">
        <h3>日志详情</h3>
        <StatusTag :tone="levelTone(detail.level)">{{ detail.level }}</StatusTag>
        <span class="spacer" />
        <button class="btn sm" type="button" @click="detail = null">关闭</button>
      </header>
      <div class="drawer-body">
        <div class="row"><span>时间</span><b class="mono">{{ fmtTime(detail.createdAt) }}</b></div>
        <div class="row"><span>类型</span><b>{{ detail.type }}</b></div>
        <div class="row"><span>ID</span><b class="mono">{{ detail.id }}</b></div>
        <pre class="content-full">{{ detail.content }}</pre>
      </div>
    </aside>
  </div>

  <div v-if="deleteScope" class="modal-mask" @click.self="deleteScope = null">
    <div class="modal danger-modal">
      <h3>删除日志</h3>
      <p class="muted">
        将删除“{{ deleteScope.type || '全部类型' }} / {{ deleteScope.level || '全部级别' }}”的日志。
        <strong>不支持按日期删除</strong>，一旦执行不可撤销。
      </p>
      <input v-model="deleteConfirmText" placeholder="输入 DELETE 确认" />
      <div class="modal-actions">
        <button class="btn" type="button" @click="deleteScope = null">取消</button>
        <button class="btn danger" type="button" :disabled="deleteConfirmText.trim() !== 'DELETE'" @click="confirmDelete">
          确认删除
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.notice {
  display: flex;
  align-items: flex-start;
  gap: var(--s3);
  margin-bottom: var(--s4);
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-13);
}
.notice.error { border-left-color: var(--red); background: var(--red-soft); color: var(--red-ink); align-items: center; }
.notice.error button {
  margin-left: auto;
  padding: 0 var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.tabs-inline {
  display: flex;
  gap: var(--s5);
  margin: calc(-1 * var(--s4)) calc(-1 * var(--s4)) var(--s4);
  padding: 0 var(--s4);
  border-bottom: 1px solid var(--border);
}
.tabs-inline button {
  position: relative;
  padding: var(--s3) 0;
  border: 0;
  background: none;
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.tabs-inline button.on { color: var(--brand-ink); font-weight: 600; }
.tabs-inline button.on::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 2px;
  background: var(--brand);
}
.kpis { display: grid; grid-template-columns: repeat(6, 1fr); gap: var(--s4); margin-bottom: var(--s4); }
.field { display: inline-flex; align-items: center; gap: var(--s1); font-size: var(--fs-12); color: var(--text-3); }
.field input,
.field select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-12);
}
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--text-3);
  font-size: var(--fs-12);
  font-weight: 500;
  text-align: left;
  white-space: nowrap;
}
td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); }
td.mono { font-variant-numeric: tabular-nums; white-space: nowrap; }
td.content {
  max-width: 640px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-2);
}
.log-row { cursor: pointer; }
.log-row:hover { background: var(--bg-4); }
.pager { display: flex; align-items: center; gap: var(--s3); margin-top: var(--s3); }
.spacer { flex: 1; }
.empty { padding: var(--s5); color: var(--text-3); text-align: center; }
.muted { color: var(--text-3); font-size: var(--fs-12); }
.btn {
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.btn:hover { background: #e9eaee; }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn.sm { padding: 0 var(--s2); font-size: var(--fs-12); }
.btn.danger { background: var(--red-soft); color: var(--red-ink); }
.drawer-mask {
  position: fixed;
  inset: 0;
  z-index: 55;
  display: flex;
  justify-content: flex-end;
  background: rgb(29 33 41 / 35%);
}
.drawer {
  display: flex;
  flex-direction: column;
  width: min(680px, 100%);
  background: var(--bg-2);
  border-left: 1px solid var(--border);
}
.drawer-head {
  display: flex;
  align-items: center;
  gap: var(--s3);
  padding: var(--s4);
  border-bottom: 1px solid var(--border);
}
.drawer-head h3 { margin: 0; font-size: var(--fs-16); }
.drawer-body { flex: 1; padding: var(--s4); overflow-y: auto; }
.row { display: flex; gap: var(--s3); margin-bottom: var(--s3); font-size: var(--fs-13); }
.row > span { min-width: 64px; color: var(--text-3); font-size: var(--fs-12); }
.row b { font-weight: 600; }
.content-full {
  margin: 0;
  padding: var(--s3);
  border-radius: var(--r-sm);
  background: var(--bg-4);
  font-size: var(--fs-12);
  white-space: pre-wrap;
  word-break: break-all;
}
.modal-mask {
  position: fixed;
  inset: 0;
  z-index: 65;
  display: grid;
  place-items: center;
  padding: var(--s4);
  background: rgb(29 33 41 / 45%);
}
.modal {
  width: min(480px, 100%);
  padding: var(--s5);
  border-top: 3px solid var(--red);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.modal input {
  width: 100%;
  margin: var(--s3) 0 var(--s4);
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(3, 1fr); }
}
</style>
