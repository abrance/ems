<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import { getDriver, listDrivers, listProtocols, type Driver, type Protocol } from '@/api/protocol'

/**
 * 管理 · 协议与驱动。
 *
 * 页内两个 Tab：
 *   协议配置 —— 通信通道（Modbus TCP / CANbus / GPIO…），按类型筛选
 *   驱动管理 —— 设备型号适配，点「详情」看驱动元信息与点位能力
 *
 * 两个概念要分清：协议是「怎么连」，驱动是「连上之后怎么读」。设备两个都要绑。
 *
 * 本批只做**只读**：创建/编辑/删除协议涉及后台热重载协议服务，属于有副作用的操作，
 * 需要先确认重载语义（是否需要重启采集）再做，不在这里顺手加上。
 */
const tab = ref<'protocol' | 'driver'>('protocol')

const protocols = ref<Protocol[]>([])
const drivers = ref<Driver[]>([])
const typeFilter = ref('')
const loading = ref(false)
const error = ref('')

const driverDetail = ref<Driver | null>(null)
const detailLoading = ref(false)

const protocolTypes = computed(() => [...new Set(protocols.value.map((p) => p.protocolType))].sort())

const shownProtocols = computed(() =>
  typeFilter.value ? protocols.value.filter((p) => p.protocolType === typeFilter.value) : protocols.value,
)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [p, d] = await Promise.all([listProtocols(), listDrivers()])
    protocols.value = p
    drivers.value = d
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function openDriver(d: Driver) {
  detailLoading.value = true
  driverDetail.value = d
  try {
    const full = await getDriver(d.driverName)
    if (full) driverDetail.value = full
  } catch {
    // 详情拿不到就先用列表里的字段，页面不空
  } finally {
    detailLoading.value = false
  }
}

function humanSize(n: number): string {
  if (!n) return '—'
  return n < 1024 ? `${n} B` : `${(n / 1024).toFixed(1)} KB`
}

