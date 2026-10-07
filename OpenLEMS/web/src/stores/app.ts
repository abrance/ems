import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

const SIDE_KEY = 'lems:ui:sideCollapsed'
const THEME_KEY = 'lems:ui:theme'
const DENSITY_KEY = 'lems:ui:density'
const LANDING_KEY = 'lems:ui:landing'

export type Theme = 'light' | 'dark'
export type Density = 'standard' | 'compact'

/**
 * 数据场景：**当前只有一支**。
 *
 * 以前这里是可切换的下拉（广东现货日报 / 山西现货交易 / 储能套利），但：
 *   1. 切换它不改变任何数据 —— 没有任何页面按场景过滤；
 *   2. 另外两个场景**根本没有数据源**（本仓库的数据只有广东：gd_metric.csv 的
 *      96 点电价负荷、广东现货日报、EMS 现场设备、TimesFM 预测）。
 * 于是它只是个假控件，已改成静态标签 + 说明。
 *
 * 下面这份清单保留下来，是为了随时能回答「要接入什么才能多一个场景」。
 */
export const SCENE = '广东现货日报'

/** 规划中的场景与各自缺的数据源（只用于提示文案，不做成可选） */
export const SCENES_PLANNED: Array<{ name: string; need: string }> = [
  { name: '山西现货交易', need: '山西的市场披露：出清价/节点价、机组开机与检修、新能源出力' },
  { name: '储能套利', need: '储能侧结算口径：容量电价、需量结算、充放结算价' },
]

/** 壳层 UI 状态：侧栏收放、运行日。页面级状态不放这里。 */
export const useAppStore = defineStore('app', () => {
  const sideCollapsed = ref(localStorage.getItem(SIDE_KEY) === '1')
  const runDate = ref('')
  const theme = ref<Theme>((localStorage.getItem(THEME_KEY) as Theme) || 'light')
  const density = ref<Density>((localStorage.getItem(DENSITY_KEY) as Density) || 'standard')
  /** 默认首页：留空表示用 nav 里的第一项 */
  const landing = ref(localStorage.getItem(LANDING_KEY) || '')

  watch(sideCollapsed, (v) => localStorage.setItem(SIDE_KEY, v ? '1' : '0'))
  watch(
    theme,
    (v) => {
      localStorage.setItem(THEME_KEY, v)
      applyTheme(v, density.value)
    },
    { immediate: true },
  )
  watch(
    density,
    (v) => {
      localStorage.setItem(DENSITY_KEY, v)
      applyTheme(theme.value, v)
    },
    { immediate: true },
  )
  watch(landing, (v) => localStorage.setItem(LANDING_KEY, v))

  function applyTheme(t: Theme, d: Density) {
    const el = document.documentElement
    if (t === 'dark') el.dataset.theme = 'dark'
    else delete el.dataset.theme
    if (d === 'compact') el.dataset.density = 'compact'
    else delete el.dataset.density
  }

  function toggleSide() {
    sideCollapsed.value = !sideCollapsed.value
  }

  return { sideCollapsed, runDate, theme, density, landing, toggleSide }
})
