import { createRouter, createWebHistory } from 'vue-router'
import HomePage from '../pages/HomePage.vue'
import OrderListPage from '../pages/OrderListPage.vue'

const routes = [
  { path: '/', name: 'home', component: HomePage },
  { path: '/orders', name: 'orders', component: OrderListPage },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
