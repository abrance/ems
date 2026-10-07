<script setup lang="ts">
/**
 * 顶栏只放身份与入口：Logo、站点、用户。
 * 不放流量 / CPU / 时钟这类实时指标（属于「配置 · 系统信息」页）—— 顶栏越素，主区越显眼。
 */
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ROLE_LABEL, session, signOut } from '@/auth/session'

const props = defineProps<{ collapsed: boolean }>()
const emit = defineEmits<{ (e: 'toggle-side'): void }>()

const router = useRouter()
const menuOpen = ref(false)

/** 绕过模式下 token 是注入的假令牌，但角色是真的 —— 头像用角色首字，别假装有用户名 */
const account = computed(() => session.role || 'admin')
const avatarText = computed(() => (session.role === 'admin' ? '管' : '用'))
const roleLabel = computed(() => ROLE_LABEL[session.role] ?? session.role ?? '')

async function doSignOut() {
  menuOpen.value = false
  await signOut()
  await router.replace({ path: '/login' })
}
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
    <div class="user">
      <button class="user-menu" type="button" :aria-expanded="menuOpen" @click="menuOpen = !menuOpen">
        <span class="avatar">{{ avatarText }}</span>{{ account }}
      </button>
      <div v-if="menuOpen" class="menu">
        <div class="menu-who">{{ account }}<small>{{ roleLabel }}</small></div>
        <button class="menu-item" type="button" @click="doSignOut">退出登录</button>
      </div>
    </div>
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
.user { position: relative; }
.menu {
  position: absolute;
  top: calc(100% + var(--s1));
  right: 0;
  min-width: 160px;
  padding: var(--s1);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  z-index: 40;
}
.menu-who {
  display: flex;
  flex-direction: column;
  padding: var(--s2) var(--s3);
  font-size: var(--fs-13);
  color: var(--text-1);
  border-bottom: 1px solid var(--divider);
}
.menu-who small { font-size: var(--fs-12); color: var(--text-3); }
.menu-item {
  width: 100%;
  margin-top: var(--s1);
  padding: var(--s2) var(--s3);
  border: 0;
  border-radius: var(--r-xs);
  background: transparent;
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
  text-align: left;
  cursor: pointer;
}
.menu-item:hover { background: var(--bg-3); color: var(--brand-ink); }
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
