<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ACCOUNTS, signIn } from '@/auth/session'

/**
 * 登录页。
 *
 * 后端的登录接口没有「用户名」字段 —— role（admin / user）本身就是账号，
 * 密码存在系统设置里。所以这里的「账号」填的就是 admin 或 user，
 * 传错值会直接吃一个 400，所以在本地先拦一次。
 */
const route = useRoute()
const router = useRouter()

const account = ref('admin')
const password = ref('')
const error = ref('')
const busy = ref(false)

/** 会话过期被踢回来时给一句明确的解释，而不是让用户猜为什么退出了 */
const expired = computed(() => route.query.expired === '1')

async function submit() {
  error.value = ''
  const name = account.value.trim().toLowerCase()
  if (!(ACCOUNTS as readonly string[]).includes(name)) {
    error.value = `账号不存在。本系统当前只有 ${ACCOUNTS.join(' / ')} 两个内置账号`
    return
  }
  if (!password.value) {
    error.value = '请输入密码'
    return
  }

  busy.value = true
  try {
    await signIn(name, password.value)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : ''
    // 不把 /login 自己当跳转目标，否则原地打转
    await router.replace(redirect && redirect !== '/login' ? redirect : '/')
  } catch (err) {
    error.value = err instanceof Error ? err.message : '登录失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="login">
    <form class="card" @submit.prevent="submit">
      <div class="brand">
        <span class="mark">EMS</span>
        <div class="brand-text">
          <strong>能源管理系统</strong>
          <span>Energy Management System</span>
        </div>
      </div>

      <h1 class="title">登录</h1>

      <div v-if="expired && !error" class="alert info">登录已过期，请重新登录。</div>
      <div v-if="error" class="alert" role="alert">{{ error }}</div>

      <label class="field">
        <span>账号</span>
        <input v-model="account" type="text" name="account" autocomplete="username" placeholder="admin" />
      </label>
      <label class="field">
        <span>密码</span>
        <input
          v-model="password"
          type="password"
          name="password"
          autocomplete="current-password"
          placeholder="请输入密码"
        />
      </label>

      <button class="submit" type="button" :disabled="busy" @click="submit">
        {{ busy ? '登录中…' : '登录' }}
      </button>

      <p class="hint">
        内置账号：<code>admin</code>（管理员）/ <code>user</code>（普通用户）。
        <strong>初次部署</strong>的初始密码是 <code>admin</code>（部署时可用 <code>EMS_ADMIN_PASSWORD</code> 覆盖）；
        升级/已有库会沿用原有密码，不会被重置。
        登录后请尽快修改。
      </p>
    </form>
  </div>
</template>

<style scoped>
.login {
  height: 100%;
  display: grid;
  place-items: center;
  padding: var(--s4);
  background: var(--bg-1);
}
.card {
  width: 100%;
  max-width: 360px;
  display: flex;
  flex-direction: column;
  gap: var(--s3);
  padding: var(--s6);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.brand { display: flex; align-items: center; gap: var(--s3); }
.brand .mark {
  width: 40px;
  height: 40px;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  background: var(--brand-btn);
  color: #fff;
  font-size: var(--fs-13);
  font-weight: 800;
}
.brand-text { display: flex; flex-direction: column; }
.brand-text strong { font-size: var(--fs-16); color: var(--text-1); }
.brand-text span { font-size: var(--fs-12); color: var(--text-3); }
.title {
  margin: var(--s2) 0 0;
  font-size: var(--fs-18);
  color: var(--text-1);
}
.alert {
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--red);
  border-radius: var(--r-sm);
  background: var(--red-soft);
  color: var(--red-ink);
  font-size: var(--fs-13);
}
.alert.info { border-left-color: var(--brand); background: var(--brand-soft); color: var(--brand-ink); }
.field { display: flex; flex-direction: column; gap: var(--s1); font-size: var(--fs-13); color: var(--text-3); }
.field input {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-1);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-14);
}
.field input:focus {
  outline: 2px solid var(--brand);
  outline-offset: -1px;
}
.submit {
  margin-top: var(--s2);
  padding: var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--brand-btn);
  color: #fff;
  font: inherit;
  font-size: var(--fs-14);
  font-weight: 600;
  cursor: pointer;
}
.submit:hover:not(:disabled) { background: var(--brand-btn-h); }
.submit:disabled { opacity: 0.6; cursor: default; }
.hint {
  margin: var(--s1) 0 0;
  font-size: var(--fs-12);
  line-height: 1.8;
  color: var(--text-3);
}
.hint code {
  padding: 0 var(--s1);
  border-radius: var(--r-xs);
  background: var(--bg-3);
  color: var(--text-2);
}
</style>
