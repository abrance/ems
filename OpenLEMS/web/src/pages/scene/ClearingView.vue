<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import SourceBadge from '@/components/SourceBadge.vue'
import StatusTag from '@/components/StatusTag.vue'
import { DATASET_LABELS, getReportData, isPending, listReports, type SpotMetric } from '@/api/spotreport'

/**
 * 场景 · 市场出清。
 *
 * 已接（都来自日报结构化数据 /api/spot-report/data）：
 *   - 高峰 / 低谷 地市节点电价档位（图2-1 / 图2-2，数据集 node_price_peak / node_price_valley）
 *   - 日前成交电价（表1(二)）、实时成交电价（表2(二)）
 *
 * 未接（日报没有对应提取项，页面底部逐条点名，不画空图）：
 *   日前市场火电启停状态统计图(t96) 与统计表、日前调频价格（容量/里程）、
 *   日前输电约束及阻塞情况。
 *
 * 档位数据官方接口不提供（pm-api 只有表1(二)/表2(二)），现存档位行要么是历史
 * 图像采样（origin=cv），要么已作废（void，接口层已过滤）。所以空态是常态，
 * 面板页脚按实际 origin 渲染，没有行就不写来源。
 */
const METRIC_PRICE_BUCKET = '价格档位'
const DATASET_PEAK = 'node_price_peak'
const DATASET_VALLEY = 'node_price_valley'

/** 档位配色：浅底 + 深字，按档位序号取（颜色只做区分，档位文本才是准的） */
const BUCKET_TONES = [
  { bg: 'var(--green-soft)', fg: 'var(--green-ink)' },
  { bg: '#eef3ff', fg: 'var(--blue-ink)' },
  { bg: '#fdf6d8', fg: '#7a5c00' },
  { bg: 'var(--brand-soft)', fg: 'var(--brand-ink)' },
  { bg: 'var(--red-soft)', fg: 'var(--red-ink)' },
]

const runDate = ref('')
const availableDates = ref<string[]>([])
const metrics = ref<SpotMetric[]>([])
const loading = ref(false)
const error = ref('')

function bucketMetrics(dataset: string): SpotMetric[] {
  return metrics.value.filter((m) => m.dataset === dataset && m.metric === METRIC_PRICE_BUCKET)
}

const priceMetrics = (dataset: string): SpotMetric[] =>
  metrics.value.filter((m) => m.dataset === dataset)

const ORIGIN_LABELS: Record<string, string> = {
  'pm-api': '官方直采（origin=pm-api）',
  manual: '人工复核确认（origin=manual）',
  cv: '图像采样预填（origin=cv）',
}

/** 页脚按实际来源渲染；一条行都没有就不写来源（空态下写来源没意义） */
function sourceFoot(dataset: string): string | false {
  const origins = [...new Set(bucketMetrics(dataset).map((m) => m.origin || ''))].filter(Boolean)
  if (!origins.length) return false
  return `数据来源：${origins.map((o) => ORIGIN_LABELS[o] ?? o).join(' / ')}`
}

/**
 * 档位顺序从数据里推导（区间文本的第一段数字），不写死阈值 ——
 * 日报更换档位划分时页面不用改。
 */
function bucketOrder(rows: SpotMetric[]): string[] {
  const uniq = [...new Set(rows.map((m) => m.textValue).filter(Boolean))]
  return uniq.sort((a, b) => lowerBound(a) - lowerBound(b))
}

function lowerBound(text: string): number {
  const m = text.match(/[-+]?inf|[-+]?\d+(?:\.\d+)?/i)
  if (!m) return Number.POSITIVE_INFINITY
  const raw = m[0].toLowerCase()
  if (raw.includes('inf')) return raw.startsWith('-') ? Number.NEGATIVE_INFINITY : Number.POSITIVE_INFINITY
  return Number(raw)
}

function toneFor(bucket: string, order: string[]) {
  const i = order.indexOf(bucket)
  return BUCKET_TONES[i >= 0 ? i % BUCKET_TONES.length : 0]
}

/** 城市 → 档位 */
function cityBuckets(dataset: string): { city: string; bucket: string; pending: boolean; source: string }[] {
  return bucketMetrics(dataset)
    .map((m) => ({ city: m.scope, bucket: m.textValue, pending: isPending(m), source: m.source }))
    .sort((a, b) => a.city.localeCompare(b.city, 'zh-CN'))
}

