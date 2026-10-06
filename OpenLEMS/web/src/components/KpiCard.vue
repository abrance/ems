<script setup lang="ts">
defineProps<{
  label: string
  value: string | number
  unit?: string
  hint?: string
  /** 环比方向：涨（红）/ 跌（绿），不传则中性 */
  dir?: 'up' | 'down'
}>()
</script>

<template>
  <div class="kpi">
    <div class="label">{{ label }}</div>
    <div class="value" :class="dir">
      {{ value }}<small v-if="unit">{{ unit }}</small>
    </div>
    <div v-if="hint" class="hint">{{ hint }}</div>
  </div>
</template>

<style scoped>
.kpi {
  display: flex;
  flex-direction: column;
  gap: var(--s1);
  min-width: 0;
  padding: var(--s3) var(--s4);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.label { font-size: var(--fs-12); color: var(--text-3); }
.value {
  font-size: var(--fs-22);
  font-weight: 700;
  line-height: 1.25;
  font-variant-numeric: tabular-nums;
}
.value small {
  margin-left: var(--s1);
  font-size: var(--fs-12);
  font-weight: 500;
  color: var(--text-3);
}
.value.up { color: var(--red-ink); }
.value.down { color: var(--green-ink); }
.hint { font-size: var(--fs-12); color: var(--text-3); }
</style>
