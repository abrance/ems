<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import { fetchDeviceNames, type DeviceType } from '@/api/device'
import {
  fetchPolicyConfig,
  fetchPolicyConfigFields,
  formatFieldValue,
  updateSetting,
  type ConfigField,
  type PolicyConfig,
} from '@/api/policy'

/**
 * 配置 · 储能参数（全局默认）。
 *
 * 表单字段全部来自后端 GET /api/policy/config-fields —— 它是反射 Go struct tag
 * 得到的（name/ct/vt/min/max/unit/group）。前端不复制字段表，struct 改了表单跟着改。
 *
 * 保存走 POST /api/setting/update，值是 JSON 字符串。
 * 策略插件每个调度周期重新读 setting，所以生效语义是「立即生效，下一个调度周期采纳」。
 * 这一点写在面板脚注里，不靠人猜。
 */
const SETTING_ID = 'policy_microgrid'
const SETTING_GROUP = 'policy'

/** 与充放电保护直接相关的分组，默认展开 */
const PRIMARY_GROUPS = ['储能参数', '峰谷套利', '需量控制']

/**
 * 设备绑定字段 → 可选的设备类型。
 *
 * 为什么要这张表：这两项是「策略读哪台设备的功率」的**口径开关**，选错就是静默的口径错误。
 * 它们在后端 schema 里是 singleSelect / multiSelect，但这一页以前只认 switch 与 number，
 * 其余一律当文本框 —— 于是变成「手打一个 UUID」的输入框，实际等于不可改。
 *
 * 类型限制不是装饰：`gridDeviceId` 只能是电表 —— 指到储能柜会把储能自身的出力算进
 * 「关口功率」，形成反馈回路（策略自己的代码注释写了这一点）。
 */
const DEVICE_FIELD_TYPES: Record<string, DeviceType[]> = {
  gridDeviceId: ['ammeter'],
  essDevices: ['ess', 'sess'],
  pvDevices: ['pv'],
  loadDevices: ['load'],
  cpDevices: ['cp'],
}

/** 类型限制之外再给一句人话，避免「选得到但选错」 */
const DEVICE_FIELD_HINT: Record<string, string> = {
  gridDeviceId:
    '策略用它读关口功率。不要指向储能柜 —— 关口功率里已包含储能自身出力，会形成反馈回路；有独立计量点就指那个。',
  essDevices: '参与充放的储能柜；一台都没配时策略不会动作。',
}

function isDeviceField(f: ConfigField): boolean {
  return f.component === 'singleSelect' || f.component === 'multiSelect'
}

/** 设备列表：字段 key → [{id, name}]，按上面那张表过滤类型 */
const deviceOptions = ref<Record<string, { id: string; name: string }[]>>({})

const fields = ref<ConfigField[]>([])
const groups = ref<string[]>([])
const original = ref<PolicyConfig>({})
/** 编辑中的值，键为字段 key */
const draft = reactive<Record<string, unknown>>({})
const collapsed = reactive<Record<string, boolean>>({})

const loading = ref(false)
const error = ref('')
const saving = ref(false)
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined
/** 保存前展示的差异 */
const pendingDiff = ref<{ key: string; field: ConfigField; before: unknown; after: unknown }[] | null>(null)

const fieldsByGroup = computed(() => {
  const map = new Map<string, ConfigField[]>()
  for (const g of groups.value) map.set(g, [])
  for (const f of fields.value) {
    const key = f.group || '其它'
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(f)
  }
  // 主分组排前面，其余保持后端顺序
  return [...map.entries()].sort((a, b) => {
    const ai = PRIMARY_GROUPS.indexOf(a[0])
    const bi = PRIMARY_GROUPS.indexOf(b[0])
    if (ai === -1 && bi === -1) return 0
    if (ai === -1) return 1
    if (bi === -1) return -1
    return ai - bi
  })
})

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