/** 档位 → 命中城市数 */
function bucketCounts(dataset: string, order: string[]): { bucket: string; count: number; pending: number }[] {
  const rows = bucketMetrics(dataset)
  return order.map((b) => ({
    bucket: b,
    count: rows.filter((m) => m.textValue === b).length,
    pending: rows.filter((m) => m.textValue === b && isPending(m)).length,
  }))
}

const peakOrder = computed(() => bucketOrder(bucketMetrics(DATASET_PEAK)))
const valleyOrder = computed(() => bucketOrder(bucketMetrics(DATASET_VALLEY)))

const pendingCount = computed(() => metrics.value.filter(isPending).length)

const tradeCols = [
  { key: 'scope', title: '对象' },
  { key: 'metric', title: '指标' },
  { key: 'value', title: '数值', num: true },
  { key: 'unit', title: '单位' },
  { key: 'atTime', title: '出现时刻' },
  { key: 'momPct', title: '环比', num: true },
  { key: 'source', title: '来源' },
]

function cell(m: SpotMetric, key: string): string {
  switch (key) {
    case 'value':
      return m.textValue || String(m.value)
    case 'momPct':
      return m.momPct == null ? '—' : `${m.momPct.toFixed(2)}%`
    case 'atTime':
      return m.atTime || '—'
    default:
      return String((m as unknown as Record<string, unknown>)[key] ?? '—')
  }
}

async function loadDates() {
  try {
    const list = await listReports()
    availableDates.value = list.map((r) => r.date)
    if (!runDate.value && availableDates.value.length) runDate.value = availableDates.value[0]
  } catch {
    availableDates.value = []
  }
}

