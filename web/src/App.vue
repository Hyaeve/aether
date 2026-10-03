<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, state, reload, notify, notices, bytes } from './lib'
import Icon from './components/Icon.vue'
import LoginPage from './pages/LoginPage.vue'
import StoragePage from './pages/StoragePage.vue'
import TasksPage from './pages/TasksPage.vue'
import SettingsPage from './pages/SettingsPage.vue'
import FilesPage from './pages/FilesPage.vue'
import DashboardPage from './pages/DashboardPage.vue'
import LogsPage from './pages/LogsPage.vue'
import PlannedPage from './pages/PlannedPage.vue'

const route = useRoute(), router = useRouter()
const accountMenu = ref(false), mobileNav = ref(false), connectionError = ref(''), online = ref(true)
const theme = ref(localStorage.getItem('aether-theme') || 'light')
const media = window.matchMedia('(prefers-color-scheme: dark)')
function applyTheme() { document.documentElement.dataset.theme = theme.value === 'system' ? media.matches ? 'dark' : 'light' : theme.value }
watch(theme, () => { localStorage.setItem('aether-theme', theme.value); applyTheme() })
applyTheme()
const navGroups = [
  { name: '工作空间', items: [{ path: '/dashboard', label: '仪表盘', icon: 'LayoutDashboard' }, { path: '/files', label: '文件管理', icon: 'FolderOpen' }, { path: '/storage', label: '存储管理', icon: 'HardDrive' }, { path: '/backup', label: '备份中心', icon: 'ArchiveRestore' }] },
  { name: '服务与连接', items: [{ path: '/webdav', label: 'WebDAV 服务', icon: 'Network' }, { path: '/mounts', label: '本地挂载', icon: 'Monitor' }, { path: '/links', label: '以太链接', icon: 'Waypoints' }] },
  { name: '任务与工具', items: [{ path: '/tasks/strm', label: 'STRM 任务', icon: 'FileVideo' }, { path: '/tasks/cache', label: '缓存任务', icon: 'Database' }, { path: '/tasks/organize', label: '目录整理', icon: 'FolderTree' }, { path: '/tasks/scrape', label: 'STRM 刮削', icon: 'ScanSearch' }, { path: '/tools', label: '辅助工具', icon: 'Wrench' }] },
  { name: '系统', items: [{ path: '/logs', label: '系统日志', icon: 'ScrollText' }, { path: '/settings', label: '系统设置', icon: 'Settings2' }] }
]
const currentPath = computed(() => route.path === '/' ? '/storage' : route.path)
const activeNav = computed(() => navGroups.flatMap(g => g.items).filter(n => currentPath.value === n.path || currentPath.value.startsWith(n.path + '/')).at(-1))
const title = computed(() => activeNav.value?.label || '工作空间')
const group = computed(() => navGroups.find(g => g.items.some(i => i.path === activeNav.value?.path))?.name || '工作空间')
const toolItems = ['115 STRM 增强', '115 分享 STRM', '夸克 STRM 接管', '整理规则', '二级分类', '洗版策略', 'AI 辅助识别', '识别规则：最小视频、整理黑名单、自定义识别词、自定义匹配', 'TMDB 配置', '代理配置']
const planned = computed(() => ({
  '/backup': { title: '备份中心', icon: 'ArchiveRestore', items: ['备份计划', '备份历史', '恢复与校验'] },
  '/mounts': { title: '本地挂载', icon: 'Monitor', items: ['FUSE 挂载管理', '挂载点状态'] },
  '/links': { title: '以太链接', icon: 'Waypoints', items: ['Audiobookshelf 反向代理', 'Emby 反向代理', '飞牛影视反向代理'] },
  '/tasks/organize': { title: '目录整理', icon: 'FolderTree', items: ['目录整理任务', '整理规则与预览'] },
  '/tasks/scrape': { title: 'STRM 刮削', icon: 'ScanSearch', items: ['媒体识别', 'TMDB 元数据', 'NFO 与封面'] },
  '/tools': { title: '辅助工具', icon: 'Wrench', items: toolItems }
}[currentPath.value]))
let timer
async function bootstrap() {
  connectionError.value = ''
  try { Object.assign(state, await api('/auth/status')); await reload(); state.loaded = true; online.value = true }
  catch (e) { connectionError.value = e.message; online.value = false }
}
async function logout() {
  try { await api('/auth/logout', 'POST'); state.authenticated = false; accountMenu.value = false } catch (e) { notify(e.message, true) }
}
function closeMenus() { accountMenu.value = false }
watch(() => route.fullPath, () => { mobileNav.value = false; accountMenu.value = false })
onMounted(() => {
  bootstrap()
  media.addEventListener('change', applyTheme)
  document.addEventListener('click', closeMenus)
  timer = setInterval(async () => { if (state.authenticated) { try { await reload(); online.value = true } catch { online.value = false } } }, 5000)
})
onUnmounted(() => { clearInterval(timer); media.removeEventListener('change', applyTheme); document.removeEventListener('click', closeMenus) })
</script>
<template>
  <div v-if="!state.loaded" class="boot-screen"><img src="/aether.svg" alt="Aether" /><h2>Aether 以太</h2><p v-if="connectionError" class="error-message">{{ connectionError }}</p><button v-if="connectionError" class="btn" @click="bootstrap">重新连接</button><p v-else>正在连接工作空间…</p></div>
  <LoginPage v-else-if="!state.authenticated" />
  <div v-else class="app-shell">
    <div v-if="mobileNav" class="nav-overlay" @click="mobileNav = false" />
    <aside class="sidebar" :class="{ open: mobileNav }">
      <RouterLink to="/dashboard" class="brand"><img src="/aether.svg" alt="" /><span>Aether<small>以太 · 存储工作空间</small></span></RouterLink>
      <div class="workspace-switch"><span class="workspace-avatar">A</span><span>我的以太空间<small>个人存储控制台</small></span><Icon name="ChevronsUpDown" :size="15" /></div>
      <nav aria-label="主导航"><section v-for="g in navGroups" :key="g.name" class="nav-group"><h2>{{ g.name }}</h2><RouterLink v-for="item in g.items" :key="item.path" :to="item.path" :class="{ active: activeNav?.path === item.path }"><Icon :name="item.icon" :size="19" /><span>{{ item.label }}</span><span v-if="item.path === '/storage' && state.storages.length" class="nav-count">{{ state.storages.length }}</span><span v-if="activeNav?.path === item.path" class="active-mark" /></RouterLink></section></nav>
      <div class="sidebar-foot"><span><i :class="{ offline: !online }" />{{ online ? '以太服务已连接' : '连接已中断' }}</span><small>v0.1.0</small></div>
    </aside>
    <div class="main-shell">
      <header class="topbar"><button class="icon-btn mobile-menu" aria-label="打开导航" @click="mobileNav = !mobileNav"><Icon name="Menu" /></button><div class="breadcrumbs"><span>{{ group }}</span><Icon name="ChevronRight" :size="14" /><strong>{{ title }}</strong></div><div class="topbar-actions"><div class="traffic-stat upload"><Icon name="ArrowUp" :size="14" /><span>上传 <b>{{ bytes(state.traffic.uploaded) }}</b></span></div><div class="traffic-stat download"><Icon name="ArrowDown" :size="14" /><span>下载 <b>{{ bytes(state.traffic.downloaded) }}</b></span></div><div class="theme-switch"><button v-for="t in [{ id: 'light', label: '日光', icon: 'Sun' }, { id: 'dark', label: '夜间', icon: 'Moon' }, { id: 'system', label: '系统', icon: 'Monitor' }]" :key="t.id" class="icon-btn" :class="{ selected: theme === t.id }" :title="t.label" :aria-label="t.label" :aria-pressed="theme === t.id" @click="theme = t.id"><Icon :name="t.icon" :size="17" /></button></div><div class="account-control" @click.stop><button class="account-button" title="账户菜单" aria-label="账户菜单" :aria-expanded="accountMenu" @click="accountMenu = !accountMenu"><Icon name="UserRound" :size="19" /></button><div v-if="accountMenu" class="account-dropdown"><div>{{ state.username }}<small>管理员</small></div><button @click="router.push('/settings/account')"><Icon name="Settings2" :size="16" />账户设置</button><button @click="logout"><Icon name="LogOut" :size="16" />退出登录</button></div></div></div></header>
      <div class="page-tabs"><div class="current-tab"><Icon :name="activeNav?.icon || 'HardDrive'" :size="15" />{{ title }}<span /></div><span class="page-tabs-caption">AETHER WORKSPACE</span></div>
      <main class="page-content" :key="currentPath">
        <div v-if="!online" class="error-message">服务连接已中断，正在重试…</div>
        <StoragePage v-if="currentPath === '/storage'" />
        <DashboardPage v-else-if="currentPath === '/dashboard'" />
        <FilesPage v-else-if="currentPath === '/files'" />
        <TasksPage v-else-if="['/tasks/strm', '/tasks/cache'].includes(currentPath)" :kind="currentPath.endsWith('cache') ? 'cache' : 'strm'" />
        <SettingsPage v-else-if="currentPath === '/tasks/cache/settings'" section="cache" />
        <SettingsPage v-else-if="currentPath === '/webdav'" section="webdav" />
        <SettingsPage v-else-if="currentPath.startsWith('/settings')" :section="currentPath.endsWith('account') ? 'account' : 'general'" />
        <LogsPage v-else-if="currentPath === '/logs'" />
        <PlannedPage v-else-if="planned" v-bind="planned" />
        <div v-else class="empty-state"><h1>页面不存在</h1><RouterLink class="btn" to="/storage">返回存储管理</RouterLink></div>
      </main>
    </div>
  </div>
  <div class="toast-stack" aria-live="polite"><div v-for="n in notices" :key="n.id" class="toast" :class="{ error: n.error }"><Icon :name="n.error ? 'CircleAlert' : 'CircleCheck'" :size="19" /><span>{{ n.message }}</span><button class="icon-btn" aria-label="关闭通知" @click="notices.splice(notices.indexOf(n), 1)"><Icon name="X" :size="15" /></button></div></div>
</template>
