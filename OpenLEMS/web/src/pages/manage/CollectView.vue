<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  batchExtract,
  batchStatus,
  collectReport,
  confirmMetrics,
  DATASET_LABELS,
  extractReport,
  getReportData,
  isPending,
  listAudit,
  listReports,
  updateMetric,
  type BatchStatus,
  type SpotExtractInfo,
  type SpotMetric,
  type SpotMetricAudit,
  type SpotReport,
} from '@/api/spotreport'

/**
 * 管理 · 采集管理 —— overlay（spot-overlay.js）能力的迁入点。
 *
 * 覆盖 overlay 原来的四件事：
 *   1. 采集台：粘微信文章链接入库
 *   2. 日报档案：按运行日列出，带复核进度
 *   3. 结构化数据 / 复核队列：逐条修订 + 一键确认（origin 为 manual 才算人工确认，pm-api 由官方直采直接成立）
 *   4. 重跑：单日解析（只更新未复核值，已确认值原样保留）+ 批量重跑（先 dryRun 预览再执行）
 *
 * 不造数：拿不到数据就显示空态与原样错误，不复刻 overlay 的默认假数据。
 */
const URL_PLACEHOLDER = 'https://mp.weixin.qq.com/s/...'

const reports = ref<SpotReport[]>([])
const selectedDate = ref('')
const metrics = ref<SpotMetric[]>([])
const extract = ref<SpotExtractInfo | null>(null)
const tradeDate = ref('')
const audit = ref<SpotMetricAudit[]>([])

const loading = ref(false)
const detailLoading = ref(false)
const error = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const collectUrl = ref('')
const collecting = ref(false)
const extracting = ref(false)

/** 修订草稿：键为 `runDate|dataset|scope|metric` */
const edits = reactive<Record<string, string>>({})
const savingKey = ref('')

const batchPreview = ref<{ total: number; dates: string[]; skippedConfirmed: string[] } | null>(null)
const batch = ref<BatchStatus | null>(null)
const batchRunning = ref(false)
let pollTimer: number | undefined

const pendingMetrics = computed(() => metrics.value.filter(isPending))

/**
 * 「已确认值原样保留」在后端只有告警文本，接口没有结构化字段（不动后端签名），
 * 所以按前缀把文本拆回来。标题里的 N 就用明细条数：preserved 计数只存在于本次
 * 解析响应里，刷新页面即丢，用它会出现「标题 N 条、明细空」的自相矛盾。
 */
const PRESERVED_PREFIX = '保留已确认值：'
const FORCE_SKIP_PREFIX = '已跳过官方/人工数据'
const PRESERVED_RE =
  /^保留已确认值：运行日\s+(\S+)\s+(.+?)\/(.+?)（.*?）已确认\s+(.+?)，本次解析值\s+(.+?)\s+未覆盖/
const FORCE_SKIP_RE =
  /^已跳过官方\/人工数据（origin=(.+?)）：运行日\s+(\S+)\s+(.+?)\/(.+?)\s+保留\s+(.+?)，force 不覆盖，本次解析值\s+(.+?)\s+已丢弃/
const ORIGIN_LABELS: Record<string, string> = { 'pm-api': '官方直采', manual: '人工确认' }

interface PreservedNote {
  runDate: string
  scope: string
  metric: string
  oldValue: string
  newValue: string
  originLabel: string
}

function parsePreservedNotes(warnings: string[]): PreservedNote[] {
  const out: PreservedNote[] = []
  const seen = new Set<string>()
  for (const w of warnings) {
    let hit: PreservedNote | null = null
    const preserved = PRESERVED_RE.exec(w)
    const skipped = preserved ? null : FORCE_SKIP_RE.exec(w)
    if (preserved) {
      hit = {
        runDate: preserved[1],
        scope: preserved[2],
        metric: preserved[3],
        oldValue: preserved[4],
        newValue: preserved[5],
        originLabel: '',
      }
    } else if (skipped) {
      hit = {
        runDate: skipped[2],
        scope: skipped[3],
        metric: skipped[4],
        oldValue: skipped[5],
        newValue: skipped[6] === '本次未提取到' ? '' : skipped[6],
        originLabel: ORIGIN_LABELS[skipped[1]] ?? skipped[1],
      }
    }
    if (!hit) continue
    const key = `${hit.runDate}|${hit.scope}|${hit.metric}`
    if (seen.has(key)) continue
    seen.add(key)
    // 保留告警里不带 origin，从当前页数据按定位键回查（force 跳过的那条自带 origin）
    if (!hit.originLabel) {
      const m = metrics.value.find(
        (x) => x.runDate === hit!.runDate && x.scope === hit!.scope && x.metric === hit!.metric,
      )
      hit.originLabel = ORIGIN_LABELS[m?.origin ?? ''] ?? '已确认'
    }
    out.push(hit)
  }
  return out
}

