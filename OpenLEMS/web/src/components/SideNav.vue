<script setup lang="ts">
import { computed } from 'vue'
import { findTabByKey } from '@/nav'

const props = defineProps<{ tabKey: string; pageKey: string; collapsed: boolean }>()
const tab = computed(() => findTabByKey(props.tabKey))
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="side-group">{{ tab?.title }}</div>
    <template v-for="page in tab?.pages ?? []" :key="page.key">
      <!-- paused = 已停用：只展示，不渲染成链接，避免点进去加载一个不再维护的页面 -->
      <span
        v-if="page.status === 'paused'"
        class="side-item is-paused"
        :class="{ active: page.key === pageKey }"
        :title="`${page.title}（未开放）`"
        aria-disabled="true"
        data-paused="true"
      >
        <span class="dot" />
        <span class="side-label">{{ page.title }}</span>
        <span class="badge">未开放</span>
      </span>
      <RouterLink
        v-else
        class="side-item"
        :class="{ active: page.key === pageKey, 'is-todo': page.status === 'todo' }"
        :title="page.title"
        :to="page.path"
      >
        <span class="dot" />
        <span class="side-label">{{ page.title }}</span>
        <span v-if="page.status === 'todo'" class="badge">TODO</span>
      </RouterLink>
    </template>
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
/* 未开放：保留同样的排版与层级，但没有 hover、没有指针、点了也不跳转 */
.side-item.is-paused { color: var(--text-3); cursor: default; }
.side-item.is-paused:hover { background: transparent; }
.side-item.is-paused.active { color: var(--brand-ink); }
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
