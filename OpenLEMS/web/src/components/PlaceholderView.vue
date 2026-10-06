<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { findPageByPath } from '@/nav'
import PanelBox from './PanelBox.vue'

/**
 * 未实现页面的统一占位。
 * 不画假图表、不编假数字：直接说明为什么是空的、以及这页将包含哪些面板。
 * 结构来自 nav，所以占位页本身也不会跑偏。
 */
const route = useRoute()
const current = computed(() => findPageByPath(route.path)?.page)

const title =
  computed(() =>
    current.value?.status === 'todo' ? '本期不做' : '本期做，界面待实现',
  )
</script>

<template>
  <div class="notice">
    <strong>{{ title }}</strong>
    <span v-if="current?.note">　{{ current.note }}</span>
  </div>
  <div class="grid">
    <PanelBox
      v-for="(name, i) in current?.panels ?? []"
      :key="name"
      :title="name"
      :span="(current?.panels?.length ?? 1) > 3 && i < 2 ? 6 : 12"
      :foot="false"
    >
      <div class="pending">待实现</div>
    </PanelBox>
    <PanelBox v-if="!current?.panels?.length" title="待定义" :foot="false">
      <div class="pending">该页结构尚未确定</div>
    </PanelBox>
  </div>
</template>

<style scoped>
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
.grid {
  display: grid;
  grid-template-columns: repeat(12, 1fr);
  gap: var(--s4);
}
.pending {
  display: grid;
  place-items: center;
  min-height: 96px;
  color: var(--text-3);
  font-size: var(--fs-13);
}
@media (max-width: 1280px) {
  .grid { grid-template-columns: repeat(6, 1fr); }
}
</style>