const preservedNotes = computed(() => parsePreservedNotes(extract.value?.warnings ?? []))
const otherWarnings = computed(() =>
  (extract.value?.warnings ?? []).filter(
    (w) => !w.startsWith(PRESERVED_PREFIX) && !w.startsWith(FORCE_SKIP_PREFIX),
  ),
)
const preservedOpen = ref(false)
const grouped = computed(() => {
  const byDataset = new Map<string, SpotMetric[]>()
  for (const m of metrics.value) {
    if (!byDataset.has(m.dataset)) byDataset.set(m.dataset, [])
    byDataset.get(m.dataset)!.push(m)
  }
  return [...byDataset.entries()].sort(
    (a, b) => Object.keys(DATASET_LABELS).indexOf(a[0]) - Object.keys(DATASET_LABELS).indexOf(b[0]),
  )
})

function keyOf(m: SpotMetric): string {
  return `${m.runDate}|${m.dataset}|${m.scope}|${m.metric}`
}

/** 审计操作名：之前只认 edit，confirm/deprecated 全被写成「重跑覆盖」，事后排查会被误导 */
const AUDIT_OP_LABELS: Record<string, string> = {
  edit: '人工修订',
  confirm: '人工确认',
  overwritten_by_extract: '重跑覆盖',
  deprecated_by_pm_api: '官方源取代',
  restored_by_pm_api_scope_fix: '复原（作废范围修正）',
}
const auditOpLabel = (op: string) => AUDIT_OP_LABELS[op] ?? op

/** 审计时间：后端已统一成 Asia/Shanghai 的 RFC3339，这里只换成本地可读格式，不再做时区换算 */
function fmtAuditTime(s: string): string {
  return s ? s.slice(0, 19).replace('T', ' ') : '—'
}

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4500)
}

