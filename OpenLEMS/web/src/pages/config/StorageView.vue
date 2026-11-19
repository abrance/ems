<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import {
  cleanRetentionNow,
  fetchRetention,
  fetchStorageStats,
  humanBytes,
  updateRetention,
  type RetentionStatus,
  type StorageStats,
} from '@/api/system'

/**
 * 配置 · 存储管理。
 *
 * 两件事放在一页：
 *   1. 存储占用（/system/storage/stats）
 *   2. 保留策略（/system/storage-time）—— 后端把它挂在 system 下，
 *      所以不再单开「数据保留」页，避免同一功能两个入口。
 *
 * 生效语义（已实现）：后端有定时清理任务（默认每 24 小时一次，启动后 5 分钟首跑），
 *   按三条设置删除历史数据；也可以在这里点「立即清理」。上次清理的时间/结果都显示出来 ——
 *   保留策略最怕的是「设了但没人跑」，所以必须能看见它跑过。
 *
 *   两条要知道的边界：
 *   1. **电价历史不会被删**。电价存在系统类里（id=price），清理系统类时显式排除它 ——
 *      长周期电价预测 / 预测复盘 / 策略反演依赖好几年的历史价格。
 *   2. 协议指标算设备侧数据，跟「设备数据保留」走。
 */
const stats = ref<StorageStats | null>(null)
const loading = ref(false)
const error = ref('')
const saving = ref(false)
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const form = reactive({ device: 30, system: 30, log: 90 })
const retention = ref<RetentionStatus | null>(null)
const cleaning = ref(false)

async function loadRetention() {
  try {
    retention.value = await fetchRetention()
    // 设置值以保留策略接口为准（存储统计里的 retention_hours 是存储层自己的统一值）
    form.device = retention.value.deviceRetentionDays || form.device
    form.system = retention.value.systemRetentionDays || form.system
    form.log = retention.value.logRetentionDays || form.log
  } catch {
    retention.value = null
  }
}

