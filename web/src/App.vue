<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, state, reload, notify, notices, bytes } from './lib'
import Icon from './components/Icon.vue'
import ThinScroll from './components/ThinScroll.vue'
import AudioPlayer from './components/AudioPlayer.vue'
import { audioSession, closeAudio } from './audio-session'
import { embyNoticeDisplay, noticeTime } from './library-notices'
import ShareTransfer from './components/ShareTransfer.vue'
import { watchShareClipboard } from './share-clipboard'
import { replacementJobs, refreshReplacementNotices } from './replacement-notices'
import { recentNotices, noticeResult } from './task-notices'
import ProviderIcon from './components/ProviderIcon.vue'
import TaskTabs from './components/TaskTabs.vue'
import FileTabs from './components/FileTabs.vue'
import LoginPage from './pages/LoginPage.vue'
import StoragePage from './pages/StoragePage.vue'
import TasksPage from './pages/TasksPage.vue'
import AutomationPage from './pages/AutomationPage.vue'
import SettingsPage from './pages/SettingsPage.vue'
import FilesPage from './pages/FilesPage.vue'
import DashboardPage from './pages/DashboardPage.vue'
import LogsPage from './pages/LogsPage.vue'
import PlannedPage from './pages/PlannedPage.vue'
import ToolsPage from './pages/ToolsPage.vue'
import LinksPage from './pages/LinksPage.vue'
import OverflowTooltip from './components/OverflowTooltip.vue'
import MountsPage from './pages/MountsPage.vue'
import ScrapePage from './pages/ScrapePage.vue'
import TransfersPage from './pages/TransfersPage.vue'

