<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  activateStrategy,
  createStrategy,
  deleteStrategy,
  listStrategies,
  normalizePoints,
  updateStrategy,
  WEEKDAY_LABELS,
  type EStatus,
  type ETimeRangeType,
  type EWeekdayType,
  type EnergyStorageStrategy,
  type StrategyPayload,
} from '@/api/strategy'
import { fetchBacktest, type EffectBacktest } from '@/api/effect'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 管理 · 策略管理（储能充放计划）。
 *
 * 数据结构（后端 s_policy.SStrategyConfig）：
 *   日期范围 + 时间范围（weekday/custom/monthly）+ 24 小时功率点位表 [[hour, percent]]
 * 本页只编辑这三块 + 名称/优先级/启用状态。
 *
 * 「试运行」直接调 /api/effect/backtest —— 与「储能效果评估」用**同一套成本模型**。
 * 另造一套估算的话，同一个策略在两个页面会给出两个收益，没人知道该信哪个。
 */
const list = ref<EnergyStorageStrategy[]>([])
const loading = ref(false)
const error = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const filter = reactive<{ status: '' | EStatus; keyword: string }>({ status: '', keyword: '' })

const drawerOpen = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const form = reactive<{
  name: string
  description: string
  priority: number
  status: EStatus
  startDate: string
  endDate: string
  isLongTerm: boolean
  timeType: ETimeRangeType
  weekdayType: EWeekdayType
  customDays: number[]
  customMonths: number[]
  points: number[][]
}>({
  name: '',
  description: '',
  priority: 3,
  status: 'Enable',
  startDate: '',
  endDate: '',
  isLongTerm: true,
  timeType: 'weekday',
  weekdayType: 'all',
  customDays: [],
  customMonths: [],
  points: [],
})

/** 删除需输名确认（设计稿 §11.4 规矩 3） */
const deleteTarget = ref<EnergyStorageStrategy | null>(null)
const deleteConfirmText = ref('')

const backtest = ref<EffectBacktest | null>(null)
const backtestLoading = ref(false)
const backtestError = ref('')

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await listStrategies({
      page: 1,
      pageSize: 100,
      status: filter.status || undefined,
      keyword: filter.keyword || undefined,
    })
    list.value = res.list
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function rangeText(s: EnergyStorageStrategy): string {
  const d = s.dateRange
  if (!d) return '—'
  if (d.isLongTerm) return `长期 · 自 ${d.startDate}`
  return `${d.startDate} → ${d.endDate || '不限'}`
}

const TIME_TYPE_LABEL: Record<ETimeRangeType, string> = {
  weekday: '按星期',
  custom: '自定义日期',
  monthly: '按月',
}
const WEEKDAY_TYPE_LABEL: Record<EWeekdayType, string> = {
  workday: '工作日',
  weekend: '周末',
  all: '每天',
}

function timeText(s: EnergyStorageStrategy): string {
  const t = s.timeRange
  if (!t) return '—'
  if (t.type === 'weekday') return `${TIME_TYPE_LABEL.weekday} · ${WEEKDAY_TYPE_LABEL[t.weekdayType ?? 'all']}`
  if (t.type === 'custom') {
    const days = (t.customDays ?? []).map((d) => WEEKDAY_LABELS[d - 1] ?? d).join('/')
    return `${TIME_TYPE_LABEL.custom} · ${days || '未选'}`
  }
  const days = (t.customDays ?? []).join('/')
  const months = (t.customMonths ?? []).join('/')
  return `${TIME_TYPE_LABEL.monthly} · ${months || '每月'} ${days ? `${days} 日` : ''}`
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    name: '',
    description: '',
    priority: 3,
    status: 'Enable' as EStatus,
    startDate: new Date().toISOString().slice(0, 10),
    endDate: '',
    isLongTerm: true,
    timeType: 'weekday' as ETimeRangeType,
    weekdayType: 'all' as EWeekdayType,
    customDays: [],
    customMonths: [],
    points: [],
  })
  drawerOpen.value = true
}

