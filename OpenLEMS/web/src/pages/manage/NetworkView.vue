<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  listInterfaces,
  ping,
  setInterfaceState,
  updateInterface,
  type InterfaceSummary,
  type PingResult,
} from '@/api/network'

/**
 * 管理 · 网络配置。
 *
 * 两个坑写在页面上：
 *   1. `/network/interface/list` 后端有 5 分钟缓存 —— 改完配置不会自动失效，
 *      页面必须显式 forceRefresh 再读，否则会看到旧值。
 *   2. 改 IP / 关网卡可能直接断掉当前连接。所以提交前给明确警告，
 *      且不自动跳转、不自动重载页面。
 */
const ifaces = ref<InterfaceSummary[]>([])
const loading = ref(false)
const error = ref('')
const busy = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const selectedName = ref('')
const edit = reactive({ dhcp: false, ipv4: '', subnetMask: '', gateway: '', dns: '' })
const pendingSave = ref(false)
const pendingDown = ref(false)

const pingTarget = ref('')
const pingResult = ref<PingResult | null>(null)
const pinging = ref(false)

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 5000)
}

const selected = computed(() => ifaces.value.find((i) => i.name === selectedName.value) ?? null)

/** 只显示有 IP 的接口时更干净，但要保留开关给用户自己决定 */
const showAll = ref(true)
const shown = computed(() =>
  showAll.value ? ifaces.value : ifaces.value.filter((i) => i.ipv4?.length && i.name !== 'lo'),
)

