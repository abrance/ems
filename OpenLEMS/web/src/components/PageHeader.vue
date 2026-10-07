<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { findTabByKey } from '@/nav'
import { SCENE, SCENES_PLANNED, useAppStore } from '@/stores/app'

const route = useRoute()
const app = useAppStore()

const tab = computed(() => findTabByKey(String(route.meta.tab ?? '')))
const page = computed(() => tab.value?.pages.find((p) => p.key === String(route.meta.page ?? '')))
const isScene = computed(() => tab.value?.scene === true)

/** 悬停说明：当前只有一支场景，其余各自缺什么数据源 */
const sceneHint = computed(() =>
  [
    `当前数据全部来自广东：gd_metric 的 96 点电价/负荷、广东现货日报、EMS 现场设备、TimesFM 预测。`,
    `规划中的场景（尚无数据源）：`,
    ...SCENES_PLANNED.map((s) => `· ${s.name} —— 需要 ${s.need}`),
  ].join('\n'),
)
</script>

<template>
  <header class="pagehead">
    <div class="headline">
      <div class="crumbs">{{ tab?.title }} / {{ page?.title }}</div>
      <h1>{{ page?.title }}</h1>
    </div>
    <span class="spacer" />
    <template v-if="isScene">
      <!-- 数据场景：只有一支，所以是**标签**不是下拉。
           以前做成可选，但切换不改变任何数据（没有页面按场景过滤），
           另外两个场景也没有数据源 —— 那是假控件。标题里写清要接入什么。 -->
      <span class="scene-label" data-scene :title="sceneHint">数据场景：{{ SCENE }}</span>
      <label class="field">
        运行日
        <input v-model="app.runDate" type="date" />
      </label>
    </template>
  </header>
</template>

<style scoped>
.pagehead {
  flex: 0 0 var(--h-pagehead);
  height: var(--h-pagehead);
  display: flex;
  align-items: center;
  gap: var(--s3);
  padding: 0 var(--s4);
  background: var(--bg-2);
  border-bottom: 1px solid var(--border);
}
.crumbs { font-size: var(--fs-12); color: var(--text-3); }
h1 {
  margin: 0;
  font-size: var(--fs-18);
  font-weight: 600;
  letter-spacing: -0.01em;
}
.spacer { flex: 1; }
.scene-label {
  padding: var(--s1) var(--s3);
  border-radius: var(--r-pill);
  background: var(--bg-3);
  color: var(--text-2);
  font-size: var(--fs-12);
  cursor: help;
}
.field {
  display: inline-flex;
  align-items: center;
  gap: var(--s2);
  font-size: var(--fs-13);
  color: var(--text-3);
}
.field select,
.field input {
  padding: var(--s1) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.field select:focus-visible,
.field input:focus-visible {
  outline: 2px solid var(--brand-line);
  outline-offset: 1px;
  border-color: var(--brand-line);
}
</style>