/** 把后端返回值规整成表单可用的类型（number 字段要真的是 number；数组字段要真的是数组） */
function normalize(f: ConfigField, raw: unknown): unknown {
  if (f.component === 'multiSelect') {
    if (Array.isArray(raw)) return raw.map(String)
    if (raw === undefined || raw === null || raw === '') return []
    return [String(raw)]
  }
  if (raw === undefined || raw === null) {
    if (f.default !== undefined && f.default !== '') return normalize(f, f.default)
    return f.valueType === 'bool' ? false : ''
  }
  if (f.valueType === 'bool') return raw === true || raw === 'true'
  if (f.valueType === 'int') return Math.round(Number(raw))
  if (f.valueType === 'float') return Number(raw)
  return String(raw)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [schema, current] = await Promise.all([fetchPolicyConfigFields(), fetchPolicyConfig()])
    fields.value = schema.fields
    groups.value = schema.groups
    original.value = current.config ?? {}
    for (const f of schema.fields) {
      draft[f.key] = normalize(f, original.value[f.key])
    }
    for (const g of schema.groups) {
      collapsed[g] = !PRIMARY_GROUPS.includes(g)
    }
    await loadDeviceOptions(schema.fields)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

/** 只给 schema 里真实存在的设备字段取选项（现在就是 gridDeviceId 与 essDevices 两个） */
async function loadDeviceOptions(list: ConfigField[]) {
  const wanted = list.filter(isDeviceField)
  const entries = await Promise.all(
    wanted.map(async (f) => {
      const types = DEVICE_FIELD_TYPES[f.key]
      if (!types) return [f.key, []] as const
      try {
        const names = await fetchDeviceNames(types)
        return [f.key, Object.entries(names).map(([id, name]) => ({ id, name }))] as const
      } catch {
        // 设备列表拿不到不让整页报错：字段退化成文本框，下面会提示「没有可选设备」
        return [f.key, []] as const
      }
    }),
  )
  deviceOptions.value = Object.fromEntries(entries)
}

/** 已绑定的设备名（选择器下面显示「当前」用；多选按顿号连） */
function boundName(f: ConfigField): string {
  const raw = draft[f.key]
  const ids = Array.isArray(raw) ? raw.map(String) : raw ? [String(raw)] : []
  if (!ids.length) return f.component === 'multiSelect' ? '未选' : '未绑定'
  const opts = deviceOptions.value[f.key] ?? []
  return ids
    .map((id) => {
      const hit = opts.find((o) => o.id === id)
      return hit ? hit.name : `${id}（不在可选列表：类型不匹配或设备已删除）`
    })
    .join('、')
}

/** 字段下方那句话：描述 + 设备绑定的口径提醒 + 当前绑定 */
function hintOf(f: ConfigField): string {
  const parts = [f.description || f.key]
  if (DEVICE_FIELD_HINT[f.key]) parts.push(DEVICE_FIELD_HINT[f.key])
  if (isDeviceField(f)) parts.push(`当前：${boundName(f)}`)
  return parts.join(' · ')
}

function isDirty(f: ConfigField): boolean {
  const before = normalize(f, original.value[f.key])
  const after = normalize(f, draft[f.key])
  // 数组字段（多选设备）不能用 String() 比：Array/String 的相等性会看错
  return JSON.stringify(after) !== JSON.stringify(before)
}

function fieldError(f: ConfigField): string {
  // 只校验数字字段。设备选择/布尔/文本没有「数值范围」这回事 ——
  // 之前这里对**所有非布尔字段**都跑 Number()，于是文本字段（如预测服务地址）永远报
  // 「需要是数字」，把保存整个堵死。
  if (f.valueType !== 'int' && f.valueType !== 'float') return ''
  const v = draft[f.key]
  if (v === '' || v === undefined || v === null) return ''
  const n = Number(v)
  if (Number.isNaN(n)) return '需要是数字'
  if (f.min !== undefined && f.min !== null && n < f.min) return `不能小于 ${f.min}`
  if (f.max !== undefined && f.max !== null && n > f.max) return `不能大于 ${f.max}`
  return ''
}

const dirtyFields = computed(() => fields.value.filter(isDirty))
const hasError = computed(() => fields.value.some((f) => fieldError(f) !== ''))

function askSave() {
  if (hasError.value) {
    showToast('red', '有字段未通过校验，请先修正')
    return
  }
  const diff = dirtyFields.value.map((f) => ({
    key: f.key,
    field: f,
    before: normalize(f, original.value[f.key]),
    after: draft[f.key],
  }))
  if (!diff.length) {
    showToast('green', '没有改动')
    return
  }
  // 改前看 diff（设计稿 §11.4 规矩 2）
  pendingDiff.value = diff
}

async function confirmSave() {
  const diff = pendingDiff.value
  if (!diff) return
  pendingDiff.value = null
  saving.value = true
  try {
    // 只回写后端已有的键 + 本次改动的键，避免把未知字段带进去
    const next: PolicyConfig = { ...original.value }
    for (const d of diff) next[d.key] = d.after
    await updateSetting({
      id: SETTING_ID,
      group: SETTING_GROUP,
      value: JSON.stringify(next),
      remark: '储能参数（全局默认）',
    })
    original.value = next
    showToast('green', `已保存 ${diff.length} 项，下一个调度周期生效`)
  } catch (err) {
    showToast('red', `保存失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    saving.value = false
  }
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
    这些是<strong>全局默认</strong>约束：策略与手动控制没单独指定时按这里走。
    保存后**立即生效，下一个调度周期采纳**（策略每个周期重新读配置）。
  </div>

  <div class="grid">
    <PanelBox
      v-for="[group, list] in fieldsByGroup"
      :key="group"
      :title="group"
      :sub="`${list.length} 项`"
      :span="6"
      :loading="loading && !fields.length"
      :foot="PRIMARY_GROUPS.includes(group) ? '与充放电保护直接相关' : '非充放电主线条，按需调整'"
    >
      <template #actions>
        <button class="btn sm" type="button" @click="collapsed[group] = !collapsed[group]">
          {{ collapsed[group] ? '展开' : '收起' }}
        </button>
      </template>

      <div v-show="!collapsed[group]" class="fields">
        <div v-for="f in list" :key="f.key" class="field-item">
          <label :for="`f-${f.key}`">
            {{ f.name || f.key }}<span v-if="f.unit" class="unit">{{ f.unit }}</span>
            <span v-if="isDirty(f)" class="dirty">已改</span>
          </label>

          <label v-if="f.component === 'switch'" class="switch-row">
            <input
              :id="`f-${f.key}`"
              v-model="draft[f.key]"
              type="checkbox"
              :true-value="true"
              :false-value="false"
            />
            <span>{{ draft[f.key] ? '开' : '关' }}</span>
          </label>
          <!-- 设备绑定：后端 schema 标了 singleSelect / multiSelect，原来落到文本框（手打 UUID），
               现在渲染成设备选择器，并按 DEVICE_FIELD_TYPES 限制可选类型 -->
          <select
            v-else-if="f.component === 'singleSelect'"
            :id="`f-${f.key}`"
            v-model="draft[f.key]"
          >
            <option value="">未绑定</option>
            <option v-for="o in deviceOptions[f.key] ?? []" :key="o.id" :value="o.id">
              {{ o.name }}
            </option>
          </select>
          <div v-else-if="f.component === 'multiSelect'" class="device-multi">
            <label v-for="o in deviceOptions[f.key] ?? []" :key="o.id" class="device-opt">
              <input v-model="draft[f.key]" type="checkbox" :value="o.id" />
              <span>{{ o.name }}</span>
            </label>
            <span v-if="!(deviceOptions[f.key] ?? []).length" class="hint">
              没有可选设备（先在「管理 · 设备管理」接入）。
            </span>
          </div>
          <input
            v-else-if="f.component === 'number'"
            :id="`f-${f.key}`"
            v-model="draft[f.key]"
            type="number"
            :step="f.step ?? (f.valueType === 'int' ? 1 : 0.01)"
            :min="f.min"
            :max="f.max"
          />
          <input :id="`f-${f.key}`" v-else v-model="draft[f.key]" type="text" />

          <span v-if="fieldError(f)" class="err">{{ fieldError(f) }}</span>
          <span v-else class="hint">
            {{ hintOf(f) }}
            <template v-if="f.min !== undefined || f.max !== undefined">
              （范围 {{ f.min ?? '—' }} ~ {{ f.max ?? '—' }}）
            </template>
          </span>
        </div>
      </div>
    </PanelBox>

    <PanelBox title="保存" :span="12" :foot="false">
      <div class="save-row">
        <span class="muted">
          {{ dirtyFields.length ? `${dirtyFields.length} 项待保存` : '暂无改动' }}
        </span>
        <span class="spacer" />
        <button class="btn" type="button" :disabled="loading" @click="load">重新加载</button>
        <button class="btn primary" type="button" :disabled="saving || !dirtyFields.length" @click="askSave">
          {{ saving ? '保存中…' : '保存' }}
        </button>
      </div>
    </PanelBox>
  </div>

  <div v-if="pendingDiff" class="modal-mask" @click.self="pendingDiff = null">
    <div class="modal">
      <h3>确认修改（{{ pendingDiff.length }} 项）</h3>
      <p class="muted">保存后立即生效，下一个调度周期采纳。</p>
      <ul class="diff">
        <li v-for="d in pendingDiff" :key="d.key">
          <span class="k">{{ d.field.name || d.key }}</span>
          <span class="before">{{ formatFieldValue(d.field, d.before) }}</span>
          <span class="arrow">→</span>
          <span class="after">{{ formatFieldValue(d.field, d.after) }}</span>
        </li>
      </ul>
      <div class="modal-actions">
        <button class="btn" type="button" @click="pendingDiff = null">取消</button>
        <button class="btn primary" type="button" @click="confirmSave">确认保存</button>
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
.fields { display: flex; flex-direction: column; gap: var(--s3); }
.field-item { display: flex; flex-direction: column; gap: var(--s1); min-width: 0; }
.field-item label {
  display: flex;
  align-items: center;
  gap: var(--s2);
  font-size: var(--fs-12);
  color: var(--text-3);
}
.unit { color: var(--text-3); }
.dirty { background: var(--brand-soft); color: var(--brand-ink); border-radius: var(--r-pill); padding: 0 var(--s2); }
.field-item input {
  width: 100%;
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.field-item input:focus-visible {
  outline: 2px solid var(--brand-line);
  outline-offset: 1px;
  border-color: var(--brand-line);
}
.switch-row { display: flex; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-1); }
.switch-row input { width: auto; }
.device-multi { display: flex; flex-direction: column; gap: var(--s1); }
.device-opt { display: flex; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-1); }
.device-opt input { width: auto; }
.hint { font-size: var(--fs-12); color: var(--text-3); }
.err { font-size: var(--fs-12); color: var(--red-ink); }
.save-row { display: flex; align-items: center; gap: var(--s2); }
.spacer { flex: 1; }
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
.btn.sm { padding: 0 var(--s2); font-size: var(--fs-12); }
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
  width: min(560px, 100%);
  padding: var(--s5);
  border-top: 3px solid var(--brand);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.diff {
  max-height: 40vh;
  margin: var(--s4) 0;
  padding: 0;
  overflow-y: auto;
  list-style: none;
  font-size: var(--fs-13);
}
.diff li { display: flex; align-items: center; gap: var(--s3); padding: var(--s2) 0; border-bottom: 1px solid var(--divider); }
.diff .k { flex: 1; color: var(--text-2); }
.diff .before { color: var(--text-3); text-decoration: line-through; }
.diff .arrow { color: var(--text-3); }
.diff .after { color: var(--brand-ink); font-weight: 600; }
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12; }
}
</style>
