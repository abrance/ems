<script setup lang="ts">
import { NAV } from '@/nav'
defineProps<{ active: string }>()
</script>

<template>
  <nav class="tabs">
    <RouterLink
      v-for="tab in NAV"
      :key="tab.key"
      class="tab"
      :class="{ active: tab.key === active }"
      :to="tab.pages[0].path"
    >
      {{ tab.title }}
    </RouterLink>
  </nav>
</template>

<style scoped>
.tabs {
  flex: 0 0 var(--h-tabs);
  height: var(--h-tabs);
  display: flex;
  align-items: stretch;
  gap: var(--s1);
  padding: 0 var(--s3);
  background: var(--bg-2);
  border-bottom: 1px solid var(--border);
  overflow-x: auto;
  white-space: nowrap;
  z-index: 20;
}
.tab {
  position: relative;
  display: flex;
  align-items: center;
  padding: 0 var(--s5);
  font-size: var(--fs-14);
  color: var(--text-2);
}
.tab:hover { color: var(--text-1); }
.tab.active { color: var(--brand-ink); font-weight: 600; }
.tab.active::after {
  content: '';
  position: absolute;
  left: var(--s5);
  right: var(--s5);
  bottom: 0;
  height: 2px;
  border-radius: var(--r-xs);
  background: var(--brand);
}
</style>