async function load(force = false) {
  loading.value = true
  error.value = ''
  try {
    ifaces.value = await listInterfaces({ includeLoopback: true, forceRefresh: force })
    if (!selectedName.value && ifaces.value.length) select(ifaces.value[0])
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function select(i: InterfaceSummary) {
  selectedName.value = i.name
  const v4 = i.ipv4?.[0]
  edit.dhcp = i.dhcp
  edit.ipv4 = v4?.ipv4 ?? ''
  edit.subnetMask = v4?.subnetMask ?? ''
  edit.gateway = (i.gateway ?? []).join(', ')
  edit.dns = (i.dns ?? []).join(', ')
}

function splitList(v: string): string[] {
  return v
    .split(/[,，\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

// 每段必须是 0~255：早期写的 (\d{1,3}\.){3}\d{1,3} 会放过 999.1.1.1，
// 是冒烟用例先发现的（断言它必须被拦住），这里收紧
const OCTET = '(25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)'
const IPV4_RE = new RegExp(`^${OCTET}(\\.${OCTET}){3}$`)

const editIssue = computed(() => {
  if (!edit.dhcp) {
    if (!IPV4_RE.test(edit.ipv4.trim())) return 'IP 地址格式不正确'
    if (!IPV4_RE.test(edit.subnetMask.trim())) return '子网掩码格式不正确'
  }
  for (const g of splitList(edit.gateway)) if (!IPV4_RE.test(g)) return `网关格式不正确：${g}`
  for (const d of splitList(edit.dns)) if (!IPV4_RE.test(d)) return `DNS 格式不正确：${d}`
  return ''
})

function askSave() {
  if (editIssue.value) {
    showToast('red', editIssue.value)
    return
  }
  pendingSave.value = true
}

async function confirmSave() {
  pendingSave.value = false
  if (!selected.value) return
  busy.value = selected.value.name
  try {
    await updateInterface(selected.value.name, {
      dhcp: edit.dhcp,
      ipv4: edit.dhcp ? [] : [{ ipv4: edit.ipv4.trim(), subnetMask: edit.subnetMask.trim() }],
      gateway: splitList(edit.gateway),
      dns: splitList(edit.dns),
    })
    showToast('green', '已提交；列表已强制刷新（后端有缓存，这里显式穿透）')
    // 改完必须 forceRefresh，否则会读到缓存里的旧配置
    await load(true)
  } catch (err) {
    showToast('red', `保存失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    busy.value = ''
  }
}

function askToggleState() {
  if (!selected.value) return
  pendingDown.value = true
}

async function confirmToggleState() {
  const target = selected.value
  pendingDown.value = false
  if (!target) return
  busy.value = target.name
  try {
    await setInterfaceState(target.name, !target.isUp)
    showToast('green', target.isUp ? `已禁用 ${target.name}` : `已启用 ${target.name}`)
    await load(true)
  } catch (err) {
    showToast('red', `操作失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    busy.value = ''
  }
}

async function doPing() {
  const target = pingTarget.value.trim()
  if (!target) {
    showToast('red', '请填写要 ping 的地址')
    return
  }
  pinging.value = true
  pingResult.value = null
  try {
    pingResult.value = await ping(target)
  } catch (err) {
    showToast('red', `ping 失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    pinging.value = false
  }
}

onMounted(() => load(false))
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load(true)">重试</button>
  </div>

  <div class="grid">
    <PanelBox
      title="网络接口"
      :sub="`共 ${shown.length} 个`"
      :span="4"
      :loading="loading"
      :foot="false"
    >
      <template #actions>
        <label class="field"><input v-model="showAll" type="checkbox" />全部</label>
        <button class="btn sm" type="button" @click="load(true)">强制刷新</button>
      </template>
      <ul class="ifaces">
        <li
          v-for="i in shown"
          :key="i.name"
          class="iface"
          :class="{ on: i.name === selectedName }"
          @click="select(i)"
        >
          <div class="top">
            <b>{{ i.name }}</b>
            <StatusTag :tone="i.isUp ? 'green' : 'gray'">{{ i.isUp ? 'UP' : 'DOWN' }}</StatusTag>
            <StatusTag v-if="i.dhcp" tone="blue">DHCP</StatusTag>
          </div>
          <div class="ip mono">{{ i.ipv4?.[0]?.ipv4 || '无 IPv4' }}</div>
          <div class="mac mono">{{ i.mac || '—' }}</div>
        </li>
        <li v-if="!shown.length && !loading" class="empty">没有接口。</li>
      </ul>
    </PanelBox>

    <PanelBox
      title="接口配置"
      :sub="selected?.name || '未选择'"
      :span="8"
      :loading="loading"
      foot="改配置不会自动失效后端缓存，保存后页面已强制刷新一次"
    >
      <template #actions>
        <button
          v-if="selected"
          class="btn sm"
          type="button"
          :disabled="busy === selected.name"
          @click="askToggleState"
        >
          {{ selected.isUp ? '禁用接口' : '启用接口' }}
        </button>
      </template>

      <div v-if="!selected" class="empty">左侧选一个接口。</div>
      <template v-else>
        <div class="notice">
          <strong>可能断连</strong>
          <span>　改 IP 或禁用当前正在使用的接口，会让你立刻失去连接。请确认有带外访问途径。</span>
        </div>

        <div class="fields">
          <label class="check">
            <input v-model="edit.dhcp" type="checkbox" />自动获取（DHCP）
          </label>
          <label>IP 地址<input v-model="edit.ipv4" :disabled="edit.dhcp" placeholder="192.168.1.100" /></label>
          <label>子网掩码<input v-model="edit.subnetMask" :disabled="edit.dhcp" placeholder="255.255.255.0" /></label>
          <label>网关<input v-model="edit.gateway" placeholder="192.168.1.1，多个用逗号分隔" /></label>
          <label>DNS<input v-model="edit.dns" placeholder="223.5.5.5，多个用逗号分隔" /></label>
        </div>
        <div v-if="editIssue" class="err">{{ editIssue }}</div>
        <div class="actions">
          <button class="btn primary" type="button" :disabled="busy === selected.name || !!editIssue" @click="askSave">
            {{ busy === selected.name ? '保存中…' : '保存' }}
          </button>
        </div>
      </template>
    </PanelBox>

    <PanelBox title="连通性测试" :span="12" :foot="'数据来源：/api/network/ping'">
      <div class="ping">
        <input v-model="pingTarget" placeholder="IP 或域名，如 223.5.5.5" @keyup.enter="doPing" />
        <button class="btn primary" type="button" :disabled="pinging" @click="doPing">
          {{ pinging ? '测试中…' : 'Ping' }}
        </button>
      </div>
      <div v-if="pingResult" class="ping-result">
        <StatusTag :tone="pingResult.success ? 'green' : 'red'">
          {{ pingResult.success ? '通' : '不通' }}
        </StatusTag>
        <span class="mono">{{ pingResult.target }}</span>
        <span v-if="pingResult.success" class="mono">延迟 {{ pingResult.latency.toFixed(1) }} ms</span>
        <span v-else class="err">{{ pingResult.error || '无响应' }}</span>
      </div>
    </PanelBox>
  </div>

  <div v-if="pendingSave" class="modal-mask" @click.self="pendingSave = false">
    <div class="modal">
      <h3>确认修改网络配置</h3>
      <p class="muted">接口 {{ selected?.name }}。若改的是当前上网口，提交后可能立刻断连。</p>
      <dl>
        <dt>模式</dt><dd>{{ edit.dhcp ? 'DHCP' : '静态' }}</dd>
        <template v-if="!edit.dhcp">
          <dt>IP</dt><dd class="mono">{{ edit.ipv4 }}</dd>
          <dt>掩码</dt><dd class="mono">{{ edit.subnetMask }}</dd>
        </template>
        <dt>网关</dt><dd class="mono">{{ edit.gateway || '—' }}</dd>
        <dt>DNS</dt><dd class="mono">{{ edit.dns || '—' }}</dd>
      </dl>
      <div class="modal-actions">
        <button class="btn" type="button" @click="pendingSave = false">取消</button>
        <button class="btn primary" type="button" @click="confirmSave">确认提交</button>
      </div>
    </div>
  </div>

  <div v-if="pendingDown" class="modal-mask" @click.self="pendingDown = false">
    <div class="modal danger-modal">
      <h3>{{ selected?.isUp ? '禁用' : '启用' }}接口 {{ selected?.name }}</h3>
      <p class="muted">禁用会立刻中断该接口上的所有通信。</p>
      <div class="modal-actions">
        <button class="btn" type="button" @click="pendingDown = false">取消</button>
        <button class="btn danger" type="button" @click="confirmToggleState">确认{{ selected?.isUp ? '禁用' : '启用' }}</button>
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
.ifaces { max-height: 420px; margin: 0; padding: 0; overflow-y: auto; list-style: none; }
.iface { padding: var(--s3); border-bottom: 1px solid var(--divider); cursor: pointer; }
.iface:hover { background: var(--bg-4); }
.iface.on { background: var(--brand-soft); }
.iface .top { display: flex; align-items: center; gap: var(--s2); }
.iface .top b { font-size: var(--fs-13); }
.iface .ip { margin-top: var(--s1); font-size: var(--fs-13); }
.iface .mac { font-size: var(--fs-12); color: var(--text-3); }
.fields { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--s3) var(--s4); }
.fields label { display: flex; flex-direction: column; gap: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.fields label.check { flex-direction: row; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-1); }
.fields input {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.fields input:disabled { background: var(--bg-3); color: var(--text-3); }
.fields input:focus-visible { outline: 2px solid var(--brand-line); outline-offset: 1px; }
.actions { display: flex; justify-content: flex-end; margin-top: var(--s4); }
.ping { display: flex; gap: var(--s3); }
.ping input {
  flex: 1;
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.ping-result { display: flex; align-items: center; gap: var(--s3); margin-top: var(--s3); font-size: var(--fs-13); }
.field { display: inline-flex; align-items: center; gap: var(--s1); font-size: var(--fs-12); color: var(--text-3); }
.mono { font-variant-numeric: tabular-nums; }
.err { margin-top: var(--s2); font-size: var(--fs-12); color: var(--red-ink); }
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
  width: min(480px, 100%);
  padding: var(--s5);
  border-top: 3px solid var(--brand);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.modal.danger-modal { border-top-color: var(--red); }
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.modal dl { display: grid; grid-template-columns: 64px 1fr; gap: var(--s2) var(--s3); margin: var(--s4) 0; font-size: var(--fs-13); }
.modal dt { color: var(--text-3); }
.modal dd { margin: 0; }
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); margin-top: var(--s4); }
.muted { color: var(--text-3); font-size: var(--fs-13); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .fields { grid-template-columns: 1fr; }
}
</style>
