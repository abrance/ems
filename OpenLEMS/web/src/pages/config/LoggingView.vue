<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PanelBox from '@/components/PanelBox.vue'
import { fetchCurrentUser } from '@/api/auth'
import {
  fetchPublicSettings,
  LOG_LEVEL_OPTIONS,
  LOG_MODULES,
  saveSettingValue,
  SETTING_LOG_LEVEL_GLOBAL,
  SETTING_SYSTEM_ENABLE_DEBUG_LOG,
} from '@/api/settings'

/**
 * 配置 · 日志级别。
 *
 * 这里要管的是**两套不同**的日志级别，页面上必须分开写，否则会互相误伤：
 *
 *   全局日志级别  → 系统日志（c_log.Info / Infof 这类），落到文件与控制台
 *   业务日志级别  → 业务日志（c_log.BizInfo / BizErrorf 这类），**按模块**入库 + biz 输出
 *
 * 两者都是改完立即生效（写设置会清设置缓存；全局那条还会立刻 SetLevelStr）。
 * 默认值都是「不过滤」，与加这个功能之前的行为一致。
 */
const settings = ref<Record<string, string>>({})
/** 写设置需要 admin（POST 推断）。普通用户会话下本页只读，否则每个控件都会吃 403 */
const isAdmin = ref(true)
const loading = ref(false)
const saving = ref('')
const error = ref('')
const ok = ref('')

const globalLevel = computed(() => settings.value[SETTING_LOG_LEVEL_GLOBAL] ?? 'ALL')
const debugSwitchOn = computed(() => (settings.value[SETTING_SYSTEM_ENABLE_DEBUG_LOG] ?? 'false') === 'true')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [me, list] = await Promise.all([fetchCurrentUser().catch(() => null), fetchPublicSettings()])
    isAdmin.value = (me?.role ?? 'admin') === 'admin'
    const list2 = list
    const map: Record<string, string> = {}
    for (const s of list2) map[s.id] = s.value
    settings.value = map
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取设置失败'
  } finally {
    loading.value = false
  }
}

async function save(id: string, value: string) {
  saving.value = id
  error.value = ''
  ok.value = ''
  try {
    await saveSettingValue(id, value)
    settings.value = { ...settings.value, [id]: value }
    ok.value = `已保存：${id} = ${value}（立即生效，无需重启）`
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存失败'
    // 保存失败要把界面上的值回滚成后端实际值，避免显示一个没生效的级别
    await load()
  } finally {
    saving.value = ''
  }
}

function onGlobalChange(e: Event) {
  void save(SETTING_LOG_LEVEL_GLOBAL, (e.target as HTMLSelectElement).value)
}

function onModuleChange(id: string, e: Event) {
  void save(id, (e.target as HTMLSelectElement).value)
}

function toggleDebug(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  void save(SETTING_SYSTEM_ENABLE_DEBUG_LOG, on ? 'true' : 'false')
}

onMounted(load)
</script>

