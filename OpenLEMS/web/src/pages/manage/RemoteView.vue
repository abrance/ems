<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  fetchModbusStatus,
  fetchMqttStatus,
  reloadModbus,
  reloadMqtt,
  setModbusEnabled,
  setMqttEnabled,
  type ModbusStatus,
  type MqttStatus,
} from '@/api/remote'

/**
 * 管理 · 远程调度。
 *
 * 两个方向容易混，页面上写清楚：
 *   Modbus —— EMS 是**服务端**（监听端口，上层来读）
 *   MQTT   —— EMS 是**客户端**（连 broker，主动推送）
 *
 * 开关与重载都会实际启停服务。开关切换前二次确认；重载是让服务重读配置。
 * 改配置后要重载才生效 —— 这个语义写在按钮旁，不让人猜。
 */
const tab = ref<'modbus' | 'mqtt'>('modbus')

const modbus = ref<ModbusStatus | null>(null)
const mqtt = ref<MqttStatus | null>(null)
const loading = ref(false)
const error = ref('')
const busy = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const pendingToggle = ref<{ kind: 'modbus' | 'mqtt'; next: boolean } | null>(null)

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [m, q] = await Promise.all([
      fetchModbusStatus().catch(() => null),
      fetchMqttStatus().catch(() => null),
    ])
    modbus.value = m
    mqtt.value = q
    if (!m && !q) error.value = '两个远程服务都拿不到状态'
  } finally {
    loading.value = false
  }
}

function askToggle(kind: 'modbus' | 'mqtt', next: boolean) {
  pendingToggle.value = { kind, next }
}