const route = useRoute(), router = useRouter()
const accountMenu = ref(false), notificationMenu = ref(false), mobileNav = ref(false), connectionError = ref(''), online = ref(true)
const clipboardShare = ref(null)
watch(() => [state.authenticated, state.username], () => closeAudio(), { flush: 'sync' })
let stopClipboard
watch(() => [state.loaded && state.authenticated, state.username], ([ready]) => {
  stopClipboard?.(); clipboardShare.value = null
  if (ready) stopClipboard = watchShareClipboard({ ready: () => state.loaded && state.authenticated, open: share => { clipboardShare.value = share; closeMenus() }, blocked: reason => notify(reason === 'insecure' ? '自动监听剪贴板需要 HTTPS 或 localhost；当前页面可按 Ctrl+V 粘贴分享链接。' : '请允许浏览器读取剪贴板；也可按 Ctrl+V 粘贴分享链接。', true) })
}, { flush: 'post' })
function listenShares() { closeMenus(); window.dispatchEvent(new Event('aether:listen-shares')) }
const theme = ref(localStorage.getItem('aether-theme') || 'light')
const themes = [{ id: 'light', label: '日光', icon: 'Sun' }, { id: 'dark', label: '夜间', icon: 'Moon' }, { id: 'system', label: '跟随系统', icon: 'Monitor' }]
const activeTheme = computed(() => themes.find(t => t.id === theme.value) || themes[0])
function cycleTheme() { theme.value = themes[(themes.indexOf(activeTheme.value) + 1) % themes.length].id }
const media = window.matchMedia('(prefers-color-scheme: dark)')
function applyTheme() { document.documentElement.dataset.theme = theme.value === 'system' ? media.matches ? 'dark' : 'light' : theme.value }
watch(theme, () => { localStorage.setItem('aether-theme', theme.value); applyTheme() })
applyTheme()
const navigation = [
  { path: '/dashboard', label: '仪表盘', icon: 'Gauge' }, { path: '/files', label: '文件服务', icon: 'FolderOpen' },
  { path: '/storage', label: '存储管理', icon: 'HardDrive' }, { path: '/transfer', label: '传输中心', icon: 'ArrowLeftRight' },
  { path: '/tasks', label: '任务管理', icon: 'ListTodo' }, { path: '/links', label: '以太链接', icon: 'Waypoints' },
  { path: '/tools', label: '辅助工具', icon: 'Wrench' }, { path: '/logs', label: '系统日志', icon: 'ScrollText' },
  { path: '/settings', label: '系统设置', icon: 'Settings' }
]
const currentPath = computed(() => route.path === '/' ? '/storage' : route.path === '/tasks' ? '/tasks/strm' : route.path)
const activeNav = computed(() => navigation.find(n => currentPath.value === n.path || currentPath.value.startsWith(n.path + '/')))
const title = computed(() => activeNav.value?.label || '以太')
const planned = computed(() => ({
  '/backup': { title: '备份中心', icon: 'ArchiveRestore', items: ['备份计划', '备份历史', '恢复与校验'] },
  '/files/mounts': { title: '本地挂载', icon: 'CloudDownload', items: ['FUSE 挂载管理', '挂载点状态'] },
  '/links': { title: '以太链接', icon: 'Waypoints', items: ['Audiobookshelf 反向代理', 'Emby 反向代理', '飞牛影视反向代理'] },
  '/tasks/organize': { title: '目录整理', icon: 'FolderTree', items: ['目录整理任务', '整理规则与预览'] },
  '/tasks/scrape': { title: 'STRM 刮削', icon: 'ScanSearch', items: ['媒体识别', 'TMDB 元数据', 'NFO 与封面'] }
}[currentPath.value]))
const dismissedKeys = ref([])
const taskNotices = computed(() => recentNotices([
  ...state.tasks.filter(t => ['success', 'error', 'cancelled', 'interrupted'].includes(t.status) && t.lastRun && !t.lastRun.startsWith('0001')).map(t => ({ ...t, message: noticeResult(t), provider: state.storages.find(s => s.id === t.storageId)?.type, taskIcon: ({cache:'Database',strm:'FileVideo',cas:'Layers3',ed2k:'Link',organize:'FolderTree'})[t.kind] || 'ListTodo', key: `${t.id}:${t.lastRun}:${t.status}` })),
  ...(state.libraryNotices || []).map(n => ({ id: n.id, key: `emby:${n.id}:${n.time}`, ...embyNoticeDisplay(n), lastRun: n.time, status: 'success', kind: 'emby', logQuery: `[${n.id}]`, legacyQuery: n.series || n.taskName || n.name })),
  ...replacementJobs.value.map(n => ({ key: `replace:${n.id}:${n.status}`, id: n.id, name: 'STRM 替换', lastRun: n.updatedAt || n.time, status: n.status === 'completed' ? 'success' : n.status === 'failed' ? 'error' : 'running', kind: 'replace', message: `${n.status === 'running' ? '进行中' : n.status === 'completed' ? '已完成' : '失败'} · 已替换 ${n.changed} 个文件${n.error ? ` · ${n.error}` : ''}` }))
].filter(n => !dismissedKeys.value.includes(n.key))))
const readKeys = ref([])
watch(() => state.username, user => {
  try { const saved = JSON.parse(localStorage.getItem(`aether-read:${user}`) || '[]'); readKeys.value = Array.isArray(saved) ? saved : [] } catch { readKeys.value = [] }
  try { const saved = JSON.parse(localStorage.getItem(`aether-dismissed:${user}`) || '[]'); dismissedKeys.value = Array.isArray(saved) ? saved.slice(-500) : [] } catch { dismissedKeys.value = [] }
}, { immediate: true })
function clearNotifications() {
  dismissedKeys.value = [...new Set([...dismissedKeys.value, ...taskNotices.value.map(n => n.key)])].slice(-500)
  localStorage.setItem(`aether-dismissed:${state.username}`, JSON.stringify(dismissedKeys.value))
}
const unread = computed(() => taskNotices.value.filter(t => !readKeys.value.includes(t.key)).length)
function markRead() {
  readKeys.value = taskNotices.value.map(t => t.key)
  localStorage.setItem(`aether-read:${state.username}`, JSON.stringify(readKeys.value))
}
function openNotifications() { notificationMenu.value = !notificationMenu.value; accountMenu.value = false; if (notificationMenu.value) markRead() }
function openNotice(t) {
  router.push({ path: '/logs', query: { module: t.kind === 'emby' ? 'links' : t.kind === 'replace' ? 'files' : 'tasks', notice: t.id, q: t.logQuery || t.name, fallback: t.legacyQuery || t.name, time: t.lastRun } })
  closeMenus()
}
watch(taskNotices, () => { if (notificationMenu.value) markRead() })
let timer
let trafficTimer, trafficRequest = false, trafficGeneration = 0
const trafficRates = ref(null)
function rateText(value) { return value == null ? '--' : `${bytes(value)}/s` }
async function refreshTraffic() {
  if (!state.authenticated || trafficRequest) return
  const generation = trafficGeneration
  trafficRequest = true
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 3000)
  try {
    const response = await fetch('/api/traffic', { credentials: 'same-origin', signal: controller.signal, cache: 'no-store' })
    if (!response.ok) throw new Error('traffic unavailable')
    const data = await response.json()
    if (generation === trafficGeneration) trafficRates.value = data
  } catch { if (generation === trafficGeneration) trafficRates.value = null }
  finally { clearTimeout(timeout); trafficRequest = false }
}
watch(() => state.authenticated, () => { trafficGeneration++; trafficRates.value = null; refreshTraffic() })
async function bootstrap() {
  connectionError.value = ''
  try { Object.assign(state, await api('/auth/status')); await reload(); refreshReplacementNotices(); state.loaded = true; online.value = true }
  catch (e) { connectionError.value = e.message; online.value = false }
}
async function logout() {
  try { await api('/auth/logout', 'POST'); state.authenticated = false; closeMenus() } catch (e) { notify(e.message, true) }
}
function closeMenus() { accountMenu.value = false; notificationMenu.value = false }
function escapeMenus(event) { if (event.key === 'Escape') closeMenus() }
watch(() => route.fullPath, () => { mobileNav.value = false; closeMenus() })
onMounted(() => {
  bootstrap(); media.addEventListener('change', applyTheme)
  trafficTimer = setInterval(refreshTraffic, 1000)
  document.addEventListener('click', closeMenus); document.addEventListener('keydown', escapeMenus)
  timer = setInterval(async () => { if (state.authenticated) { refreshReplacementNotices(); try { await reload(); online.value = true } catch { online.value = false } } }, 5000)
})
onUnmounted(() => { stopClipboard?.(); trafficGeneration++; clearInterval(trafficTimer); clearInterval(timer); media.removeEventListener('change', applyTheme); document.removeEventListener('click', closeMenus); document.removeEventListener('keydown', escapeMenus) })
</script>
<template>
  <OverflowTooltip />
  <div v-if="!state.loaded" class="boot-screen"><img src="/aether.svg" alt="Aether" /><h2>Aether 以太</h2><p v-if="connectionError" class="error-message">{{ connectionError }}</p><button v-if="connectionError" class="btn" @click="bootstrap">重新连接</button><p v-else>正在连接…</p></div>
  <LoginPage v-else-if="!state.authenticated" />
  <div v-else class="app-shell">
    <div v-if="mobileNav" class="nav-overlay" @click="mobileNav = false" />
    <aside class="sidebar" :class="{ open: mobileNav }">
      <div class="brand"><img src="/aether.svg" alt="" /><span>Aether<small>云端本地 · 以太空间</small></span></div>
      <nav aria-label="主导航"><div class="nav-group"><button v-for="item in navigation" :key="item.path" role="link" :aria-current="activeNav?.path === item.path ? 'page' : undefined" :class="{ active: activeNav?.path === item.path, 'nav-bottom': item.path === '/logs' }" @click="router.push(item.path)"><Icon :name="item.icon" :size="22" /><span>{{ item.label }}</span></button></div></nav>
    </aside>
    <div class="main-shell">
      <header class="topbar">
        <button class="icon-btn mobile-menu" aria-label="打开导航" @click="mobileNav = !mobileNav"><Icon name="Menu" /></button>
        <div class="breadcrumbs"><span>Aether</span><Icon name="ChevronRight" :size="16" /><strong>{{ title }}</strong></div>
        <div class="topbar-actions">
          <div class="traffic-stat upload" aria-label="上传速率"><Icon name="ArrowUp" /><b>{{ rateText(trafficRates?.uploadRate) }}</b></div>
          <div class="traffic-stat download" aria-label="下载速率"><Icon name="ArrowDown" /><b>{{ rateText(trafficRates?.downloadRate) }}</b></div>
          <button class="icon-btn theme-toggle" :aria-label="`主题：${activeTheme.label}`" @click="cycleTheme"><Icon :name="activeTheme.icon" :size="22" /></button>
          <div class="notification-control" @click.stop>
            <button class="icon-btn notification-button" aria-label="任务通知" :aria-expanded="notificationMenu" @click="openNotifications"><Icon name="Bell" :size="22" /><span v-if="unread" class="notification-badge">{{ unread > 99 ? '99+' : unread }}</span></button>
            <section v-if="notificationMenu" class="notification-dropdown" aria-label="任务通知列表"><header><h2>最近通知</h2><button class="icon-btn notice-clear" aria-label="清除通知" :disabled="!taskNotices.length" @click="clearNotifications"><Icon name="Trash2" :size="15" /></button></header><p v-if="!taskNotices.length" class="small-empty">暂无通知</p><ThinScroll v-else class="notice-scroll" content-class="notification-list" :thickness="2"><button v-for="t in taskNotices" :key="t.key" @click="openNotice(t)"><span v-if="t.taskIcon" class="notice-provider"><ProviderIcon :type="t.provider" /><Icon :name="t.taskIcon" /></span><Icon v-else :name="t.kind === 'emby' ? 'EmbyNotice' : t.status === 'running' ? 'LoaderCircle' : t.status === 'success' ? 'CircleCheck' : 'CircleAlert'" :class="t.status === 'success' ? 'success-text' : t.status === 'running' ? 'spin' : 'danger-text'" /><span><strong>{{ t.name }}</strong><small>{{ t.message || (t.status === 'success' ? '已完成' : t.status === 'error' ? '执行失败' : '已停止或中断') }} · <time :datetime="t.lastRun">{{ noticeTime(t.lastRun) }}</time></small></span></button></ThinScroll></section>
          </div>
          <div class="account-control" @click.stop>
            <button class="account-button" aria-label="账号菜单" :aria-expanded="accountMenu" @click="accountMenu = !accountMenu; notificationMenu = false"><Icon name="UserRound" :size="22" /></button>
            <div v-if="accountMenu" class="account-dropdown"><button @click="router.push('/settings/account'); closeMenus()"><Icon name="UserRound" :size="22" />账号设置</button><button @click="listenShares"><Icon name="Share2" :size="22" />监听分享链接</button><button @click="router.push('/settings/about'); closeMenus()"><Icon name="Info" :size="22" />关于以太</button><button @click="logout"><Icon name="LogOut" :size="22" />退出登录</button></div>
          </div>
        </div>
      </header>
      <ThinScroll class="page-scroll" :thickness="currentPath.includes('scrape') ? 2 : 1" :key="currentPath">
      <main class="page-content">
        <div v-if="!online" class="error-message">服务连接已中断，正在重试…</div>
        <FileTabs v-if="currentPath === '/files/webdav'" />
        <section v-if="currentPath.startsWith('/tasks/') && !['/tasks/strm', '/tasks/cas', '/tasks/ed2k', '/tasks/cache', '/tasks/cache/settings', '/tasks/scrape', '/tasks/automation'].includes(currentPath)" class="task-heading"><TaskTabs /></section>
        <StoragePage v-if="currentPath === '/storage'" />
        <TransfersPage v-else-if="currentPath.startsWith('/transfer') || currentPath === '/backup'" :rules="currentPath === '/transfer/backup' || currentPath === '/backup'" />
        <DashboardPage v-else-if="currentPath === '/dashboard'" />
        <FilesPage v-else-if="currentPath === '/files'" />
        <MountsPage v-else-if="currentPath === '/files/mounts'" />
        <TasksPage v-else-if="['/tasks/strm', '/tasks/cache', '/tasks/cas', '/tasks/ed2k'].includes(currentPath)" :kind="currentPath.split('/').at(-1)" />
        <ScrapePage v-else-if="currentPath === '/tasks/scrape'" />
        <AutomationPage v-else-if="currentPath === '/tasks/automation'" />
        <SettingsPage v-else-if="currentPath === '/tasks/cache/settings'" section="cache" />
        <SettingsPage v-else-if="currentPath === '/files/webdav'" section="webdav" />
        <SettingsPage v-else-if="currentPath.startsWith('/settings')" :section="currentPath.endsWith('about') ? 'about' : currentPath.endsWith('logs') ? 'logs' : 'account'" />
        <LinksPage v-else-if="['/links/manage', '/links/cache'].includes(currentPath)" />
        <ToolsPage v-else-if="currentPath === '/tools'" />
        <LogsPage v-else-if="currentPath === '/logs'" />
        <PlannedPage v-else-if="planned" v-bind="planned" />
        <div v-else class="empty-state"><h1>页面不存在</h1><RouterLink class="btn" to="/storage">返回存储管理</RouterLink></div>
      </main>
      </ThinScroll>
    </div>
  </div>
  <ShareTransfer v-if="clipboardShare && state.authenticated" :provider="clipboardShare.provider" :initial-links="clipboardShare.links" @close="clipboardShare = null" />
  <AudioPlayer v-if="audioSession.file && state.authenticated" :file="audioSession.file" :queue="audioSession.queue" :activation="audioSession.activation" @change="audioSession.file = $event" @close="closeAudio" />
  <TransitionGroup name="toast-slide" tag="div" class="toast-stack" aria-live="polite"><div v-for="n in notices" :key="n.id" class="toast" :class="{ error: n.error }" :role="n.error ? 'alert' : 'status'"><span class="toast-symbol"><Icon :name="n.error ? 'X' : n.progress !== undefined && n.progress < 1 ? 'LoaderCircle' : 'Check'" :size="15" /></span><span>{{ n.message }}<progress v-if="n.progress !== undefined" :value="n.progress" max="1" aria-label="转存提交进度" style="display:block;width:100%;height:4px;margin-top:6px;accent-color:var(--primary)" /></span><button class="icon-btn" aria-label="关闭通知" @click="notices.splice(notices.indexOf(n), 1)"><Icon name="X" :size="15" /></button></div></TransitionGroup>
</template>
