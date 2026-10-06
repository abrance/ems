<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchDeviceNames, type DeviceType } from '@/api/device'
import { fetchCustomServices, type DriverService } from '@/api/control'
import {
  buildRule,
  conditionText,
  createAutomation,
  deleteAutomation,
  listAutomations,
  parseRule,
  RULE_OPERATORS,
  toggleAutomation,
  updateAutomation,
  type Automation,
  type ExecuteRuleItem,
} from '@/api/automation'

/**
 * 管理 · 自动化联动。
 *
 * 规则读法：`anyMatch` 里任一条件满足 → 执行 `executeRule.rules`。
 * 设备条件在模型里是**表达式字符串**（`Power>30`），不是结构化比较，
 * 所以编辑时用「点位 + 比较符 + 值」拼；读回来解析展示，解析不了就原样显示，不猜。
 *
 * 执行动作的 params 是**位置参数**，顺序必须与驱动服务声明一致 ——
 * 所以选完服务后按它的参数定义渲染输入框（与手动控制页同一约定）。
 */
const list = ref<Automation[]>([])
const devices = ref<Record<string, string>>({})
const loading = ref(false)
const error = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const filterEnabled = ref<'' | '1' | '0'>('')

const drawer = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
/** 每个执行规则对应设备的服务清单缓存 */
const servicesByDevice = reactive<Record<string, DriverService[]>>({})

interface UiDeviceCond {
  deviceId: string
  point: string
  op: string
  value: string
}
interface UiTimeCond {
  startTime: string
  endTime: string
  dayOfWeek: string
}
interface UiCondition {
  kind: 'device' | 'time'
  device: UiDeviceCond
  time: UiTimeCond
}
interface UiExecuteRule {
  deviceId: string
  service: string
  params: string[]
}

const form = reactive<{
  name: string
  enabled: boolean
  executionInterval: number
  conditions: UiCondition[]
  executes: UiExecuteRule[]
}>({
  name: '',
  enabled: true,
  executionInterval: 0,
  conditions: [],
  executes: [],
})

const deleteTarget = ref<Automation | null>(null)
const deleteConfirmText = ref('')

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await listAutomations({
      enabled: filterEnabled.value === '' ? undefined : filterEnabled.value === '1',
    })
    list.value = res.list
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function ensureServices(deviceId: string) {
  if (!deviceId || servicesByDevice[deviceId]) return
  try {
    servicesByDevice[deviceId] = await fetchCustomServices(deviceId)
  } catch {
    servicesByDevice[deviceId] = []
  }
}

function serviceDef(deviceId: string, serviceKey: string): DriverService | undefined {
  return (servicesByDevice[deviceId] ?? []).find((s) => s.key === serviceKey)
}

/* ---------- 摘要 ---------- */
function triggerSummary(a: Automation): string {
  const any = a.triggerRule?.anyMatch ?? []
  if (!any.length) return '（无触发条件）'
  return any.map(conditionText).join(' 或 ')
}

function executeSummary(a: Automation): string {
  const rules = a.executeRule?.rules ?? []
  if (!rules.length) return '（无执行动作）'
  return rules.map((r) => `${r.deviceId} → ${r.service}`).join('；')
}

function intervalText(a: Automation): string {
  const s = a.triggerRule?.executionInterval ?? 0
  return s > 0 ? `间隔 ${s}s` : '实时'
}

/* ---------- 编辑 ---------- */
function emptyCondition(kind: 'device' | 'time'): UiCondition {
  return {
    kind,
    device: { deviceId: '', point: '', op: '>', value: '' },
    time: { startTime: '', endTime: '', dayOfWeek: '' },
  }
}

function emptyExecute(): UiExecuteRule {
  return { deviceId: '', service: '', params: [] }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    name: '',
    enabled: true,
    executionInterval: 0,
    conditions: [emptyCondition('device')],
    executes: [emptyExecute()],
  })
  drawer.value = true
}

