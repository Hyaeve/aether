import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './style.css'
import { preloadLogos } from './assets-preload'
import { tabScroll } from './tab-scroll'
import { registerPWA } from './pwa'
preloadLogos()
registerPWA()

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/backup', redirect: '/transfer/backup' },
    { path: '/links', redirect: '/links/manage' },
    { path: '/notifications', redirect: '/notifications/telegram' },
    { path: '/virtual/tianyi', redirect: '/virtual/115' },
    { path: '/webdav', redirect: '/files/webdav' },
    { path: '/mounts', redirect: '/files/mounts' },
    { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
  ]
})
createApp(App).directive('tab-scroll', tabScroll).use(router).mount('#app')