function openEdit(s: EnergyStorageStrategy) {
  editingId.value = s.id
  Object.assign(form, {
    name: s.name,
    description: s.description ?? '',
    priority: s.priority,
    status: s.status,
    startDate: s.dateRange?.startDate ?? '',
    endDate: s.dateRange?.endDate ?? '',
    isLongTerm: s.dateRange?.isLongTerm ?? true,
    timeType: s.timeRange?.type ?? ('weekday' as ETimeRangeType),
    weekdayType: s.timeRange?.weekdayType ?? ('all' as EWeekdayType),
    customDays: [...(s.timeRange?.customDays ?? [])],
    customMonths: [...(s.timeRange?.customMonths ?? [])],
    points: normalizePoints(s.config?.points).map((p) => [p[0], p[1]]),
  })
  drawerOpen.value = true
}

function addPoint() {
  const used = new Set(form.points.map((p) => p[0]))
  const next = [...Array(24).keys()].find((h) => !used.has(h))
  if (next === undefined) {
    showToast('red', '24 个小时都已有点位')
    return
  }
  form.points.push([next, 50])
  form.points = normalizePoints(form.points)
}

function removePoint(hour: number) {
  form.points = form.points.filter((p) => p[0] !== hour)
}

function toggleDay(day: number) {
  const i = form.customDays.indexOf(day)
  if (i >= 0) form.customDays.splice(i, 1)
  else form.customDays.push(day)
}

function toggleMonth(m: number) {
  const i = form.customMonths.indexOf(m)
  if (i >= 0) form.customMonths.splice(i, 1)
  else form.customMonths.push(m)
}

const formError = computed(() => {
  if (!form.name.trim()) return '策略名称必填'
  if (!form.startDate) return '开始日期必填'
  if (!form.isLongTerm && form.endDate && form.endDate < form.startDate) return '结束日期不能早于开始日期'
  if (form.timeType === 'custom' && !form.customDays.length) return '自定义日期至少要选一个'
  if (form.timeType === 'monthly' && !form.customDays.length) return '按月至少选一个日期'
  for (const [hour, pct] of form.points) {
    if (hour < 0 || hour > 23) return `点位小时越界：${hour}`
    if (pct < 0 || pct > 100) return `点位百分比需要在 0~100：${pct}`
  }
  return ''
})

function buildPayload(): StrategyPayload {
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    priority: Number(form.priority) || 1,
    status: form.status,
    dateRange: {
      startDate: form.startDate,
      endDate: form.isLongTerm ? undefined : form.endDate,
      isLongTerm: form.isLongTerm,
    },
    timeRange: {
      type: form.timeType,
      weekdayType: form.timeType === 'weekday' ? form.weekdayType : undefined,
      customDays: form.timeType === 'weekday' ? [] : [...form.customDays].sort((a, b) => a - b),
      customMonths: form.timeType === 'monthly' ? [...form.customMonths].sort((a, b) => a - b) : [],
    },
    config: { points: normalizePoints(form.points) },
  }
}

