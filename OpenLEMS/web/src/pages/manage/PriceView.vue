<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  createPrice,
  deletePrice,
  endMinutes,
  fetchCurrentPrice,
  listPrices,
  PRICE_TYPE_LABEL,
  PRICE_TYPE_TONE,
  PRICE_TYPES,
  togglePrice,
  toMinutes,
  updatePrice,
  validateSegments,
  type PriceConfig,
  type PricePayload,
  type PriceSegment,
} from '@/api/price'
import type { EStatus } from '@/api/strategy'

/**
 * 管理 · 电价管理（峰平谷时段）。
 *
 * 这里定义的时段是**全站唯一来源**：策略套利阈值、预测复盘的分时段相关度/离散度都读它。
 * 本页不做预测、不画收益 —— 只负责把「几点到几点是什么价」配清楚并画出甘特条。
 *
 * 校验分两级：
 *   error   起止非法 / 时段重叠 / 电价为负 —— 阻断保存；
 *   warning 未铺满 24 小时 —— 只提醒。业务上允不允许留空不由前端拍板。
 */
const list = ref<PriceConfig[]>([])
const current = ref<PriceConfig | null>(null)
const loading = ref(false)
const error = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const filter = reactive<{ status: '' | EStatus }>({ status: '' })

const drawerOpen = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const form = reactive<{
  description: string
  priority: number
  status: EStatus
  startDate: string
  endDate: string
  isLongTerm: boolean
  timeType: 'weekday' | 'custom' | 'monthly'
  weekdayType: 'workday' | 'weekend' | 'all'
  customDays: number[]
  customMonths: number[]
  segments: PriceSegment[]
}>({
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
  segments: [],
})

const deleteTarget = ref<PriceConfig | null>(null)
const deleteConfirmText = ref('')

const WEEKDAY_LABELS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [res, cur] = await Promise.all([
      listPrices({ status: filter.status || undefined }),
      fetchCurrentPrice().catch(() => null),
    ])
    list.value = res.list
    current.value = cur
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function rangeText(p: PriceConfig): string {
  const d = p.dateRange
  if (!d) return '—'
  return d.isLongTerm ? `长期 · 自 ${d.startDate}` : `${d.startDate} → ${d.endDate || '不限'}`
}

function timeText(p: PriceConfig): string {
  const t = p.timeRange
  if (!t) return '—'
  if (t.type === 'weekday') {
    const map: Record<string, string> = { workday: '工作日', weekend: '周末', all: '每天' }
    return `按星期 · ${map[t.weekdayType ?? 'all'] ?? t.weekdayType}`
  }
  if (t.type === 'custom') {
    const days = (t.customDays ?? []).map((d) => WEEKDAY_LABELS[d - 1] ?? d).join('/')
    return `自定义日期 · ${days || '未选'}`
  }
  const months = (t.customMonths ?? []).join('/')
  const days = (t.customDays ?? []).join('/')
  return `按月 · ${months || '每月'} ${days ? `${days} 日` : ''}`
}

/** 甘特条：把时段按分钟数铺到 24 小时上 */
function ganttBars(segments: PriceSegment[]) {
  return segments
    .map((s, i) => {
      const start = toMinutes(s.startTime)
      const end = endMinutes(s.endTime)
      return { seg: s, index: i, left: (start / 1440) * 100, width: ((end - start) / 1440) * 100 }
    })
    .filter((b) => Number.isFinite(b.left) && b.width > 0)
}

const issues = computed(() => validateSegments(form.segments))
const errors = computed(() => issues.value.filter((i) => i.level === 'error'))
const warnings = computed(() => issues.value.filter((i) => i.level === 'warning'))

function issueFor(i: number): string {
  return errors.value.find((e) => e.index === i)?.message ?? ''
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    description: '',
    priority: 3,
    status: 'Enable' as EStatus,
    startDate: new Date().toISOString().slice(0, 10),
    endDate: '',
    isLongTerm: true,
    timeType: 'weekday' as const,
    weekdayType: 'all' as const,
    customDays: [],
    customMonths: [],
    segments: [],
  })
  drawerOpen.value = true
}