async function load(date = runDate.value) {
  if (!date) return
  runDate.value = date
  loading.value = true
  error.value = ''
  try {
    const data = await getReportData(date)
    metrics.value = data.metrics ?? []
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
    metrics.value = []
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await loadDates()
  await load()
})
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load()">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="运行日" :span="12" :foot="false">
      <div class="toolbar">
        <label class="field">
          运行日
          <select v-model="runDate" @change="load()">
            <option v-for="d in availableDates" :key="d" :value="d">{{ d }}</option>
          </select>
        </label>
        <button class="btn" type="button" :disabled="loading" @click="load()">查询</button>
        <span class="spacer" />
        <StatusTag v-if="pendingCount" tone="brand">本日 {{ pendingCount }} 项未复核</StatusTag>
        <StatusTag v-else-if="metrics.length" tone="green">本日全部已确认</StatusTag>
      </div>
      <div class="hint">
        出清结果来自当日日报的表1(二) / 表2(二)；未复核项在页面上标出，不计入趋势聚合。
      </div>
    </PanelBox>

    <PanelBox
      v-for="cfg in [
        { ds: DATASET_PEAK, title: '高峰时刻地市节点电价档位', sub: '图2-1', order: peakOrder },
        { ds: DATASET_VALLEY, title: '低谷时刻地市节点电价档位', sub: '图2-2', order: valleyOrder },
      ]"
      :key="cfg.ds"
      :title="cfg.title"
      :sub="`${cfg.sub} · ${DATASET_LABELS[cfg.ds] ?? cfg.ds}`"
      :span="6"
      :loading="loading"
      :foot="sourceFoot(cfg.ds)"
    >
      <template v-if="cityBuckets(cfg.ds).length">
        <div class="buckets">
          <div
            v-for="b in bucketCounts(cfg.ds, cfg.order)"
            :key="b.bucket"
            class="bucket"
            :style="{ background: toneFor(b.bucket, cfg.order).bg, color: toneFor(b.bucket, cfg.order).fg }"
          >
            <b>{{ b.bucket }}</b>
            <span>{{ b.count }} 市<template v-if="b.pending">（{{ b.pending }} 未复核）</template></span>
          </div>
        </div>
        <div class="cities">
          <span
            v-for="c in cityBuckets(cfg.ds)"
            :key="c.city"
            class="city"
            :style="{ background: toneFor(c.bucket, cfg.order).bg, color: toneFor(c.bucket, cfg.order).fg }"
            :title="`${c.city} ${c.bucket}（${c.pending ? '未复核' : '已确认'}）`"
          >
            {{ c.city }}
            <template v-if="c.pending">·</template>
          </span>
        </div>
        <div class="hint">
          色块代表档位区间；带「·」的是未复核项。数值只存在于图片色块里，OCR 取不到，
          因此这一块是采样预填，务必人工确认后再用。
        </div>
      </template>
      <div v-else class="empty">该日报没有{{ cfg.title }}数据。</div>
    </PanelBox>

    <PanelBox
      title="日前成交电价"
      sub="表1(二)"
      :span="6"
      :loading="loading"
      foot="数据来源：广东电力交易中心披露接口直采（origin=pm-api），运行日 = 当日"
    >
      <template #actions>
        <SourceBadge tone="blue" label="官网·日频 T+2" />
      </template>
      <table v-if="priceMetrics('day_ahead_trade').length">
        <thead>
          <tr>
            <th v-for="c in tradeCols" :key="c.key" :class="{ num: c.num }">{{ c.title }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(m, i) in priceMetrics('day_ahead_trade')" :key="i">
            <td v-for="c in tradeCols" :key="c.key" :class="{ num: c.num }">{{ cell(m, c.key) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">该日报没有日前成交电价数据。</div>
    </PanelBox>

    <PanelBox
      title="实时成交电价"
      sub="表2(二) · 承载滞后运行日"
      :span="6"
      :loading="loading"
      foot="数据来源：广东电力交易中心披露接口直采（origin=pm-api）；运行日为滞后 T-2，别与当日口径混用"
    >
      <template #actions>
        <SourceBadge tone="blue" label="官网·日频 T+2" />
      </template>
      <div v-if="priceMetrics('realtime_trade').length" class="warn">
        这批数据的运行日可能是滞后运行日（T-2），与上方「运行日」不是同一天。
      </div>
      <table v-if="priceMetrics('realtime_trade').length">
        <thead>
          <tr>
            <th v-for="c in tradeCols" :key="c.key" :class="{ num: c.num }">{{ c.title }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(m, i) in priceMetrics('realtime_trade')" :key="i">
            <td v-for="c in tradeCols" :key="c.key" :class="{ num: c.num }">{{ cell(m, c.key) }}</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">该日报没有实时成交电价数据。</div>
    </PanelBox>

    <PanelBox title="待数据源的面板" :span="12" foot="设计稿 §5.4 里列出的其余几块">
      <div class="pending-list">
        <div class="pending">
          <StatusTag tone="brand">待数据源</StatusTag>
          <span>
            日前市场火电启停状态统计图（t96 时刻）与统计表：需要对 96 个时刻的开停机组组合，
            日报没有提取。
          </span>
        </div>
        <div class="pending">
          <StatusTag tone="brand">待数据源</StatusTag>
          <span>日前调频价格（调频容量价格 / 调频里程价格）：单位与来源表待定，日报未提取。</span>
        </div>
        <div class="pending">
          <StatusTag tone="brand">待数据源</StatusTag>
          <span>日前输电约束及阻塞情况：需要越限断面、时点与限额，日报未提取。</span>
        </div>
      </div>
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
.toolbar { display: flex; align-items: center; gap: var(--s3); flex-wrap: wrap; }
.spacer { flex: 1; }
.field { display: inline-flex; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
.field select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.hint { margin-top: var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.warn {
  margin-bottom: var(--s3);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-12);
}
.buckets { display: flex; gap: var(--s1); margin-bottom: var(--s3); flex-wrap: wrap; }
.bucket {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: var(--s2) var(--s3);
  border-radius: var(--r-sm);
  font-size: var(--fs-12);
  font-variant-numeric: tabular-nums;
}
.bucket b { font-size: var(--fs-13); }
.cities { display: flex; flex-wrap: wrap; gap: var(--s1); }
.city { padding: var(--s1) var(--s2); border-radius: var(--r-sm); font-size: var(--fs-12); }
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
.pending-list { display: flex; flex-direction: column; gap: var(--s3); }
.pending { display: flex; align-items: flex-start; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
.empty { padding: var(--s4); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
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
.btn:disabled { opacity: 0.5; cursor: default; }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
