<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import { DATASETS, defaultParams, findDataset, type DatasetResult } from '@/exports/datasets'
import { buildCsv, downloadCsv, safeFilename, stamp } from '@/utils/csv'

/**
 * 管理 · 报表导出（基础版）。
 *
 * 与设计稿的差距要说清：设计稿写的是「报表模板 + 异步生成任务 + 历史文件」，
 * 那需要后端任务队列与文件存储。这里做的是**基础版**：挑一个数据集、给参数、
 * 预览、下载 CSV —— 数据在浏览器里本来就有（各页用的同一批接口），
 * 前端拼 CSV 即可，不绕后端。
 *
 * 代价是受浏览器内存限制，所以每个数据集都有区间上限（见 exports/datasets.ts）。
 */
const activeKey = ref(DATASETS[0].key)
const dataset = computed(() => findDataset(activeKey.value) ?? DATASETS[0])
const params = ref<Record<string, string>>(defaultParams(DATASETS[0]))

const loading = ref(false)
const error = ref('')
const result = ref<DatasetResult | null>(null)

/** 预览行数：整张表可能有几万行，DOM 只渲染前若干行 */
const PREVIEW_ROWS = 20

const csv = computed(() => (result.value ? buildCsv(result.value.columns, result.value.rows) : ''))
const previewRows = computed(() => (result.value ? result.value.rows.slice(0, PREVIEW_ROWS) : []))
const csvHead = computed(() => csv.value.split('\r\n').slice(0, 4).join('\n'))

function fileName(): string {
  const parts = [dataset.value.title, result.value?.suffix, stamp()].filter(Boolean)
  return `${safeFilename(parts.join('-'))}.csv`
}

function reset() {
  result.value = null
  error.value = ''
}

watch(activeKey, () => {
  params.value = defaultParams(dataset.value)
  reset()
})

