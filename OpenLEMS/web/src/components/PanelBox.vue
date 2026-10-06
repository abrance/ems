<script setup lang="ts">
/**
 * 面板四件套：head（左侧 3px 主色竖条 + 标题 + 副标题 + 操作）
 *               body（slot）
 *               foot（数据来源 / 口径 / 更新时间，默认占位）
 * 口径脚注是本产品的硬要求：所有对外数字必须能回答「哪来的、怎么算的」。
 */
defineProps<{
  title: string
  sub?: string
  span?: number
  /** 面板左下角口径说明；给 false 表示不要这一条 */
  foot?: string | false
  loading?: boolean
}>()
</script>

<template>
  <section class="panel" :style="{ '--span': span ?? 12 }">
    <header class="panel-head">
      <h3 class="panel-title">{{ title }}</h3>
      <span v-if="sub" class="panel-sub">{{ sub }}</span>
      <span class="spacer" />
      <slot name="actions" />
    </header>
    <div class="panel-body">
      <div v-if="loading" class="panel-state">加载中…</div>
      <slot v-else />
    </div>
    <footer v-if="foot !== false" class="panel-foot">{{ foot || '数据来源：LEMS' }}</footer>
  </section>
</template>

<style scoped>
.panel {
  grid-column: span var(--span, 12);
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.panel-head {
  display: flex;
  align-items: center;
  gap: var(--s3);
  min-height: 48px;
  padding: 0 var(--s4);
  border-bottom: 1px solid var(--divider);
}
.panel-head::before {
  content: '';
  width: 3px;
  height: 16px;
  border-radius: var(--r-xs);
  background: var(--brand);
  flex: 0 0 auto;
}
.panel-title {
  margin: 0;
  font-size: var(--fs-16);
  font-weight: 600;
  letter-spacing: -0.01em;
}
.panel-sub { font-size: var(--fs-12); color: var(--text-3); }
.spacer { flex: 1; }
.panel-body { flex: 1; min-width: 0; padding: var(--s4); }
.panel-state {
  display: grid;
  place-items: center;
  min-height: 140px;
  color: var(--text-3);
  font-size: var(--fs-13);
}
.panel-foot {
  padding: var(--s2) var(--s4);
  border-top: 1px solid var(--divider);
  font-size: var(--fs-12);
  color: var(--text-3);
}
</style>
