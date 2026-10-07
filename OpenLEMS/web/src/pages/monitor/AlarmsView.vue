<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  ALARM_LEVELS,
  ALARM_LEVEL_TAG,
  clearAlarmHistory,
  clearCurrentAlarms,
  fetchCurrentAlarms,
  fetchHistoryAlarms,
  fetchIgnoreAlarms,
  ignoreAlarm,
  unignoreAlarm,
  type CurrentAlarm,
  type HistoryAlarm,
  type IgnoreAlarm,
} from '@/api/alarm'
import { fetchDeviceNames } from '@/api/device'
import { session } from '@/auth/session'

/**
 * 运行监控 · 告警中心。
 *
 * 后端一直有完整的告警能力（当前 / 历史 / 忽略规则 / 清除），但前端此前没有任何入口 ——
 * 这一页是「补上缺失的界面」，不是把别处的东西搬过来。
 *
 * 三条数据的关系要说清（否则「忽略」很容易被当成「清除」）：
 *   当前告警  —— 设备此刻仍处于告警状态的点位
 *   历史告警  —— 触发过并已恢复的记录（带触发/清除时间）
 *   忽略规则  —— 命中 (设备, 点位) 的告警不再进入「当前告警」，但**不影响历史**
 */
type Tab = 'current' | 'history' | 'ignore'

const tab = ref<Tab>('current')
const loading = ref(false)
const error = ref('')
const ok = ref('')

const current = ref<CurrentAlarm[]>([])
const history = ref<HistoryAlarm[]>([])
const ignores = ref<IgnoreAlarm[]>([])
const totals = ref({ current: 0, history: 0, ignored: 0 })

const level = ref('')
const date = ref('')
const deviceId = ref('')
const deviceNames = ref<Record<string, string>>({})

const isAdmin = computed(() => session.role === 'admin')

const deviceOptions = computed(() =>
  Object.entries(deviceNames.value).map(([id, name]) => ({ id, name })),
)

async function loadNames() {
  try {
    // 告警自带设备名，这里只是给筛选下拉用；拿不到不影响主体
    deviceNames.value = await fetchDeviceNames(['ess', 'pcs', 'bms', 'ammeter', 'pv', 'load', 'ca', 'fire', 'hum'])
  } catch {
    deviceNames.value = {}
  }
}

async function loadCurrent() {
  const res = await fetchCurrentAlarms({ deviceId: deviceId.value, level: level.value, pageSize: 100 })
  current.value = res.items
  totals.value = { current: res.total, history: res.historyTotal, ignored: res.ignoreTotal }
}

async function loadHistory() {
  const res = await fetchHistoryAlarms({ deviceId: deviceId.value, level: level.value, date: date.value, pageSize: 100 })
  history.value = res.items
  totals.value = { ...totals.value, history: res.total }
}