async function cleanNow() {
  cleaning.value = true
  try {
    retention.value = await cleanRetentionNow()
    showToast('green', `已清理：日志文件删除 ${retention.value.lastLogFilesDeleted} 个（${humanBytes(retention.value.lastLogBytesFreed)}），耗时 ${retention.value.lastRunTimeMs}ms`)
    await load()
  } catch (err) {
    showToast('red', `清理失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    cleaning.value = false
  }
}
const pendingConfirm = ref(false)

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    stats.value = await fetchStorageStats()
    await loadRetention()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
    stats.value = null
  } finally {
    loading.value = false
  }
}

const retentionIssue = computed(() => {
  for (const [label, v] of [
    ['设备数据', form.device],
    ['系统数据', form.system],
    ['日志', form.log],
  ] as const) {
    if (!Number.isFinite(Number(v)) || Number(v) < 1) return `${label}保留天数必须大于 0`
  }
  return ''
})

function askSave() {
  if (retentionIssue.value) {
    showToast('red', retentionIssue.value)
    return
  }
  // 改前看 diff（设计稿 §11.4 规矩 2）
  pendingConfirm.value = true
}

async function confirmSave() {
  pendingConfirm.value = false
  saving.value = true
  try {
    await updateRetention({
      deviceRetentionDays: Number(form.device),
      systemRetentionDays: Number(form.system),
      logRetentionDays: Number(form.log),
    })
    showToast('green', '已保存；立即清理或等定时任务（间隔见下方）即可生效')
    await load()
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

  <div class="grid">
    <PanelBox
      title="存储占用"
      :span="12"
      :loading="loading"
      :foot="stats ? `数据区间 ${stats.oldest_timestamp} → ${stats.newest_timestamp} · 平均序列 ${humanBytes(stats.avg_series_size)} · 写入速率 ${stats.samples_per_second.toFixed(1)} 样本/秒` : '数据来源：/api/system/storage/stats'"
    >
      <template #actions>
        <button class="btn sm" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
      <div class="kpis">
        <KpiCard label="时序条数" :value="stats?.total_series ?? '—'" hint="总时间序列数" />
        <KpiCard label="样本数" :value="stats?.total_samples?.toLocaleString() ?? '—'" hint="总样本量" />
        <KpiCard label="占用空间" :value="stats ? humanBytes(stats.storage_size) : '—'" :hint="stats ? `${stats.storage_size_mb.toFixed(1)} MB` : ''" />
        <KpiCard label="保留时长" :value="stats ? (stats.retention_hours / 24).toFixed(0) : '—'" unit="天" :hint="`约 ${stats?.retention_hours?.toFixed(0) ?? '—'} 小时`" />
      </div>
    </PanelBox>

    <PanelBox
      title="保留策略"
      :span="12"
      :loading="loading"
      foot="定时清理 + 手动清理都会真正删数据；上次清理结果见上方状态"
    >
      <div class="retention-state" :class="{ bad: !!retention?.lastError }">
        <template v-if="retention?.lastRunAt">
          <strong>上次清理 {{ retention.lastRunAt }}</strong>
          <span>
            　删除日志文件 {{ retention.lastLogFilesDeleted }} 个（{{ humanBytes(retention.lastLogBytesFreed) }}）· 耗时 {{ retention.lastRunTimeMs }}ms ·
            截止：设备 {{ retention.lastDeviceBefore }} / 系统 {{ retention.lastSystemBefore }} / 日志 {{ retention.lastLogBefore }}
          </span>
          <span v-if="retention.lastError" class="err">　⚠️ {{ retention.lastError }}</span>
        </template>
        <template v-else>
          <strong>还没有执行过清理</strong>
          <span>　自动清理每 {{ retention?.intervalHours ?? 24 }} 小时一次（服务启动 5 分钟后首跑），也可以现在手动跑一次。</span>
        </template>
      </div>
      <p class="note-line">
        清理按上面的天数删除历史数据。<strong>电价历史不会被删</strong> —— 它存在系统类里但被显式排除，
        长周期电价预测 / 预测复盘 / 策略反演依赖它。协议指标算设备侧数据，跟「设备数据保留」走。
      </p>
      <div class="fields">
        <label>设备数据保留（天）<input v-model.number="form.device" type="number" min="1" /></label>
        <label>系统数据保留（天）<input v-model.number="form.system" type="number" min="1" /></label>
        <label>日志保留（天）<input v-model.number="form.log" type="number" min="1" /></label>
      </div>
      <div class="actions">
        <span v-if="retentionIssue" class="err">{{ retentionIssue }}</span>
        <span class="spacer" />
        <button class="btn" type="button" :disabled="cleaning" @click="cleanNow">
          {{ cleaning ? '清理中…' : '立即清理' }}
        </button>
        <button class="btn primary" type="button" :disabled="saving" @click="askSave">
          {{ saving ? '保存中…' : '保存' }}
        </button>
      </div>
    </PanelBox>
  </div>

  <div v-if="pendingConfirm" class="modal-mask" @click.self="pendingConfirm = false">
    <div class="modal">
      <h3>确认修改保留策略</h3>
      <p class="muted">保存后由存储服务在下次清理时采纳，期间不会立刻删除既有数据。</p>
      <dl>
        <dt>设备数据</dt><dd>{{ form.device }} 天</dd>
        <dt>系统数据</dt><dd>{{ form.system }} 天</dd>
        <dt>日志</dt><dd>{{ form.log }} 天</dd>
      </dl>
      <div class="modal-actions">
        <button class="btn" type="button" @click="pendingConfirm = false">取消</button>
        <button class="btn primary" type="button" @click="confirmSave">确认保存</button>
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
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.retention-state {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--s1);
  margin-bottom: var(--s2);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--green);
  border-radius: var(--r-sm);
  background: var(--green-soft);
  color: var(--green-ink);
  font-size: var(--fs-12);
}
.retention-state.bad { border-left-color: var(--red); background: var(--red-soft); color: var(--red-ink); }
.retention-state .err { color: var(--red-ink); }
.note-line {
  margin: 0 0 var(--s3);
  font-size: var(--fs-12);
  color: var(--text-3);
  line-height: 1.8;
}
.fields { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--s4); }
.fields label { display: flex; flex-direction: column; gap: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.fields input {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.actions { display: flex; align-items: center; gap: var(--s2); margin-top: var(--s4); }
.spacer { flex: 1; }
.err { font-size: var(--fs-12); color: var(--red-ink); }
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
  width: min(440px, 100%);
  padding: var(--s5);
  border-top: 3px solid var(--brand);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.modal dl { display: grid; grid-template-columns: 88px 1fr; gap: var(--s2) var(--s3); margin: var(--s4) 0; font-size: var(--fs-13); }
.modal dt { color: var(--text-3); }
.modal dd { margin: 0; font-variant-numeric: tabular-nums; }
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); }
.muted { color: var(--text-3); font-size: var(--fs-13); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(2, 1fr); }
  .fields { grid-template-columns: 1fr; }
}
</style>
