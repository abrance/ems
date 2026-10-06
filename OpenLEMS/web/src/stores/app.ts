import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

const SIDE_KEY = 'lems:ui:sideCollapsed'
const SCENE_KEY = 'lems:ui:scene'
const THEME_KEY = 'lems:ui:theme'
const DENSITY_KEY = 'lems:ui:density'
const LANDING_KEY = 'lems:ui:landing'

export type Theme = 'light' | 'dark'
export type Density = 'standard' | 'compact'

/** 场景切换器的候选；本期只落地「广东现货日报」一支 */
export const SCENES = ['广东现货日报', '山西现货交易', '储能套利'] as const

/** 壳层 UI 状态：侧栏收放、场景、运行日。页面级状态不放这里。 */
export const useAppStore = defineStore('app', () => {
  const sideCollapsed = ref(localStorage.getItem(SIDE_KEY) === '1')
  const scene = ref(localStorage.getItem(SCENE_KEY) ?? SCENES[0])
  const runDate = ref('')
  const theme = ref<Theme>((localStorage.getItem(THEME_KEY) as Theme) || 'light')
  const density = ref<Density>((localStorage.getItem(DENSITY_KEY) as Density) || 'standard')
  /** 默认首页：留空表示用 nav 里的第一项 */
  const landing = ref(localStorage.getItem(LANDING_KEY) || '')

  watch(sideCollapsed, (v) => localStorage.setItem(SIDE_KEY, v ? '1' : '0'))
  watch(scene, (v) => localStorage.setItem(SCENE_KEY, v))
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

  return { sideCollapsed, scene, runDate, theme, density, landing, toggleSide }
})
