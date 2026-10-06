<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, state, bytes, notify } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import Modal from '../components/Modal.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import { copyText } from '../clipboard'
const preferenceKey = `aether-files:${state.username}`
let saved = {}
try { saved = JSON.parse(localStorage.getItem(preferenceKey) || '{}') || {} } catch {}
const mode = ref(saved.mode === 'grid' ? 'grid' : 'list'), favoritesOpen = ref(!!saved.open), favorites = ref(Array.isArray(saved.favorites) ? saved.favorites : [])
watch([mode, favoritesOpen, favorites], () => localStorage.setItem(preferenceKey, JSON.stringify({ mode: mode.value, open: favoritesOpen.value, favorites: favorites.value })), { deep: true })
const route = useRoute()
const selected = ref(route.query.storage || state.storages.find(s => s.enabled)?.id || '')
const current = ref('/'), history = ref([]), files = ref([]), busy = ref(false), error = ref(''), query = ref('')
const searchInput = ref('')
const sortKey = ref('name'), ascending = ref(true), selection = ref([]), anchor = ref(''), menu = ref(null), renameID = ref(''), newName = ref(''), operation = ref(''), deleting = ref(false)
const columns = [{ key: 'name', label: '名称' }, { key: 'size', label: '大小' }, { key: 'type', label: '类型' }, { key: 'modified', label: '修改时间' }]
const type = f => f.isDir ? '文件夹' : f.name.split('.').at(-1).toUpperCase()
const visible = computed(() => files.value.filter(f => f.name.toLowerCase().includes(query.value.toLowerCase())).sort((a, b) => {
  const value = f => sortKey.value === 'type' ? type(f) : sortKey.value === 'size' ? f.size : f[sortKey.value] || ''
  const av = value(a), bv = value(b)
  return (ascending.value ? 1 : -1) * (typeof av === 'number' ? av - bv : String(av).localeCompare(String(bv), 'zh-CN', { numeric: true }))
}))
const targetPools = computed(() => state.storages.filter(s => s.enabled && s.type === state.storages.find(s => s.id === selected.value)?.type))
function sort(key) { ascending.value = key === sortKey.value ? !ascending.value : true; sortKey.value = key }
function select(event, f) {
  if (event.target.closest('input')) return
  if (event.shiftKey && anchor.value) {
    const a = visible.value.findIndex(x => x.id === anchor.value), b = visible.value.indexOf(f)
    selection.value = visible.value.slice(Math.max(0, Math.min(a,b)), Math.max(a,b)+1).map(x => x.id)
  } else if (event.ctrlKey || event.metaKey) {
    selection.value = selection.value.includes(f.id) ? selection.value.filter(id => id !== f.id) : [...selection.value, f.id]
  } else selection.value = [f.id]
  anchor.value = f.id
}
function context(event, f) {
  if (!selection.value.includes(f.id)) selection.value = [f.id]
  menu.value = { x: Math.max(8,Math.min(event.clientX,window.innerWidth-160)), y: Math.max(8,Math.min(event.clientY,window.innerHeight-205)) }
}
function closeMenu() { menu.value = null }
async function rename() {
  closeMenu()
  const f = files.value.find(f => f.id === selection.value[0])
  if (!f || selection.value.length !== 1) return
  renameID.value = f.id; newName.value = f.name
  await nextTick(); document.querySelector('.file-rename')?.focus(); document.querySelector('.file-rename')?.select()
}
async function act(action, extra = {}) {
  busy.value = true; closeMenu()
  try { await api('/files/action', 'POST', { storageId: selected.value, source: current.value, action, ids: [...selection.value], ...extra }); renameID.value = ''; operation.value = ''; deleting.value = false; selection.value = []; await load(true); notify('文件操作完成') }
  catch (e) { notify(e.message, true); await load(true) } finally { busy.value = false }
}
function keys(e) {
  if (e.key === 'Escape') { closeMenu(); renameID.value = '' }
  if (e.key === 'F2' && !document.querySelector('.modal') && !['INPUT','TEXTAREA'].includes(e.target.tagName)) { e.preventDefault(); rename() }
}
onMounted(() => { document.addEventListener('click', closeMenu); document.addEventListener('keydown', keys) })
onUnmounted(() => { document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', keys); requestId++ })
const poolFavorites = computed(() => favorites.value.filter(f => f.storage === selected.value && f.id !== '/' && f.history?.length))
const isFavorite = computed(() => poolFavorites.value.some(f => f.id === current.value))
function star() {
  if (!selected.value || !history.value.length || current.value === '/') return
  if (isFavorite.value) favorites.value = favorites.value.filter(f => !(f.storage === selected.value && f.id === current.value))
  else favorites.value.push({ storage: selected.value, id: current.value, name: history.value.at(-1)?.name || '根目录', history: JSON.parse(JSON.stringify(history.value)) })
}
function jump(index) {
  selection.value = []; renameID.value = ''
  current.value = index < 0 ? '/' : index === history.value.length - 1 ? current.value : history.value[index + 1].id
  history.value = history.value.slice(0, index + 1); load()
}
function favoriteJump(item) { selection.value = []; current.value = item.id; history.value = item.history || []; load() }
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
function enter(f) { if (renameID.value) return; if (f.isDir) { query.value = ''; searchInput.value = ''; selection.value = []; history.value.push({ id: current.value, name: f.name }); current.value = f.id; load() } }
watch(selected, () => { current.value = '/'; history.value = []; selection.value = []; renameID.value = ''; load() }, { immediate: true })
async function copy(f) { try { await copyText(f.url); notify('播放链接已复制') } catch { notify('当前浏览器不允许访问剪贴板', true) } }
</script>
<template>
  <div class="file-browser">
  <div class="file-toolbar"><button class="icon-btn" aria-label="展开收藏栏" :aria-expanded="favoritesOpen" @click="favoritesOpen = !favoritesOpen"><Icon name="PanelLeft" /></button><RoundedSelect v-model="selected" label="选择存储池" :options="state.storages.filter(s => s.enabled).map(s => ({ value: s.id, label: s.name }))" /><div class="path-bar"><nav class="file-breadcrumbs" aria-label="文件路径"><button :disabled="busy" @click="jump(-1)">根目录</button><template v-for="(entry, index) in history" :key="index"><Icon name="ChevronRight" :size="14" /><button :disabled="busy" @click="jump(index)">{{ entry.name }}</button></template></nav></div><button class="icon-btn bordered" aria-label="刷新目录" :disabled="busy || !selected" @click="load(true)"><Icon name="RefreshCw" :class="{ spin: busy }" /></button><div class="search-field"><Icon name="Search" :size="16" /><input v-model="searchInput" @keydown.enter="query = searchInput" aria-label="搜索当前目录" placeholder="搜索当前目录…" /></div><button class="icon-btn" :aria-label="mode === 'list' ? '当前列表视图，切换网格' : '当前网格视图，切换列表'" @click="mode = mode === 'list' ? 'grid' : 'list'"><Icon :name="mode === 'list' ? 'List' : 'LayoutGrid'" /></button></div>
  <div class="file-workspace" :class="{ 'with-favorites': favoritesOpen }">
  <aside class="file-favorites" :inert="!favoritesOpen" :aria-hidden="!favoritesOpen" aria-label="目录收藏"><div class="favorites-heading"><h3>收藏夹</h3><button class="icon-btn" :aria-label="isFavorite ? '取消收藏目录' : '收藏当前目录'" :aria-pressed="isFavorite" :disabled="!selected || !history.length || current === '/'" @click="star"><Icon name="Star" /></button></div><div v-for="item in poolFavorites" :key="item.id" :class="{ active: current === item.id }"><button @click="favoriteJump(item)"><Icon name="Folder" /><span>{{ item.name }}</span></button></div><p v-if="!poolFavorites.length" class="muted">暂无收藏</p></aside>
  <section class="file-view">
  <div v-if="error" class="error-message" role="alert">{{ error }}</div>
  <div v-if="busy" class="empty-state"><Icon name="LoaderCircle" class="spin" :size="30" /><p>正在读取目录…</p></div>
  <div v-else-if="!selected || !visible.length" class="empty-state"><span class="empty-icon"><Icon name="FolderOpen" :size="36" /></span><h3>{{ !selected ? '尚未连接存储' : '目录为空' }}</h3><button v-if="!selected" class="btn" @click="$router.push('/storage')">前往存储管理</button></div>
  <div v-else-if="mode === 'grid'" class="file-grid"><article v-for="f in visible" :key="f.id" class="file-grid-item" :class="{ selected: selection.includes(f.id) }" tabindex="0" @click="select($event,f)" @dblclick="enter(f)" @contextmenu.prevent.stop="context($event,f)" @keydown.enter="enter(f)"><button class="file-grid-name" :aria-label="f.name"><Icon :name="f.isDir ? 'Folder' : 'FileVideo'" :size="38" :class="{ 'folder-color': f.isDir }" /><strong v-if="renameID !== f.id" :data-tooltip="f.name">{{ f.name }}</strong></button><input v-if="renameID === f.id" v-model="newName" class="file-rename" aria-label="新名称" @keydown.enter.stop.prevent="act('rename', { name: newName })" /><small>{{ f.isDir ? '文件夹' : bytes(f.size) }}</small></article></div>
  <div v-else class="table-wrap"><table><thead><tr><th v-for="col in columns" :key="col.key" :aria-sort="sortKey === col.key ? ascending ? 'ascending' : 'descending' : 'none'"><button class="file-sort" @click="sort(col.key)">{{ col.label }}<span class="sort-triangles" :class="{ ascending: sortKey === col.key && ascending, descending: sortKey === col.key && !ascending }"><i /><i /></span></button></th></tr></thead><tbody><tr v-for="f in visible" :key="f.id" :class="{ selected: selection.includes(f.id) }" tabindex="0" @click="select($event,f)" @dblclick="enter(f)" @contextmenu.prevent.stop="context($event,f)" @keydown.enter="enter(f)"><td><input v-if="renameID === f.id" v-model="newName" class="file-rename" aria-label="新名称" @keydown.enter.stop.prevent="act('rename', { name: newName })" /><button v-else class="file-name"><Icon :name="f.isDir ? 'Folder' : 'FileVideo'" :class="{ 'folder-color': f.isDir }" :size="21" /><strong>{{ f.name }}</strong></button></td><td>{{ f.isDir ? '—' : bytes(f.size) }}</td><td>{{ type(f) }}</td><td>{{ !f.modified || f.modified.startsWith('0001') ? '—' : new Date(f.modified).toLocaleString('zh-CN') }}</td></tr></tbody></table></div>
  </section></div></div>
  <Teleport to="body"><div v-if="menu" class="context-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop>
    <button v-if="selection.length === 1" @click="rename"><Icon name="Pencil" />重命名</button>
    <button @click="operation = 'move'; closeMenu()"><Icon name="FolderInput" />移动到</button><button @click="operation = 'copy'; closeMenu()"><Icon name="Copy" />复制到</button>
    <button class="danger-text" @click="deleting = true; closeMenu()"><Icon name="Trash2" />删除</button>
    <button v-if="selection.length === 1 && files.find(f => f.id === selection[0])?.url" @click="copy(files.find(f => f.id === selection[0])); closeMenu()"><Icon name="Link" />复制播放链接</button>
  </div></Teleport>
  <TaskSourcePicker v-if="operation" :storages="targetPools" :storage="selected" @close="operation = ''" @select="act(operation, { targetStorage: $event.storageId, target: $event.source })" />
  <Modal v-if="deleting" title="删除文件" @close="deleting = false"><div class="modal-body">确认删除选中的 {{ selection.length }} 项？将按存储池的删除模式处理。</div><footer class="modal-footer"><button class="btn" @click="deleting = false">取消</button><button class="btn danger" :disabled="busy" @click="act('delete')">确认删除</button></footer></Modal>
</template>