async function openEdit(a: Automation) {
  editingId.value = a.id
  const conditions: UiCondition[] = (a.triggerRule?.anyMatch ?? []).map((c) => {
    if (c.deviceCondition) {
      const parsed = parseRule(c.deviceCondition.rule)
      return {
        kind: 'device' as const,
        device: {
          deviceId: c.deviceCondition.deviceId,
          point: parsed.point,
          op: parsed.op,
          value: parsed.value,
        },
        time: { startTime: '', endTime: '', dayOfWeek: '' },
      }
    }
    const t = c.timeCondition ?? {}
    return {
      kind: 'time' as const,
      device: { deviceId: '', point: '', op: '>', value: '' },
      time: {
        startTime: t.startTime ?? '',
        endTime: t.endTime ?? '',
        dayOfWeek: t.dayOfWeek != null ? String(t.dayOfWeek) : '',
      },
    }
  })

  const executes: UiExecuteRule[] = (a.executeRule?.rules ?? []).map((r) => ({
    deviceId: r.deviceId,
    service: r.service,
    params: (r.params ?? []).map((p) => String(p)),
  }))

  Object.assign(form, {
    name: a.name,
    enabled: a.enabled,
    executionInterval: a.triggerRule?.executionInterval ?? 0,
    conditions: conditions.length ? conditions : [emptyCondition('device')],
    executes: executes.length ? executes : [emptyExecute()],
  })
  drawer.value = true
  for (const e of executes) await ensureServices(e.deviceId)
}

async function onExecuteDeviceChange(idx: number) {
  const rule = form.executes[idx]
  rule.service = ''
  rule.params = []
  await ensureServices(rule.deviceId)
}

function onExecuteServiceChange(idx: number) {
  const rule = form.executes[idx]
  const def = serviceDef(rule.deviceId, rule.service)
  // 参数个数按服务声明重置，避免上一份服务的参数串到这一份
  rule.params = (def?.params ?? []).map((p) => p.default ?? '')
}

const formIssue = computed(() => {
  if (!form.name.trim()) return '名称必填'
  if (!form.conditions.length) return '至少一个触发条件'
  for (const [i, c] of form.conditions.entries()) {
    if (c.kind === 'device') {
      if (!c.device.deviceId) return `第 ${i + 1} 个条件：未选设备`
      if (!c.device.point.trim()) return `第 ${i + 1} 个条件：未填点位`
      if (c.device.value.trim() === '') return `第 ${i + 1} 个条件：未填比较值`
    } else if (!c.time.startTime && !c.time.endTime && !c.time.dayOfWeek) {
      return `第 ${i + 1} 个条件：时间条件为空`
    }
  }
  if (!form.executes.length) return '至少一个执行动作'
  for (const [i, e] of form.executes.entries()) {
    if (!e.deviceId) return `第 ${i + 1} 个动作：未选设备`
    if (!e.service) return `第 ${i + 1} 个动作：未选服务`
  }
  if (!Number.isFinite(Number(form.executionInterval)) || Number(form.executionInterval) < 0) {
    return '执行间隔不能为负'
  }
  return ''
})