onMounted(load)
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <div class="grid">
    <PanelBox
      title="协议与驱动"
      :sub="tab === 'protocol' ? `${shownProtocols.length} 个协议` : `${drivers.length} 个驱动`"
      :span="12"
      :loading="loading"
      :foot="tab === 'protocol' ? '协议是「怎么连」；设备绑定协议后由驱动决定「怎么读」' : '驱动按设备型号适配，内置驱动随程序发布'"
    >
      <template #actions>
        <label v-if="tab === 'protocol' && protocolTypes.length" class="field">
          类型
          <select v-model="typeFilter">
            <option value="">全部</option>
            <option v-for="t in protocolTypes" :key="t" :value="t">{{ t }}</option>
          </select>
        </label>
        <button class="btn sm" type="button" @click="load">刷新</button>
      </template>

      <div class="tabs-inline">
        <button type="button" :class="{ on: tab === 'protocol' }" @click="tab = 'protocol'">协议配置</button>
        <button type="button" :class="{ on: tab === 'driver' }" @click="tab = 'driver'">驱动管理</button>
      </div>

      <!-- 协议 -->
      <table v-if="tab === 'protocol'">
        <thead>
          <tr>
            <th>名称</th>
            <th>类型</th>
            <th>地址</th>
            <th class="num">端口</th>
            <th class="num">超时(ms)</th>
            <th>日志级别</th>
            <th>状态</th>
            <th>绑定设备</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in shownProtocols" :key="p.protocolId">
            <td>{{ p.protocolName }}<span class="id">{{ p.protocolId }}</span></td>
            <td><StatusTag :tone="p.protocolType === 'gpio' ? 'purple' : 'blue'">{{ p.protocolType }}</StatusTag></td>
            <td class="mono">{{ p.protocolAddress || '—' }}</td>
            <td class="num">{{ p.protocolPort || '—' }}</td>
            <td class="num">{{ p.protocolTimeout || '—' }}</td>
            <td>{{ p.protocolLogLevel || '—' }}</td>
            <td>
              <StatusTag :tone="p.protocolActive ? 'green' : 'gray'">
                {{ p.protocolActive ? '已激活' : '未激活' }}
              </StatusTag>
            </td>
            <td>{{ p.boundDeviceName || '—' }}</td>
          </tr>
          <tr v-if="!shownProtocols.length && !loading">
            <td colspan="8" class="empty">没有协议配置。</td>
          </tr>
        </tbody>
      </table>

      <!-- 驱动 -->
      <table v-else>
        <thead>
          <tr>
            <th>驱动</th>
            <th>品牌 / 型号</th>
            <th>设备类型</th>
            <th>协议</th>
            <th>版本</th>
            <th>更新</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in drivers" :key="d.driverName">
            <td>{{ d.driverName }}<span class="id">{{ d.driverDescription }}</span></td>
            <td>{{ d.brand || '—' }} / {{ d.model || '—' }}</td>
            <td>{{ d.driverType || '—' }}</td>
            <td><StatusTag tone="blue">{{ d.protocolType || '—' }}</StatusTag></td>
            <td>{{ d.driverVersion || '—' }}</td>
            <td>{{ d.driverLastUpdate || '—' }}</td>
            <td>
              <StatusTag :tone="d.driverStatus ? 'green' : 'gray'">
                {{ d.driverStatus ? '运行中' : '未加载' }}
              </StatusTag>
              <StatusTag v-if="d.driverIsEmbedded" tone="gray">内置</StatusTag>
            </td>
            <td><button class="btn sm" type="button" @click="openDriver(d)">详情</button></td>
          </tr>
          <tr v-if="!drivers.length && !loading">
            <td colspan="8" class="empty">没有驱动。</td>
          </tr>
        </tbody>
      </table>
    </PanelBox>
  </div>

  <div v-if="driverDetail" class="drawer-mask" @click.self="driverDetail = null">
    <aside class="drawer">
      <header class="drawer-head">
        <h3>{{ driverDetail.driverName }}</h3>
        <StatusTag :tone="driverDetail.driverStatus ? 'green' : 'gray'">
          {{ driverDetail.driverStatus ? '运行中' : '未加载' }}
        </StatusTag>
        <span class="spacer" />
        <button class="btn sm" type="button" @click="driverDetail = null">关闭</button>
      </header>
      <div class="drawer-body" :class="{ loading: detailLoading }">
        <div class="rows">
          <div class="row"><span>描述</span><b>{{ driverDetail.driverDescription || '—' }}</b></div>
          <div class="row"><span>设备类型</span><b>{{ driverDetail.driverType || '—' }}</b></div>
          <div class="row"><span>协议类型</span><b>{{ driverDetail.protocolType || '—' }}</b></div>
          <div class="row"><span>品牌 / 型号</span><b>{{ driverDetail.brand || '—' }} / {{ driverDetail.model || '—' }}</b></div>
          <div class="row"><span>版本</span><b>{{ driverDetail.driverVersion || '—' }}</b></div>
          <div class="row"><span>编译时间</span><b>{{ driverDetail.buildTime || '—' }}</b></div>
          <div class="row"><span>提交</span><b class="mono">{{ driverDetail.commitHash || '—' }}</b></div>
          <div class="row"><span>文件</span><b class="mono">{{ driverDetail.driverPath || '—' }}</b></div>
          <div class="row"><span>大小</span><b>{{ humanSize(driverDetail.driverFileSizeByte) }}</b></div>
          <div class="row"><span>哈希</span><b class="mono wrap">{{ driverDetail.driverHash || '—' }}</b></div>
          <div class="row"><span>备注</span><b>{{ driverDetail.remark || '—' }}</b></div>
        </div>
      </div>
    </aside>
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
.id { display: block; color: var(--text-3); font-size: var(--fs-12); }
.empty { padding: var(--s5); color: var(--text-3); text-align: center; }
.field { display: inline-flex; align-items: center; gap: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.field select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-12);
}
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
.btn:hover { background: #e9eaee; }
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
  width: min(560px, 100%);
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
.spacer { flex: 1; }
.drawer-body { flex: 1; padding: var(--s4); overflow-y: auto; }
.rows { display: flex; flex-direction: column; gap: var(--s3); }
.row { display: flex; align-items: baseline; gap: var(--s3); font-size: var(--fs-13); }
.row > span { min-width: 88px; color: var(--text-3); font-size: var(--fs-12); }
.row b { font-weight: 600; }
.mono { font-variant-numeric: tabular-nums; }
.wrap { word-break: break-all; white-space: normal; }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
