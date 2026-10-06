<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import { rebootApply, rebootExecute, updateHostname, updateSystemTime } from '@/api/system'

/**
 * 配置 · 系统操作。
 *
 * 三类操作，危险度递增：
 *   1. 改主机名 —— 可逆，需符合后端校验（字母数字与短横线，≤63）
 *   2. 改系统时间 / 时区 —— 影响所有时间戳与调度，需二次确认
 *   3. 重启 —— 两步：先「申请」（30 秒内有效），再「执行」；需输入主机名确认
 *
 * 重启的两步是后端设计（apply → execute），不是前端加的保险，页面照它的语义走。
 * 页面上把「已申请，等待执行」这个中间态显式做出来，否则用户不知道 30 秒在倒计时。
 */
const hostname = ref('')
const hostnameSaving = ref(false)

const clock = reactive({ time: '', timezone: '' })
const clockSaving = ref(false)
const clockConfirm = ref(false)

const rebootStep = ref<'idle' | 'applied'>('idle')
const rebootBusy = ref(false)
const rebootConfirm = ref(false)
const rebootConfirmText = ref('')
const rebootSecondsLeft = ref(0)
let countdown: number | undefined
let clockTicker: number | undefined

const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined

function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4500)
}

function nowText(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function syncNow() {
  clock.time = nowText()
  try {
    clock.timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  } catch {
    clock.timezone = ''
  }
}

/* ---------- 主机名 ---------- */
const hostnameIssue = computed(() => {
  const v = hostname.value.trim()
  if (!v) return '主机名不能为空'
  if (!/^[a-zA-Z0-9-]{1,63}$/.test(v)) return '只能包含字母、数字与短横线，长度 ≤ 63'
  return ''
})

async function saveHostname() {
  if (hostnameIssue.value) {
    showToast('red', hostnameIssue.value)
    return
  }
  hostnameSaving.value = true
  try {
    await updateHostname(hostname.value.trim())
    showToast('green', '主机名已更新')
  } catch (err) {
    showToast('red', `更新失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    hostnameSaving.value = false
  }
}

/* ---------- 时间 ---------- */
const TIME_RE = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/

const clockIssue = computed(() => {
  if (!clock.time.trim()) return '时间不能为空'
  if (!TIME_RE.test(clock.time.trim())) return '时间格式应为 YYYY-MM-DD HH:mm:ss'
  return ''
})

function askSaveClock() {
  if (clockIssue.value) {
    showToast('red', clockIssue.value)
    return
  }
  clockConfirm.value = true
}

async function confirmSaveClock() {
  clockConfirm.value = false
  clockSaving.value = true
  try {
    await updateSystemTime(clock.time.trim(), clock.timezone.trim() || undefined)
    showToast('green', '系统时间已更新')
  } catch (err) {
    showToast('red', `更新失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    clockSaving.value = false
  }
}

/* ---------- 重启：申请 → 执行 ---------- */
async function applyReboot() {
  rebootBusy.value = true
  try {
    await rebootApply()
    rebootStep.value = 'applied'
    rebootSecondsLeft.value = 30
    window.clearInterval(countdown)
    countdown = window.setInterval(() => {
      rebootSecondsLeft.value -= 1
      if (rebootSecondsLeft.value <= 0) {
        window.clearInterval(countdown)
        rebootStep.value = 'idle'
        showToast('red', '重启申请已过期（30 秒），请重新申请')
      }
    }, 1000)
    showToast('green', '已提交重启申请，请在 30 秒内确认执行')
  } catch (err) {
    showToast('red', `申请失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    rebootBusy.value = false
  }
}

function askExecuteReboot() {
  rebootConfirmText.value = ''
  rebootConfirm.value = true
}

async function confirmExecuteReboot() {
  if (rebootConfirmText.value.trim() !== hostname.value.trim()) return
  rebootConfirm.value = false
  rebootBusy.value = true
  try {
    await rebootExecute()
    showToast('green', '已下发重启，服务将短暂中断')
    rebootStep.value = 'idle'
    window.clearInterval(countdown)
  } catch (err) {
    showToast('red', `执行失败：${err instanceof Error ? err.message : '未知错误'}`)
  } finally {
    rebootBusy.value = false
  }
}

onMounted(() => {
  syncNow()
  // 时钟只在「没在倒计时」时跟着走，否则会盖掉用户正在编辑的值
  clockTicker = window.setInterval(() => {
    if (rebootStep.value === 'idle') syncNow()
  }, 1000)
})

onBeforeUnmount(() => {
  window.clearInterval(countdown)
  window.clearInterval(clockTicker)
  window.clearTimeout(toastTimer)
})
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div class="grid">
    <PanelBox title="主机名" :span="6" foot="只影响本机标识，不影响网络配置">
      <div class="field">
        <label for="hostname">主机名</label>
        <input id="hostname" v-model="hostname" placeholder="字母、数字与短横线，≤ 63" />
        <span v-if="hostnameIssue" class="err">{{ hostnameIssue }}</span>
      </div>
      <div class="actions">
        <button class="btn primary" type="button" :disabled="hostnameSaving || !!hostnameIssue" @click="saveHostname">
          {{ hostnameSaving ? '保存中…' : '保存' }}
        </button>
      </div>
    </PanelBox>

    <PanelBox title="系统时间与时区" :span="6" foot="生效语义：立即生效；会影响到所有时间戳与调度">
      <div class="field">
        <label for="clock">系统时间</label>
        <input id="clock" v-model="clock.time" placeholder="YYYY-MM-DD HH:mm:ss" />
        <span v-if="clockIssue" class="err">{{ clockIssue }}</span>
      </div>
      <div class="field">
        <label for="tz">时区</label>
        <input id="tz" v-model="clock.timezone" placeholder="Asia/Shanghai" />
      </div>
      <div class="actions">
        <button class="btn" type="button" @click="syncNow">取本机当前时间</button>
        <button class="btn primary" type="button" :disabled="clockSaving || !!clockIssue" @click="askSaveClock">
          {{ clockSaving ? '保存中…' : '保存' }}
        </button>
      </div>
    </PanelBox>

    <PanelBox title="重启" :span="12" foot="后端是两步：申请（30 秒内有效）→ 执行；不是前端加的保险">
      <div class="notice">
        <strong>重启会中断所有采集与控制</strong>
        <span>　策略与自动化在服务恢复前不会执行。请先确认现场处在安全状态。</span>
      </div>

      <div class="reboot">
        <div class="step">
          <span class="k">1. 申请重启</span>
          <button class="btn" type="button" :disabled="rebootBusy || rebootStep === 'applied'" @click="applyReboot">
            申请
          </button>
          <StatusTag v-if="rebootStep === 'applied'" tone="brand">
            已申请 · {{ rebootSecondsLeft }}s 内有效
          </StatusTag>
          <span v-else class="muted">未申请</span>
        </div>

        <div class="step">
          <span class="k">2. 确认执行</span>
          <button
            class="btn danger"
            type="button"
            :disabled="rebootBusy || rebootStep !== 'applied'"
            @click="askExecuteReboot"
          >
            执行重启
          </button>
          <span class="muted">需先完成第 1 步，且输入主机名确认</span>
        </div>
      </div>
    </PanelBox>
  </div>

  <div v-if="clockConfirm" class="modal-mask" @click.self="clockConfirm = false">
    <div class="modal">
      <h3>确认修改系统时间</h3>
      <p class="muted">改错时间会让历史数据的时间轴错乱，且难以还原。请核对：</p>
      <dl>
        <dt>时间</dt><dd>{{ clock.time }}</dd>
        <dt>时区</dt><dd>{{ clock.timezone || '（不变）' }}</dd>
      </dl>
      <div class="modal-actions">
        <button class="btn" type="button" @click="clockConfirm = false">取消</button>
        <button class="btn primary" type="button" @click="confirmSaveClock">确认修改</button>
      </div>
    </div>
  </div>

  <div v-if="rebootConfirm" class="modal-mask" @click.self="rebootConfirm = false">
    <div class="modal danger-modal">
      <h3>确认执行重启</h3>
      <p class="muted">此操作会立即中断服务。请输入主机名确认：</p>
      <input v-model="rebootConfirmText" :placeholder="hostname" />
      <div class="modal-actions">
        <button class="btn" type="button" @click="rebootConfirm = false">取消</button>
        <button
          class="btn danger"
          type="button"
          :disabled="rebootConfirmText.trim() !== hostname.trim()"
          @click="confirmExecuteReboot"
        >
          确认重启
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.field { display: flex; flex-direction: column; gap: var(--s2); margin-bottom: var(--s3); }
.field label { font-size: var(--fs-12); color: var(--text-3); }
.field input {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.field input:focus-visible { outline: 2px solid var(--brand-line); outline-offset: 1px; }
.actions { display: flex; gap: var(--s2); justify-content: flex-end; }
.notice {
  display: flex;
  align-items: flex-start;
  gap: var(--s3);
  margin-bottom: var(--s4);
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--red);
  border-radius: var(--r-sm);
  background: var(--red-soft);
  color: var(--red-ink);
  font-size: var(--fs-13);
}
.reboot { display: flex; flex-direction: column; gap: var(--s3); }
.step { display: flex; align-items: center; gap: var(--s3); font-size: var(--fs-13); }
.step .k { min-width: 96px; color: var(--text-3); font-size: var(--fs-12); }
.err { font-size: var(--fs-12); color: var(--red-ink); }
.muted { color: var(--text-3); font-size: var(--fs-12); }
.btn {
  padding: var(--s1) var(--s4);
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
.btn.danger { background: var(--red-soft); color: var(--red-ink); }
.toast {
  position: fixed;
  top: calc(var(--h-appbar) + var(--s2));
  left: 50%;
  z-index: 70;
  transform: translateX(-50%);
  padding: var(--s3) var(--s5);
  border-radius: var(--r-sm);
  font-size: var(--fs-13);
  box-shadow: 0 4px 12px rgb(0 0 0 / 12%);
}
.toast.green { background: var(--green-soft); color: var(--green-ink); }
.toast.red { background: var(--red-soft); color: var(--red-ink); }
.modal-mask {
  position: fixed;
  inset: 0;
  z-index: 65;
  display: grid;
  place-items: center;
  padding: var(--s4);
  background: rgb(29 33 41 / 45%);
}
.modal {
  width: min(440px, 100%);
  padding: var(--s5);
  border-top: 3px solid var(--brand);
  border-radius: var(--r-md);
  background: var(--bg-2);
}
.modal.danger-modal { border-top-color: var(--red); }
.modal h3 { margin: 0 0 var(--s2); font-size: var(--fs-16); }
.modal dl { display: grid; grid-template-columns: 64px 1fr; gap: var(--s2) var(--s3); margin: var(--s4) 0; font-size: var(--fs-13); }
.modal dt { color: var(--text-3); }
.modal dd { margin: 0; font-variant-numeric: tabular-nums; }
.modal input {
  width: 100%;
  margin: var(--s3) 0 var(--s4);
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.modal-actions { display: flex; justify-content: flex-end; gap: var(--s2); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
