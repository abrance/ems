<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchDeviceNames, fetchHistory, fetchTelemetry, type ChartData, type DeviceType } from '@/api/device'
import { controlDevice, fetchCustomServices, type DriverService, type FieldDefinition } from '@/api/control'
import { fetchPolicyConfig } from '@/api/policy'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 手动控制 —— 人工充放电 / 实时控制。
 *
 * 设计要点（design.md §11.2、§12）：
 * 1. 服务列表来自设备驱动的自定义服务，不在前端写死「充电 / 放电」。
 *    驱动换一套，页面跟着变。
 * 2. parameters 是**位置参数数组**，顺序必须与驱动声明一致 —— 这是最容易错的地方，
 *    所以表单按 params 顺序渲染，提交前再把订单展示一遍让人确认。
 * 3. 下发前二次确认，明确显示发往哪台设备、哪个服务、什么参数。
 * 4. 保护分两层：
 *    - 驱动层：参数自带的 min/max/required（驱动声明什么就用什么）；
 *    - EMS 层：从「配置 · 储能参数」（setting policy_microgrid）推导出的 SOC 区间与
 *      功率上限，下发前一并校验，被拦时提示来自哪一条约束。
 *    「语义级」保护（知道某服务是充电还是放电，据此判断方向）需要驱动声明充/放语义，
 *    目前驱动没有这个声明，所以不做，也不假装能做。
 */
const deviceTypes: DeviceType[] = ['pcs', 'bms', 'ess', 'sess']

const devices = ref<Record<string, string>>({})
const deviceId = ref('')
const services = ref<DriverService[]>([])
const telemetry = ref<Record<string, { lastUpdateTime: string | null; telemetryValues: Record<string, unknown> }>>({})
const history = ref<ChartData | null>(null)

const loading = ref(false)
const error = ref('')
const busy = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined
let pollTimer: number | undefined

/** 每个服务一份表单值：{ 服务key: { 参数key: 值 } } */
const form = reactive<Record<string, Record<string, string | number | boolean>>>({})
/** 字段级错误，键为 `服务key.参数key` */
const fieldErrors = reactive<Record<string, string>>({})
/** 待确认的下发内容；非空即弹出确认框 */
const pending = ref<{ service: DriverService; params: unknown[] } | null>(null)

const current = computed(() => (deviceId.value ? telemetry.value[deviceId.value] : undefined))
const values = computed(() => current.value?.telemetryValues ?? {})

/**
 * EMS 侧保护约束，来自「配置 · 储能参数」（setting policy_microgrid）。
 * 这里只做「值级」校验（SOC 区间、功率上限、需量余量）。
 * 「语义级」保护（知道这个服务是充电还是放电，据此判断方向）需要驱动声明
 * 充/放语义，目前驱动没有这个声明，所以不假装能做。
 */
const policy = ref<Record<string, unknown>>({})

function numOr(v: unknown, fallback: number): number {
  const n = Number(v)
  return Number.isFinite(n) && n !== 0 ? n : fallback
}

const limits = computed(() => {
  const socMin = numOr(policy.value.socMinRatio, 0.1)
  const socMax = numOr(policy.value.socMaxRatio, 0.95)
  const deviceCharge = Number(values.value.MaxChargePower)
  const deviceDischarge = Number(values.value.MaxDischargePower)
  const chargeByRatio = deviceCharge > 0 ? deviceCharge * numOr(policy.value.chargePowerRatio, 1) : NaN
  const dischargeByRatio =
    deviceDischarge > 0 ? deviceDischarge * numOr(policy.value.dischargePowerRatio, 1) : NaN
  const demandOn = policy.value.enableDemandControl === true || policy.value.enableDemandControl === 'true'
  return {
    socMinPct: socMin * 100,
    socMaxPct: socMax * 100,
    chargeCap: Number.isFinite(chargeByRatio) ? Math.min(deviceCharge, chargeByRatio) : deviceCharge,
    dischargeCap: Number.isFinite(dischargeByRatio)
      ? Math.min(deviceDischarge, dischargeByRatio)
      : deviceDischarge,
    demandOn,
    demandTarget: numOr(policy.value.demandTarget, 0),
  }
})

const socNow = computed(() => {
  const v = Number(values.value.SOC)
  return Number.isFinite(v) ? v : null
})

