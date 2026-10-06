<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppBar from '@/components/AppBar.vue'
import TabBar from '@/components/TabBar.vue'
import SideNav from '@/components/SideNav.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useAppStore } from '@/stores/app'
import { setUnauthorizedHandler } from '@/api/client'

/**
 * 本期没有登录页：会话要么来自 AUTH_BYPASS_ROLE 注入，要么来自旧 SPA 留下的 localStorage。
 * 一旦后端没开绕过，所有接口 401，界面会全空 —— 这里把原因显式说出来。
 */
const unauthorized = ref(false)
onMounted(() => setUnauthorizedHandler(() => (unauthorized.value = true)))
onBeforeUnmount(() => setUnauthorizedHandler(null))

const route = useRoute()
const app = useAppStore()

const tabKey = computed(() => String(route.meta.tab ?? 'scene'))
const pageKey = computed(() => String(route.meta.page ?? ''))
</script>

<template>
  <div class="shell">
    <AppBar :collapsed="app.sideCollapsed" @toggle-side="app.toggleSide()" />
    <TabBar :active="tabKey" />
    <div class="body">
      <SideNav :tab-key="tabKey" :page-key="pageKey" :collapsed="app.sideCollapsed" />
      <main class="main">
        <PageHeader />
        <div v-if="unauthorized" class="auth-warn">
          接口返回 401：当前没有有效会话。后端需设置 <code>AUTH_BYPASS_ROLE</code>
          （admin / user），或由已登录的旧界面写入会话后再打开本页。
        </div>
        <div class="content">
          <RouterView />
        </div>
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.body { flex: 1; display: flex; min-height: 0; }
.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.content {
  flex: 1;
  padding: var(--s4);
  overflow-y: auto;
}
.auth-warn {
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--red);
  background: var(--red-soft);
  color: var(--red-ink);
  font-size: var(--fs-13);
}
.auth-warn code {
  padding: 0 var(--s1);
  border-radius: var(--r-xs);
  background: rgb(255 255 255 / 60%);
}
</style>