async function confirmToggle() {
  const task = pendingToggle.value
  if (!task) return
  pendingToggle.value = null
  busy.value = task.kind
  try {
    if (task.kind === 'modbus') await setModbusEnabled(task.next)
    else await setMqttEnabled(task.next)
    showToast('green', task.next ? '服务已开启' : '服务已关闭')
    await load()
  } catch (err) {
    showToast('red', `操作失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    busy.value = ''
  }
}

async function doReload(kind: 'modbus' | 'mqtt') {
  busy.value = `${kind}-reload`
  try {
    if (kind === 'modbus') await reloadModbus()
    else await reloadMqtt()
    showToast('green', '已触发重载，服务会重读配置')
    await load()
  } catch (err) {
    showToast('red', `重载失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    busy.value = ''
  }
}

const modbusOnline = computed(() => modbus.value?.deviceStatus?.filter((d) => d.isOnline).length ?? 0)
const mqttConnected = computed(() => mqtt.value?.clientStatus?.filter((c) => c.isConnected).length ?? 0)

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
      title="服务状态"
      :span="12"
      :loading="loading"
      foot="Modbus：EMS 作服务端，上层来读 · MQTT：EMS 作客户端，主动推送"
    >
      <template #actions>
        <button class="btn sm" type="button" @click="load">刷新</button>
      </template>
      <div class="kpis">
        <KpiCard
          label="Modbus 服务"
          :value="modbus?.isRunning ? '运行中' : '已停止'"
          :hint="modbus ? `监听 ${modbus.listenPort} · 连接 ${modbus.connectionCount}` : '状态未知'"
        />
        <KpiCard
          label="Modbus 从站"
          :value="modbus ? `${modbusOnline}/${modbus.deviceCount}` : '—'"
          hint="在线 / 总数"
        />
        <KpiCard
          label="MQTT 服务"
          :value="mqtt?.isRunning ? '运行中' : '已停止'"
          :hint="mqtt ? `客户端配置 ${mqtt.clientCount} 份` : '状态未知'"
        />
        <KpiCard
          label="MQTT 连接"
          :value="mqtt ? `${mqttConnected}/${mqtt.clientCount}` : '—'"
          hint="已连接 / 总数"
        />
      </div>
    </PanelBox>

    <PanelBox title="远程调度" :span="12" :loading="loading" :foot="false">
      <template #actions>
        <StatusTag v-if="modbus?.isRunning" tone="green">Modbus 运行中</StatusTag>
        <StatusTag v-else tone="gray">Modbus 已停止</StatusTag>
        <StatusTag v-if="mqtt?.isRunning" tone="green">MQTT 运行中</StatusTag>
        <StatusTag v-else tone="gray">MQTT 已停止</StatusTag>
      </template>

      <div class="tabs-inline">
        <button type="button" :class="{ on: tab === 'modbus' }" @click="tab = 'modbus'">Modbus</button>
        <button type="button" :class="{ on: tab === 'mqtt' }" @click="tab = 'mqtt'">MQTT</button>
      </div>

      <!-- Modbus -->
      <template v-if="tab === 'modbus'">
        <div class="toolbar">
          <span class="muted">改配置后需重载才生效。</span>
          <span class="spacer" />
          <button class="btn" type="button" :disabled="busy === 'modbus-reload'" @click="doReload('modbus')">
            {{ busy === 'modbus-reload' ? '重载中…' : '重载配置' }}
          </button>
          <button
            class="btn"
            type="button"
            :disabled="busy === 'modbus'"
            @click="askToggle('modbus', !modbus?.isRunning)"
          >
            {{ modbus?.isRunning ? '停止服务' : '启动服务' }}
          </button>
        </div>

        <table v-if="modbus?.deviceStatus?.length">
          <thead>
            <tr>
              <th>设备</th>
              <th class="num">从站 ID</th>
              <th class="num">起始地址</th>
              <th>在线</th>
              <th>最后更新</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in modbus.deviceStatus" :key="d.deviceId">
              <td>{{ d.deviceId }}</td>
              <td class="num">{{ d.modbusId }}</td>
              <td class="num">0x{{ d.startAddr.toString(16).toUpperCase() }}</td>
              <td>
                <StatusTag :tone="d.isOnline ? 'green' : 'gray'">
                  {{ d.isOnline ? '在线' : '离线' }}
                </StatusTag>
              </td>
              <td>{{ d.lastUpdateTime || '—' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty">没有挂载到 Modbus 的设备。在「设备管理」里给设备绑定协议后会出现。</div>
      </template>

      <!-- MQTT -->
      <template v-else>
        <div class="toolbar">
          <span class="muted">改配置后需重载才生效。</span>
          <span class="spacer" />
          <button class="btn" type="button" :disabled="busy === 'mqtt-reload'" @click="doReload('mqtt')">
            {{ busy === 'mqtt-reload' ? '重载中…' : '重载配置' }}
          </button>
          <button
            class="btn"
            type="button"
            :disabled="busy === 'mqtt'"
            @click="askToggle('mqtt', !mqtt?.isRunning)"
          >
            {{ mqtt?.isRunning ? '停止服务' : '启动服务' }}
          </button>
        </div>

        <table v-if="mqtt?.clientStatus?.length">
          <thead>
            <tr>
              <th>客户端 ID</th>
              <th>系统序列号</th>
              <th>Topic</th>
              <th>连接</th>
              <th>运行</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(c, i) in mqtt.clientStatus" :key="c.clientId || i">
              <td>{{ c.clientId || '—' }}</td>
              <td class="mono">{{ c.systemNumber || '—' }}</td>
              <td class="mono">{{ c.topic || '—' }}</td>
              <td>
                <StatusTag :tone="c.isConnected ? 'green' : 'red'">
                  {{ c.isConnected ? '已连接' : '未连接' }}
                </StatusTag>
              </td>
              <td>
                <StatusTag :tone="c.isRunning ? 'green' : 'gray'">
                  {{ c.isRunning ? '运行中' : '已停止' }}
                </StatusTag>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="empty">没有 MQTT 客户端配置。</div>
      </template>
    </PanelBox>
  </div>

  <div v-if="pendingToggle" class="modal-mask" @click.self="pendingToggle = null">
    <div class="modal">
      <h3>{{ pendingToggle.next ? '启动' : '停止' }}{{ pendingToggle.kind === 'modbus' ? ' Modbus ' : ' MQTT ' }}服务</h3>
      <p class="muted">
        该操作会实际启停远程服务：停止后上层平台将读不到数据（Modbus）或收不到推送（MQTT）。
      </p>
      <div class="modal-actions">
        <button class="btn" type="button" @click="pendingToggle = null">取消</button>
        <button class="btn primary" type="button" @click="confirmToggle">
          确认{{ pendingToggle.next ? '启动' : '停止' }}
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
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
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
.toolbar { display: flex; align-items: center; gap: var(--s2); margin-bottom: var(--s3); }
.spacer { flex: 1; }
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
td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); white-space: nowrap; }
th.num, td.num { text-align: right; }
.mono { font-variant-numeric: tabular-nums; }
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
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); margin-top: var(--s4); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(2, 1fr); }
}
</style>
