<script setup lang="ts">
defineProps<{
  label: string
  value: string | number
  unit?: string
  hint?: string
  /** 环比方向：涨（红）/ 跌（绿），不传则中性 */
  dir?: 'up' | 'down'
  /** 角标：例如历史存档视图上的「历史」，提醒这不是当前数据 */
  badge?: string
}>()
</script>

<template>
  <div class="kpi">
    <div class="label">
      {{ label }}<span v-if="badge" class="badge">{{ badge }}</span>
    </div>
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
.badge {
  margin-left: var(--s2);
  padding: 0 var(--s2);
  border-radius: var(--r-pill);
  background: var(--gray-soft);
  color: var(--gray-ink);
  font-size: var(--fs-12);
}
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
