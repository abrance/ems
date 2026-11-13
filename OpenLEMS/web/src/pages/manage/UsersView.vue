<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import { changePassword, fetchCurrentUser } from '@/api/auth'
import { ACCOUNTS, ROLE_LABEL, session } from '@/auth/session'

/**
 * 管理 · 用户与权限。
 *
 * 先说清一个容易做错的前提：**本系统没有用户表**。
 * 后端登录接口只有 `role` + `password`，`admin` / `user` 两个角色本身就是账号，
 * 密码存在系统设置里（bcrypt）。所以这一页不是「用户增删改查」，而是：
 *   - 两个内置账号是什么、各自能做什么
 *   - 改名密码（含「改哪个账号」与「证明是本人」两件事）
 *   - 哪些相关能力**现在其实不生效**（如实写出来，别做成假控件）
 */
const ADMIN = 'admin'

const serverRole = ref('')
const busy = ref(false)
const error = ref('')
const ok = ref('')

// 表单
const target = ref(session.role || ADMIN)
const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')

const isAdmin = computed(() => (serverRole.value || session.role) === ADMIN)
/** 绕过模式注入的是假令牌，与真实登录区分开，否则会让人以为自己是真登录进来的 */
const isBypass = computed(() => session.token === 'auth-bypass')

const expireText = computed(() => {
  if (!session.expireAt) return isBypass.value ? '不过期（绕过模式）' : '未知'
  return new Date(session.expireAt * 1000).toLocaleString('zh-CN', { hour12: false })
})

async function loadSession() {
  try {
    const res = await fetchCurrentUser()
    serverRole.value = res.role
    // 服务端说的角色优先；非管理员时目标账号只能是它自己
    if (res.role !== ADMIN) target.value = res.role
  } catch {
    // 拿不到就以本地会话为准，页面照常可用（真失效时会被 401 踢回登录页）
    serverRole.value = session.role
  }
}

async function submit() {
  error.value = ''
  ok.value = ''
  if (!oldPassword.value) {
    error.value = '请输入当前密码'
    return
  }
  if (!newPassword.value) {
    error.value = '请输入新密码'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    error.value = '两次输入的新密码不一致'
    return
  }
  if (newPassword.value === oldPassword.value) {
    error.value = '新密码不能与当前密码相同'
    return
  }

  busy.value = true
  try {
    await changePassword({
      role: target.value,
      oldPassword: oldPassword.value,
      newPassword: newPassword.value,
    })
    ok.value =
      `已修改「${target.value}」的密码。` +
      '注意：其它已登录的会话不会因此失效，需要它们各自重新登录才会用到新密码。'
    oldPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : '修改失败'
  } finally {
    busy.value = false
  }
}

onMounted(loadSession)
</script>

