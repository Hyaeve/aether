import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './style.css'
import { preloadLogos } from './assets-preload'
import { tabScroll } from './tab-scroll'
import { registerPWA } from './pwa'
import { HanText } from './han-spacing'
import {installCardRemovalCleanup} from './config-card-removal.js'
preloadLogos()
registerPWA()

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/backup', redirect: '/transfer/backup' },
    { path: '/links', redirect: '/links/manage' },
    { path: '/rules', redirect: '/rules/naming' },
    { path: '/notifications', redirect: '/notifications/telegram' },
    { path: '/archives', redirect: '/archives/organize' },
    { path: '/virtual/tianyi', redirect: '/virtual/115' },
    { path: '/webdav', redirect: '/files/webdav' },
    { path: '/mounts', redirect: '/files/mounts' },
    { path: '/:pathMatch(.*)*', component: { template: '<div />' } }
  ]
})
installCardRemovalCleanup(router)
createApp(App).component('AetherHanText', HanText).directive('tab-scroll', tabScroll).use(router).mount('#app')