function openEdit(p: PriceConfig) {
  editingId.value = p.id
  Object.assign(form, {
    description: p.description,
    priority: p.priority,
    status: p.status,
    startDate: p.dateRange?.startDate ?? '',
    endDate: p.dateRange?.endDate ?? '',
    isLongTerm: p.dateRange?.isLongTerm ?? true,
    timeType: (p.timeRange?.type as typeof form.timeType) ?? 'weekday',
    weekdayType: (p.timeRange?.weekdayType as typeof form.weekdayType) ?? 'all',
    customDays: [...(p.timeRange?.customDays ?? [])],
    customMonths: [...(p.timeRange?.customMonths ?? [])],
    segments: (p.priceSegments ?? []).map((s) => ({ ...s })),
  })
  drawerOpen.value = true
}

function addSegment() {
  // 新时段接在已有最后一段之后，减少手填
  const last = form.segments[form.segments.length - 1]
  const start = last ? last.endTime : '00:00'
  form.segments.push({ startTime: start, endTime: start === '00:00' ? '08:00' : '23:59', priceType: 'Flat', price: 0.5 })
}

function removeSegment(i: number) {
  form.segments.splice(i, 1)
}

function toggleDay(d: number) {
  const i = form.customDays.indexOf(d)
  if (i >= 0) form.customDays.splice(i, 1)
  else form.customDays.push(d)
}

function toggleMonth(m: number) {
  const i = form.customMonths.indexOf(m)
  if (i >= 0) form.customMonths.splice(i, 1)
  else form.customMonths.push(m)
}

const formError = computed(() => {
  if (form.description.trim().length < 2) return '描述至少 2 个字'
  if (!form.startDate) return '开始日期必填'
  if (!form.isLongTerm && form.endDate && form.endDate < form.startDate) return '结束日期不能早于开始日期'
  if (!form.segments.length) return '至少配置一个电价时段'
  if (form.timeType === 'custom' && !form.customDays.length) return '自定义日期至少要选一个'
  if (form.timeType === 'monthly' && !form.customDays.length) return '按月至少选一个日期'
  const err = errors.value[0]
  return err ? `第 ${err.index + 1} 个时段：${err.message}` : ''
})