async function generate() {
  loading.value = true
  error.value = ''
  result.value = null
  try {
    result.value = await dataset.value.load(params.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '生成失败'
  } finally {
    loading.value = false
  }
}

function download() {
  if (!result.value) return
  downloadCsv(fileName(), csv.value)
}

onMounted(reset)
</script>

<template>
  <div class="grid">
    <PanelBox
      title="数据集"
      sub="选一个数据集，填参数，生成预览后下载 CSV"
      :span="12"
      :foot="'数据来自下列接口：' + dataset.source"
    >
      <div class="ds-list">
        <button
          v-for="d in DATASETS"
          :key="d.key"
          type="button"
          class="ds"
          :class="{ on: d.key === activeKey }"
          @click="activeKey = d.key"
        >
          <strong>{{ d.title }}</strong>
          <small>{{ d.desc }}</small>
        </button>
      </div>

      <div class="toolbar">
        <label v-for="p in dataset.params" :key="p.key" class="field">
          {{ p.label }}
          <select v-if="p.type === 'select'" v-model="params[p.key]">
            <option v-for="o in p.options" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
          <input
            v-else-if="p.type === 'number'"
            v-model="params[p.key]"
            type="number"
            :min="p.min"
            :max="p.max"
          />
          <input v-else v-model="params[p.key]" type="date" />
          <small v-if="p.hint">{{ p.hint }}</small>
        </label>
        <span class="spacer" />
        <button class="btn primary" type="button" :disabled="loading" @click="generate">
          {{ loading ? '生成中…' : '生成预览' }}
        </button>
        <button class="btn" type="button" :disabled="!result" @click="download">下载 CSV</button>
      </div>
    </PanelBox>

    <PanelBox
      title="预览"
      :sub="result ? `${result.rows.length} 行 × ${result.columns.length} 列` : '尚未生成'"
      :span="12"
      :loading="loading"
      :foot="result ? `下载文件名：${fileName()}（UTF-8 带 BOM，Excel 直接打开不乱码）` : '生成后这里显示前 20 行'"
    >
      <div v-if="error" class="alert error">{{ error }}</div>

      <div v-if="result && !result.rows.length" class="empty">该参数下没有数据，换个区间再试。</div>

      <div v-if="result && result.rows.length" class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th v-for="c in result.columns" :key="c">{{ c }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in previewRows" :key="i">
              <td v-for="(cell, j) in row" :key="j">{{ cell }}</td>
            </tr>
          </tbody>
        </table>
        <p v-if="result.rows.length > PREVIEW_ROWS" class="more">
          仅预览前 {{ PREVIEW_ROWS }} 行，共 {{ result.rows.length }} 行（下载的是全部）。
        </p>
      </div>

      <div v-if="result && result.rows.length" class="raw">
        <div class="raw-title">CSV 开头（原文，含转义）</div>
        <pre>{{ csvHead }}</pre>
      </div>
    </PanelBox>

    <PanelBox
      title="没做的部分"
      sub="设计稿写的是异步任务 + 文件历史；这里是基础版，差距列清楚"
      :span="12"
      foot="不假装有能力：下面每条都能在代码里对上"
    >
      <ul class="notes">
        <li><strong>没有报表模板与生成任务</strong>：设计稿想的是「选模板 → 后端异步生成 → 完成后通知 → 文件保留期」。本页是即时前端生成，没有任务、没有历史文件、没有保留期。</li>
        <li><strong>数据量受浏览器内存限制</strong>：因此每个数据集都有区间上限（电网日指标 400 天、现货日报 31 天）。要导更长的区间得改成后端生成。</li>
        <li><strong>现货日报是一天一次请求</strong>：接口按运行日取数，没有区间批量接口，所以区间上限压到 31 天。</li>
        <li><strong>CSV 只有一张表</strong>：不做多 sheet、不做 Excel 样式、不打包 zip。需要多表请分次导出。</li>
        <li><strong>导的是当前数据</strong>：人工确认前的日报指标会带 <code>origin=cv</code> 之类的来源标记，导出去以后没人替你分辨 —— 所以来源/状态列必须一起导。</li>
      </ul>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.ds-list { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s3); }
.ds {
  display: flex;
  flex-direction: column;
  gap: var(--s1);
  padding: var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.ds:hover { border-color: var(--brand); }
.ds.on { border-color: var(--brand); background: var(--brand-soft); }
.ds strong { font-size: var(--fs-13); }
.ds.on strong { color: var(--brand-ink); }
.ds small { font-size: var(--fs-12); color: var(--text-3); line-height: 1.6; }
.toolbar { display: flex; align-items: flex-start; gap: var(--s3); flex-wrap: wrap; margin-top: var(--s4); }
.spacer { flex: 1; }
.field { display: flex; flex-direction: column; gap: var(--s1); font-size: var(--fs-12); color: var(--text-3); }
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
.field input[type='date'] { width: 140px; }
.field input[type='number'] { width: 100px; }
.alert { margin-bottom: var(--s3); padding: var(--s2) var(--s3); border-radius: var(--r-sm); font-size: var(--fs-13); }
.alert.error { border-left: 3px solid var(--red); background: var(--red-soft); color: var(--red-ink); }
.table-wrap { overflow-x: auto; border: 1px solid var(--border); border-radius: var(--r-sm); }
.table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
.table th,
.table td {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--divider);
  text-align: left;
  white-space: nowrap;
}
.table th { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); font-weight: 500; }
.table td { color: var(--text-1); font-variant-numeric: tabular-nums; }
.more { margin: var(--s2) var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.raw { margin-top: var(--s3); }
.raw-title { font-size: var(--fs-12); color: var(--text-3); margin-bottom: var(--s1); }
.raw pre {
  margin: 0;
  padding: var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-1);
  color: var(--text-2);
  font-size: var(--fs-12);
  line-height: 1.7;
  overflow-x: auto;
}
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.notes { margin: 0; padding-left: var(--s5); font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
.notes code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
.btn {
  padding: var(--s2) var(--s4);
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
  .ds-list { grid-template-columns: repeat(2, 1fr); }
}
</style>