<template>
  <div class="grid">
    <PanelBox
      title="当前会话"
      :span="12"
      :foot="'会话来源：localStorage ems:user:token（与后端 AUTH_BYPASS_ROLE 注入的引导脚本同一套键）'"
    >
      <div class="kpis">
        <KpiCard label="当前账号" :value="serverRole || session.role || '—'" hint="账号即角色，见下「内置账号」" />
        <KpiCard
          label="角色"
          :value="ROLE_LABEL[serverRole || session.role] || '—'"
          :hint="isAdmin ? '可读 + 可执行管理类操作' : '只读：管理类接口会返回 403'"
        />
        <KpiCard label="会话过期时间" :value="expireText" hint="前端据此判定会话是否有效" />
        <KpiCard
          label="会话类型"
          :value="isBypass ? '鉴权绕过' : '正常登录'"
          :hint="isBypass ? '后端开了 AUTH_BYPASS_ROLE，任何访问者都是这个角色' : '由登录接口签发'"
        />
      </div>
    </PanelBox>

    <PanelBox
      title="内置账号"
      sub="本系统没有用户表：两个角色本身就是账号"
      :span="6"
      foot="密码存在系统设置里（admin_password / user_password），bcrypt 哈希"
    >
      <div class="table">
        <div class="th">账号</div>
        <div class="th">说明</div>
        <div class="th">读接口</div>
        <div class="th">管理类接口</div>
        <div class="strong">admin</div>
        <div>管理员</div>
        <div class="num">✓</div>
        <div class="num">✓</div>
        <div class="strong">user</div>
        <div>普通用户</div>
        <div class="num">✓</div>
        <div class="num no">✗</div>
      </div>
      <p class="note-line">
        加用户 / 自定义角色需要新的后端模型（用户表、角色、权限点）。本期只有这两级。
      </p>
    </PanelBox>

    <PanelBox
      title="权限规则"
      sub="后端实际执行的规则，不是界面自己定的"
      :span="6"
      foot="权威判定在后端；前端不隐藏入口，越权调用会拿到 403「权限不足」"
    >
      <ul class="notes">
        <li><strong>按方法推断</strong>：接口没写 <code>role</code> 标签时，GET 需要 user，POST / PUT / DELETE 需要 admin。</li>
        <li><strong>接口可显式覆盖</strong>：如 <code>/auth/logout</code>、<code>/auth/change-password</code> 都标了 <code>role:"user"</code>，普通用户即可调用。</li>
        <li><strong>admin ⊇ user</strong>：管理员能做普通用户能做的一切，不需要额外放权。</li>
        <li><strong>界面不做权限隐藏</strong>：入口都在，点了才知道会不会 403 —— 这样不会出现「同一功能对两种角色完全不同的界面」而没人发现后端没拦。</li>
      </ul>
    </PanelBox>

    <PanelBox
      title="修改密码"
      :sub="isAdmin ? '管理员可重置普通用户，也可改自己' : '普通用户只能改自己'"
      :span="12"
      foot="改的是目标账号的密码；「当前密码」填的是你自己（当前会话角色）的密码，用于证明是本人操作"
    >
      <div v-if="error" class="alert error">{{ error }}</div>
      <div v-if="ok" class="alert ok">{{ ok }}</div>

      <div class="form">
        <label class="field">
          <span>目标账号</span>
          <select v-model="target" :disabled="!isAdmin">
            <option v-for="a in ACCOUNTS" :key="a" :value="a" :disabled="!isAdmin && a === 'admin'">
              {{ a }}（{{ ROLE_LABEL[a] }}）
            </option>
          </select>
          <small v-if="!isAdmin">普通用户只能改自己的密码，目标不可选</small>
        </label>
        <label class="field">
          <span>当前密码</span>
          <input v-model="oldPassword" type="password" autocomplete="current-password" placeholder="你自己的密码" />
        </label>
        <label class="field">
          <span>新密码</span>
          <input v-model="newPassword" type="password" autocomplete="new-password" placeholder="至少 1 位" />
        </label>
        <label class="field">
          <span>确认新密码</span>
          <input v-model="confirmPassword" type="password" autocomplete="new-password" />
        </label>
      </div>
      <div class="actions">
        <button class="btn primary" type="button" :disabled="busy" @click="submit">
          {{ busy ? '提交中…' : '修改密码' }}
        </button>
      </div>
    </PanelBox>

    <PanelBox
      title="现状与限制"
      sub="这几条是后端当前的真实行为，写出来免得当成已实现"
      :span="12"
      foot="不假装有能力：下面每一处都能在代码里对上"
    >
      <ul class="notes">
        <li><strong>没有用户表</strong>：账号 = 两个固定角色。多用户、自定义角色、权限点、按人审计都不存在。</li>
        <li>
          <strong>改密码不吊销其它会话</strong>：会话保存在服务端文件里，改密码只换密码哈希，
          已经登录的会话继续有效（除非它自己退出或过期）。被改密码的账号不会立刻被踢下线。
        </li>
        <li>
          <strong>登录没有失败次数限制</strong>：<code>/auth/login</code> 不鉴权、也没有频率限制或锁定，
          密码是每个角色一个共享口令 → 可以一直试。默认口令又很弱，所以部署后必须立刻改掉。
        </li>
        <li>
          <strong><code>password_length</code> 设置不生效</strong>：这个设置项存在，但没有任何代码校验它 ——
          设成 8 也一样能存 1 位的密码。
        </li>
        <li>
          <strong><code>session_admin_timeout</code> / <code>session_user_timeout</code> 不生效</strong>：
          登录控制器用 <code>g.Cfg().MustGet(ctx, "int:"+值)</code> 去读它，这是个无效的配置键，
          永远回退默认 2 小时。而且服务端会话上限是全局的（<code>SetSessionMaxAge</code>），
          就算修好读取，per-role 的会话时长也做不到 —— 所以本页<strong>没有</strong>给这两个设置做控件。
        </li>
      </ul>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.alert {
  margin-bottom: var(--s3);
  padding: var(--s2) var(--s3);
  border-radius: var(--r-sm);
  font-size: var(--fs-13);
}
.alert.error { border-left: 3px solid var(--red); background: var(--red-soft); color: var(--red-ink); }
.alert.ok { border-left: 3px solid var(--green); background: var(--green-soft); color: var(--green-ink); }
.table {
  display: grid;
  grid-template-columns: 72px 1fr 84px 108px;
  gap: 1px;
  background: var(--border);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
  font-size: var(--fs-13);
}
.table > div { padding: var(--s2) var(--s3); background: var(--bg-2); }
.table .th { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.table .num { text-align: right; font-variant-numeric: tabular-nums; }
.table .no { color: var(--text-3); }
.table .strong { color: var(--brand-ink); font-weight: 600; }
.note-line { margin: var(--s3) 0 0; font-size: var(--fs-12); color: var(--text-3); line-height: 1.8; }
.form { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.field { display: flex; flex-direction: column; gap: var(--s1); font-size: var(--fs-13); color: var(--text-3); }
.field small { font-size: var(--fs-12); color: var(--text-3); }
.field input,
.field select {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-1);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.field input:disabled,
.field select:disabled { color: var(--text-3); background: var(--bg-3); }
.actions { margin-top: var(--s4); }
.notes { margin: 0; padding-left: var(--s5); font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
.notes code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
.btn {
  padding: var(--s2) var(--s5);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.btn:disabled { opacity: 0.5; cursor: default; }
.btn.primary { background: var(--brand-btn); color: #fff; }
.btn.primary:hover:not(:disabled) { background: var(--brand-btn-h); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(2, 1fr); }
  .form { grid-template-columns: repeat(2, 1fr); }
}
</style>
