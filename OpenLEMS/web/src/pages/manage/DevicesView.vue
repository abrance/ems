<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  fetchDetailedList,
  fetchPointDefinitions,
  setDeviceEnabled,
  type DeviceDetailedInfo,
  type PointDefinitions,
} from '@/api/device'

/**
 * 管理 · 设备管理。
 *
 * 左树右表：树由 /device/detailed-list 的扁平列表按 pid 组装（一次请求拿全信息），
 * 右侧是该设备的详情与点表（/device/{id}/points/definitions）。
 *
 * 本期设备接入只做 mock（emsdevice），所以页面不做「接入向导」—— 那是新增能力，
 * 需要先定驱动选择与点位映射的交互，不在本批范围。
 */
const devices = ref<DeviceDetailedInfo[]>([])
const selectedId = ref('')
const defs = ref<PointDefinitions | null>(null)
const loading = ref(false)
const defsLoading = ref(false)
const error = ref('')
const busy = ref('')
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

/** 扁平列表 → 树。孤儿节点（pid 指向不存在的设备）挂到根上，不丢数据 */
interface TreeNode {
  device: DeviceDetailedInfo
  children: TreeNode[]
  depth: number
}

const tree = computed<TreeNode[]>(() => {
  const byId = new Map<string, TreeNode>()
  for (const d of devices.value) byId.set(d.id, { device: d, children: [], depth: 0 })
  const roots: TreeNode[] = []
  for (const node of byId.values()) {
    const parent = node.device.pid && byId.get(node.device.pid)
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  const sortRec = (nodes: TreeNode[], depth: number) => {
    nodes.sort((a, b) => (a.device.sort ?? 0) - (b.device.sort ?? 0) || a.device.name.localeCompare(b.device.name, 'zh-CN'))
    for (const n of nodes) {
      n.depth = depth
      sortRec(n.children, depth + 1)
    }
  }
  sortRec(roots, 0)
  return roots
})

function flatten(nodes: TreeNode[], out: TreeNode[] = []): TreeNode[] {
  for (const n of nodes) {
    out.push(n)
    flatten(n.children, out)
  }
  return out
}

const flatTree = computed(() => flatten(tree.value))
const selected = computed(() => devices.value.find((d) => d.id === selectedId.value) ?? null)

/** 点表按分组切分；没有 groupKey 的归到「未分组」 */
const fieldGroups = computed(() => {
  const fields = defs.value?.fields ?? []
  const groups = defs.value?.groups ?? []
  const out: { key: string; name: string; fields: typeof fields }[] = []
  for (const g of groups) {
    const inGroup = fields.filter((f) => f.groupKey === g.key)
    if (inGroup.length) out.push({ key: g.key, name: g.name || g.key, fields: inGroup })
  }
  const loose = fields.filter((f) => !f.groupKey || !groups.some((g) => g.key === f.groupKey))
  if (loose.length) out.push({ key: '__none__', name: '未分组', fields: loose })
  return out
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    devices.value = await fetchDetailedList()
    if (!selectedId.value && devices.value.length) {
      await select(devices.value[0].id)
    } else if (selectedId.value) {
      await select(selectedId.value)
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function select(id: string) {
  selectedId.value = id
  defsLoading.value = true
  defs.value = null
  try {
    defs.value = await fetchPointDefinitions(id)
  } catch {
    // 点表拿不到不影响看设备详情，降级为空并靠页面提示说明
    defs.value = { isVirtualDevice: false, groups: [], fields: [] }
  } finally {
    defsLoading.value = false
  }
}

async function toggleEnabled(d: DeviceDetailedInfo) {
  busy.value = d.id
  try {
    await setDeviceEnabled(d.id, !d.enabled)
    showToast('green', d.enabled ? `已停用「${d.name}」` : `已启用「${d.name}」`)
    await load()
  } catch (err) {
    showToast('red', `操作失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    busy.value = ''
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

  <div class="grid">
    <PanelBox title="设备树" :sub="`共 ${devices.length} 台`" :span="4" :loading="loading" :foot="false">
      <template #actions>
        <button class="btn sm" type="button" @click="load">刷新</button>
      </template>
      <ul class="tree">
        <li
          v-for="n in flatTree"
          :key="n.device.id"
          class="node"
          :class="{ on: n.device.id === selectedId, off: !n.device.enabled }"
          :style="{ paddingLeft: `${n.depth * 16 + 8}px` }"
          @click="select(n.device.id)"
        >
          <span class="name">{{ n.device.name }}</span>
          <StatusTag v-if="!n.device.enabled" tone="gray">停用</StatusTag>
          <StatusTag v-else-if="n.device.failedMessage" tone="red">异常</StatusTag>
        </li>
        <li v-if="!devices.length && !loading" class="empty">没有设备。本期只接 emsdevice mock。</li>
      </ul>
    </PanelBox>

    <PanelBox
      title="设备详情"
      :sub="selected?.id || '未选择'"
      :span="8"
      :loading="loading"
      :foot="selected?.failedMessage ? `初始化失败：${selected.failedMessage}` : '数据来源：/api/device/detailed-list'"
    >
      <template #actions>
        <button
          v-if="selected"
          class="btn sm"
          type="button"
          :disabled="busy === selected.id"
          @click="toggleEnabled(selected)"
        >
          {{ selected.enabled ? '停用' : '启用' }}
        </button>
      </template>

      <div v-if="!selected" class="empty">左侧选一台设备。</div>
      <template v-else>
        <div class="rows">
          <div class="row"><span>设备 ID</span><b>{{ selected.id }}</b></div>
          <div class="row"><span>父设备</span><b>{{ selected.pid || '（根）' }}</b></div>
          <div class="row"><span>设备类型</span><b>{{ selected.deviceType || '—' }}<em v-if="selected.isVirtualDevice">虚拟设备</em></b></div>
          <div class="row">
            <span>驱动</span>
            <b>
              {{ selected.driver || '—' }}
              <em v-if="selected.driverBrand || selected.driverModel">
                {{ selected.driverBrand }} {{ selected.driverModel }} v{{ selected.driverVersion }}
              </em>
            </b>
          </div>
          <div class="row">
            <span>协议</span>
            <b>
              {{ selected.protocolName || '—' }}
              <em>{{ selected.protocolType }} {{ selected.protocolAddress }}</em>
            </b>
          </div>
          <div class="row">
            <span>存储</span>
            <b>
              <StatusTag :tone="selected.storageEnable ? 'green' : 'gray'">
                {{ selected.storageEnable ? '开启' : '关闭' }}
              </StatusTag>
              <em>间隔 {{ selected.storageIntervalSec }} 秒</em>
            </b>
          </div>
          <div class="row">
            <span>调试模式</span>
            <b>
              <StatusTag :tone="selected.enableDebug ? 'brand' : 'gray'">
                {{ selected.enableDebug ? '开启' : '关闭' }}
              </StatusTag>
            </b>
          </div>
          <div class="row">
            <span>设备参数</span>
            <b class="mono">{{ selected.params && Object.keys(selected.params).length ? JSON.stringify(selected.params) : '—' }}</b>
          </div>
        </div>
      </template>
    </PanelBox>

    <PanelBox
      title="点表"
      :sub="selected ? `${defs?.fields?.length ?? 0} 个点位` : ''"
      :span="12"
      :loading="defsLoading"
      foot="数据来源：/api/device/{deviceId}/points/definitions"
    >
      <div v-if="!selected" class="empty">左侧选一台设备。</div>
      <div v-else-if="!defs?.fields?.length" class="empty">
        该设备没有点位定义（可能是虚拟设备，或驱动未声明点位）。
      </div>
      <section v-for="g in fieldGroups" :key="g.key" class="group">
        <h4>{{ g.name }}<span class="count">{{ g.fields.length }}</span></h4>
        <table>
          <thead>
            <tr>
              <th>点位</th>
              <th>Key</th>
              <th>类型</th>
              <th>组件</th>
              <th class="num">范围</th>
              <th>单位</th>
              <th>默认</th>
              <th>说明</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="f in g.fields" :key="f.key">
              <td>{{ f.name || f.key }}<span v-if="f.required" class="req">*</span></td>
              <td class="mono">{{ f.key }}</td>
              <td>{{ f.valueType }}</td>
              <td>{{ f.componentType }}</td>
              <td class="num">{{ f.min ?? '—' }} ~ {{ f.max ?? '—' }}</td>
              <td>{{ f.unit || '—' }}</td>
              <td>{{ f.default ?? '—' }}</td>
              <td class="desc">{{ f.description || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </section>
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
  padding: 0 var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.tree { max-height: 480px; margin: 0; padding: 0; overflow-y: auto; list-style: none; }
.node {
  display: flex;
  align-items: center;
  gap: var(--s2);
  padding: var(--s2) var(--s3);
  border-radius: var(--r-sm);
  font-size: var(--fs-13);
  cursor: pointer;
}
.node:hover { background: var(--bg-3); }
.node.on { background: var(--brand-soft); color: var(--brand-ink); font-weight: 600; }
.node.off .name { color: var(--text-3); }
.rows { display: flex; flex-direction: column; gap: var(--s3); }
.row { display: flex; align-items: baseline; gap: var(--s3); font-size: var(--fs-13); }
.row > span { min-width: 88px; color: var(--text-3); font-size: var(--fs-12); }
.row b { display: flex; align-items: center; gap: var(--s2); font-weight: 600; }
.row em { color: var(--text-3); font-style: normal; font-size: var(--fs-12); font-weight: 400; }
.group { margin-bottom: var(--s5); }
.group h4 { display: flex; align-items: center; gap: var(--s2); margin: 0 0 var(--s2); font-size: var(--fs-14); }
.count { padding: 0 var(--s2); border-radius: var(--r-pill); background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
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
td.mono { font-variant-numeric: tabular-nums; }
td.desc { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-3); }
.req { margin-left: var(--s1); color: var(--red-ink); }
.empty { padding: var(--s4); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.btn {
  padding: 0 var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.btn:disabled { opacity: 0.5; cursor: default; }
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