const socOutOfRange = computed(
  () => socNow.value !== null && (socNow.value < limits.value.socMinPct || socNow.value > limits.value.socMaxPct),
)

/** 服务参数里以 kW 为单位的字段，按保护上限校验 */
function powerCapFor(p: FieldDefinition): number | null {
  const unit = (p.unit ?? '').toUpperCase()
  if (unit !== 'KW') return null
  const cap = limits.value.chargeCap || limits.value.dischargeCap
  return Number.isFinite(cap) && cap > 0 ? cap : null
}

/** 关键量按设备是否上报来决定显不显示，不硬编码全部点位 */
const KEY_METRICS = [
  { key: 'SOC', label: 'SOC', unit: '%' },
  { key: 'Power', label: '有功功率', unit: 'kW' },
  { key: 'SOH', label: 'SOH', unit: '%' },
  { key: 'MaxChargePower', label: '充电功率上限', unit: 'kW' },
  { key: 'MaxDischargePower', label: '放电功率上限', unit: 'kW' },
]

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

function num(v: unknown, digits = 1): string {
  if (v === null || v === undefined || v === '') return '—'
  const n = Number(v)
  return Number.isNaN(n) ? String(v) : n.toFixed(digits)
}

async function loadDevices() {
  loading.value = true
  error.value = ''
  try {
    devices.value = await fetchDeviceNames(deviceTypes)
    const ids = Object.keys(devices.value)
    if (!ids.length) {
      error.value = '没有可控制的设备。需先在「管理 · 设备管理」接入 PCS / 储能柜。'
      deviceId.value = ''
      return
    }
    if (!deviceId.value || !(deviceId.value in devices.value)) deviceId.value = ids[0]
  } catch (err) {
    error.value = err instanceof Error ? err.message : '设备列表加载失败'
  } finally {
    loading.value = false
  }
}

async function loadHistory() {
  const keys = KEY_METRICS.map((m) => m.key).filter((k) => k in values.value)
  if (!deviceId.value || !keys.length) {
    history.value = null
    return
  }
  const end = Date.now()
  try {
    history.value = await fetchHistory({
      deviceId: deviceId.value,
      telemetryKeys: keys,
      startTime: end - 2 * 3600 * 1000,
      endTime: end,
      step: 0,
    })
  } catch {
    // 历史拿不到不影响手动控制主流程，静默降级
    history.value = null
  }
}

async function loadDeviceData() {
  if (!deviceId.value) {
    services.value = []
    telemetry.value = {}
    history.value = null
    return
  }
  loading.value = true
  error.value = ''
  try {
    const [svc, tel, pol] = await Promise.all([
      fetchCustomServices(deviceId.value),
      fetchTelemetry(deviceId.value),
      fetchPolicyConfig().catch(() => ({ settingId: '', config: {} })),
    ])
    policy.value = pol.config ?? {}
    services.value = svc
    telemetry.value = tel
    for (const s of svc) {
      if (!form[s.key]) form[s.key] = {}
      for (const p of s.params ?? []) {
        if (form[s.key][p.key] === undefined) {
          form[s.key][p.key] = p.default ?? (p.valueType === 'bool' ? false : '')
        }
      }
    }
    await loadHistory()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '设备数据加载失败'
  } finally {
    loading.value = false
  }
}

/** 校验一个字段；规则只来自驱动声明，不自造业务规则 */
function validateField(p: FieldDefinition, raw: unknown): string {
  const label = p.name || p.key
  if (p.required && (raw === '' || raw === undefined || raw === null)) return `${label} 必填`
  if (p.valueType === 'bool' || raw === '' || raw === undefined || raw === null) return ''
  const n = Number(raw)
  if (Number.isNaN(n)) return `${label} 需要是数字`
  if (p.min !== undefined && p.min !== null && n < p.min) return `${label} 不能小于 ${p.min}`
  if (p.max !== undefined && p.max !== null && n > p.max) return `${label} 不能大于 ${p.max}`
  // EMS 侧保护：不超过「储能参数」推导出的功率上限
  const cap = powerCapFor(p)
  if (cap !== null && n > cap) return `${label} 超过保护上限 ${cap} kW（来自配置 · 储能参数）`
  return ''
}

