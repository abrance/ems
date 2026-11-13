<script setup lang="ts">
/**
 * 数据源角标：标清这块数字「哪来的、滞后多久」。
 *
 * 为什么要单独一个组件而不是各处手写 span：市场侧现在有三条源（披露接口日频 T+2、
 * 主页 24 点 T-6、主页地市节点价 T+0），同一个页面里出现两块不同批次的数据时，
 * 用户唯一能判断新鲜度的线索就是这个角标。写法统一才不会有的地方写「官方」、
 * 有的地方写「pm-api」，让人以为是两回事。
 *
 * tone 与语义绑定（不按页面随手挑）：
 *   blue  = 官方披露接口日频（T+2）
 *   brand = 官方主页 24 点（T-6）
 *   green = 官方主页地市节点价（T+0）
 *   gray  = 已停用 / 无数据源
 */
withDefaults(
  defineProps<{
    tone?: 'blue' | 'brand' | 'green' | 'gray'
    /** 角标文本，例如「官网·日频 T+2」 */
    label: string
  }>(),
  { tone: 'blue' },
)
</script>

<template>
  <span class="source-badge" :class="tone">{{ label }}</span>
</template>

<style scoped>
.source-badge {
  display: inline-block;
  padding: 1px var(--s2);
  border-radius: var(--r-pill);
  font-size: var(--fs-12);
  line-height: 18px;
  white-space: nowrap;
}
.blue { background: var(--blue-soft); color: var(--blue-ink); }
.brand { background: var(--brand-soft); color: var(--brand-ink); }
.green { background: var(--green-soft); color: var(--green-ink); }
.gray { background: var(--gray-soft); color: var(--gray-ink); }
</style>
