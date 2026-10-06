<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, state, reload, notify, notices, bytes, date } from './lib'
import Icon from './components/Icon.vue'
import TaskTabs from './components/TaskTabs.vue'
import FileTabs from './components/FileTabs.vue'
import LoginPage from './pages/LoginPage.vue'
import StoragePage from './pages/StoragePage.vue'
import TasksPage from './pages/TasksPage.vue'
import SettingsPage from './pages/SettingsPage.vue'
import FilesPage from './pages/FilesPage.vue'
import DashboardPage from './pages/DashboardPage.vue'
import LogsPage from './pages/LogsPage.vue'
import PlannedPage from './pages/PlannedPage.vue'
import ToolsPage from './pages/ToolsPage.vue'
import LinksPage from './pages/LinksPage.vue'

const route = useRoute(), router = useRouter()
const accountMenu = ref(false), notificationMenu = ref(false), mobileNav = ref(false), connectionError = ref(''), online = ref(true)
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
  { path: '/storage', label: '存储管理', icon: 'HardDrive' }, { path: '/backup', label: '备份中心', icon: 'ArchiveRestore' },
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
const taskNotices = computed(() => state.tasks.filter(t => ['success', 'error', 'cancelled', 'interrupted'].includes(t.status) && t.lastRun && !t.lastRun.startsWith('0001')).map(t => ({ ...t, key: `${t.id}:${t.lastRun}:${t.status}` })).sort((a, b) => new Date(b.lastRun) - new Date(a.lastRun)).slice(0, 30))
const readKeys = ref([])
watch(() => state.username, user => {
  try { const saved = JSON.parse(localStorage.getItem(`aether-read:${user}`) || '[]'); readKeys.value = Array.isArray(saved) ? saved : [] } catch { readKeys.value = [] }
}, { immediate: true })
const unread = computed(() => taskNotices.value.filter(t => !readKeys.value.includes(t.key)).length)
function markRead() {
  readKeys.value = taskNotices.value.map(t => t.key)
  localStorage.setItem(`aether-read:${state.username}`, JSON.stringify(readKeys.value))
}
function openNotifications() { notificationMenu.value = !notificationMenu.value; accountMenu.value = false; if (notificationMenu.value) markRead() }
watch(taskNotices, () => { if (notificationMenu.value) markRead() })
let timer
async function bootstrap() {
  connectionError.value = ''
  try { Object.assign(state, await api('/auth/status')); await reload(); state.loaded = true; online.value = true }
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
  document.addEventListener('click', closeMenus); document.addEventListener('keydown', escapeMenus)
  timer = setInterval(async () => { if (state.authenticated) { try { await reload(); online.value = true } catch { online.value = false } } }, 5000)
})
onUnmounted(() => { clearInterval(timer); media.removeEventListener('change', applyTheme); document.removeEventListener('click', closeMenus); document.removeEventListener('keydown', escapeMenus) })
</script>
<template>
  <div v-if="!state.loaded" class="boot-screen"><img src="/aether.svg" alt="Aether" /><h2>Aether 以太</h2><p v-if="connectionError" class="error-message">{{ connectionError }}</p><button v-if="connectionError" class="btn" @click="bootstrap">重新连接</button><p v-else>正在连接…</p></div>
  <LoginPage v-else-if="!state.authenticated" />
  <div v-else class="app-shell">
    <div v-if="mobileNav" class="nav-overlay" @click="mobileNav = false" />
    <aside class="sidebar" :class="{ open: mobileNav }">
      <button class="brand" @click="router.push('/dashboard')"><img src="/aether.svg" alt="" /><span>Aether<small>云端本地 · 以太空间</small></span></button>
      <nav aria-label="主导航"><div class="nav-group"><button v-for="item in navigation" :key="item.path" role="link" :aria-current="activeNav?.path === item.path ? 'page' : undefined" :class="{ active: activeNav?.path === item.path, 'nav-bottom': item.path === '/logs' }" @click="router.push(item.path)"><Icon :name="item.icon" :size="22" /><span>{{ item.label }}</span></button></div></nav>
    </aside>
    <div class="main-shell">
      <header class="topbar">
        <button class="icon-btn mobile-menu" aria-label="打开导航" @click="mobileNav = !mobileNav"><Icon name="Menu" /></button>
        <div class="breadcrumbs"><span>Aether</span><Icon name="ChevronRight" :size="16" /><strong>{{ title }}</strong></div>
        <div class="topbar-actions">
          <div class="traffic-stat upload"><Icon name="ArrowUp" /><span>上传 <b>{{ bytes(state.traffic.uploaded) }}</b></span></div>
          <div class="traffic-stat download"><Icon name="ArrowDown" /><span>下载 <b>{{ bytes(state.traffic.downloaded) }}</b></span></div>
          <button class="icon-btn theme-toggle" :aria-label="`主题：${activeTheme.label}`" @click="cycleTheme"><Icon :name="activeTheme.icon" :size="22" /></button>
          <div class="notification-control" @click.stop>
            <button class="icon-btn notification-button" aria-label="任务通知" :aria-expanded="notificationMenu" @click="openNotifications"><Icon name="Bell" :size="22" /><span v-if="unread" class="notification-badge">{{ unread > 99 ? '99+' : unread }}</span></button>
            <section v-if="notificationMenu" class="notification-dropdown" aria-label="任务通知列表"><h2>最近任务</h2><p v-if="!taskNotices.length" class="small-empty">暂无已结束任务</p><button v-for="t in taskNotices" :key="t.key" @click="router.push(`/tasks/${['cas', 'cache'].includes(t.kind) ? t.kind : 'strm'}`); closeMenus()"><Icon :name="t.status === 'success' ? 'CircleCheck' : 'CircleAlert'" :class="t.status === 'success' ? 'success-text' : 'danger-text'" /><span><strong>{{ t.name }}</strong><small>{{ t.status === 'success' ? '已完成' : t.status === 'error' ? '执行失败' : '已停止或中断' }} · {{ date(t.lastRun) }}</small></span></button></section>
          </div>
          <div class="account-control" @click.stop>
            <button class="account-button" aria-label="账号菜单" :aria-expanded="accountMenu" @click="accountMenu = !accountMenu; notificationMenu = false"><Icon name="UserRound" :size="22" /></button>
            <div v-if="accountMenu" class="account-dropdown"><button @click="router.push('/settings/account'); closeMenus()"><Icon name="UserRound" :size="22" />账号设置</button><button @click="router.push('/settings/about'); closeMenus()"><Icon name="Info" :size="22" />关于以太</button><button @click="logout"><Icon name="LogOut" :size="22" />退出登录</button></div>
          </div>
        </div>
      </header>
      <main class="page-content" :key="currentPath">
        <div v-if="!online" class="error-message">服务连接已中断，正在重试…</div>
        <FileTabs v-if="currentPath === '/files' || currentPath.startsWith('/files/')" />
        <section v-if="currentPath.startsWith('/tasks/') && !['/tasks/strm', '/tasks/cas', '/tasks/cache'].includes(currentPath)" class="task-heading"><TaskTabs /></section>
        <StoragePage v-if="currentPath === '/storage'" />
        <DashboardPage v-else-if="currentPath === '/dashboard'" />
        <FilesPage v-else-if="currentPath === '/files'" />
        <TasksPage v-else-if="['/tasks/strm', '/tasks/cache', '/tasks/cas'].includes(currentPath)" :kind="currentPath.split('/').at(-1)" />
        <SettingsPage v-else-if="currentPath === '/tasks/cache/settings'" section="cache" />
        <SettingsPage v-else-if="currentPath === '/files/webdav'" section="webdav" />
        <SettingsPage v-else-if="currentPath.startsWith('/settings')" :section="currentPath.endsWith('about') ? 'about' : currentPath.endsWith('logs') ? 'logs' : 'account'" />
        <LinksPage v-else-if="['/links/manage', '/links/cache'].includes(currentPath)" />
        <ToolsPage v-else-if="currentPath === '/tools'" />
        <LogsPage v-else-if="currentPath === '/logs'" />
        <PlannedPage v-else-if="planned" v-bind="planned" />
        <div v-else class="empty-state"><h1>页面不存在</h1><RouterLink class="btn" to="/storage">返回存储管理</RouterLink></div>
      </main>
    </div>
  </div>
  <TransitionGroup name="toast-slide" tag="div" class="toast-stack" aria-live="polite"><div v-for="n in notices" :key="n.id" class="toast" :class="{ error: n.error }" :role="n.error ? 'alert' : 'status'"><span class="toast-symbol"><Icon :name="n.error ? 'X' : 'Check'" :size="15" /></span><span>{{ n.message }}</span><button class="icon-btn" aria-label="关闭通知" @click="notices.splice(notices.indexOf(n), 1)"><Icon name="X" :size="15" /></button></div></TransitionGroup>
</template>
