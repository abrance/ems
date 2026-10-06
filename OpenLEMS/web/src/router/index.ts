import { createRouter, createWebHistory } from 'vue-router'
import { buildRoutes } from './routes'

export const router = createRouter({
  history: createWebHistory(),
  routes: buildRoutes(),
})
