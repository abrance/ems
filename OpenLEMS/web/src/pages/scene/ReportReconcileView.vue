<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  datasetLabel,
  fetchReportReconcile,
  type Reconcile,
  type ReconcileDay,
  type ReconcileItem,
} from '@/api/reportdaily'

/**
 * 市场数据 · 日报核对（只读）。
 *
 * 一页两列一差值：
 *   左 = 日报原值（OCR，独立表 gd_report_daily）
 *   右 = 官网直采（pm-api，官方披露接口）
 *   差值 = 只在**真同义**的 2 行上算（日前/实时 发电侧均价），后端算好给前端。
 *
 * 三条不可动的口径（用户拍的）：
 *   1. OCR 落独立表，绝不写 gd_spot_metric —— 这页也就没有任何写回/确认/覆盖按钮；
 *   2. 左/右/差值三列各读各的源，不覆盖、不合并、无 fallback；
 *   3. 缺日空态：reportValue 为 null 就显示「—」，**不填 0、不画 0 线**。
 *
 * 为什么不给「通过/不通过」结论：口径没定阈值，前端自己定阈值就是造结论。
 * 着色只按 |Δ%| 分两档提示（≤1% 中性灰，>1% 黄色），不写「超标」。
 */
const range = reactive({ from: '', to: '' })
const data = ref<Reconcile | null>(null)
const loading = ref(false)
const error = ref('')
let controller: AbortController | null = null

async function load() {
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  try {
    data.value = await fetchReportReconcile(
      { from: range.from || undefined, to: range.to || undefined },
      controller.signal,
    )
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') return
    error.value = err instanceof Error ? err.message : '加载失败'
    data.value = null
  } finally {
    loading.value = false
  }
}

onMounted(load)
function onDateChange() {
  void load()
}

/* ---------- 页头：两侧 asOf 各标各的 ---------- */

const asOfReport = computed(() => data.value?.asOfReport ?? '')
const asOfOfficial = computed(() => data.value?.asOfOfficial ?? '')
const days = computed<ReconcileDay[]>(() => data.value?.days ?? [])
const warnings = computed<string[]>(() => data.value?.warnings ?? [])
const empty = computed(() => !loading.value && days.value.length === 0)

/** 缺日报日数：用于 KPI 与页头提示，页面正文不因此换一套结构 */
const missingDays = computed(() => days.value.filter((d) => d.missingReport).length)

/** 参与差值的行数（compare=diff 且有双值），以及超过 1% 的行数 */
const diffStats = computed(() => {
  let comparable = 0
  let over = 0
  for (const day of days.value) {
    for (const item of day.items) {
      if (item.compare !== 'diff' || item.diff === null || item.diffPct === null) continue
      comparable++
      if (Math.abs(item.diffPct) > 1) over++
    }
  }
  return { comparable, over }
})

/* ---------- 读数格式 ---------- */

/** 三位小数以内的数字按需显示：电价通常两位，MW/台不带小数 */
function fmt(v: number | null, unit = ''): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—'
  const digits = unit === 'MW' || unit === '台' ? 0 : 2
  return v.toFixed(digits)
}

/** 差值：四舍五入到两位后再判符号，避免 -0.002 显示成「-0.00」 */
function fmtSigned(v: number | null): string {
  if (v === null || !Number.isFinite(v)) return '—'
  const rounded = Number(v.toFixed(2))
  return (rounded > 0 ? '+' : '') + rounded.toFixed(2)
}

function fmtPct(v: number | null): string {
  if (v === null || !Number.isFinite(v)) return '—'
  const rounded = Number(v.toFixed(2))
  return (rounded > 0 ? '+' : '') + rounded.toFixed(2) + '%'
}

/** 差值着色：≤1% 中性，>1% 黄色。不给结论。 */
function diffTone(item: ReconcileItem): string {
  if (item.diffPct === null) return 'muted'
  return Math.abs(item.diffPct) > 1 ? 'warn' : 'calm'
}

/** 行标签：日前/实时/申报 + 对象 + 指标 */
function rowLabel(item: ReconcileItem): string {
  return `${datasetLabel(item.dataset)} · ${item.scope} · ${item.metric}`
}
</script>

