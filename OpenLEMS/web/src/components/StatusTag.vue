<script setup lang="ts">
import { computed } from 'vue'

/**
 * 全站统一状态徽标。语义色一律「浅底 + 深字」，不用饱和底配白字（对比过不了）。
 * 现网是 Arco 的 CSS 变量体系，这里保持同样的视觉语言。
 */
const props = withDefaults(
  defineProps<{
    tone?: 'gray' | 'brand' | 'green' | 'red' | 'blue' | 'purple'
  }>(),
  { tone: 'gray' },
)

const style = computed(() => ({
  '--tag-bg': `var(--${props.tone === 'brand' ? 'brand' : props.tone}-soft)`,
  '--tag-fg': `var(--${props.tone === 'brand' ? 'brand' : props.tone}-ink)`,
}))
</script>

<template>
  <span class="status-tag" :style="style"><slot /></span>
</template>

<style scoped>
.status-tag {
  display: inline-block;
  padding: 1px var(--s2);
  border-radius: var(--r-pill);
  font-size: var(--fs-12);
  line-height: 18px;
  background: var(--tag-bg);
  color: var(--tag-fg);
  white-space: nowrap;
}
</style>
