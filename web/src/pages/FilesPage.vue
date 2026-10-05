<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, state, bytes, notify } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
const preferenceKey = `aether-files:${state.username}`
let saved = {}
try { saved = JSON.parse(localStorage.getItem(preferenceKey) || '{}') || {} } catch {}
const mode = ref(saved.mode === 'grid' ? 'grid' : 'list'), favoritesOpen = ref(!!saved.open), favorites = ref(Array.isArray(saved.favorites) ? saved.favorites : [])
watch([mode, favoritesOpen, favorites], () => localStorage.setItem(preferenceKey, JSON.stringify({ mode: mode.value, open: favoritesOpen.value, favorites: favorites.value })), { deep: true })
const route = useRoute()
const selected = ref(route.query.storage || state.storages.find(s => s.enabled)?.id || '')
const current = ref('/'), history = ref([]), files = ref([]), busy = ref(false), error = ref(''), query = ref('')
const visible = computed(() => files.value.filter(f => f.name.toLowerCase().includes(query.value.toLowerCase())).sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name)))
const poolFavorites = computed(() => favorites.value.filter(f => f.storage === selected.value))
const isFavorite = computed(() => poolFavorites.value.some(f => f.id === current.value))
function star() {
  if (!selected.value) return
  if (isFavorite.value) favorites.value = favorites.value.filter(f => !(f.storage === selected.value && f.id === current.value))
  else favorites.value.push({ storage: selected.value, id: current.value, name: history.value.at(-1)?.name || '根目录', history: JSON.parse(JSON.stringify(history.value)) })
}
function jump(index) {
  current.value = index < 0 ? '/' : index === history.value.length - 1 ? current.value : history.value[index + 1].id
  history.value = history.value.slice(0, index + 1); load()
}
function favoriteJump(item) { current.value = item.id; history.value = item.history || []; load() }
let requestId = 0
async function load(refresh = false) {
  const id = ++requestId
  if (!selected.value) { files.value = []; return }
  busy.value = true; error.value = ''
  try {
    const result = await api(`/files?storage=${encodeURIComponent(selected.value)}&path=${encodeURIComponent(current.value)}&refresh=${refresh}`)
    if (id === requestId) files.value = result
  } catch (e) { if (id === requestId) { error.value = e.message; files.value = [] } }
  finally { if (id === requestId) busy.value = false }
}
function enter(f) { if (f.isDir) { history.value.push({ id: current.value, name: f.name }); current.value = f.id; load() } }
function back() { current.value = history.value.pop()?.id || '/'; load() }
watch(selected, () => { current.value = '/'; history.value = []; load() }, { immediate: true })
async function copy(f) { try { await navigator.clipboard.writeText(f.url); notify('播放链接已复制') } catch { notify('当前浏览器不允许访问剪贴板', true) } }
</script>
<template>
  <div class="file-toolbar"><button class="icon-btn" aria-label="展开收藏栏" :aria-expanded="favoritesOpen" @click="favoritesOpen = !favoritesOpen"><Icon name="PanelLeft" /></button><RoundedSelect v-model="selected" label="选择存储池" :options="state.storages.filter(s => s.enabled).map(s => ({ value: s.id, label: s.name }))" /><div class="path-bar"><button class="icon-btn" :disabled="!history.length || busy" aria-label="上级目录" @click="back"><Icon name="ArrowUp" /></button><nav class="file-breadcrumbs" aria-label="文件路径"><button :disabled="busy" @click="jump(-1)">根目录</button><template v-for="(entry, index) in history" :key="index"><Icon name="ChevronRight" :size="14" /><button :disabled="busy" @click="jump(index)">{{ entry.name }}</button></template></nav><button class="icon-btn" :aria-label="isFavorite ? '取消收藏目录' : '收藏当前目录'" :aria-pressed="isFavorite" :disabled="!selected" @click="star"><Icon name="Star" /></button></div><button class="icon-btn bordered" aria-label="刷新目录" :disabled="busy || !selected" @click="load(true)"><Icon name="RefreshCw" :class="{ spin: busy }" /></button><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="搜索当前目录" placeholder="搜索当前目录…" /></div><button class="icon-btn" :aria-label="mode === 'list' ? '当前列表视图，切换网格' : '当前网格视图，切换列表'" @click="mode = mode === 'list' ? 'grid' : 'list'"><Icon :name="mode === 'list' ? 'List' : 'LayoutGrid'" /></button></div>
  <div class="file-workspace" :class="{ 'with-favorites': favoritesOpen }">
  <aside v-if="favoritesOpen" class="file-favorites" aria-label="目录收藏"><h3>收藏目录</h3><div v-for="item in poolFavorites" :key="item.id"><button @click="favoriteJump(item)"><Icon name="Folder" /><span>{{ item.name }}</span></button><button class="icon-btn" :aria-label="`移除收藏 ${item.name}`" @click="favorites = favorites.filter(f => f !== item)"><Icon name="X" :size="14" /></button></div><p v-if="!poolFavorites.length" class="muted">暂无收藏</p></aside>
  <section class="file-view">
  <div v-if="error" class="error-message" role="alert">{{ error }}</div>
  <div v-if="busy" class="empty-state"><Icon name="LoaderCircle" class="spin" :size="30" /><p>正在读取目录…</p></div>
  <div v-else-if="!selected || !visible.length" class="empty-state"><span class="empty-icon"><Icon name="FolderOpen" :size="36" /></span><h3>{{ !selected ? '尚未连接存储' : '目录为空' }}</h3><button v-if="!selected" class="btn" @click="$router.push('/storage')">前往存储管理</button></div>
  <div v-else-if="mode === 'grid'" class="file-grid"><article v-for="f in visible" :key="f.id" class="file-grid-item"><button :disabled="!f.isDir" class="file-grid-name" @click="enter(f)"><Icon :name="f.isDir ? 'Folder' : 'FileVideo'" :size="38" :class="{ 'folder-color': f.isDir }" /><strong :title="f.name">{{ f.name }}</strong></button><small>{{ f.isDir ? '文件夹' : bytes(f.size) }}</small><div v-if="!f.isDir" class="row-actions"><button class="icon-btn" aria-label="复制播放链接" @click="copy(f)"><Icon name="Link" /></button><a class="icon-btn" :href="f.url" target="_blank" rel="noreferrer" aria-label="下载文件"><Icon name="Download" /></a></div></article></div>
  <div v-else class="table-wrap"><table><thead><tr><th>名称</th><th>大小</th><th>类型</th><th>修改时间</th><th class="right">操作</th></tr></thead><tbody><tr v-for="f in visible" :key="f.id"><td><button class="file-name" :disabled="!f.isDir" @click="enter(f)"><Icon :name="f.isDir ? 'Folder' : 'FileVideo'" :class="{ 'folder-color': f.isDir }" :size="21" /><strong>{{ f.name }}</strong></button></td><td>{{ f.isDir ? '—' : bytes(f.size) }}</td><td>{{ f.isDir ? '文件夹' : f.name.split('.').at(-1).toUpperCase() }}</td><td>{{ f.modified?.startsWith('0001') ? '—' : new Date(f.modified).toLocaleString('zh-CN') }}</td><td><div v-if="!f.isDir" class="row-actions"><button class="icon-btn" aria-label="复制播放链接" @click="copy(f)"><Icon name="Link" :size="16" /></button><a class="icon-btn" :href="f.url" target="_blank" rel="noreferrer" aria-label="下载文件"><Icon name="Download" :size="16" /></a></div></td></tr></tbody></table></div>
  </section></div>
  <div class="page-foot"><span>{{ visible.length }} 个项目</span><span>目录缓存 {{ state.settings.cacheEnabled ? '已开启' : '已关闭' }}</span></div>
</template>