<template>
  <div class="page">
    <!-- 页头：两列来源与 asOf 常驻，不能只在 hover 里出现 -->
    <div class="banner">
      <StatusTag tone="gray">只读</StatusTag>
      <span class="src" data-asof-report>
        日报 OCR · 截至 <b>{{ asOfReport || '无' }}</b>
      </span>
      <span class="arrow">⇄</span>
      <span class="src" data-asof-official>
        官网直采 pm-api · 截至 <b>{{ asOfOfficial || '无' }}</b>
      </span>
      <span class="spacer" />
      <span class="note" data-no-writeback>不覆盖、不合并、不写回</span>
    </div>

    <p class="explain">
      左列是日报图上的原值（OCR，落独立表），右列是广东电力交易中心披露接口直采（pm-api）。
      差值只在<b>真同义</b>的两行上算：日前/实时 发电侧均价。申报均价是「简单均价 vs 加权均价」、
      分电源与申报结构官网没有对应口径 —— 那些行只并排对照，不算差值，也不给通过/不通过结论。
    </p>

    <div class="controls">
      <label class="field">
        起
        <input v-model="range.from" type="date" @change="onDateChange" />
      </label>
      <label class="field">
        止
        <input v-model="range.to" type="date" @change="onDateChange" />
      </label>
      <span class="hint">留空 = 最近 30 个运行日</span>
    </div>

    <div v-if="error" class="blocked">
      <p class="blocked-title">读取失败</p>
      <p>{{ error }}</p>
    </div>

    <template v-else-if="loading">
      <PanelBox title="核对表" :foot="false" loading />
    </template>

    <template v-else-if="empty">
      <div class="blocked" data-empty>
        <p class="blocked-title">无数据（空态）</p>
        <p>
          这个区间两边都没有数据。本页只读已有记录，不做采集、不补数 ——
          空态就是空态，不画 0 线、不用 0 填。
        </p>
        <p class="muted">左列需要先解析日报（信息管理 → 解析数据），右列需要官方直采任务跑过。</p>
      </div>
    </template>

    <template v-else>
      <div class="kpis">
        <KpiCard
          label="核对天数"
          :value="days.length"
          unit="天"
          :hint="`区间 ${data?.from} ~ ${data?.to}`"
        />
        <KpiCard
          label="缺日报"
          :value="missingDays"
          unit="天"
          :hint="missingDays ? '这些天只显官网列，左列与差值留空' : '区间内每天都有日报'"
        />
        <KpiCard
          label="可比行"
          :value="diffStats.comparable"
          unit="行"
          hint="仅日前/实时 发电侧均价（真同义）"
        />
        <KpiCard
          label="|Δ%| > 1%"
          :value="diffStats.over"
          unit="行"
          hint="只做提示，不判通过/不通过"
        />
      </div>

      <PanelBox
        title="日报原值 vs 官网直采"
        :sub="`${days.length} 天 · 三列并排`"
        foot="左：日报 OCR（独立表）· 右：官网直采 pm-api · 差值 = 日报 − 官网，仅同口径行"
      >
        <div v-if="warnings.length" class="warnings" data-warnings>
          <p v-for="w in warnings" :key="w">· {{ w }}</p>
        </div>

        <table class="table">
          <thead>
            <tr>
              <th class="col-date">运行日</th>
              <th class="col-row">指标</th>
              <th class="num">日报原值</th>
              <th class="num">官网直采</th>
              <th class="num">差值</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="day in days" :key="day.runDate">
              <tr
                v-for="(item, i) in day.items"
                :key="`${day.runDate}-${item.dataset}-${item.scope}-${item.metric}`"
                :class="{ missing: day.missingReport }"
                :data-missing-report="day.missingReport ? '1' : undefined"
              >
                <td v-if="i === 0" class="col-date" :rowspan="day.items.length">
                  <div class="date">{{ day.runDate }}</div>
                  <div v-if="day.missingReport" class="date-note" data-day-missing>当日无日报（采集缺口）</div>
                  <div v-else-if="day.reportDate && day.reportDate !== day.runDate" class="date-note">
                    出自 {{ day.reportDate }} 日报
                  </div>
                </td>
                <td class="col-row">{{ rowLabel(item) }}</td>
                <td class="num" :data-report-value="item.reportValue === null ? 'null' : String(item.reportValue)">
                  <template v-if="item.reportValue !== null">
                    {{ fmt(item.reportValue, item.unit) }}<small class="unit">{{ item.unit }}</small>
                  </template>
                  <span v-else class="muted">—</span>
                </td>
                <td class="num" :data-official-value="item.officialValue === null ? 'null' : String(item.officialValue)">
                  <template v-if="item.officialValue !== null">
                    {{ fmt(item.officialValue, item.unit) }}<small class="unit">{{ item.unit }}</small>
                  </template>
                  <span v-else class="muted" data-official-missing>官网无此口径</span>
                </td>
                <td class="num">
                  <template v-if="item.compare === 'diff' && item.diff !== null">
                    <span class="diff" :class="diffTone(item)" :data-diff="item.diff">
                      {{ fmtSigned(item.diff) }}
                      <small class="pct">{{ fmtPct(item.diffPct) }}</small>
                    </span>
                  </template>
                  <span
                    v-else-if="item.compare === 'caliber'"
                    class="muted"
                    :title="item.note"
                    data-compare="caliber"
                  >
                    口径不同
                  </span>
                  <span v-else-if="item.compare === 'diff'" class="muted">—</span>
                  <span v-else class="muted" data-compare="official_missing">—</span>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </PanelBox>
    </template>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--s4); padding: var(--s4); }
