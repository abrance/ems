<script setup lang="ts">
/**
 * 顶栏只放身份与入口：Logo、站点、用户。
 * 不放流量 / CPU / 时钟这类实时指标（属于「配置 · 系统信息」页）—— 顶栏越素，主区越显眼。
 */
const props = defineProps<{ collapsed: boolean }>()
const emit = defineEmits<{ (e: 'toggle-side'): void }>()
</script>

<template>
  <header class="appbar">
    <div class="brand"><span class="mark">EMS</span>能源管理系统</div>
    <button class="site-switch" type="button">站点：储能柜示范站</button>
    <span class="spacer" />
    <button
      class="icon-btn"
      type="button"
      :title="props.collapsed ? '展开侧栏' : '收起侧栏'"
      @click="emit('toggle-side')"
    >
      ☰
    </button>
    <button class="user-menu" type="button"><span class="avatar">管</span>admin</button>
  </header>
</template>

<style scoped>
.appbar {
  flex: 0 0 var(--h-appbar);
  height: var(--h-appbar);
  display: flex;
  align-items: center;
  gap: var(--s4);
  padding: 0 var(--s4);
  background: var(--bg-2);
  border-bottom: 1px solid var(--border);
  z-index: 30;
}
.brand { display: flex; align-items: center; gap: var(--s2); font-size: var(--fs-16); font-weight: 700; }
.brand .mark {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  background: var(--brand-btn);
  color: #fff;
  font-size: var(--fs-12);
  font-weight: 800;
}
.site-switch {
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.site-switch:hover { background: #e9eaee; }
.spacer { flex: 1; }
.icon-btn {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  cursor: pointer;
}
.icon-btn:hover { background: #e9eaee; }
.user-menu {
  display: flex;
  align-items: center;
  gap: var(--s2);
  padding: var(--s1) var(--s3) var(--s1) var(--s1);
  border: 0;
  border-radius: var(--r-pill);
  background: var(--bg-3);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.user-menu:hover { background: #e9eaee; }
.avatar {
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  border-radius: var(--r-pill);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-12);
  font-weight: 700;
}
</style>
