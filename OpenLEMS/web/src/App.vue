<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from './layouts/AppShell.vue'

const route = useRoute()
/** 登录页没有 Tab / 侧栏 / 顶栏，套壳层会让它看起来像「应用的一部分」 */
const bare = computed(() => route.meta.public === true)
</script>

<template>
  <RouterView v-if="bare" />
  <AppShell v-else />
</template>
