<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import { NAV } from '@/nav'
import { useAppStore, type Density, type Theme } from '@/stores/app'

/**
 * 配置 · 界面与语言。
 *
 * 主题与密度是**纯前端**偏好，存 localStorage，立即生效 —— 不需要后端接口。
 * 语言目前没有 i18n 基建（文案全写在组件里），所以这一项明确标为待做，
 * 不做假的下拉框。
 */
const app = useAppStore()

const themes: { v: Theme; t: string; hint: string }[] = [
  { v: 'light', t: '浅色', hint: '默认' },
  { v: 'dark', t: '深色', hint: '暗光环境' },
]
const densities: { v: Density; t: string; hint: string }[] = [
  { v: 'standard', t: '标准', hint: '默认间距' },
  { v: 'compact', t: '紧凑', hint: '只压间距，不动字号' },
]

const allPages = computed(() => NAV.flatMap((tab) => tab.pages.map((p) => ({ ...p, tab: tab.title }))))

const preview = ref('')
watch(
  () => app.landing,
  (v) => {
    preview.value = v ? (allPages.value.find((p) => p.path === v)?.title ?? v) : '（未设置，用导航第一项）'
  },
  { immediate: true },
)
</script>

<template>
  <div class="grid">
    <PanelBox title="主题" :span="6" foot="生效语义：立即生效，存 localStorage，不影响服务端">
      <div class="choices">
        <button
          v-for="t in themes"
          :key="t.v"
          class="choice"
          :class="{ on: app.theme === t.v }"
          type="button"
          @click="app.theme = t.v"
        >
          <span class="swatch" :class="t.v" />
          <b>{{ t.t }}</b>
          <em>{{ t.hint }}</em>
        </button>
      </div>
      <div class="hint">
        深色主题的对比度与浅色按同一标准算过（正文 ≥ 4.5:1）；语义色在深色下是「深底 + 亮字」，
        与浅色的「浅底 + 深字」对偶，状态标签会自动跟随。
      </div>
    </PanelBox>

    <PanelBox title="信息密度" :span="6" foot="生效语义：立即生效；只压缩间距，字号不变">
      <div class="choices">
        <button
          v-for="d in densities"
          :key="d.v"
          class="choice"
          :class="{ on: app.density === d.v }"
          type="button"
          @click="app.density = d.v"
        >
          <b>{{ d.t }}</b>
          <em>{{ d.hint }}</em>
        </button>
      </div>
      <div class="hint">
        紧凑模式把间距阶梯整体缩小（4/8/12/16/24 → 2/6/8/12/16），不缩字号 ——
        压字号会牺牲可读性，得不偿失。
      </div>
    </PanelBox>

    <PanelBox title="默认首页" :span="6" foot="生效语义：下次打开根路径时生效">
      <label class="field">
        落地页
        <select v-model="app.landing">
          <option value="">（未设置，用导航第一项）</option>
          <option v-for="p in allPages" :key="p.key" :value="p.path">
            {{ p.tab }} / {{ p.title }}
          </option>
        </select>
      </label>
      <div class="hint">当前：{{ preview }}</div>
    </PanelBox>

    <PanelBox title="语言" :span="6" foot="待做：需要先接 i18n 基建">
      <div class="pending">
        <StatusTag tone="brand">本期不做</StatusTag>
        <span>
          目前文案直接写在组件里，没有 i18n 资源层。加语言切换不仅仅是加一个下拉框 ——
          要把所有文案抽成 key、补英文资源、并处理日期/数字格式。等真有第二语言需求再做。
        </span>
      </div>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.choices { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--s3); }
.choice {
  display: flex;
  flex-direction: column;
  gap: var(--s1);
  align-items: flex-start;
  padding: var(--s3) var(--s4);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.choice.on { border-color: var(--brand); background: var(--brand-soft); }
.choice b { font-size: var(--fs-14); }
.choice em { color: var(--text-3); font-size: var(--fs-12); font-style: normal; }
.swatch {
  width: 100%;
  height: 36px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.swatch.light { background: linear-gradient(90deg, #f7f8fa 50%, #fff 50%); }
.swatch.dark { background: linear-gradient(90deg, #17171a 50%, #232324 50%); }
.hint { margin-top: var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.field { display: flex; flex-direction: column; gap: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.field select {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.pending { display: flex; align-items: flex-start; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
