<script setup lang="ts">
import { computed } from 'vue'
import { findTabByKey } from '@/nav'

const props = defineProps<{ tabKey: string; pageKey: string; collapsed: boolean }>()
const tab = computed(() => findTabByKey(props.tabKey))
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="side-group">{{ tab?.title }}</div>
    <RouterLink
      v-for="page in tab?.pages ?? []"
      :key="page.key"
      class="side-item"
      :class="{ active: page.key === pageKey, 'is-todo': page.status === 'todo' }"
      :title="page.title"
      :to="page.path"
    >
      <span class="dot" />
      <span class="side-label">{{ page.title }}</span>
      <span v-if="page.status === 'todo' || page.status === 'paused'" class="badge">
        {{ page.status === 'paused' ? '暂停' : 'TODO' }}
      </span>
    </RouterLink>
  </aside>
</template>

<style scoped>
.sidebar {
  flex: 0 0 var(--w-side);
  width: var(--w-side);
  padding: var(--s3) var(--s2) var(--s5);
  background: var(--bg-2);
  border-right: 1px solid var(--border);
  overflow-y: auto;
  transition: width 0.15s;
}
.sidebar.collapsed { flex-basis: var(--w-side-rail); width: var(--w-side-rail); }
.side-group {
  padding: var(--s4) var(--s3) var(--s2);
  font-size: var(--fs-12);
  letter-spacing: 0.12em;
  color: var(--text-3);
}
.side-item {
  display: flex;
  align-items: center;
  gap: var(--s2);
  padding: var(--s2) var(--s3);
  border-radius: var(--r-sm);
  color: var(--text-2);
  font-size: var(--fs-13);
  white-space: nowrap;
}
.side-item:hover { background: var(--bg-3); }
.side-item.active { background: var(--brand-soft); color: var(--brand-ink); font-weight: 600; }
.side-item.is-todo { color: var(--text-3); }
.side-item.is-todo.active { color: var(--brand-ink); }
.dot {
  width: 6px;
  height: 6px;
  flex: 0 0 auto;
  border-radius: var(--r-pill);
  background: currentColor;
  opacity: 0.45;
}
.badge {
  margin-left: auto;
  padding: 1px var(--s2);
  border-radius: var(--r-pill);
  background: var(--bg-3);
  color: var(--text-3);
  font-size: var(--fs-12);
}
.sidebar.collapsed .side-label,
.sidebar.collapsed .side-group,
.sidebar.collapsed .badge { display: none; }
.sidebar.collapsed .side-item { justify-content: center; padding: var(--s2) 0; }
</style>
