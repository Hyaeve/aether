import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './style.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/links', redirect: '/links/manage' },
    { path: '/webdav', redirect: '/files/webdav' },
    { path: '/mounts', redirect: '/files/mounts' },
    { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
  ]
})
createApp(App).use(router).mount('#app')