async function save() {
  if (formIssue.value) {
    showToast('red', formIssue.value)
    return
  }
  saving.value = true
  try {
    const executeRules: ExecuteRuleItem[] = form.executes.map((e) => {
      const def = serviceDef(e.deviceId, e.service)
      const params = (def?.params ?? []).map((p, i) => {
        const raw = e.params[i] ?? ''
        if (p.valueType === 'bool') return raw === 'true' || raw === '1' || raw === '是'
        if (p.valueType === 'int') return Math.round(Number(raw))
        if (p.valueType === 'float') return Number(raw)
        return raw
      })
      return { deviceId: e.deviceId, service: e.service, params }
    })

    const payload = {
      name: form.name.trim(),
      enabled: form.enabled,
      triggerRule: {
        anyMatch: form.conditions.map((c) =>
          c.kind === 'device'
            ? {
                deviceCondition: {
                  deviceId: c.device.deviceId,
                  from: '',
                  rule: buildRule(c.device.point, c.device.op, c.device.value),
                },
              }
            : {
                timeCondition: {
                  startTime: c.time.startTime,
                  endTime: c.time.endTime,
                  dayOfWeek: c.time.dayOfWeek === '' ? null : Number(c.time.dayOfWeek),
                },
              },
        ),
        subMatch: [],
        subMatchAll: false,
        executionInterval: Number(form.executionInterval) || 0,
      },
      executeRule: JSON.stringify({ rules: executeRules }),
    }

    if (editingId.value === null) await createAutomation(payload)
    else await updateAutomation(editingId.value, payload)
    drawer.value = false
    showToast('green', editingId.value === null ? '已创建规则' : '已保存规则')
    await load()
  } catch (err) {
    showToast('red', `保存失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    saving.value = false
  }
}

async function toggle(a: Automation) {
  try {
    await toggleAutomation(a.id, !a.enabled)
    showToast('green', a.enabled ? '已停用' : '已启用')
    await load()
  } catch (err) {
    showToast('red', `操作失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

async function confirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  if (deleteConfirmText.value.trim() !== target.name) return
  try {
    await deleteAutomation(target.id)
    deleteTarget.value = null
    showToast('green', '已删除规则')
    await load()
  } catch (err) {
    showToast('red', `删除失败：${err instanceof Error ? err.message : '未知错误'}`)
  }
}

onMounted(async () => {
  const types: DeviceType[] = ['pcs', 'bms', 'ess', 'sess', 'ammeter', 'load', 'pv', 'gpio']
  devices.value = await fetchDeviceNames(types).catch(() => ({}))
  await load()
})
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="自动化规则" :sub="`共 ${list.length} 条`" :span="12" :loading="loading" :foot="false">
      <template #actions>
        <label class="field">
          状态
          <select v-model="filterEnabled" @change="load">
            <option value="">全部</option>
            <option value="1">已启用</option>
            <option value="0">已停用</option>
          </select>
        </label>
        <button class="btn sm" type="button" @click="load">刷新</button>
        <button class="btn sm primary" type="button" @click="openCreate">+ 新建规则</button>
      </template>

      <div class="rules">
        <article v-for="a in list" :key="a.id" class="rule" :class="{ off: !a.enabled }">
          <header class="rule-head">
            <div class="title">
              <b>{{ a.name }}</b>
              <StatusTag :tone="a.enabled ? 'green' : 'gray'">{{ a.enabled ? '已启用' : '已停用' }}</StatusTag>
              <StatusTag tone="gray">{{ intervalText(a) }}</StatusTag>
            </div>
            <div class="ops">
              <button class="btn sm" type="button" @click="openEdit(a)">编辑</button>
              <button class="btn sm" type="button" @click="toggle(a)">{{ a.enabled ? '停用' : '启用' }}</button>
              <button class="btn sm danger" type="button" @click="((deleteTarget = a), (deleteConfirmText = ''))">
                删除
              </button>
            </div>
          </header>
          <div class="flow">
            <div class="side">
              <span class="label">当（任一满足）</span>
              <span class="cond">{{ triggerSummary(a) }}</span>
            </div>
            <div class="arrow">→</div>
            <div class="side">
              <span class="label">则执行</span>
              <span class="cond">{{ executeSummary(a) }}</span>
            </div>
          </div>
        </article>
        <div v-if="!list.length && !loading" class="empty">还没有自动化规则。点「+ 新建规则」。</div>
      </div>
    </PanelBox>
  </div>

  <!-- 编辑抽屉 -->
  <div v-if="drawer" class="drawer-mask" @click.self="drawer = false">
    <aside class="drawer">
      <header class="drawer-head">
        <h3>{{ editingId === null ? '新建自动化规则' : `编辑规则 #${editingId}` }}</h3>
        <span class="spacer" />
        <button class="btn sm" type="button" @click="drawer = false">关闭</button>
      </header>

      <div class="drawer-body">
        <div class="form-grid">
          <label class="wide">名称 <input v-model="form.name" placeholder="例如：过温停充" /></label>
          <label>
            启用
            <select v-model="form.enabled">
              <option :value="true">启用</option>
              <option :value="false">停用</option>
            </select>
          </label>
          <label>执行间隔（秒，0=实时）<input v-model.number="form.executionInterval" type="number" min="0" /></label>
        </div>

        <section class="block">
          <div class="block-head">
            <h4>触发条件（任一满足即触发）</h4>
            <button class="btn sm" type="button" @click="form.conditions.push(emptyCondition('device'))">+ 设备条件</button>
            <button class="btn sm" type="button" @click="form.conditions.push(emptyCondition('time'))">+ 时间条件</button>
          </div>

          <div v-for="(c, i) in form.conditions" :key="i" class="cond-row">
            <div class="cond-top">
              <StatusTag :tone="c.kind === 'device' ? 'blue' : 'purple'">
                {{ c.kind === 'device' ? '设备条件' : '时间条件' }}
              </StatusTag>
              <span class="spacer" />
              <button
                class="btn sm danger"
                type="button"
                :disabled="form.conditions.length <= 1"
                @click="form.conditions.splice(i, 1)"
              >
                移除
              </button>
            </div>

            <div v-if="c.kind === 'device'" class="cond-body">
              <label>
                设备
                <select v-model="c.device.deviceId">
                  <option value="">请选择</option>
                  <option v-for="(name, id) in devices" :key="id" :value="id">{{ name }}（{{ id }}）</option>
                </select>
              </label>
              <label>点位<input v-model="c.device.point" placeholder="如 Power / SOC" /></label>
              <label>
                比较
                <select v-model="c.device.op">
                  <option v-for="op in RULE_OPERATORS" :key="op" :value="op">{{ op }}</option>
                </select>
              </label>
              <label>值<input v-model="c.device.value" placeholder="如 30" /></label>
            </div>

            <div v-else class="cond-body">
              <label>开始<input v-model="c.time.startTime" type="time" /></label>
              <label>结束<input v-model="c.time.endTime" type="time" /></label>
              <label>
                星期
                <select v-model="c.time.dayOfWeek">
                  <option value="">不限</option>
                  <option v-for="(d, k) in ['周日', '周一', '周二', '周三', '周四', '周五', '周六']" :key="k" :value="String(k)">
                    {{ d }}
                  </option>
                </select>
              </label>
            </div>
          </div>
        </section>

        <section class="block">
          <div class="block-head">
            <h4>执行动作</h4>
            <button class="btn sm" type="button" @click="form.executes.push(emptyExecute())">+ 动作</button>
          </div>

          <div v-for="(e, i) in form.executes" :key="i" class="cond-row">
            <div class="cond-top">
              <StatusTag tone="brand">动作 {{ i + 1 }}</StatusTag>
              <span class="spacer" />
              <button class="btn sm danger" type="button" :disabled="form.executes.length <= 1" @click="form.executes.splice(i, 1)">
                移除
              </button>
            </div>

            <div class="cond-body">
              <label>
                设备
                <select v-model="e.deviceId" @change="onExecuteDeviceChange(i)">
                  <option value="">请选择</option>
                  <option v-for="(name, id) in devices" :key="id" :value="id">{{ name }}（{{ id }}）</option>
                </select>
              </label>
              <label>
                服务
                <select v-model="e.service" :disabled="!e.deviceId" @change="onExecuteServiceChange(i)">
                  <option value="">请选择</option>
                  <option v-for="s in servicesByDevice[e.deviceId] ?? []" :key="s.key" :value="s.key">
                    {{ s.name || s.key }}
                  </option>
                </select>
              </label>
              <div v-if="e.service && (serviceDef(e.deviceId, e.service)?.params?.length ?? 0)" class="params">
                <label v-for="(p, pi) in serviceDef(e.deviceId, e.service)?.params ?? []" :key="p.key">
                  {{ p.name || p.key }}<span v-if="p.unit" class="unit">{{ p.unit }}</span>
                  <input v-model="e.params[pi]" :placeholder="p.valueType" />
                </label>
              </div>
              <div v-else-if="e.service" class="muted">该服务无参数。</div>
            </div>
          </div>
        </section>

        <div v-if="formIssue" class="err">{{ formIssue }}</div>
      </div>

      <footer class="drawer-foot">
        <span class="muted">位置参数按服务声明顺序下发，与手动控制页同一约定</span>
        <span class="spacer" />
        <button class="btn" type="button" @click="drawer = false">取消</button>
        <button class="btn primary" type="button" :disabled="saving || !!formIssue" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </button>
      </footer>
    </aside>
  </div>

  <div v-if="deleteTarget" class="modal-mask" @click.self="deleteTarget = null">
    <div class="modal">
      <h3>删除自动化规则</h3>
      <p class="muted">将删除「{{ deleteTarget.name }}」。请输入规则名确认。</p>
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
  padding: 0 var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.rules { display: flex; flex-direction: column; gap: var(--s3); }
.rule { padding: var(--s3) var(--s4); border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-2); }
.rule.off { background: var(--bg-4); }
.rule-head { display: flex; align-items: center; gap: var(--s3); flex-wrap: wrap; }
.title { display: flex; align-items: center; gap: var(--s2); }
.title b { font-size: var(--fs-14); }
.ops { display: flex; gap: var(--s1); margin-left: auto; }
.flow { display: flex; align-items: center; gap: var(--s3); margin-top: var(--s3); }
.side { display: flex; flex-direction: column; gap: var(--s1); min-width: 0; flex: 1; }
.side .label { font-size: var(--fs-12); color: var(--text-3); }
.side .cond { font-size: var(--fs-13); word-break: break-word; }
.arrow { color: var(--text-3); }
.empty { padding: var(--s5); color: var(--text-3); text-align: center; }
.field { display: inline-flex; align-items: center; gap: var(--s1); font-size: var(--fs-12); color: var(--text-3); }
.field select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-12);
}
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
  width: min(820px, 100%);
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
.drawer-foot {
  display: flex;
  align-items: center;
  gap: var(--s3);
  padding: var(--s3) var(--s4);
  border-top: 1px solid var(--border);
}
.spacer { flex: 1; }
.form-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--s3) var(--s4); }
.form-grid label,
.cond-body label,
.params label { display: flex; flex-direction: column; gap: var(--s1); font-size: var(--fs-12); color: var(--text-3); }
.form-grid label.wide { grid-column: span 2; }
input,
select {
  width: 100%;
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
input:focus-visible,
select:focus-visible { outline: 2px solid var(--brand-line); outline-offset: 1px; }
.block { margin-top: var(--s5); }
.block-head { display: flex; align-items: center; gap: var(--s2); margin-bottom: var(--s3); }
.block-head h4 { flex: 1; margin: 0; font-size: var(--fs-14); }
.cond-row { margin-bottom: var(--s3); padding: var(--s3); border-radius: var(--r-md); background: var(--bg-4); }
.cond-top { display: flex; align-items: center; gap: var(--s2); margin-bottom: var(--s2); }
.cond-body { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s3); }
.params { display: grid; grid-column: span 4; grid-template-columns: repeat(3, 1fr); gap: var(--s2); grid-auto-rows: min-content; }
.unit { color: var(--text-3); }
.err { margin-top: var(--s3); font-size: var(--fs-12); color: var(--red-ink); }
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
.btn.danger { background: var(--red-soft); color: var(--red-ink); }
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
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .cond-body { grid-template-columns: repeat(2, 1fr); }
  .params { grid-column: span 2; grid-template-columns: repeat(2, 1fr); }
  .form-grid { grid-template-columns: 1fr; }
  .form-grid label.wide { grid-column: span 1; }
}
</style>
