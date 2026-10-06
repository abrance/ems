<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { findTabByKey } from '@/nav'
import { useAppStore, SCENES } from '@/stores/app'

const route = useRoute()
const app = useAppStore()

const tab = computed(() => findTabByKey(String(route.meta.tab ?? '')))
const page = computed(() => tab.value?.pages.find((p) => p.key === String(route.meta.page ?? '')))
const isScene = computed(() => tab.value?.scene === true)
</script>

<template>
  <header class="pagehead">
    <div class="headline">
      <div class="crumbs">{{ tab?.title }} / {{ page?.title }}</div>
      <h1>{{ page?.title }}</h1>
    </div>
    <span class="spacer" />
    <template v-if="isScene">
      <label class="field">
        场景
        <select v-model="app.scene">
          <option v-for="s in SCENES" :key="s" :value="s">{{ s }}</option>
        </select>
      </label>
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