/** 按驱动声明的顺序转成位置参数数组 */
function coerce(service: DriverService): unknown[] {
  const raw = form[service.key] ?? {}
  return (service.params ?? []).map((p) => {
    const v = raw[p.key]
    if (p.valueType === 'bool') return Boolean(v)
    if (p.valueType === 'int') return Math.round(Number(v))
    if (p.valueType === 'float') return Number(v)
    return String(v)
  })
}

function askExecute(service: DriverService) {
  for (const k of Object.keys(fieldErrors)) delete fieldErrors[k]
  let ok = true
  for (const p of service.params ?? []) {
    const msg = validateField(p, (form[service.key] ?? {})[p.key])
    if (msg) {
      fieldErrors[`${service.key}.${p.key}`] = msg
      ok = false
    }
  }
  if (!ok) {
    showToast('red', '参数校验未通过，请检查红色提示')
    return
  }
  pending.value = { service, params: coerce(service) }
}

async function confirmExecute() {
  const task = pending.value
  if (!task) return
  pending.value = null
  busy.value = task.service.key
  try {
    await controlDevice(deviceId.value, task.service.key, task.params)
    showToast('green', `已下发：${task.service.name}`)
    await loadDeviceData()
  } catch (err) {
    showToast('red', `下发失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    busy.value = ''
  }
}

/* ---------- 最近 2 小时充放曲线 ---------- */
const chartEl = ref<HTMLElement>()
useECharts(chartEl, (): EChartsOption => {
  const h = history.value
  const hasSoc = (h?.series ?? []).some((s) => s.name.toUpperCase().includes('SOC'))
  return {
    grid: { left: 48, right: 48, top: 32, bottom: 28 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: {
      type: 'category',
      data: (h?.xAxis.data ?? []).map((t) => {
        const n = Number(t)
        return Number.isNaN(n)
          ? t
          : new Date(n).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
      }),
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
    },
    // 功率(kW) 与 SOC(%) 量纲不同，分轴
    yAxis: [
      {
        type: 'value',
        name: 'kW',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: '#eef0f3' } },
      },
      ...(hasSoc
        ? [
            {
              type: 'value' as const,
              name: '%',
              min: 0,
              max: 100,
              nameTextStyle: { fontSize: 12 },
              axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
              splitLine: { show: false },
            },
          ]
        : []),
    ],
    series: (h?.series ?? []).map((s) => {
      const soc = s.name.toUpperCase().includes('SOC')
      return {
        name: soc ? 'SOC (%)' : `${s.name}${s.unit ? ` (${s.unit})` : ''}`,
        type: 'line' as const,
        smooth: true,
        showSymbol: false,
        yAxisIndex: soc && hasSoc ? 1 : 0,
        itemStyle: { color: soc ? CHART_COLORS.cyan : CHART_COLORS.brand },
        data: s.data.map((v) => (v === '' ? null : Number(v))),
      }
    }),
  }
}, [history])

onMounted(async () => {
  await loadDevices()
  await loadDeviceData()
  // 实时状态 10s 一拉。壳层还没接 WebSocket 推送，先用轮询；
  // 等 /station 或 /telemetry 的 WS 接进来再换成订阅。
  pollTimer = window.setInterval(() => {
    if (!document.hidden) void loadHistory()
  }, 10000)
})

onBeforeUnmount(() => {
  window.clearInterval(pollTimer)
  window.clearTimeout(toastTimer)
})

watch(deviceId, loadDeviceData)
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="loadDevices().then(loadDeviceData)">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="目标设备" :span="12" :foot="false">
      <template #actions>
        <button class="btn" type="button" :disabled="loading" @click="loadDeviceData">刷新</button>
      </template>
      <div v-if="socOutOfRange" class="notice error">
        <strong>SOC 越界</strong>
        <span>
          　当前 {{ socNow?.toFixed(0) }}%，不在配置允许的
          {{ limits.socMinPct.toFixed(0) }}% ~ {{ limits.socMaxPct.toFixed(0) }}% 区间内。
          继续下发可能加速电池衰减，请先确认现场状态。
        </span>
      </div>
      <div class="notice">
        <strong>实时下发</strong>
        <span>　指令会立即作用于现场设备。执行前请确认设备处于可操作状态，且已退出调试 / 检修。</span>
      </div>
      <label class="field">
        设备
        <select v-model="deviceId" :disabled="loading || !Object.keys(devices).length">
          <option v-for="(name, id) in devices" :key="id" :value="id">{{ name }}（{{ id }}）</option>
        </select>
        <span v-if="loading" class="muted">加载中…</span>
      </label>
    </PanelBox>

    <div class="kpis">
      <KpiCard
        label="通信状态"
        :value="current?.lastUpdateTime ? '在线' : '—'"
        :hint="
          current?.lastUpdateTime
            ? `更新于 ${new Date(current.lastUpdateTime).toLocaleTimeString('zh-CN')}`
            : '无遥测'
        "
      />
      <KpiCard
        v-for="m in KEY_METRICS"
        :key="m.key"
        :label="m.label"
        :value="m.key in values ? num(values[m.key]) : '—'"
        :unit="m.key in values ? m.unit : ''"
        :hint="m.key in values ? '' : '该设备未上报此点位'"
      />
    </div>

    <PanelBox title="保护约束" sub="来源：配置 · 储能参数" :span="12" :foot="false">
      <div class="limits">
        <div class="limit">
          <span class="lk">SOC 允许区间</span>
          <span class="lv">
            {{ limits.socMinPct.toFixed(0) }}% ~ {{ limits.socMaxPct.toFixed(0) }}%
          </span>
          <StatusTag :tone="socOutOfRange ? 'red' : 'green'">
            {{ socNow === null ? '无 SOC 遥测' : socOutOfRange ? '当前越界' : `当前 ${socNow.toFixed(0)}%` }}
          </StatusTag>
        </div>
        <div class="limit">
          <span class="lk">功率上限（设备 × 配置比例）</span>
          <span class="lv">
            充 {{ Number.isFinite(limits.chargeCap) ? limits.chargeCap.toFixed(0) : '—' }} kW ·
            放 {{ Number.isFinite(limits.dischargeCap) ? limits.dischargeCap.toFixed(0) : '—' }} kW
          </span>
          <span class="muted">下发功率超过此值会被拦下</span>
        </div>
        <div class="limit">
          <span class="lk">需量控制</span>
          <span class="lv">
            {{ limits.demandOn ? `启用 · 目标 ${limits.demandTarget} kW` : '未启用' }}
          </span>
          <span class="muted">关口功率越限由策略处理，手动下发不代判</span>
        </div>
      </div>
    </PanelBox>

    <PanelBox
      v-if="history?.series?.length"
      title="最近 2 小时充放"
      sub="功率 / SOC"
      :span="12"
      foot="数据来源：设备历史库 · 10 秒自动刷新"
    >
      <div ref="chartEl" class="chart" />
    </PanelBox>

    <PanelBox v-if="!services.length && !loading" title="可执行服务" :span="12" :foot="false">
      <div class="pending">
        <StatusTag tone="brand">无可用服务</StatusTag>
        <span>该设备驱动没有声明自定义服务，因此没有可手动下发的指令。</span>
      </div>
    </PanelBox>

    <PanelBox
      v-for="service in services"
      :key="service.key"
      :title="service.name || service.key"
      :sub="service.description"
      :span="6"
      :foot="`服务 key：${service.key} · 参数按驱动声明顺序下发`"
    >
      <div v-if="!(service.params ?? []).length" class="pending">该服务无参数，点击执行即可。</div>
      <div v-else class="fields">
        <div v-for="p in service.params" :key="p.key" class="field-item">
          <label :for="`${service.key}-${p.key}`">
            {{ p.name || p.key }}<span v-if="p.required" class="req">*</span>
            <span v-if="p.unit" class="unit">{{ p.unit }}</span>
          </label>

          <select
            v-if="p.valueExplain?.length"
            :id="`${service.key}-${p.key}`"
            v-model="form[service.key][p.key]"
          >
            <option v-for="e in p.valueExplain" :key="e.key" :value="e.key">{{ e.value }}</option>
          </select>
          <select
            v-else-if="p.valueType === 'bool'"
            :id="`${service.key}-${p.key}`"
            v-model="form[service.key][p.key]"
          >
            <option :value="true">是</option>
            <option :value="false">否</option>
          </select>
          <input
            v-else
            :id="`${service.key}-${p.key}`"
            v-model="form[service.key][p.key]"
            :type="p.valueType === 'string' ? 'text' : 'number'"
            :step="p.step ?? 1"
            :min="p.min"
            :max="p.max"
          />
          <span v-if="fieldErrors[`${service.key}.${p.key}`]" class="err">
            {{ fieldErrors[`${service.key}.${p.key}`] }}
          </span>
          <span v-else-if="p.description" class="hint">{{ p.description }}</span>
        </div>
      </div>

      <template #actions>
        <button
          class="btn primary"
          type="button"
          :disabled="busy === service.key"
          @click="askExecute(service)"
        >
          {{ busy === service.key ? '下发中…' : '执行' }}
        </button>
      </template>
    </PanelBox>
  </div>

  <div v-if="pending" class="modal-mask" @click.self="pending = null">
    <div class="modal">
      <h3>确认下发指令</h3>
      <p class="muted">请逐项核对。指令下发后立即生效，无法撤销。</p>
      <dl>
        <dt>设备</dt>
        <dd>{{ devices[deviceId] }}（{{ deviceId }}）</dd>
        <dt>服务</dt>
        <dd>{{ pending.service.name }}（{{ pending.service.key }}）</dd>
        <dt>参数</dt>
        <dd>
          <span v-if="!pending.params.length">无</span>
          <span v-else class="params">
            <span v-for="(p, i) in pending.service.params ?? []" :key="p.key">
              {{ p.name || p.key }} = {{ pending.params[i] }}{{ p.unit || '' }}
            </span>
          </span>
        </dd>
      </dl>
      <div class="modal-actions">
        <button class="btn" type="button" @click="pending = null">取消</button>
        <button class="btn primary" type="button" @click="confirmExecute">确认下发</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.kpis {
  grid-column: span 12;
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: var(--s4);
}
.chart { height: 300px; }
.limits { display: flex; flex-direction: column; gap: var(--s3); }
.limit { display: flex; align-items: center; gap: var(--s3); font-size: var(--fs-13); flex-wrap: wrap; }
.limit .lk { min-width: 180px; color: var(--text-3); font-size: var(--fs-12); }
.limit .lv { font-variant-numeric: tabular-nums; }
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
.field {
  display: inline-flex;
  align-items: center;
  gap: var(--s2);
  font-size: var(--fs-13);
  color: var(--text-3);
}
.field select {
  min-width: 260px;
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.fields { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--s3) var(--s4); }
.field-item { display: flex; flex-direction: column; gap: var(--s1); min-width: 0; }
.field-item label {
  display: flex;
  align-items: center;
  gap: var(--s1);
  font-size: var(--fs-12);
  color: var(--text-3);
}
.req { color: var(--red-ink); }
.unit { color: var(--text-3); }
.field-item input,
.field-item select {
  width: 100%;
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.field-item input:focus-visible,
.field-item select:focus-visible,
.field select:focus-visible {
  outline: 2px solid var(--brand-line);
  outline-offset: 1px;
  border-color: var(--brand-line);
}
.hint { font-size: var(--fs-12); color: var(--text-3); }
.err { font-size: var(--fs-12); color: var(--red-ink); }
.pending {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--s2);
  min-height: 96px;
  font-size: var(--fs-13);
  color: var(--text-3);
}
.btn {
  padding: var(--s1) var(--s4);
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
.btn.primary { background: var(--brand-btn); color: #fff; }
.btn.primary:hover:not(:disabled) { background: var(--brand-btn-h); }
.toast {
  position: fixed;
  top: calc(var(--h-appbar) + var(--s2));
  left: 50%;
  z-index: 50;
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
  z-index: 60;
  display: grid;
  place-items: center;
  padding: var(--s4);
  background: rgb(29 33 41 / 45%);
}
.modal {
  width: min(520px, 100%);
  padding: var(--s5);
  border: 0;
  border-top: 3px solid var(--brand);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.modal dl {
  display: grid;
  grid-template-columns: 64px 1fr;
  gap: var(--s2) var(--s3);
  margin: var(--s4) 0;
  font-size: var(--fs-13);
}
.modal dt { color: var(--text-3); }
.modal dd { margin: 0; }
.params { display: flex; flex-direction: column; gap: var(--s1); }
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); }
@media (max-width: 1280px) {
  .kpis { grid-template-columns: repeat(3, 1fr); }
  .fields { grid-template-columns: 1fr; }
}
</style>