function buildPayload(): PricePayload {
  return {
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
    priceSegments: [...form.segments].sort((a, b) => toMinutes(a.startTime) - toMinutes(b.startTime)),
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
    if (editingId.value === null) await createPrice(payload)
    else await updatePrice(editingId.value, payload)
    drawerOpen.value = false
    showToast('green', editingId.value === null ? '已创建电价配置' : '已保存电价配置')
    await load()
  } catch (err) {
    showToast('red', `保存失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    saving.value = false
  }
}

async function toggle(p: PriceConfig) {
  const next: EStatus = p.status === 'Enable' ? 'Disable' : 'Enable'
  try {
    await togglePrice(p.id, next)
    showToast('green', next === 'Enable' ? '已启用' : '已停用')
    await load()
  } catch (err) {
    showToast('red', `操作失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

async function confirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  if (deleteConfirmText.value.trim() !== target.description) return
  try {
    await deletePrice(target.id)
    deleteTarget.value = null
    showToast('green', '已删除')
    await load()
  } catch (err) {
    showToast('red', `删除失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

function tone(value: string) {
  return PRICE_TYPE_TONE[value] ?? PRICE_TYPE_TONE.Flat
}

onMounted(load)
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <div class="notice">
    这里配置的峰平谷时段是<strong>全站唯一来源</strong>：策略套利阈值、预测复盘的分时段
    相关度与离散度都读它。改这里会同时影响那两处。
    <template v-if="current">当前生效：#{{ current.id }} {{ current.description }}</template>
    <template v-else>当前没有命中的电价配置。</template>
  </div>

  <div class="grid">
    <PanelBox title="电价配置" :sub="`共 ${list.length} 条`" :span="12" :loading="loading" :foot="false">
      <template #actions>
        <select v-model="filter.status" class="mini" @change="load">
          <option value="">全部状态</option>
          <option value="Enable">启用</option>
          <option value="Disable">停用</option>
        </select>
        <button class="btn sm" type="button" @click="load">刷新</button>
        <button class="btn sm primary" type="button" @click="openCreate">+ 新增配置</button>
      </template>

      <div class="cards">
        <article v-for="p in list" :key="p.id" class="card" :class="{ active: current?.id === p.id }">
          <header class="card-head">
            <div>
              <div class="desc">{{ p.description }}</div>
              <div class="meta">
                {{ rangeText(p) }} · {{ timeText(p) }} · 优先级 {{ p.priority }}
              </div>
            </div>
            <div class="tags">
              <StatusTag :tone="p.status === 'Enable' ? 'green' : 'gray'">
                {{ p.status === 'Enable' ? '启用' : '停用' }}
              </StatusTag>
              <StatusTag :tone="p.isActive ? 'blue' : 'gray'">
                {{ p.isActive ? '时间命中' : '未命中' }}
              </StatusTag>
              <StatusTag v-if="current?.id === p.id" tone="brand">当前生效</StatusTag>
            </div>
            <div class="ops">
              <button class="btn sm" type="button" @click="openEdit(p)">编辑</button>
              <button class="btn sm" type="button" @click="toggle(p)">
                {{ p.status === 'Enable' ? '停用' : '启用' }}
              </button>
              <button
                class="btn sm danger"
                type="button"
                @click="((deleteTarget = p), (deleteConfirmText = ''))"
              >
                删除
              </button>
            </div>
          </header>

          <div class="gantt">
            <div
              v-for="b in ganttBars(p.priceSegments)"
              :key="b.index"
              class="bar"
              :style="{
                left: `${b.left}%`,
                width: `${b.width}%`,
                background: tone(b.seg.priceType).bg,
                color: tone(b.seg.priceType).fg,
              }"
              :title="`${b.seg.startTime}-${b.seg.endTime} ${PRICE_TYPE_LABEL[b.seg.priceType]} ${b.seg.price}`"
            >
              <b>{{ PRICE_TYPE_LABEL[b.seg.priceType] ?? b.seg.priceType }}</b>
              <span>{{ b.seg.price }}</span>
            </div>
            <div v-if="!p.priceSegments?.length" class="gantt-empty">未配置时段</div>
          </div>
          <div class="axis">
            <span v-for="h in [0, 6, 12, 18, 24]" :key="h" :style="{ left: `${(h / 24) * 100}%` }">
              {{ h }}h
            </span>
          </div>
        </article>

        <div v-if="!list.length && !loading" class="empty">暂无电价配置</div>
      </div>
    </PanelBox>
  </div>

  <!-- 编辑抽屉 -->
  <div v-if="drawerOpen" class="drawer-mask" @click.self="drawerOpen = false">
    <aside class="drawer">
      <header class="drawer-head">
        <h3>{{ editingId === null ? '新增电价配置' : `编辑电价 #${editingId}` }}</h3>
        <button class="btn sm" type="button" @click="drawerOpen = false">关闭</button>
      </header>

      <div class="drawer-body">
        <div class="form-grid">
          <label class="wide">描述 <input v-model="form.description" /></label>
          <label>优先级（1 最高） <input v-model.number="form.priority" type="number" min="1" max="5" /></label>
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

        <section class="segments">
          <div class="seg-head">
            <h4>时段与电价</h4>
            <button class="btn sm" type="button" @click="addSegment">+ 加时段</button>
          </div>

          <!-- 实时预览：改一下就能看到铺开的样子 -->
          <div class="gantt preview">
            <div
              v-for="b in ganttBars(form.segments)"
              :key="b.index"
              class="bar"
              :style="{
                left: `${b.left}%`,
                width: `${b.width}%`,
                background: tone(b.seg.priceType).bg,
                color: tone(b.seg.priceType).fg,
              }"
            >
              <b>{{ PRICE_TYPE_LABEL[b.seg.priceType] ?? b.seg.priceType }}</b>
              <span>{{ b.seg.price }}</span>
            </div>
          </div>

          <div class="seg-list">
            <div v-for="(s, i) in form.segments" :key="i" class="seg" :class="{ bad: !!issueFor(i) }">
              <input v-model="s.startTime" type="time" />
              <span class="muted">→</span>
              <input v-model="s.endTime" type="time" />
              <select v-model="s.priceType">
                <option v-for="t in PRICE_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
              </select>
              <input v-model.number="s.price" type="number" step="0.01" min="0" />
              <span class="muted">元/kWh</span>
              <button class="btn sm danger" type="button" @click="removeSegment(i)">删除</button>
              <div v-if="issueFor(i)" class="seg-err">{{ issueFor(i) }}</div>
            </div>
            <div v-if="!form.segments.length" class="muted">还没有时段。点「+ 加时段」开始。</div>
          </div>

          <div v-for="w in warnings" :key="w.message" class="warn">{{ w.message }}</div>
        </section>
      </div>

      <footer class="drawer-foot">
        <span v-if="formError" class="err">{{ formError }}</span>
        <span class="spacer" />
        <button class="btn" type="button" @click="drawerOpen = false">取消</button>
        <button class="btn primary" type="button" :disabled="saving || !!errors.length" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </button>
      </footer>
    </aside>
  </div>

  <div v-if="deleteTarget" class="modal-mask" @click.self="deleteTarget = null">
    <div class="modal">
      <h3>删除电价配置</h3>
      <p class="muted">
        将删除「{{ deleteTarget.description }}」。此操作不可撤销，请输入描述确认。
      </p>
      <input v-model="deleteConfirmText" :placeholder="deleteTarget.description" />
      <div class="modal-actions">
        <button class="btn" type="button" @click="deleteTarget = null">取消</button>
        <button
          class="btn danger"
          type="button"
          :disabled="deleteConfirmText.trim() !== deleteTarget.description"
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
  gap: var(--s2);
  flex-wrap: wrap;
  margin-bottom: var(--s4);
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-13);
}
.notice.error { border-left-color: var(--red); background: var(--red-soft); color: var(--red-ink); }
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
.cards { display: flex; flex-direction: column; gap: var(--s4); }
.card {
  padding: var(--s3) var(--s4) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.card.active { border-color: var(--brand-line); background: var(--brand-soft); }
.card-head { display: flex; align-items: flex-start; gap: var(--s3); flex-wrap: wrap; }
.card-head > div:first-child { flex: 1; min-width: 220px; }
.desc { font-size: var(--fs-14); font-weight: 600; }
.meta { font-size: var(--fs-12); color: var(--text-3); }
.tags { display: flex; gap: var(--s1); flex-wrap: wrap; }
.ops { display: flex; gap: var(--s1); }
.gantt {
  position: relative;
  height: 44px;
  margin-top: var(--s3);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  overflow: hidden;
}
.gantt.preview { height: 40px; margin-bottom: var(--s3); }
.bar {
  position: absolute;
  top: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  font-size: var(--fs-12);
  border-right: 1px solid var(--bg-2);
  overflow: hidden;
  white-space: nowrap;
}
.bar b { font-size: var(--fs-13); }
.gantt-empty { display: grid; height: 100%; place-items: center; color: var(--text-3); font-size: var(--fs-12); }
.axis { position: relative; height: 16px; font-size: var(--fs-12); color: var(--text-3); }
.axis span { position: absolute; transform: translateX(-50%); }
.empty { padding: var(--s5); color: var(--text-3); text-align: center; }
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
  width: min(760px, 100%);
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
  width: 100%;
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.chips { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s1); margin-top: var(--s3); }
.chip-label { margin-right: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
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
.segments { margin-top: var(--s5); }
.seg-head { display: flex; align-items: center; gap: var(--s3); margin-bottom: var(--s3); }
.seg-head h4 { flex: 1; margin: 0; font-size: var(--fs-14); }
.seg-list { display: flex; flex-direction: column; gap: var(--s2); }
.seg { display: flex; align-items: center; gap: var(--s2); flex-wrap: wrap; }
.seg.bad input, .seg.bad select { border-color: var(--red); }
.seg input[type='time'] { width: 120px; }
.seg input[type='number'] { width: 96px; }
.seg select { width: 96px; }
.seg input,
.seg select {
  padding: var(--s2) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.seg-err { flex-basis: 100%; font-size: var(--fs-12); color: var(--red-ink); }
.warn { margin-top: var(--s2); font-size: var(--fs-12); color: var(--brand-ink); }
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
