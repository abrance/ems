<script setup lang="ts">
import { onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchCredentials, SOURCE_LABEL, type CredentialsStatus, type CredentialItem } from '@/api/credentials'

/**
 * 配置 · 采集与凭据。
 *
 * **只读**：密钥在服务端配置文件 / 环境变量里，改后要重启，做成网页表单反而不安全也不实用。
 * 页面回答三件事：走七牛还是本地、哪几项没配、各项从哪来（环境变量 / 配置文件）。
 * 密钥值后端打码返回，前端也不展示。
 */
const status = ref<CredentialsStatus | null>(null)
const loading = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    status.value = await fetchCredentials()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
    status.value = null
  } finally {
    loading.value = false
  }
}

function toneOf(it: CredentialItem): 'green' | 'gray' {
  return it.configured ? 'green' : 'gray'
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
      title="凭据状态"
      :sub="status ? `存储方式：${status.storageMode === 'qiniu' ? '七牛云' : '本地磁盘'}` : ''"
      :span="12"
      :loading="loading"
      :foot="status?.note || '数据来源：/api/system/credentials'"
    >
      <template #actions>
        <button class="btn sm" type="button" :disabled="loading" @click="load">刷新</button>
      </template>

      <div class="notice">
        <strong>这里不能改</strong>
        <span>
          　密钥保存在服务端配置文件或环境变量里，改动需要重启服务。页面只读，
          避免把长期密钥变成随手可改的网页表单；密钥值也一律不回显。
        </span>
      </div>

      <section class="group">
        <h4>图片存储（七牛）</h4>
        <table>
          <thead>
            <tr><th>配置项</th><th>状态</th><th>来源</th><th>值</th></tr>
          </thead>
          <tbody>
            <tr v-for="it in status?.qiniu ?? []" :key="it.name">
              <td>{{ it.name }}</td>
              <td>
                <StatusTag :tone="toneOf(it)">{{ it.configured ? '已配置' : '未配置' }}</StatusTag>
              </td>
              <td>{{ SOURCE_LABEL[it.source] ?? it.source }}</td>
              <td class="mono">{{ it.value || '—' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="status && status.storageMode !== 'qiniu'" class="tip">
          当前存储方式为<strong>本地磁盘</strong>：七牛密钥不全时会退回本地，日报图片存
          <code>/uploads</code>。要切到七牛，补齐上表中未配置的项后重启。
        </div>
      </section>

      <section class="group">
        <h4>OCR 服务</h4>
        <table>
          <thead>
            <tr><th>配置项</th><th>状态</th><th>来源</th><th>值</th></tr>
          </thead>
          <tbody>
            <tr v-for="it in status?.ocr ?? []" :key="it.name">
              <td>{{ it.name }}</td>
              <td>
                <StatusTag :tone="toneOf(it)">{{ it.configured ? '已配置' : '未配置' }}</StatusTag>
              </td>
              <td>{{ SOURCE_LABEL[it.source] ?? it.source }}</td>
              <td class="mono">{{ it.value || '—' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="status && !status.ocr.every((i) => i.configured)" class="tip">
          未配置 OCR 时，日报可以正常采集归档，但<strong>不能解析成结构化数据</strong> ——
          「管理 · 采集管理」里的解析会报错。
        </div>
      </section>
    </PanelBox>
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
.group { margin-bottom: var(--s5); }
.group h4 { margin: 0 0 var(--s2); font-size: var(--fs-14); }
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--border);
  color: var(--text-3);
  font-size: var(--fs-12);
  font-weight: 500;
  text-align: left;
}
td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); }
td.mono { font-variant-numeric: tabular-nums; }
.tip {
  margin-top: var(--s3);
  padding: var(--s2) var(--s3);
  border-radius: var(--r-sm);
  background: var(--bg-4);
  color: var(--text-2);
  font-size: var(--fs-12);
}
.tip code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
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
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
