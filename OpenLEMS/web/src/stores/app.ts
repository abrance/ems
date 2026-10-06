import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

const SIDE_KEY = 'lems:ui:sideCollapsed'
const SCENE_KEY = 'lems:ui:scene'

/** 场景切换器的候选；本期只落地「广东现货日报」一支 */
export const SCENES = ['广东现货日报', '山西现货交易', '储能套利'] as const

/** 壳层 UI 状态：侧栏收放、场景、运行日。页面级状态不放这里。 */
export const useAppStore = defineStore('app', () => {
  const sideCollapsed = ref(localStorage.getItem(SIDE_KEY) === '1')
  const scene = ref(localStorage.getItem(SCENE_KEY) ?? SCENES[0])
  const runDate = ref('')

  watch(sideCollapsed, (v) => localStorage.setItem(SIDE_KEY, v ? '1' : '0'))
  watch(scene, (v) => localStorage.setItem(SCENE_KEY, v))

  function toggleSide() {
    sideCollapsed.value = !sideCollapsed.value
  }

  return { sideCollapsed, scene, runDate, toggleSide }
})