<template>
  <div class="grid">
    <PanelBox
      title="全局日志级别"
      sub="管系统日志（c_log.Info / Infof 这类）→ 写日志文件与控制台"
      :span="12"
      :loading="loading"
      foot="默认 ALL，与配置里的 logger.level 一致；本项改完立即生效（运行期覆盖配置初值）"
    >
      <div v-if="!isAdmin" class="alert warn">
        当前账号是普通用户：本页<strong>只读</strong>。写设置需要管理员（管理类接口会返回 403「权限不足」）。
      </div>
      <div v-if="error" class="alert error">{{ error }}</div>
      <div v-if="ok" class="alert ok">{{ ok }}</div>

      <div class="row">
        <label class="field">
          <span>级别</span>
          <select :value="globalLevel" :disabled="!isAdmin" @change="onGlobalChange">
            <option v-for="o in LOG_LEVEL_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </label>
        <p class="hint">
          选到 WARN 就只写 WARN / ERROR —— 这会立刻减少日志文件里的行数，也是这个设置的主要用途。
          <strong>不影响下面各模块的业务日志级别</strong>（那是另一条输出链路）。
        </p>
      </div>
    </PanelBox>

    <PanelBox
      title="业务日志级别（按模块）"
      sub="管业务日志（c_log.BizInfo / BizErrorf 这类）→ 入库 + biz 控制台输出"
      :span="12"
      :loading="loading"
      foot="低于所选级别的日志既不入库也不输出；默认 DEBUG = 不过滤，与加这个功能之前一致"
    >
      <div class="table">
        <div class="th">模块</div>
        <div class="th">含义</div>
        <div class="th">级别</div>
        <template v-for="m in LOG_MODULES" :key="m.id">
          <div class="strong">{{ m.title }}</div>
          <div class="muted">{{ m.remark }}</div>
          <div class="ctrl">
            <select
              :value="settings[m.id] ?? 'DEBUG'"
              :disabled="!isAdmin || saving === m.id"
              @change="(e) => onModuleChange(m.id, e as Event)"
            >
              <option v-for="o in LOG_LEVEL_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </div>
        </template>
      </div>
    </PanelBox>

    <PanelBox
      title="调试日志开关"
      sub="已有开关，与上面的级别是<strong>两个条件</strong>，都要满足才记录"
      :span="12"
      :loading="loading"
      foot="SystemEnableDebugLog：ems 的 DEBUG 是否入库。设备侧另有逐设备的调试开关（在「管理 · 设备管理」里）"
    >
      <label class="switch">
        <input
          type="checkbox"
          :checked="debugSwitchOn"
          :disabled="!isAdmin || saving === SETTING_SYSTEM_ENABLE_DEBUG_LOG"
          @change="toggleDebug"
        />
        <span>启用系统调试日志（ems 的 DEBUG 入库）</span>
      </label>
      <p class="hint">
        与级别的关系：<code>ems = DEBUG</code> <strong>且</strong> 这个开关打开，ems 的 DEBUG 才入库。
        默认两者都偏向「不记」—— 这个开关出厂是关的，所以就算级别是 DEBUG，ems 的 DEBUG 也不会入库。
      </p>
    </PanelBox>

    <PanelBox
      title="现状与限制"
      sub="这几条是后端当前的真实行为，别当成已实现"
      :span="12"
      foot="不假装有能力：每条都能在代码里对上"
    >
      <ul class="notes">
        <li>
          <strong>两套级别互不影响</strong>：上面的全局级别只过滤系统日志，下面的模块级别只过滤业务日志。
          想「整机安静点」通常两个都要调。
        </li>
        <li>
          <strong>设备侧还有第三道条件</strong>：<code>device</code> 的 DEBUG 要满足
          「模块级别 ≤ DEBUG」+「该设备的调试模式已开」才入库 —— 后者是逐设备的，在设备管理页里开。
        </li>
        <li>
          <strong>级别是按日志类型，不是按具体功能</strong>：只有 ems / device / protocol / policy /
          automation / remote 六个类型，做不到「只关某个协议的 DEBUG」。
        </li>
        <li>
          <strong>日志保留天数会同时管数据库和磁盘</strong>：除了清掉库里的旧日志行，
          数据保留任务还会按这个天数删除 <code>out/logs/app</code> 下超期的 <code>.log</code> 文件（正在写的那个不删）。
          结果在「存储管理 → 保留策略」里能看到“文件数/释放空间”。
        </li>
        <li><strong>没有日志滚动策略的界面</strong>：文件大小/份数来自配置（5MB × 20 份 ≈ 100MB），运行时不可改；超期的滚动文件另由上面的按天清理删除。</li>
      </ul>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.row { display: flex; align-items: flex-start; gap: var(--s4); flex-wrap: wrap; }
.alert { margin-bottom: var(--s3); padding: var(--s2) var(--s3); border-radius: var(--r-sm); font-size: var(--fs-13); }
.alert.error { border-left: 3px solid var(--red); background: var(--red-soft); color: var(--red-ink); }
.alert.ok { border-left: 3px solid var(--green); background: var(--green-soft); color: var(--green-ink); }
.alert.warn { border-left-color: var(--brand); background: var(--brand-soft); color: var(--brand-ink); }
.field { display: flex; flex-direction: column; gap: var(--s1); font-size: var(--fs-12); color: var(--text-3); }
.field select,
.ctrl select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.field select { width: 180px; }
.table {
  display: grid;
  grid-template-columns: 120px 1fr 180px;
  gap: 1px;
  background: var(--border);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
  font-size: var(--fs-13);
}
.table > div { padding: var(--s2) var(--s3); background: var(--bg-2); display: flex; align-items: center; }
.table .th { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.table .strong { color: var(--brand-ink); font-weight: 600; }
.table .muted { color: var(--text-3); font-size: var(--fs-12); }
.switch { display: inline-flex; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-1); cursor: pointer; }
.hint { margin: var(--s3) 0 0; font-size: var(--fs-12); color: var(--text-3); line-height: 1.9; }
.hint code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
.notes { margin: 0; padding-left: var(--s5); font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
.notes code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