async function loadIgnores() {
  const res = await fetchIgnoreAlarms({ deviceId: deviceId.value, pageSize: 100 })
  ignores.value = res.items
  totals.value = { ...totals.value, ignored: res.total }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    await Promise.all([loadCurrent(), loadHistory(), loadIgnores()])
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function levelTag(lv: string) {
  return ALARM_LEVEL_TAG[lv] ?? { label: lv || '—', tone: 'gray' as const }
}

async function doIgnore(row: CurrentAlarm) {
  error.value = ''
  ok.value = ''
  try {
    await ignoreAlarm({
      deviceId: row.deviceId,
      sourceDeviceId: row.sourceDeviceId,
      point: row.point,
      pointName: row.pointName,
    })
    ok.value = `已忽略「${row.deviceName} · ${row.pointName || row.point}」；它不再出现在当前告警里，历史记录不受影响。`
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '忽略失败'
  }
}

async function doUnignore(row: IgnoreAlarm) {
  error.value = ''
  ok.value = ''
  try {
    await unignoreAlarm({ deviceId: row.deviceId, point: row.point })
    ok.value = `已恢复「${row.deviceName} · ${row.pointName || row.point}」的告警。`
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '恢复失败'
  }
}

async function doClear(which: 'current' | 'history') {
  error.value = ''
  ok.value = ''
  try {
    if (which === 'current') await clearCurrentAlarms()
    else await clearAlarmHistory()
    ok.value = which === 'current' ? '已清空当前告警。' : '已清空历史告警。'
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '清除失败'
  }
}

/* 级别分布只统计数字，不画图：KPI 已经够表达「现在有多严重」，多一张图为它多引一个图表实例不划算 */
const levelCounts = computed(() => {
  const m: Record<string, number> = {}
  for (const a of current.value) m[a.level] = (m[a.level] ?? 0) + 1
  return m
})

onMounted(async () => {
  await loadNames()
  await load()
})

watch([level, date, deviceId], load)
watch(tab, load)
</script>

<template>
  <div class="grid">
    <PanelBox
      title="告警概览"
      sub="当前告警 = 此刻仍未恢复的点位"
      :span="12"
      :loading="loading"
      foot="忽略规则只影响「当前告警」，不影响历史记录"
    >
      <div class="kpis">
        <KpiCard label="当前告警" :value="totals.current" unit="条" hint="此刻仍未恢复" />
        <KpiCard label="历史告警" :value="totals.history" unit="条" hint="触发过并已恢复" />
        <KpiCard label="已忽略" :value="totals.ignored" unit="条" hint="命中忽略规则的当前告警" />
        <KpiCard
          label="故障级"
          :value="levelCounts.Error ?? 0"
          unit="条"
          :dir="(levelCounts.Error ?? 0) > 0 ? 'up' : undefined"
          hint="级别为 Error 的当前告警"
        />
      </div>
    </PanelBox>

    <PanelBox title="筛选" :span="12" :foot="false">
      <div class="toolbar">
        <span class="radio-group">
          <button type="button" :class="{ on: tab === 'current' }" @click="tab = 'current'">当前告警</button>
          <button type="button" :class="{ on: tab === 'history' }" @click="tab = 'history'">历史告警</button>
          <button type="button" :class="{ on: tab === 'ignore' }" @click="tab = 'ignore'">忽略规则</button>
        </span>
        <label class="field">
          级别
          <select v-model="level">
            <option v-for="l in ALARM_LEVELS" :key="l.value" :value="l.value">{{ l.label }}</option>
          </select>
        </label>
        <label class="field">
          设备
          <select v-model="deviceId">
            <option value="">全部设备</option>
            <option v-for="d in deviceOptions" :key="d.id" :value="d.id">{{ d.name }}</option>
          </select>
        </label>
        <label v-if="tab === 'history'" class="field">
          日期
          <input v-model="date" type="date" />
        </label>
        <span class="spacer" />
        <button class="btn" type="button" :disabled="!isAdmin" @click="doClear('current')">清空当前</button>
        <button class="btn" type="button" :disabled="!isAdmin" @click="doClear('history')">清空历史</button>
        <button class="btn primary" type="button" :disabled="loading" @click="load">刷新</button>
      </div>
      <p v-if="!isAdmin" class="note-line">当前账号不是管理员：清除操作会返回 403，按钮已禁用。</p>
      <div v-if="error" class="alert error">{{ error }}</div>
      <div v-if="ok" class="alert ok">{{ ok }}</div>
    </PanelBox>

    <PanelBox
      :title="tab === 'current' ? '当前告警' : tab === 'history' ? '历史告警' : '忽略规则'"
      :sub="tab === 'current' ? `${current.length} 条` : tab === 'history' ? `${history.length} 条` : `${ignores.length} 条`"
      :span="12"
      :loading="loading"
    >
      <div v-if="tab === 'current' && current.length" class="table">
        <div class="th">级别</div>
        <div class="th">设备</div>
        <div class="th">点位</div>
        <div class="th">详情</div>
        <div class="th">时间</div>
        <div class="th">操作</div>
        <template v-for="(a, i) in current" :key="`${a.deviceId}-${a.point}-${i}`">
          <div><StatusTag :tone="levelTag(a.level).tone">{{ levelTag(a.level).label }}</StatusTag></div>
          <div>
            {{ a.deviceName || a.deviceId }}
            <small v-if="a.sourceDeviceName && a.sourceDeviceName !== a.deviceName" class="muted">
              （源：{{ a.sourceDeviceName }}）
            </small>
          </div>
          <div>{{ a.pointName || a.point }}</div>
          <div class="detail">{{ a.detail }}</div>
          <div class="muted">{{ a.createdAt ?? '—' }}</div>
          <div><button class="link" type="button" @click="doIgnore(a)">忽略</button></div>
        </template>
      </div>

      <div v-else-if="tab === 'current'" class="empty">当前没有告警。</div>

      <div v-if="tab === 'history' && history.length" class="table">
        <div class="th">级别</div>
        <div class="th">设备</div>
        <div class="th">点位</div>
        <div class="th">详情</div>
        <div class="th">触发</div>
        <div class="th">清除</div>
        <template v-for="(a, i) in history" :key="`${a.id}-${i}`">
          <div><StatusTag :tone="levelTag(a.level).tone">{{ levelTag(a.level).label }}</StatusTag></div>
          <div>{{ a.deviceName || a.deviceId }}</div>
          <div>{{ a.pointName || a.point }}</div>
          <div class="detail">{{ a.detail }}</div>
          <div class="muted">{{ a.triggerAt ?? '—' }}</div>
          <div class="muted">{{ a.clearAt ?? '未恢复' }}</div>
        </template>
      </div>
      <div v-else-if="tab === 'history'" class="empty">该筛选下没有历史告警。</div>

      <div v-if="tab === 'ignore' && ignores.length" class="table ignore-table">
        <div class="th">设备</div>
        <div class="th">点位</div>
        <div class="th">来源设备</div>
        <div class="th">创建时间</div>
        <div class="th">操作</div>
        <template v-for="(a, i) in ignores" :key="`${a.id}-${i}`">
          <div>{{ a.deviceName || a.deviceId }}</div>
          <div>{{ a.pointName || a.point }}</div>
          <div class="muted">{{ a.sourceDeviceName || a.sourceDeviceId || '—' }}</div>
          <div class="muted">{{ a.createdAt ?? '—' }}</div>
          <div><button class="link" type="button" @click="doUnignore(a)">恢复告警</button></div>
        </template>
      </div>
      <div v-else-if="tab === 'ignore'" class="empty">没有忽略规则。</div>
    </PanelBox>

    <PanelBox title="口径说明" :span="12" foot="改动这里的判断前，先看 api/alarm.ts 的注释">
      <ul class="notes">
        <li><strong>当前 / 历史</strong>：当前是「仍未恢复」，历史是「触发过并已恢复」，历史记录带触发与清除时间。</li>
        <li><strong>忽略 ≠ 清除</strong>：忽略是一条 <code>(设备, 点位)</code> 规则，命中的告警不再进入当前告警；历史记录照样保留。清除是把告警记录删掉（管理员）。</li>
        <li><strong>告警来源可以是别的设备</strong>：储能柜的告警往往来自 PCS / BMS / 电表，所以每行都带「源设备」。</li>
        <li><strong>级别</strong>：Warn / Alarm / Error 三档（另有 None 表示正常），与日志级别不是一套。</li>
      </ul>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.toolbar { display: flex; align-items: center; gap: var(--s3); flex-wrap: wrap; }
.spacer { flex: 1; }
.radio-group { display: inline-flex; padding: 2px; border-radius: var(--r-sm); background: var(--bg-3); }
.radio-group button {
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-xs);
  background: transparent;
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.radio-group button.on { background: var(--bg-2); color: var(--brand-ink); font-weight: 600; }
.field { display: inline-flex; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
.field input,
.field select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.alert { margin-top: var(--s3); padding: var(--s2) var(--s3); border-radius: var(--r-sm); font-size: var(--fs-13); }
.alert.error { border-left: 3px solid var(--red); background: var(--red-soft); color: var(--red-ink); }
.alert.ok { border-left: 3px solid var(--green); background: var(--green-soft); color: var(--green-ink); }
.note-line { margin: var(--s3) 0 0; font-size: var(--fs-12); color: var(--text-3); }
.table {
  display: grid;
  grid-template-columns: 80px 1.2fr 1.2fr 2fr 1.4fr 90px;
  gap: 1px;
  background: var(--border);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
  font-size: var(--fs-13);
}
.table.ignore-table { grid-template-columns: 1.2fr 1.2fr 1.2fr 1.4fr 110px; }
.table > div { padding: var(--s2) var(--s3); background: var(--bg-2); min-width: 0; }
.table .th { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.table .detail { color: var(--text-2); overflow-wrap: anywhere; }
.table .muted { color: var(--text-3); font-size: var(--fs-12); }
.link {
  border: 0;
  background: transparent;
  color: var(--brand-ink);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
  padding: 0;
}
.link:hover { text-decoration: underline; }
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.notes { margin: 0; padding-left: var(--s5); font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
.notes code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
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
.btn:disabled { opacity: 0.5; cursor: default; }
.btn.primary { background: var(--brand-btn); color: #fff; }
.btn.primary:hover:not(:disabled) { background: var(--brand-btn-h); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(2, 1fr); }
  .table, .table.ignore-table { grid-template-columns: 1fr 1fr; }
}
</style>