.banner {
  display: flex;
  align-items: center;
  gap: var(--s3);
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-3);
  font-size: var(--fs-13);
}
.src { color: var(--text-2); font-variant-numeric: tabular-nums; }
.src b { color: var(--text-1); font-weight: 600; }
.arrow { color: var(--text-3); }
.spacer { flex: 1; }
.note { color: var(--text-3); font-size: var(--fs-12); }
.explain {
  margin: 0;
  color: var(--text-2);
  font-size: var(--fs-13);
  line-height: 1.8;
}
.explain b { color: var(--text-1); }
.controls { display: flex; align-items: center; gap: var(--s3); }
.field {
  display: inline-flex;
  align-items: center;
  gap: var(--s2);
  font-size: var(--fs-13);
  color: var(--text-3);
}
.field input {
  padding: var(--s1) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.hint { color: var(--text-3); font-size: var(--fs-12); }
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.warnings {
  margin-bottom: var(--s3);
  padding: var(--s2) var(--s3);
  border: 1px solid var(--yellow, #e6a23c);
  border-radius: var(--r-sm);
  background: var(--yellow-soft, #fdf6ec);
  color: var(--text-2);
  font-size: var(--fs-12);
  line-height: 1.7;
}
.warnings p { margin: 0; }
.table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
.table th,
.table td {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--divider);
  text-align: left;
  vertical-align: top;
}
.table th { color: var(--text-3); font-weight: 500; font-size: var(--fs-12); }
.table .num { text-align: right; font-variant-numeric: tabular-nums; }
.table tr.missing td { color: var(--text-3); background: var(--bg-3); }
.col-date { width: 9rem; }
.col-row { width: 40%; }
.date { font-variant-numeric: tabular-nums; }
.date-note { color: var(--text-3); font-size: var(--fs-12); margin-top: 2px; }
.unit { margin-left: 3px; color: var(--text-3); font-size: var(--fs-12); }
.diff { font-variant-numeric: tabular-nums; }
.diff.calm { color: var(--text-2); }
.diff.warn {
  padding: 0 var(--s2);
  border-radius: var(--r-xs);
  background: var(--yellow-soft, #fdf6ec);
  color: var(--yellow-ink, #b88230);
}
.pct { margin-left: var(--s1); font-size: var(--fs-12); }
.muted { color: var(--text-3); }
.blocked {
  display: flex;
  flex-direction: column;
  gap: var(--s2);
  align-items: flex-start;
  padding: var(--s5);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  color: var(--text-2);
  font-size: var(--fs-13);
}
.blocked p { margin: 0; }
.blocked-title { font-size: var(--fs-16); font-weight: 600; }
@media (max-width: 1280px) {
  .kpis { grid-template-columns: repeat(2, 1fr); }
}
</style>
