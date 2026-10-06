<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import { fetchStorageStats, humanBytes, updateRetention, type StorageStats } from '@/api/system'

/**
 * 配置 · 存储管理。
 *
 * 两件事放在一页：
 *   1. 存储占用（/system/storage/stats）
 *   2. 保留策略（/system/storage-time）—— 后端把它挂在 system 下，
 *      所以不再单开「数据保留」页，避免同一功能两个入口。
 *
 * 生效语义：保留天数改动由存储服务在下次清理时采纳；这里只提交，不假装立刻删数据。
 */
const stats = ref<StorageStats | null>(null)
const loading = ref(false)
const error = ref('')
const saving = ref(false)
const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

const form = reactive({ device: 30, system: 30, log: 90 })
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
    if (stats.value?.retention_hours) {
      // 存储层只回一个统一保留时长，按天展示；三类各自的设置以表单为准
      const days = Math.max(1, Math.round(stats.value.retention_hours / 24))
      form.device = form.system = form.log = days
    }
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
    showToast('green', '已保存；保留策略在存储服务下次清理时生效')
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
      foot="生效语义：下次清理任务生效（不是立刻删数据）"
    >
      <div class="fields">
        <label>设备数据保留（天）<input v-model.number="form.device" type="number" min="1" /></label>
        <label>系统数据保留（天）<input v-model.number="form.system" type="number" min="1" /></label>
        <label>日志保留（天）<input v-model.number="form.log" type="number" min="1" /></label>
      </div>
      <div class="actions">
        <span v-if="retentionIssue" class="err">{{ retentionIssue }}</span>
        <span class="spacer" />
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