async function loadReports() {
  loading.value = true
  error.value = ''
  try {
    reports.value = await listReports()
    if (!selectedDate.value && reports.value.length) {
      await selectReport(reports.value[0].date)
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function selectReport(date: string) {
  selectedDate.value = date
  detailLoading.value = true
  try {
    const [data, auditList] = await Promise.all([
      getReportData(date),
      listAudit(date, 50).catch(() => []),
    ])
    metrics.value = data.metrics ?? []
    extract.value = data.extract ?? null
    tradeDate.value = data.tradeDate ?? ''
    audit.value = auditList
    for (const k of Object.keys(edits)) delete edits[k]
  } catch (err) {
    error.value = err instanceof Error ? err.message : '日报数据加载失败'
    metrics.value = []
    audit.value = []
  } finally {
    detailLoading.value = false
  }
}

async function doCollect() {
  const url = collectUrl.value.trim()
  if (!url) {
    showToast('red', '请先粘贴微信文章链接')
    return
  }
  collecting.value = true
  try {
    const report = await collectReport(url)
    showToast('green', `已归档 ${report.date}，共 ${report.pageCount} 页`)
    collectUrl.value = ''
    await loadReports()
    await selectReport(report.date)
  } catch (err) {
    showToast('red', `采集失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    collecting.value = false
  }
}

async function doExtract() {
  if (!selectedDate.value) return
  extracting.value = true
  try {
    const res = await extractReport(selectedDate.value, false)
    showToast(
      'green',
      `解析完成：${res.metricCount} 条指标，保留已确认 ${res.preserved} 条，仍待确认 ${res.pending} 条`,
    )
    await loadReports()
    await selectReport(selectedDate.value)
  } catch (err) {
    showToast('red', `解析失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    extracting.value = false
  }
}

async function saveMetric(m: SpotMetric) {
  const key = keyOf(m)
  const raw = edits[key]
  const value = Number(raw)
  if (raw === undefined || raw === '' || Number.isNaN(value)) {
    showToast('red', '请填写数值')
    return
  }
  savingKey.value = key
  try {
    await updateMetric({
      date: selectedDate.value,
      runDate: m.runDate,
      dataset: m.dataset,
      scope: m.scope,
      metric: m.metric,
      value,
    })
    showToast('green', `已修订：${m.metric}`)
    await loadReports()
    await selectReport(selectedDate.value)
  } catch (err) {
    showToast('red', `修订失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    savingKey.value = ''
  }
}

async function confirmAll() {
  if (!selectedDate.value) return
  try {
    const n = await confirmMetrics(selectedDate.value)
    showToast('green', `已确认 ${n} 条`)
    await loadReports()
    await selectReport(selectedDate.value)
  } catch (err) {
    showToast('red', `确认失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

/** 批量重跑：先 dryRun 预览会覆盖哪些日报 */
async function previewBatch() {
  try {
    const res = await batchExtract({ dryRun: true })
    batchPreview.value = { total: res.total, dates: res.dates ?? [], skippedConfirmed: res.skippedConfirmed ?? [] }
  } catch (err) {
    showToast('red', `预览失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

async function startBatch() {
  batchPreview.value = null
  batchRunning.value = true
  try {
    const res = await batchExtract({ includeConfirmed: false })
    batch.value = {
      id: res.id,
      status: 'running',
      total: res.total,
      done: 0,
      succeeded: 0,
      failed: 0,
      overwritten: 0,
      current: '',
      skippedConfirmed: res.skippedConfirmed ?? [],
      errors: [],
      startedAt: '',
      finishedAt: '',
    }
    pollTimer = window.setInterval(pollBatch, 2000)
  } catch (err) {
    batchRunning.value = false
    showToast('red', `启动失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

async function pollBatch() {
  try {
    const s = await batchStatus(batch.value?.id ?? '')
    batch.value = s
    if (s.status !== 'running') {
      batchRunning.value = false
      window.clearInterval(pollTimer)
      pollTimer = undefined
      showToast('green', `重跑结束：成功 ${s.succeeded} / 失败 ${s.failed}`)
      await loadReports()
      if (selectedDate.value) await selectReport(selectedDate.value)
    }
  } catch {
    batchRunning.value = false
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
}

onMounted(loadReports)
onBeforeUnmount(() => {
  window.clearInterval(pollTimer)
  window.clearTimeout(toastTimer)
})
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="loadReports">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="采集台" sub="粘贴微信公众号文章链接" :span="12" :foot="false">
      <div class="collect">
        <input
          v-model="collectUrl"
          :placeholder="URL_PLACEHOLDER"
          @keyup.enter="doCollect"
        />
        <button class="btn primary" type="button" :disabled="collecting" @click="doCollect">
          {{ collecting ? '采集中…' : '采集入库' }}
        </button>
      </div>
      <div class="hint">
        采集会抽取标题与 4 页原图并归档；同日再次采集会覆盖该日档案。归档后在下方对该日报做解析与复核。
      </div>
    </PanelBox>

    <PanelBox
      title="日报档案"
      :sub="`共 ${reports.length} 份`"
      :span="4"
      :loading="loading"
      foot="点行查看结构化数据与复核队列"
    >
      <template #actions>
        <button class="btn sm" type="button" @click="loadReports">刷新</button>
      </template>
      <ul class="reports">
        <li
          v-for="r in reports"
          :key="r.date"
          class="report"
          :class="{ on: r.date === selectedDate }"
          @click="selectReport(r.date)"
        >
          <div class="rdate">{{ r.date }}</div>
          <div class="rtitle">{{ r.title || '—' }}</div>
          <div class="rtags">
            <StatusTag tone="gray">{{ r.pageCount }} 页</StatusTag>
            <StatusTag v-if="r.pendingCount" tone="brand">未复核 {{ r.pendingCount }}</StatusTag>
            <StatusTag v-else tone="green">已全部确认</StatusTag>
          </div>
        </li>
        <li v-if="!reports.length && !loading" class="empty">档案柜是空的，先在上面采集一篇。</li>
      </ul>
    </PanelBox>

    <PanelBox
      title="结构化数据与复核"
      :sub="selectedDate ? `运行日 ${selectedDate}` : '未选择日报'"
      :span="8"
      :loading="detailLoading"
      :foot="extract ? `提取状态 ${extract.status} · OCR ${extract.ocrModel} · 提取器 ${extract.extractorVersion} · 更新于 ${extract.updatedAt}` : '尚未解析'"
    >
      <template #actions>
        <button class="btn sm" type="button" :disabled="!selectedDate || extracting" @click="doExtract">
          {{ extracting ? '解析中…' : '解析 / 重跑' }}
        </button>
        <button
          class="btn sm primary"
          type="button"
          :disabled="!pendingMetrics.length"
          @click="confirmAll"
        >
          确认全部（{{ pendingMetrics.length }}）
        </button>
      </template>

      <div class="hint">本次仅替换未复核值，官方直采与人工确认的值不会被覆盖。</div>

      <div v-if="!selectedDate" class="empty">左侧选一份日报。</div>
      <template v-else>
        <div v-if="tradeDate && tradeDate !== selectedDate" class="hint">
          表2 承载的滞后运行日是 {{ tradeDate }}（T-2 复核值），与当日口径不是同一天。
        </div>
        <div v-if="preservedNotes.length" class="preserved">
          <button type="button" class="preserved-head" @click="preservedOpen = !preservedOpen">
            本次未改动官方直采/人工确认指标 {{ preservedNotes.length }} 条
            <span class="preserved-toggle">{{ preservedOpen ? '收起明细' : '展开明细' }}</span>
          </button>
          <ul v-if="preservedOpen" class="preserved-list">
            <li v-for="n in preservedNotes" :key="`${n.runDate}|${n.scope}|${n.metric}`">
              {{ n.runDate }} · {{ n.scope }}/{{ n.metric }}：保留 {{ n.oldValue }}（{{ n.originLabel }}），{{
                n.newValue ? `本次解析值 ${n.newValue} 未采用` : '本次未提取到新值'
              }}
            </li>
          </ul>
        </div>

        <div v-if="otherWarnings.length" class="warn">
          解析告警：{{ otherWarnings.join('；') }}
        </div>

        <section v-for="[dataset, rows] in grouped" :key="dataset" class="dataset">
          <h4>
            {{ DATASET_LABELS[dataset] ?? dataset }}
            <StatusTag v-if="rows.filter(isPending).length" tone="brand">
              未复核 {{ rows.filter(isPending).length }}
            </StatusTag>
          </h4>
          <table>
            <thead>
              <tr>
                <th>对象</th>
                <th>指标</th>
                <th class="num">数值</th>
                <th>单位</th>
                <th>来源</th>
                <th>状态</th>
                <th>修订</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in rows" :key="keyOf(m)" :class="{ pending: isPending(m) }">
                <td>{{ m.scope }}</td>
                <td>
                  {{ m.metric }}
                  <span v-if="m.atTime" class="right">@{{ m.atTime }}</span>
                </td>
                <td class="num">{{ m.textValue || m.value }}</td>
                <td>{{ m.unit }}</td>
                <td class="right">{{ m.source }}</td>
                <td>
                  <StatusTag :tone="isPending(m) ? 'brand' : 'green'">
                    {{ isPending(m) ? '未复核' : '已确认' }}
                  </StatusTag>
                </td>
                <td class="edit">
                  <input
                    v-model="edits[keyOf(m)]"
                    type="number"
                    step="0.01"
                    :placeholder="String(m.value)"
                  />
                  <button
                    class="btn sm"
                    type="button"
                    :disabled="savingKey === keyOf(m)"
                    @click="saveMetric(m)"
                  >
                    {{ savingKey === keyOf(m) ? '…' : '写入' }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </section>

        <div v-if="!metrics.length" class="empty">
          该日报还没有结构化数据。点右上「解析 / 重跑」跑一次 OCR + 模板提取。
        </div>
      </template>
    </PanelBox>

    <PanelBox title="重跑与审计" :span="12" :foot="false">
      <div class="batch">
        <div class="batch-info">
          <strong>批量重跑</strong>
          <span class="muted">
            默认跳过「已全部人工复核」的日报；先预览再执行，避免误覆盖复核结果。
          </span>
        </div>
        <button class="btn sm" type="button" :disabled="batchRunning" @click="previewBatch">预览</button>
        <button class="btn sm primary" type="button" :disabled="batchRunning" @click="startBatch">
          执行
        </button>
        <span v-if="batch" class="muted">
          {{ batch.status }} · {{ batch.done }}/{{ batch.total }} · 成功 {{ batch.succeeded }} ·
          失败 {{ batch.failed }}
          <template v-if="batch.current">（当前 {{ batch.current }}）</template>
        </span>
      </div>

      <div v-if="batchPreview" class="preview">
        <div>
          将重跑 <strong>{{ batchPreview.total }}</strong> 份日报
          <template v-if="batchPreview.dates.length">：{{ batchPreview.dates.join('、') }}</template>
        </div>
        <div v-if="batchPreview.skippedConfirmed.length" class="muted">
          跳过（已人工复核）{{ batchPreview.skippedConfirmed.length }} 份：{{
            batchPreview.skippedConfirmed.join('、')
          }}
        </div>
        <div class="preview-actions">
          <button class="btn sm" type="button" @click="batchPreview = null">取消</button>
          <button class="btn sm primary" type="button" @click="startBatch">确认执行</button>
        </div>
      </div>

      <h4 class="audit-title">指标变更审计{{ selectedDate ? ` · ${selectedDate}` : '' }}</h4>
      <table v-if="audit.length">
        <thead>
          <tr>
            <th>时间</th>
            <th>运行日</th>
            <th>数据集</th>
            <th>对象 / 指标</th>
            <th class="num">旧值 → 新值</th>
            <th>操作</th>
            <th>操作人</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(a, i) in audit" :key="i">
            <td>{{ fmtAuditTime(a.createdAt) }}</td>
            <td>{{ a.runDate }}</td>
            <td>{{ DATASET_LABELS[a.dataset] ?? a.dataset }}</td>
            <td>{{ a.scope }} / {{ a.metric }}</td>
            <td class="num">
              {{ a.oldValue ?? '—' }} → <strong>{{ a.newValue ?? '—' }}</strong>
            </td>
            <td>
              <StatusTag :tone="a.operation === 'edit' ? 'blue' : 'brand'">
                {{ auditOpLabel(a.operation) }}
              </StatusTag>
            </td>
            <td>{{ a.operator || '—' }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">还没有变更记录。</div>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.notice {
  display: flex;
  align-items: center;
  gap: var(--s3);
  margin-bottom: var(--s4);
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--red);
  border-radius: var(--r-sm);
  background: var(--red-soft);
  color: var(--red-ink);
  font-size: var(--fs-13);
}
.notice button {
  margin-left: auto;
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.collect { display: flex; gap: var(--s3); }
.collect input {
  flex: 1;
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.collect input:focus-visible { outline: 2px solid var(--brand-line); outline-offset: 1px; }
.hint { margin-top: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.warn { margin-bottom: var(--s3); font-size: var(--fs-12); color: var(--brand-ink); }
.preserved { margin-bottom: var(--s3); }
.preserved-head {
  display: flex;
  align-items: center;
  gap: var(--s2);
  padding: 0;
  border: 0;
  background: none;
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.preserved-toggle { color: var(--text-3); text-decoration: underline; }
.preserved-list {
  margin: var(--s2) 0 0;
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-3);
  font-size: var(--fs-12);
  list-style: none;
}
.preserved-list li { padding: 2px 0; font-variant-numeric: tabular-nums; }
.reports { max-height: 520px; margin: 0; padding: 0; overflow-y: auto; list-style: none; }
.report { padding: var(--s3); border-bottom: 1px solid var(--divider); cursor: pointer; }
.report:hover { background: var(--bg-4); }
.report.on { background: var(--brand-soft); }
.rdate { font-size: var(--fs-13); font-weight: 600; font-variant-numeric: tabular-nums; }
.rtitle { margin: 2px 0 var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.rtags { display: flex; gap: var(--s1); flex-wrap: wrap; }
.dataset { margin-bottom: var(--s4); }
.dataset h4 { display: flex; align-items: center; gap: var(--s2); margin: 0 0 var(--s2); font-size: var(--fs-14); }
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--text-3);
  font-size: var(--fs-12);
  font-weight: 500;
  text-align: left;
}
td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); font-variant-numeric: tabular-nums; }
th.num, td.num { text-align: right; }
tr.pending { background: #fffdf6; }
.right { color: var(--text-3); font-size: var(--fs-12); }
.edit { display: flex; gap: var(--s1); }
.edit input {
  width: 92px;
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-12);
}
.batch { display: flex; align-items: center; gap: var(--s3); flex-wrap: wrap; margin-bottom: var(--s3); }
.batch-info { display: flex; flex-direction: column; flex: 1; min-width: 240px; }
.preview {
  margin-bottom: var(--s3);
  padding: var(--s3);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-13);
}
.preview-actions { display: flex; justify-content: flex-end; gap: var(--s2); margin-top: var(--s2); }
.audit-title { margin: var(--s4) 0 var(--s2); font-size: var(--fs-14); }
.empty { padding: var(--s4); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
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
.btn.primary { background: var(--brand-btn); color: #fff; }
.btn.primary:hover:not(:disabled) { background: var(--brand-btn-h); }
.toast {
  position: fixed;
  top: calc(var(--h-appbar) + var(--s2));
  left: 50%;
  z-index: 70;
  transform: translateX(-50%);
  padding: var(--s3) var(--s5);
  border-radius: var(--r-sm);
  font-size: var(--fs-13);
  box-shadow: 0 4px 12px rgb(0 0 0 / 12%);
}
.toast.green { background: var(--green-soft); color: var(--green-ink); }
.toast.red { background: var(--red-soft); color: var(--red-ink); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
