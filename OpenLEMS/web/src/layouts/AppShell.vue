<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppBar from '@/components/AppBar.vue'
import TabBar from '@/components/TabBar.vue'
import SideNav from '@/components/SideNav.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useAppStore } from '@/stores/app'

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
</style>
