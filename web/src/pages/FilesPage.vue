<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, state, bytes, notify } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import Modal from '../components/Modal.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import { copyText } from '../clipboard'
import FileTabs from '../components/FileTabs.vue'
import PathBreadcrumbs from '../components/PathBreadcrumbs.vue'
import RenameWorkbench from '../components/RenameWorkbench.vue'
import OfflineDownload from '../components/OfflineDownload.vue'
import ShareTransfer from '../components/ShareTransfer.vue'
import { useVirtualList } from '../virtual-list'
const preferenceKey = `aether-files:${state.username}`
let saved = {}
try { saved = JSON.parse(localStorage.getItem(preferenceKey) || '{}') || {} } catch {}
const viewPreferenceKey = 'aether-files-view'
let savedMode = saved.mode
try { savedMode = localStorage.getItem(viewPreferenceKey) || savedMode } catch {}
try { if (savedMode === 'grid' || savedMode === 'list') localStorage.setItem(viewPreferenceKey, savedMode) } catch {}
const mode = ref(savedMode === 'grid' ? 'grid' : 'list'), favoritesOpen = ref(!!saved.open), favorites = ref(Array.isArray(saved.favorites) ? saved.favorites : [])
watch(mode, value => {
  try { localStorage.setItem(viewPreferenceKey, value) } catch { /* Storage may be disabled by the browser. */ }
}, { flush: 'sync' })
const route = useRoute()
const selected = ref([route.query.storage, saved.storage].find(id => state.storages.some(s => s.enabled && s.id === id)) || state.storages.find(s => s.enabled)?.id || '')
const current = ref('/'), history = ref([]), files = ref([]), busy = ref(false), error = ref(''), query = ref('')
const searchInput = ref('')
const viewport = ref(null), draft = ref(false), confirmRename = ref(false), details = ref(false), createMenu = ref(false)
const toolPosition = ref(null)
function transferChanged(event) { if (event.detail?.storageId === selected.value) load(true) }
onMounted(() => window.addEventListener('aether-files-changed', transferChanged))
onUnmounted(() => window.removeEventListener('aether-files-changed', transferChanged))
function blankTools(event) {
  if (event.target.closest('.file-row, .file-grid-item, button, input, a') || !selected.value || busy.value || uploadBusy.value || renameID.value) return
  event.preventDefault()
  closeMenu()
  toolPosition.value = { position: 'fixed', left: `${Math.max(8, Math.min(event.clientX, window.innerWidth - 210))}px`, top: `${Math.max(8, Math.min(event.clientY, window.innerHeight - 290))}px`, right: 'auto', zIndex: 100 }
  createMenu.value = true
}
const workbenchFiles = ref(null), detailBusy = ref(false), detailError = ref('')
const offlineSupported = computed(() => !!selected.value)
const shareTransfer = ref(false)
function openWorkbench() { workbenchFiles.value = [...(selection.value.length ? detailFiles.value : files.value)]; createMenu.value = false; closeMenu() }
function clearSelection(event) {
  if (renameID.value || details.value || workbenchFiles.value || event.target.closest('button,input,select,textarea,a,[role=option],.file-row,.file-grid-item,.modal,.context-menu,.rename-workbench')) return
  selection.value = []; anchor.value = ''
}
async function showDetails() {
  details.value = true; closeMenu(); detailError.value = ''; detailBusy.value = true
  const storage = selected.value, parent = current.value, chosen = [...detailFiles.value]
  try {
    for (const f of chosen.filter(f => f.isDir && (!f.sizeKnown || !f.countsKnown))) {
      const result = await api('/files/directory-size', 'POST', { storageId: storage, parent, id: f.id })
      if (storage === selected.value && parent === current.value) Object.assign(f, result)
    }
  } catch (e) { detailError.value = e.message }
  finally { detailBusy.value = false }
}
const fileUpload = ref(null), folderUpload = ref(null), uploadBusy = ref(false), uploadProgress = ref(''), offline = ref(false)
const sortKey = ref(['name', 'size', 'type', 'modified'].includes(saved.sortKey) ? saved.sortKey : 'name'), ascending = ref(saved.ascending !== false), selection = ref([]), anchor = ref(''), menu = ref(null), renameID = ref(''), newName = ref(''), operation = ref(''), deleting = ref(false), folderDownload = ref(null)
watch([favoritesOpen, favorites, selected, sortKey, ascending], () => {
  try { localStorage.setItem(preferenceKey, JSON.stringify({ open: favoritesOpen.value, favorites: favorites.value, storage: selected.value, sortKey: sortKey.value, ascending: ascending.value })) } catch {}
}, { deep: true, flush: 'sync' })
const columns = [{ key: 'name', label: '名称' }, { key: 'size', label: '大小' }, { key: 'type', label: '类型' }, { key: 'modified', label: '修改时间' }]
const type = f => f.isDir ? '文件夹' : f.name.split('.').at(-1).toUpperCase()
const fileSize = f => f.isDir && !f.sizeKnown ? '—' : bytes(f.size)
const visible = computed(() => files.value.filter(f => f.name.toLowerCase().includes(query.value.toLowerCase())).sort((a, b) => {
  const value = f => sortKey.value === 'type' ? type(f) : sortKey.value === 'size' ? f.size : f[sortKey.value] || ''
  const av = value(a), bv = value(b)
  return (ascending.value ? 1 : -1) * (typeof av === 'number' ? av - bv : String(av).localeCompare(String(bv), 'zh-CN', { numeric: true }))
}))
const displayItems = computed(() => draft.value ? [{ id: '__new__', name: '', isDir: true }, ...visible.value] : visible.value)
const gridMode = computed(() => mode.value === 'grid')
const { shown, top, bottom, columns: gridColumns, reset: resetScroll, reveal } = useVirtualList(displayItems, viewport, { rowHeight: computed(() => gridMode.value ? 148 : 52), header: computed(() => gridMode.value ? 0 : 44), grid: gridMode })
watch([query, sortKey, ascending, mode], resetScroll)
const detailFiles = computed(() => files.value.filter(f => selection.value.includes(f.id)))
const detailCID = computed(() => state.storages.find(s => s.id === selected.value)?.type === '115' && detailFiles.value.length === 1 && detailFiles.value[0].isDir ? detailFiles.value[0].id : '')
const downloadable = computed(() => detailFiles.value.length === 1 && (detailFiles.value[0].isDir || !!detailFiles.value[0].url))
function downloadFile() {
  if (!downloadable.value) return
  const file = detailFiles.value[0]
  if (file.isDir) {
    folderDownload.value = { storage: selected.value, parent: current.value, id: file.id, name: file.name }
    closeMenu()
    return
  }
  const url = new URL(file.url, window.location.origin)
  url.searchParams.set('download', '1')
  const link = document.createElement('a')
  link.href = url.toString()
  link.download = file.name
  link.target = '_blank'
  link.rel = 'noopener noreferrer'
  document.body.appendChild(link)
  link.click()
  link.remove()
  closeMenu()
}
function confirmFolderDownload() {
  window.open(`/api/files/archive?${new URLSearchParams(folderDownload.value)}`, '_blank', 'noopener')
  folderDownload.value = null
}
async function copyCID() {
  try { await copyText(String(detailCID.value)); notify('CID 已复制') } catch { notify('复制失败', true) }
}
const targetPools = computed(() => state.storages.filter(s => s.enabled && s.type === state.storages.find(s => s.id === selected.value)?.type))
function sort(key) { ascending.value = key === sortKey.value ? !ascending.value : true; sortKey.value = key }
function select(event, f) {
  if (renameID.value || f.id === '__new__' || event.target.closest('input')) return
  if (event.shiftKey && anchor.value) {
    const a = visible.value.findIndex(x => x.id === anchor.value), b = visible.value.indexOf(f)
    selection.value = visible.value.slice(Math.max(0, Math.min(a,b)), Math.max(a,b)+1).map(x => x.id)
  } else if (event.ctrlKey || event.metaKey) {
    selection.value = selection.value.includes(f.id) ? selection.value.filter(id => id !== f.id) : [...selection.value, f.id]
  } else selection.value = [f.id]
  anchor.value = f.id
}
function context(event, f) {
  if (renameID.value || f.id === '__new__') return
  if (!selection.value.includes(f.id)) selection.value = [f.id]
  menu.value = { x: Math.max(8,Math.min(event.clientX,window.innerWidth-160)), y: Math.max(8,Math.min(event.clientY,window.innerHeight-260)) }
}
function closeMenu() { menu.value = null }
async function rename() {
  closeMenu()
  const f = files.value.find(f => f.id === selection.value[0])
  if (!f || selection.value.length !== 1) return
  renameID.value = f.id; newName.value = f.name
  reveal(visible.value.findIndex(item => item.id === f.id))
  await nextTick(); document.querySelector('.file-rename')?.focus(); document.querySelector('.file-rename')?.select()
}
async function act(action, extra = {}) {
  busy.value = true; closeMenu()
  try { await api('/files/action', 'POST', { storageId: selected.value, source: current.value, action, ids: [...selection.value], ...extra }); cancelEdit(); operation.value = ''; deleting.value = false; selection.value = []; await load(true); notify('文件操作完成') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
function cancelEdit() { renameID.value = ''; draft.value = false; newName.value = ''; confirmRename.value = false }
async function createFolder() {
  createMenu.value = false
  if (!selected.value || busy.value || renameID.value) return
  draft.value = true; renameID.value = '__new__'; newName.value = ''; resetScroll()
  await nextTick(); document.querySelector('.file-rename')?.focus()
}
function commitEdit(outside = false) {
  if (!renameID.value || busy.value || confirmRename.value) return
  const name = newName.value.trim()
  if (!name) { cancelEdit(); return }
  if (draft.value) { act('mkdir', { name }); return }
  const original = files.value.find(f => f.id === renameID.value)
  if (!original || name === original.name) { cancelEdit(); return }
  if (outside) { confirmRename.value = true; return }
  act('rename', { name, ids: [renameID.value] })
}
function outsideEdit(event) {
  if (!renameID.value || event.target.closest('.file-rename,.modal,.context-menu,button,a,input,select,textarea,[role=option]')) return
  commitEdit(true)
}
function outsideCreate(event) { if (!event.target.closest('.file-create')) createMenu.value = false }
async function upload(event) {
  const chosen = [...event.target.files]
  event.target.value = ''
  if (!chosen.length) return
  uploadBusy.value = true; createMenu.value = false
  const storage = selected.value, parent = current.value
  let completed = 0
  try {
    for (const file of chosen) {
      uploadProgress.value = `${completed + 1} / ${chosen.length} · ${file.name}`
      const url = `/api/files/upload?${new URLSearchParams({ storage, parent, name: file.webkitRelativePath || file.name })}`
      const response = await fetch(url, { method: 'POST', credentials: 'same-origin', body: file })
      if (!response.ok) { const data = await response.json(); throw new Error(data.error || '上传失败') }
      completed++
    }
    notify(`已上传 ${completed} 个文件`)
  } catch (e) { notify(`已上传 ${completed} 个文件：${e.message}`, true) }
  finally { uploadBusy.value = false; uploadProgress.value = ''; await load(true) }
}
function keys(e) {
  if (e.key === 'Escape') { closeMenu(); createMenu.value = false; if (!confirmRename.value) cancelEdit() }
  if (e.key === 'F2' && !document.querySelector('.modal') && !['INPUT','TEXTAREA'].includes(e.target.tagName)) { e.preventDefault(); rename() }
}
onMounted(() => { document.addEventListener('click', clearSelection); document.addEventListener('click', closeMenu); document.addEventListener('pointerdown', outsideEdit); document.addEventListener('click', outsideCreate); document.addEventListener('keydown', keys) })
onUnmounted(() => { document.removeEventListener('click', clearSelection); document.removeEventListener('click', closeMenu); document.removeEventListener('pointerdown', outsideEdit); document.removeEventListener('click', outsideCreate); document.removeEventListener('keydown', keys); requestId++ })
const poolFavorites = computed(() => favorites.value.filter(f => f.storage === selected.value && f.id !== '/' && f.history?.length))
const isFavorite = computed(() => poolFavorites.value.some(f => f.id === current.value))
function star() {
  if (!selected.value || !history.value.length || current.value === '/') return
  if (isFavorite.value) favorites.value = favorites.value.filter(f => !(f.storage === selected.value && f.id === current.value))
  else favorites.value.push({ storage: selected.value, id: current.value, name: history.value.at(-1)?.name || '根目录', history: JSON.parse(JSON.stringify(history.value)) })
}
function jump(index) {
  if (renameID.value || uploadBusy.value) return
  selection.value = []; renameID.value = ''
  current.value = index < 0 ? '/' : index === history.value.length - 1 ? current.value : history.value[index + 1].id
  history.value = history.value.slice(0, index + 1); resetScroll(); load()
}
function favoriteJump(item) {
  if (renameID.value || uploadBusy.value) return
  // Each crumb stores its parent ID. Older shared arrays may contain descendants.
  const trail = item.history || []
  const descendant = trail.findIndex(crumb => crumb.id === item.id)
  const restored = (descendant < 0 ? trail : trail.slice(0, descendant)).map(crumb => ({ ...crumb }))
  if (descendant >= 0) item.history = restored.map(crumb => ({ ...crumb }))
  selection.value = []; anchor.value = ''; query.value = ''; searchInput.value = ''
  current.value = item.id; history.value = restored
  resetScroll(); load()
}
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
function enter(f) { if (renameID.value || uploadBusy.value) return; if (f.isDir) { query.value = ''; searchInput.value = ''; selection.value = []; history.value.push({ id: current.value, name: f.name }); current.value = f.id; resetScroll(); load() } }
watch(selected, () => { current.value = '/'; history.value = []; selection.value = []; cancelEdit(); resetScroll(); load() }, { immediate: true })
async function copy(f) { try { await copyText(f.url); notify('播放链接已复制') } catch { notify('当前浏览器不允许访问剪贴板', true) } }
</script>
<template>
  <RenameWorkbench v-if="workbenchFiles" :storage="selected" :source="current" :files="workbenchFiles" @close="workbenchFiles = null" @changed="selection = []; load(true)" />
  <template v-else>
  <div class="files-heading"><FileTabs /><div class="files-heading-actions">
    <button class="icon-btn" aria-label="刷新目录" :disabled="busy || !selected || !!renameID || uploadBusy" @click="selection = []; anchor = ''; load(true)"><Icon name="RefreshCw" :class="{ spin: busy }" /></button>
    <div class="search-field"><Icon name="Search" :size="16" /><input v-model="searchInput" :disabled="!!renameID" @keydown.enter="query = searchInput" aria-label="搜索当前目录" placeholder="搜索当前目录…" /></div>
    <div class="file-create"><button class="btn primary" :disabled="!selected || busy || uploadBusy || !!renameID" aria-label="工具" aria-haspopup="menu" :aria-expanded="createMenu" @click="toolPosition = null; createMenu = !createMenu"><Icon name="BriefcaseBusiness" />工具<Icon name="ChevronDown" :size="14" class="tools-chevron" :class="{ expanded: createMenu }" /></button>
      <Transition name="select-popup"><div v-if="createMenu" class="file-create-menu" :style="toolPosition" role="menu"><button role="menuitem" @click="createFolder"><Icon name="FolderPlus" />新建文件夹</button><button role="menuitem" @click="fileUpload.click(); createMenu = false"><Icon name="ArrowUp" />上传文件</button><button role="menuitem" @click="folderUpload.click(); createMenu = false"><Icon name="FolderInput" />上传文件夹</button><button role="menuitem" :disabled="!offlineSupported" @click="offline = true; createMenu = false"><Icon name="Download" />离线下载</button><button role="menuitem" @click="shareTransfer = true; createMenu = false"><Icon name="FolderInput" />分享转存</button><button role="menuitem" :disabled="!files.length" @click="openWorkbench"><Icon name="Pencil" />重命名</button></div></Transition>
    </div>
  </div></div>
  <input ref="fileUpload" type="file" multiple hidden @change="upload" /><input ref="folderUpload" type="file" webkitdirectory multiple hidden @change="upload" />
  <p v-if="uploadProgress" class="upload-progress" role="status">{{ uploadProgress }}</p>
  <div class="file-browser">
  <div class="file-toolbar"><button class="icon-btn" aria-label="展开收藏栏" :aria-expanded="favoritesOpen" @click="favoritesOpen = !favoritesOpen"><Icon name="PanelLeft" /></button><RoundedSelect v-model="selected" label="选择存储池" :disabled="!!renameID || uploadBusy" :options="state.storages.filter(s => s.enabled).map(s => ({ value: s.id, label: s.name }))" /><div class="path-bar"><PathBreadcrumbs :entries="history" :disabled="busy || !!renameID || uploadBusy" @jump="jump" /></div><button class="icon-btn" :disabled="!!renameID" :aria-label="mode === 'list' ? '当前列表视图，切换网格' : '当前网格视图，切换列表'" @click="mode = mode === 'list' ? 'grid' : 'list'"><Icon :name="mode === 'list' ? 'List' : 'LayoutGrid'" /></button></div>
  <div class="file-workspace" :class="{ 'with-favorites': favoritesOpen }">
  <aside class="file-favorites" :inert="!favoritesOpen" :aria-hidden="!favoritesOpen" aria-label="目录收藏"><div class="favorites-heading"><h3>收藏夹</h3><button class="icon-btn" :aria-label="isFavorite ? '取消收藏目录' : '收藏当前目录'" :aria-pressed="isFavorite" :disabled="!selected || !history.length || current === '/'" @click="star"><Icon name="Star" /></button></div><div v-for="item in poolFavorites" :key="item.id" :class="{ active: current === item.id }"><button @click="favoriteJump(item)"><Icon name="Folder" /><span>{{ item.name }}</span></button></div><p v-if="!poolFavorites.length" class="muted">暂无收藏</p></aside>
  <section ref="viewport" class="file-view" @contextmenu="blankTools">
  <div v-if="error" class="error-message" role="alert">{{ error }}</div>
  <div v-if="busy" class="empty-state"><Icon name="LoaderCircle" class="spin" :size="30" /><p>正在读取目录…</p></div>
  <div v-else-if="!selected || !displayItems.length" class="empty-state"><span class="empty-icon"><Icon name="FolderOpen" :size="36" /></span><h3>{{ !selected ? '尚未连接存储' : '目录为空' }}</h3><button v-if="!selected" class="btn" @click="$router.push('/storage')">前往存储管理</button></div>
  <div v-else-if="mode === 'grid'" class="file-grid" :style="{ gridTemplateColumns: `repeat(${gridColumns},minmax(0,1fr))` }">
    <div v-if="top" :style="{ height: `${top}px`, gridColumn: '1 / -1' }" aria-hidden="true" />
    <article v-for="f in shown" :key="f.id" class="file-grid-item" :class="{ selected: selection.includes(f.id) }" tabindex="0" @click="select($event,f)" @dblclick="enter(f)" @contextmenu.prevent.stop="context($event,f)" @keydown.enter="enter(f)"><button class="file-grid-name" :aria-label="f.name"><Icon :name="f.isDir ? 'Folder' : 'FileVideo'" :size="38" :class="{ 'folder-color': f.isDir }" /><strong v-if="renameID !== f.id" :data-tooltip="f.name">{{ f.name }}</strong></button><input v-if="renameID === f.id" v-model="newName" class="file-rename" aria-label="新名称" @keydown.enter.stop.prevent="commitEdit()" /><small>{{ f.isDir && !f.sizeKnown ? '文件夹' : bytes(f.size) }}</small></article>
    <div v-if="bottom" :style="{ height: `${bottom}px`, gridColumn: '1 / -1' }" aria-hidden="true" />
  </div>
  <div v-else class="table-wrap"><table><colgroup><col style="width:48%" /><col style="width:13%" /><col style="width:13%" /><col style="width:26%" /></colgroup><thead><tr><th v-for="col in columns" :key="col.key" :aria-sort="sortKey === col.key ? ascending ? 'ascending' : 'descending' : 'none'"><button class="file-sort" :disabled="!!renameID" @click="sort(col.key)">{{ col.label }}<span class="sort-triangles" :class="{ ascending: sortKey === col.key && ascending, descending: sortKey === col.key && !ascending }"><i /><i /></span></button></th></tr></thead><tbody>
    <tr v-if="top" class="file-spacer" :style="{ height: `${top}px` }" aria-hidden="true"><td colspan="4" /></tr>
    <tr v-for="f in shown" :key="f.id" class="file-row" :class="{ selected: selection.includes(f.id) }" tabindex="0" @click="select($event,f)" @dblclick="enter(f)" @contextmenu.prevent.stop="context($event,f)" @keydown.enter="enter(f)"><td><input v-if="renameID === f.id" v-model="newName" class="file-rename" aria-label="新名称" @keydown.enter.stop.prevent="commitEdit()" /><button v-else class="file-name"><Icon :name="f.isDir ? 'Folder' : 'FileVideo'" :class="{ 'folder-color': f.isDir }" :size="21" /><strong :data-tooltip="f.name">{{ f.name }}</strong></button></td><td>{{ fileSize(f) }}</td><td>{{ type(f) }}</td><td>{{ !f.modified || f.modified.startsWith('0001') ? '—' : new Date(f.modified).toLocaleString('zh-CN') }}</td></tr>
    <tr v-if="bottom" class="file-spacer" :style="{ height: `${bottom}px` }" aria-hidden="true"><td colspan="4" /></tr>
  </tbody></table></div>
  </section></div></div>
  <Teleport to="body"><div v-if="menu" class="context-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop>
    <button :disabled="!downloadable" @click="downloadFile"><Icon name="Download" />下载</button>
    <button v-if="selection.length === 1" @click="rename"><Icon name="Pencil" />重命名</button>
    <button @click="operation = 'move'; closeMenu()"><Icon name="FolderInput" />移动到</button><button @click="operation = 'copy'; closeMenu()"><Icon name="Copy" />复制到</button>
    <button class="danger-text" @click="deleting = true; closeMenu()"><Icon name="Trash2" />删除</button>
    <button v-if="selection.length === 1 && files.find(f => f.id === selection[0])?.url" @click="copy(files.find(f => f.id === selection[0])); closeMenu()"><Icon name="Link" />复制播放链接</button>
    <button @click="showDetails"><Icon name="Info" />查看详情</button>
  </div></Teleport>
  <TaskSourcePicker v-if="operation" :storages="targetPools" :storage="selected" @close="operation = ''" @select="act(operation, { targetStorage: $event.storageId, target: $event.source })" />
  <Modal v-if="deleting" title="删除文件" confirmation @close="deleting = false"><div class="modal-body">确认删除选中的 {{ selection.length }} 项？将按存储池的删除模式处理。</div><footer class="modal-footer"><button class="btn danger" :disabled="busy" @click="act('delete')">确认删除</button><button class="btn" @click="deleting = false">取消</button></footer></Modal>
  <Modal v-if="confirmRename" title="确认修改名称" @close="confirmRename = false"><div class="modal-body">将「{{ files.find(f => f.id === renameID)?.name }}」改为「{{ newName.trim() }}」？</div><footer class="modal-footer"><button class="btn" @click="cancelEdit">放弃修改</button><button class="btn primary" :disabled="busy" @click="act('rename', { name: newName.trim(), ids: [renameID] })">确认修改</button></footer></Modal>
  <Modal v-if="details" title="文件详情" @close="details = false"><div class="modal-body file-details"><p>{{ detailFiles.length }} 个项目 · {{ bytes(detailFiles.reduce((n, f) => n + (f.isDir && !f.sizeKnown ? 0 : f.size || 0), 0)) }}</p><p v-if="detailError" class="error-message">{{ detailError }}</p><dl v-for="f in detailFiles" :key="f.id">
    <dt>名称</dt><dd>{{ f.name }}</dd><dt>类型</dt><dd>{{ type(f) }}</dd>
    <dt>大小</dt><dd>{{ f.isDir && !f.sizeKnown ? (detailBusy ? '正在计算…' : '未完成统计') : bytes(f.size) }}</dd>
    <template v-if="f.isDir"><dt>包含</dt><dd>{{ f.countsKnown ? `${f.folderCount || 0} 个文件夹，${f.fileCount || 0} 个文件` : detailBusy ? '正在统计…' : '未完成统计' }}</dd></template>
    <dt>创建时间</dt><dd>{{ !f.created || f.created.startsWith('0001') ? '未提供' : new Date(f.created).toLocaleString('zh-CN') }}</dd>
    <dt>修改时间</dt><dd>{{ !f.modified || f.modified.startsWith('0001') ? '未提供' : new Date(f.modified).toLocaleString('zh-CN') }}</dd>
    <template v-if="detailCID"><dt>CID</dt><dd><button class="text-btn" aria-label="复制 CID" @click="copyCID">{{ detailCID }}<Icon name="Copy" :size="14" /></button></dd></template>
    <dt>位置</dt><dd>{{ state.storages.find(s => s.id === selected)?.name }} / {{ history.map(h => h.name).join(' / ') }}</dd>
    <template v-if="f.sha256"><dt>SHA256</dt><dd>{{ f.sha256 }}</dd></template><template v-if="f.md5"><dt>MD5</dt><dd>{{ f.md5 }}</dd></template></dl></div></Modal>
  <Modal v-if="folderDownload" title="下载文件夹" confirmation @close="folderDownload = null"><div class="modal-body">确认将「{{ folderDownload.name }}」打包为 ZIP 下载？打包将通过以太传输，目录较大时需要较长时间。</div><footer class="modal-footer"><button class="btn primary" @click="confirmFolderDownload">确认下载</button><button class="btn" @click="folderDownload = null">取消</button></footer></Modal>
  <OfflineDownload v-if="offline" :storage="state.storages.find(s => s.id === selected)" :parent="current" :trail="history" @close="offline = false" />
  <ShareTransfer v-if="shareTransfer" :storage="state.storages.find(s => s.id === selected)" :parent="current" :trail="history" @close="shareTransfer = false" @changed="load(true)" />
  </template>
</template>