async function save() {
  if (formError.value) {
    showToast('red', formError.value)
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    if (editingId.value === null) await createStrategy(payload)
    else await updateStrategy(editingId.value, payload)
    drawerOpen.value = false
    showToast('green', editingId.value === null ? '已创建策略' : '已保存策略')
    await load()
  } catch (err) {
    showToast('red', `保存失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    saving.value = false
  }
}

async function toggleStatus(s: EnergyStorageStrategy) {
  const next: EStatus = s.status === 'Enable' ? 'Disable' : 'Enable'
  try {
    await activateStrategy(s.id, next)
    showToast('green', next === 'Enable' ? `已启用「${s.name}」` : `已停用「${s.name}」`)
    await load()
  } catch (err) {
    showToast('red', `操作失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

function askDelete(s: EnergyStorageStrategy) {
  deleteTarget.value = s
  deleteConfirmText.value = ''
}

async function confirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  if (deleteConfirmText.value.trim() !== target.name) return
  try {
    await deleteStrategy(target.id)
    deleteTarget.value = null
    showToast('green', `已删除「${target.name}」`)
    await load()
  } catch (err) {
    showToast('red', `删除失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

/** 试运行：近 7 天历史，与效果评估同一套成本模型 */
async function runBacktest() {
  backtestLoading.value = true
  backtestError.value = ''
  try {
    const end = new Date()
    const start = new Date(end.getTime() - 6 * 86400_000)
    const fmt = (d: Date) => d.toISOString().slice(0, 10)
    backtest.value = await fetchBacktest({ start: fmt(start), end: fmt(end) })
  } catch (err) {
    backtest.value = null
    backtestError.value = err instanceof Error ? err.message : '试运行失败'
  } finally {
    backtestLoading.value = false
  }
}

const previewEl = ref<HTMLElement>()
useECharts(previewEl, (): EChartsOption => {
  const pts = normalizePoints(form.points)
  return {
    grid: { left: 48, right: 16, top: 24, bottom: 28 },
    tooltip: { trigger: 'axis', valueFormatter: (v) => `${v}%` },
    xAxis: { type: 'category', data: [...Array(24).keys()].map((h) => `${h}:00`), axisLabel: { fontSize: 12, color: CHART_COLORS.gray } },
    yAxis: {
      type: 'value',
      min: 0,
      max: 100,
      name: '%',
      nameTextStyle: { fontSize: 12 },
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: '#eef0f3' } },
    },
    series: [
      {
        name: '功率百分比',
        type: 'line',
        step: 'middle',
        showSymbol: true,
        itemStyle: { color: CHART_COLORS.brand },
        // 只在有点位的小时有值，避免把未配置的时段也画成 0
        data: [...Array(24).keys()].map((h) => {
          const hit = pts.find((p) => p[0] === h)
          return hit ? hit[1] : null
        }),
      },
    ],
  }
})

onMounted(load)
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="策略列表" :sub="`共 ${list.length} 条`" :span="12" :loading="loading" :foot="false">
      <template #actions>
        <input v-model="filter.keyword" class="mini" placeholder="搜索名称" @keyup.enter="load" />
        <select v-model="filter.status" class="mini" @change="load">
          <option value="">全部状态</option>
          <option value="Enable">启用</option>
          <option value="Disable">停用</option>
        </select>
        <button class="btn sm" type="button" @click="load">查询</button>
        <button class="btn sm primary" type="button" @click="openCreate">+ 新建策略</button>
      </template>

      <div class="scroll-y">
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th class="num">优先级</th>
              <th>状态</th>
              <th>日期范围</th>
              <th>时间范围</th>
              <th class="num">点位数</th>
              <th>当前命中</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in list" :key="s.id">
              <td>
                <div class="name">{{ s.name }}</div>
                <div class="desc">{{ s.description || '—' }}</div>
              </td>
              <td class="num">{{ s.priority }}</td>
              <td>
                <StatusTag :tone="s.status === 'Enable' ? 'green' : 'gray'">
                  {{ s.status === 'Enable' ? '启用' : '停用' }}
                </StatusTag>
              </td>
              <td>{{ rangeText(s) }}</td>
              <td>{{ timeText(s) }}</td>
              <td class="num">{{ normalizePoints(s.config?.points).length }}</td>
              <td>
                <StatusTag :tone="s.isActive ? 'blue' : 'gray'">{{ s.isActive ? '命中' : '未命中' }}</StatusTag>
              </td>
              <td class="ops">
                <button class="btn sm" type="button" @click="openEdit(s)">编辑</button>
                <button class="btn sm" type="button" @click="toggleStatus(s)">
                  {{ s.status === 'Enable' ? '停用' : '启用' }}
                </button>
                <button class="btn sm danger" type="button" @click="askDelete(s)">删除</button>
              </td>
            </tr>
            <tr v-if="!list.length && !loading">
              <td colspan="8" class="empty">暂无策略</td>
            </tr>
          </tbody>
        </table>
      </div>
    </PanelBox>

    <PanelBox
      title="试运行"
      sub="近 7 天历史 · 与效果评估同一套成本模型"
      :span="12"
      :loading="backtestLoading"
      foot="数据来源：/api/effect/backtest · 用实测负荷回放各算法"
    >
      <template #actions>
        <button class="btn sm primary" type="button" :disabled="backtestLoading" @click="runBacktest">
          {{ backtestLoading ? '回测中…' : '跑一次' }}
        </button>
      </template>

      <div v-if="backtestError" class="muted">{{ backtestError }}</div>
      <div v-else-if="!backtest" class="muted">还没跑过。点「跑一次」按近 7 天历史回放。</div>
      <template v-else>
        <div class="meta">
          区间 {{ backtest.start }} ~ {{ backtest.end }} · 采样 {{ backtest.points }} 点 ·
          额定 {{ backtest.ratedPowerKw }} kW / {{ backtest.capacityKwh }} kWh
        </div>
        <table>
          <thead>
            <tr>
              <th>算法</th>
              <th class="num">套利净收益(元)</th>
              <th class="num">需量节省(元)</th>
              <th class="num">总节省(元)</th>
              <th class="num">充/放电量(kWh)</th>
              <th class="num">终止 SOC</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in backtest.results" :key="r.algorithm">
              <td>
                {{ r.algorithm }}
                <StatusTag v-if="r.algorithm === 'idle'" tone="gray">基线</StatusTag>
              </td>
              <td class="num">{{ r.arbitrageProfit.toFixed(2) }}</td>
              <td class="num">{{ r.demandSaving.toFixed(2) }}</td>
              <td class="num"><strong>{{ r.totalSaving.toFixed(2) }}</strong></td>
              <td class="num">{{ r.chargeEnergy.toFixed(0) }} / {{ r.dischargeEnergy.toFixed(0) }}</td>
              <td class="num">{{ r.finalSocPct.toFixed(0) }}%</td>
            </tr>
          </tbody>
        </table>
      </template>
    </PanelBox>
  </div>

  <!-- 编辑抽屉 -->
  <div v-if="drawerOpen" class="drawer-mask" @click.self="drawerOpen = false">
    <aside class="drawer">
      <header class="drawer-head">
        <h3>{{ editingId === null ? '新建策略' : `编辑策略 #${editingId}` }}</h3>
        <button class="btn sm" type="button" @click="drawerOpen = false">关闭</button>
      </header>

      <div class="drawer-body">
        <div class="form-grid">
          <label>名称 <input v-model="form.name" /></label>
          <label>优先级（1 最高） <input v-model.number="form.priority" type="number" min="1" max="5" /></label>
          <label class="wide">描述 <input v-model="form.description" /></label>
          <label>
            状态
            <select v-model="form.status">
              <option value="Enable">启用</option>
              <option value="Disable">停用</option>
            </select>
          </label>
          <label>开始日期 <input v-model="form.startDate" type="date" /></label>
          <label class="check">
            <input v-model="form.isLongTerm" type="checkbox" />长期有效
          </label>
          <label v-if="!form.isLongTerm">结束日期 <input v-model="form.endDate" type="date" /></label>
          <label>
            时间范围
            <select v-model="form.timeType">
              <option value="weekday">按星期</option>
              <option value="custom">自定义星期</option>
              <option value="monthly">按月</option>
            </select>
          </label>
          <label v-if="form.timeType === 'weekday'">
            星期
            <select v-model="form.weekdayType">
              <option value="all">每天</option>
              <option value="workday">工作日</option>
              <option value="weekend">周末</option>
            </select>
          </label>
        </div>

        <div v-if="form.timeType === 'custom'" class="chips">
          <span class="chip-label">选星期</span>
          <button
            v-for="(d, i) in WEEKDAY_LABELS"
            :key="d"
            class="chip-toggle"
            :class="{ on: form.customDays.includes(i + 1) }"
            type="button"
            @click="toggleDay(i + 1)"
          >
            {{ d }}
          </button>
        </div>

        <div v-if="form.timeType === 'monthly'" class="chips">
          <span class="chip-label">选月份</span>
          <button
            v-for="m in 12"
            :key="m"
            class="chip-toggle"
            :class="{ on: form.customMonths.includes(m) }"
            type="button"
            @click="toggleMonth(m)"
          >
            {{ m }}月
          </button>
        </div>

        <div v-if="form.timeType === 'monthly'" class="chips">
          <span class="chip-label">选日期</span>
          <button
            v-for="d in 31"
            :key="d"
            class="chip-toggle"
            :class="{ on: form.customDays.includes(d) }"
            type="button"
            @click="toggleDay(d)"
          >
            {{ d }}
          </button>
        </div>

        <section class="points">
          <div class="points-head">
            <h4>功率点位（小时 → 百分比）</h4>
            <button class="btn sm" type="button" @click="addPoint">+ 加点位</button>
          </div>
          <div class="chart" ref="previewEl" />
          <div class="pt-list">
            <div v-for="(p, i) in form.points" :key="p[0]" class="pt">
              <select v-model.number="form.points[i][0]">
                <option v-for="h in 24" :key="h" :value="h - 1">{{ h - 1 }}:00</option>
              </select>
              <input v-model.number="form.points[i][1]" type="number" min="0" max="100" />
              <span class="muted">%</span>
              <button class="btn sm danger" type="button" @click="removePoint(p[0])">删除</button>
            </div>
            <div v-if="!form.points.length" class="muted">还没有点位。不加点位等于这条策略没有充放动作。</div>
          </div>
        </section>
      </div>

      <footer class="drawer-foot">
        <span v-if="formError" class="err">{{ formError }}</span>
        <span class="spacer" />
        <button class="btn" type="button" @click="drawerOpen = false">取消</button>
        <button class="btn primary" type="button" :disabled="saving" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </button>
      </footer>
    </aside>
  </div>

  <!-- 删除确认：输名 -->
  <div v-if="deleteTarget" class="modal-mask" @click.self="deleteTarget = null">
    <div class="modal">
      <h3>删除策略</h3>
      <p class="muted">
        将删除「{{ deleteTarget.name }}」。此操作不可撤销，请输入策略名称确认。
      </p>
      <input v-model="deleteConfirmText" :placeholder="deleteTarget.name" />
      <div class="modal-actions">
        <button class="btn" type="button" @click="deleteTarget = null">取消</button>
        <button
          class="btn danger"
          type="button"
          :disabled="deleteConfirmText.trim() !== deleteTarget.name"
          @click="confirmDelete"
        >
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
.scroll-y { max-height: 420px; overflow: auto; }
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th {
  position: sticky;
  top: 0;
  z-index: 1;
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
tbody tr:hover { background: var(--bg-4); }
.name { font-weight: 600; }
.desc { font-size: var(--fs-12); color: var(--text-3); }
.ops { display: flex; gap: var(--s1); }
.empty { padding: var(--s5); color: var(--text-3); text-align: center; }
.meta { margin-bottom: var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.mini {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-12);
}
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
.btn.danger { background: var(--red-soft); color: var(--red-ink); }
.chip-toggle {
  padding: var(--s1) var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.chip-toggle.on { background: var(--brand-soft); color: var(--brand-ink); font-weight: 600; }
.chips { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s1); margin-top: var(--s3); }
.chip-label { margin-right: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
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
  width: min(720px, 100%);
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
.drawer-head h3 { flex: 1; margin: 0; font-size: var(--fs-16); }
.drawer-body { flex: 1; padding: var(--s4); overflow-y: auto; }
.drawer-foot {
  display: flex;
  align-items: center;
  gap: var(--s2);
  padding: var(--s3) var(--s4);
  border-top: 1px solid var(--border);
}
.spacer { flex: 1; }
.form-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--s3) var(--s4); }
.form-grid label { display: flex; flex-direction: column; gap: var(--s1); font-size: var(--fs-12); color: var(--text-3); }
.form-grid label.wide { grid-column: span 2; }
.form-grid label.check { flex-direction: row; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-1); }
.form-grid input,
.form-grid select,
.modal input {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
  width: 100%;
}
.points { margin-top: var(--s5); }
.points-head { display: flex; align-items: center; gap: var(--s3); margin-bottom: var(--s2); }
.points-head h4 { flex: 1; margin: 0; font-size: var(--fs-14); }
.chart { height: 200px; }
.pt-list { display: flex; flex-direction: column; gap: var(--s2); margin-top: var(--s3); }
.pt { display: flex; align-items: center; gap: var(--s2); }
.pt select,
.pt input {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.pt input { width: 96px; }
.err { font-size: var(--fs-12); color: var(--red-ink); }
.muted { color: var(--text-3); font-size: var(--fs-13); }
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
  width: min(440px, 100%);
  padding: var(--s5);
  border-top: 3px solid var(--red);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.modal input { margin: var(--s3) 0 var(--s4); }
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12; }
  .form-grid { grid-template-columns: 1fr; }
  .form-grid label.wide { grid-column: span 1; }
}
</style>
