<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, notify } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import TaskTabs from '../components/TaskTabs.vue'
import Modal from '../components/Modal.vue'
import { useVirtualList } from '../virtual-list'
const task = ref(''), items = ref([]), progress = ref({}), busy = ref(false), settingsOpen = ref(false), matching = ref(null), candidates = ref([])
const query = ref(''), status = ref('all')
const scope = ref([]), scopeOpen = ref(false), scopeDraft = ref([])
const libraryRoot = ref(''), directoryPath = ref(''), directoryItems = ref(null), directoryBusy = ref(false), directoryError = ref('')
let directoryRequest = 0
const visibleDirectories = computed(() => directoryItems.value || directories.value.filter(d => d.value).map(d => d.value))
async function loadDirectories(dir = '') {
  const run = ++directoryRequest, taskId = task.value
  directoryBusy.value = true; directoryError.value = ''
  try {
    const result = await api(`/strm-scrape/directories?taskId=${encodeURIComponent(taskId)}&path=${encodeURIComponent(dir)}`)
    if (!alive || run !== directoryRequest || taskId !== task.value) return
    if (Array.isArray(result.directories)) { directoryItems.value = result.directories; directoryPath.value = dir; libraryRoot.value = result.root }
  } catch (e) { if (alive && run === directoryRequest) directoryError.value = e.message }
  finally { if (run === directoryRequest) directoryBusy.value = false }
}
function openScope() { scopeDraft.value = [...scope.value]; scopeOpen.value = true; loadDirectories('') }
const workPath = computed(() => {
  const target = state.tasks.find(t => t.id === task.value)?.target || '/data/strm'
  const base = libraryRoot.value || (/^(\/|[a-z]:[\\/])/i.test(target) ? target : `/data/strm/${target}`)
  return `${base.replace(/[\\/]+$/, '')}/${matching.value?.path?.split('/').slice(0, -1).join('/') || ''}`
})
const directories = computed(() => {
  const paths = new Set()
  for (const item of items.value) { for(const dir of item.directories || [item.path.split('/').slice(0,-1).join('/')]) { const parts=dir.split('/').filter(p=>p && p!=='.'); for(let i=1;i<=parts.length;i++) paths.add(parts.slice(0,i).join('/')) } }
  return [{value:'',label:'全部目录'}, ...[...paths].sort().map(value=>({value,label:value}))]
})
const settings = reactive({ writeMode: 'missing', episodes: true, fanart: false, actors: false, excluded: '' })
const match = reactive({ tmdb: '', kind: 'movie' })
const resetting = ref(null)
async function resetMetadata() {
  busy.value = true
  try {
    await api('/strm-scrape/reset', 'POST', { taskId: task.value, path: resetting.value.path, group: true, confirmed: true })
    resetting.value = null; await load(); notify('作品元数据已重置')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
const tasks = computed(() => state.tasks.filter(t => t.kind === 'strm' && !t.scrapeExcluded).map(t => ({ value: t.id, label: t.name })))
const statuses = { unmatched: '待匹配', pending: '待刮削', ok: '已完成', miss: '未匹配', doubt: '待确认', error: '失败' }
const itemStatus = item => item.status === 'pending' && !item.tmdb ? 'unmatched' : item.status
const filtered = computed(() => items.value.filter(i => (!scope.value.length || (i.directories || [i.path.split('/').slice(0,-1).join('/')]).some(d=>scope.value.some(s=>d===s || d.startsWith(s+'/')))) && (status.value === 'all' || itemStatus(i) === status.value) && `${i.title} ${i.path}`.toLowerCase().includes(query.value.toLowerCase())))
const viewport = ref(null)
const { shown, top, bottom, columns, reset } = useVirtualList(filtered, viewport, { rowHeight: 368, columnWidth: 155, maxColumns: 6, grid: ref(true), window: true })
watch([query, status, task, scope], reset)
const candidateQuery = ref(''), candidateBusy = ref(false), candidateError = ref('')
let alive = true, timer, request = 0, candidateRequest = 0
async function load() {
  const id = task.value, run = ++request
  if (!id) { items.value = []; return }
  try { const result = await api(`/strm-scrape/items?taskId=${encodeURIComponent(id)}&group=true`); if (alive && run === request) items.value = result }
  catch (e) { if (alive) notify(e.message, true) }
}
async function poll() {
  try {
    const result = await api('/strm-scrape/status')
    if (!alive) return
    const finished = progress.value.running && !result.running
    progress.value = result
    if (finished) { await load(); notify(result.message || '执行结束') }
  } catch (e) { if (alive) notify(e.message, true) }
  finally { if (alive) timer = setTimeout(poll, 2000) }
}
async function action(name, item) {
  busy.value = true
  try {
    const result = await api(`/strm-scrape/${name}`, 'POST', { taskId: task.value, path: item?.path || '', scopes: scope.value, group: true })
    if (name !== 'stop') progress.value = result
    else notify('正在停止刮削')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
async function saveSettings() {
  busy.value = true
  try { await api('/strm-scrape/settings', 'PUT', settings); settingsOpen.value = false; notify('刮削设置已保存') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
function rematch(item) {
  matching.value = item; candidates.value = []; candidateQuery.value = item.title
  Object.assign(match, { tmdb: item.tmdb || '', kind: item.kind })
  candidateRequest++; candidateBusy.value = false; candidateError.value = ''
}
async function searchCandidates() {
  if (!matching.value || !candidateQuery.value.trim()) return
  const run = ++candidateRequest, item = matching.value
  candidateBusy.value = true; candidateError.value = ''; candidates.value = []
  try {
    const result = await api('/strm-scrape/candidates', 'POST', { query: candidateQuery.value.trim(), kind: match.kind })
    if (alive && run === candidateRequest && matching.value === item) candidates.value = Array.isArray(result) ? result : []
  } catch (e) { if (run === candidateRequest) candidateError.value = e.message }
  finally { if (run === candidateRequest) candidateBusy.value = false }
}
async function saveMatch() {
  busy.value = true
  try {
    await api('/strm-scrape/match', 'POST', { taskId: task.value, path: matching.value.path, tmdb: Number(match.tmdb), kind: match.kind, group: true })
    matching.value = null; await load(); notify('匹配已保存')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
const preferenceKey = computed(() => `aether-scrape-task:${state.username || ''}`)
watch(task, value => {
  scope.value = []
  libraryRoot.value = ''; directoryItems.value = null; directoryPath.value = ''; directoryRequest++
  if (value) { try { localStorage.setItem(preferenceKey.value, value) } catch {} }
  load()
  if (value) loadDirectories('')
})
watch([tasks, preferenceKey], ([options, key], previous) => {
  if (!options.length) { task.value = ''; return }
  let saved = ''
  try { saved = localStorage.getItem(preferenceKey.value) || '' } catch {}
  if (previous?.[1] !== key || !options.some(option => option.value === task.value)) task.value = options.some(option => option.value === saved) ? saved : options[0].value
}, { immediate: true })
watch(() => state.tasks.find(t => t.id === task.value)?.status, (current, previous) => {
  if (previous === 'running' && current !== 'running') load()
})
onMounted(async () => {
  poll()
  try { Object.assign(settings, await api('/strm-scrape/settings')) } catch (e) { notify(e.message, true) }
})
onUnmounted(() => { alive = false; clearTimeout(timer); request++; candidateRequest++; directoryRequest++ })
</script>
<template>
  <section class="task-heading"><TaskTabs /><div class="toolbar-right"><button class="btn" @click="settingsOpen = true"><Icon name="Settings2" />刮削设置</button><button v-if="progress.running" class="btn" :disabled="busy" @click="action('stop')"><Icon name="Square" />停止</button></div></section>
  <section class="scrape-panel">
    <div class="scrape-toolbar">
      <RoundedSelect v-model="task" label="STRM 任务" placeholder="选择 STRM 任务" :disabled="progress.running" :options="tasks" />
      <button class="btn" aria-label="筛选库目录" :disabled="progress.running" @click="openScope"><Icon name="Folder" />{{ scope.length ? `已选 ${scope.length} 个目录` : '全部目录' }}</button>
      <RoundedSelect v-model="status" label="刮削状态" :options="[{ value: 'all', label: '全部状态' }, ...Object.entries(statuses).map(([value,label]) => ({value,label}))]" />
      <button class="btn" :disabled="!task || busy || progress.running" @click="action('identify')"><Icon name="ScanSearch" />识别 STRM 库</button>
      <div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索刮削记录" placeholder="搜索名称或路径" /></div>
      <button class="icon-btn" aria-label="刷新刮削索引" :disabled="!task || busy || progress.running" @click="action('scan')"><Icon name="RefreshCw" /></button>
    </div>
    <div class="scrape-progress"><span>{{ progress.running ? progress.message : progress.message || '等待执行' }}</span><small>{{ progress.done || 0 }} / {{ progress.total || 0 }}</small><progress :value="progress.done || 0" :max="progress.total || 1" /></div>
    <div ref="viewport" class="scrape-body">
      <div v-if="filtered.length" class="scrape-wall" :style="{gridTemplateColumns: `repeat(${columns}, minmax(0,1fr))`}">
        <div v-if="top" :style="{height: `${top}px`, gridColumn: '1 / -1'}" aria-hidden="true" />
        <article v-for="item in shown" :key="item.path" class="scrape-card">
          <button class="scrape-poster" :aria-label="`匹配 ${item.title}`" :disabled="progress.running || busy" @click="rematch(item)"><img v-if="item.poster" :src="item.poster" :alt="item.title" loading="lazy" referrerpolicy="no-referrer" /><template v-else><Icon :name="item.kind === 'tv' ? 'Tv' : 'Film'" :size="32" /><span>{{ item.kind === 'tv' ? '电视剧' : '电影' }}</span></template></button>
          <div class="scrape-card-body"><strong :data-tooltip="item.title">{{ item.title }}</strong><small :data-tooltip="item.path">{{ item.kind === 'tv' ? '剧集' : '电影' }}<template v-if="item.year"> · {{ item.year }}</template><template v-if="item.kind === 'tv'"> · {{ item.count || 1 }} 集</template></small><span class="scrape-status" :class="item.status">{{ statuses[itemStatus(item)] }}</span></div>
          <div class="scrape-actions"><button class="icon-btn" aria-label="重置元数据" :disabled="progress.running || busy" @click="resetting = item"><Icon name="RotateCcw" /></button><button class="icon-btn" aria-label="识别作品" :disabled="progress.running || busy" @click="rematch(item)"><Icon name="ScanSearch" /></button></div>
        </article>
        <div v-if="bottom" :style="{height: `${bottom}px`, gridColumn: '1 / -1'}" aria-hidden="true" />
      </div>
      <div v-else class="small-empty">{{ task ? '暂无刮削记录' : '请选择 STRM 任务' }}</div>
    </div>
    <footer>{{ filtered.length }} 部作品</footer>
  </section>
  <Modal v-if="resetting" title="重置作品元数据" compact confirmation @close="!busy && (resetting = null)"><div class="modal-body"><p>重置「{{ resetting.title }}」的识别结果、匹配信息和刮削状态？磁盘上的 NFO、封面和 STRM 文件会保留。</p></div><footer class="modal-footer"><button class="btn primary" :disabled="busy" @click="resetMetadata">确认重置</button><button class="btn" :disabled="busy" @click="resetting = null">取消</button></footer></Modal>
  <Modal v-if="settingsOpen" title="STRM 刮削设置" compact @close="settingsOpen = false"><form @submit.prevent="saveSettings"><div class="modal-body scrape-settings">
    <div class="field"><label>写入策略</label><RoundedSelect v-model="settings.writeMode" label="写入策略" :options="[{value:'missing',label:'仅补缺'}, {value:'overwrite',label:'覆盖已有'}]" /></div>
    <label class="toggle-line"><span>分集 NFO 与预览图</span><input v-model="settings.episodes" type="checkbox" /></label>
    <label class="toggle-line"><span>背景图</span><input v-model="settings.fanart" type="checkbox" /></label>
    <label class="toggle-line"><span>演员信息</span><input v-model="settings.actors" type="checkbox" /></label>
    <label>排除目录<input v-model="settings.excluded" placeholder="英文分号分隔" /></label>
  </div><footer class="modal-footer"><button class="btn primary" :disabled="busy"><Icon name="Save" />保存设置</button></footer></form></Modal>
  <Modal v-if="scopeOpen" title="选择刮削目录" compact @close="scopeOpen = false"><div class="modal-body scope-options"><nav class="directory-crumbs" aria-label="刮削目录路径"><button @click="loadDirectories('')">根目录</button><button v-for="(part, index) in directoryPath.split('/').filter(Boolean)" :key="index" @click="loadDirectories(directoryPath.split('/').slice(0,index+1).join('/'))">{{ part }}</button></nav><label><input type="checkbox" :checked="!scopeDraft.length" @change="scopeDraft = []" />全部目录</label><p v-if="directoryError" class="error-message">{{ directoryError }}</p><p v-if="directoryBusy" class="muted">读取目录中…</p><div v-for="dir in visibleDirectories" :key="dir" class="scope-directory"><label><input v-model="scopeDraft" type="checkbox" :value="dir" :aria-label="dir" /><Icon name="Folder" :size="17" /><span>{{ dir.split('/').at(-1) }}</span></label><button class="icon-btn" :aria-label="`打开 ${dir}`" :disabled="directoryBusy" @click="loadDirectories(dir)"><Icon name="ChevronRight" /></button></div></div><footer class="modal-footer"><button class="btn primary" :disabled="directoryBusy" @click="scope = [...scopeDraft]; scopeOpen = false">确认范围</button><button class="btn" @click="scopeOpen = false">取消</button></footer></Modal>
  <Modal v-if="matching" title="选择媒体匹配" compact wide @close="matching = null; candidateRequest++"><form @submit.prevent="saveMatch"><div class="modal-body scrape-settings">
    <div class="match-source-path" :data-tooltip="workPath">{{ workPath }}</div>
    <p>{{ matching.title }}<small v-if="matching.count > 1"> · {{ matching.count }} 个分集</small></p>
    <div class="candidate-search"><RoundedSelect v-model="match.kind" label="媒体类型" :options="[{value:'movie',label:'电影'},{value:'tv',label:'电视剧'}]" @update:model-value="match.tmdb = ''; candidates = []; candidateRequest++; candidateBusy = false" /><input v-model="candidateQuery" aria-label="候选名称" @keydown.enter.prevent="searchCandidates" /><button type="button" class="icon-btn" aria-label="搜索候选媒体" :disabled="candidateBusy" @click="searchCandidates"><Icon name="Search" /></button></div>
    <p v-if="candidateError" class="error-message" role="alert">{{ candidateError }}</p><p v-else-if="candidateBusy" role="status">正在搜索候选媒体…</p><p v-else-if="!candidates.length" class="muted">没有找到候选，可修改名称或填写 TMDB ID。</p>
    <div class="candidate-grid"><button v-for="candidate in candidates" :key="candidate.id" type="button" class="candidate-card" :aria-pressed="Number(match.tmdb) === candidate.id" :class="{ selected: Number(match.tmdb) === candidate.id }" @click="match.tmdb = candidate.id"><img v-if="candidate.poster" :src="candidate.poster" :alt="candidate.title || candidate.name" loading="lazy" referrerpolicy="no-referrer" /><span v-else class="candidate-poster"><Icon name="Film" /></span><strong>{{ candidate.title || candidate.name }}</strong><small>{{ candidate.year?.slice(0,4) || '年份未知' }} · TMDB {{ candidate.id }}</small></button></div>
    <label>TMDB ID<input v-model="match.tmdb" type="number" min="1" required /></label></div><footer class="modal-footer"><button class="btn primary" :disabled="busy || !match.tmdb">确认匹配</button></footer></form></Modal>
</template>
<style scoped>
.scrape-panel { min-height: 320px; display: flex; flex-direction: column; background: var(--surface); }
.scrape-toolbar { display: flex; align-items: center; gap: 10px; padding: 12px; }
.scrape-toolbar > .rounded-select:first-child { width: 170px; }
.scrape-toolbar > .rounded-select:nth-child(2) { width: 160px; }
.scrape-toolbar > .rounded-select:nth-child(3) { width: 120px; }
.scrape-toolbar .search-field { width: 200px; margin-left: auto; }
.scrape-progress { display: flex; align-items: center; gap: 12px; padding: 0 14px 12px; font-size: 13px; color: var(--muted); }
.scrape-progress span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 50%; }
.scrape-progress progress { height: 4px; flex: 1; accent-color: var(--primary); }
.scrape-head, .scrape-row { display: grid; grid-template-columns: minmax(0,2fr) 100px minmax(140px,1fr) 84px; gap: 12px; align-items: center; padding: 8px 14px; }
.scrape-head { background: var(--bg); color: var(--muted); font-size: 13px; }
.scrape-row { height: 66px; box-sizing: border-box; border-bottom: 1px solid color-mix(in srgb,var(--border) 50%,transparent); font-size: 14px; }
.scrape-wall { display: grid; padding: 0 12px; column-gap: 12px; }
.scrape-card { position: relative; min-width: 0; height: 356px; margin-bottom: 12px; border: 1px solid var(--border); border-radius: 16px; overflow: hidden; background: var(--surface); }
.scrape-poster { width: 100%; height: 256px; padding: 0; border: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; background: var(--bg); color: var(--muted); }
.scrape-poster img { width: 100%; height: 100%; object-fit: cover; }
.scrape-poster span { font-size: 12px; }
.scrape-card-body { min-width: 0; padding: 9px 10px; font-size: 14px; }
.scrape-card-body > strong, .scrape-card-body > small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.scrape-card-body > small { margin-top: 5px; color: var(--muted); font-size: 12px; }
.scrape-status { display: inline-flex; gap: 8px; margin-top: 7px; font-size: 12px; color: var(--muted); }
.scrape-status.ok { color: var(--success); }.scrape-status.error { color: var(--danger); }
.scrape-status em { font-style: normal; color: var(--muted); }
.scrape-row small { display: block; color: var(--muted); font-size: 12px; margin-top: 4px; }
.scrape-name, .scrape-status { min-width: 0; }
.scrape-name strong, .scrape-row small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; }
.scrape-status.ok strong { color: var(--success); }
.scrape-status.error strong { color: var(--danger); }
.scrape-actions { display: flex; position: absolute; left: 0; right: 0; top: 212px; opacity:0; pointer-events:none; transform:translateY(5px); transition:opacity .18s,transform .18s; }
.scrape-card:hover .scrape-actions, .scrape-card:focus-within .scrape-actions { opacity:1; pointer-events:auto; transform:none; }
.scrape-actions :deep(button) { background:var(--surface); border-radius:10px; box-shadow:0 2px 8px #0002; }
@media(hover:none) { .scrape-actions { opacity:1; pointer-events:auto; transform:none; } }
.scrape-actions :deep(button) { width: 50%; height: 44px; border-radius:0; background:color-mix(in srgb,var(--surface) 88%,transparent); box-shadow:none; }
.match-source-path { padding:10px 12px; background:var(--bg); border:1px solid var(--border); border-radius:8px; font-size:14px; overflow-wrap:anywhere; }
.scope-options { height:360px; overflow:auto; }
.scope-options label { display:flex; flex-direction:row; align-items:center; gap:10px; margin-bottom:10px; overflow-wrap:anywhere; }
.scope-directory { display:flex; align-items:center; justify-content:space-between; }
.scope-directory label { flex:1; min-width:0; margin:0; }
.scope-directory span { overflow-wrap:anywhere; }
.scrape-body { min-height: 0; overflow-anchor: none; }
.scrape-body::-webkit-scrollbar { display: none; }
.scrape-panel > footer { padding: 8px 14px; color: var(--muted); font-size: 12px; }
.scrape-settings { display: grid; gap: 16px; }
.candidate-grid { display: grid; grid-template-columns: repeat(5,minmax(0,1fr)); gap: 10px; max-height: 300px; overflow: auto; padding: 2px; }
.candidate-card { display: flex; flex-direction: column; gap: 4px; min-width: 0; padding: 5px; border: 1px solid var(--border); border-radius: 7px; background: var(--surface); color: var(--text); text-align: left; }
.candidate-card:hover, .candidate-card.selected { border-color: var(--primary); background: var(--primary-soft); }
.candidate-card img, .candidate-poster { width: 100%; aspect-ratio: 2 / 3; object-fit: cover; border-radius: 4px; background: var(--bg); display: grid; place-items: center; color: var(--muted); }
.candidate-card strong, .candidate-card small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.candidate-card small { color: var(--muted); font-size: 11px; }
.candidate-search { display: grid; grid-template-columns: 100px minmax(0,1fr) 36px; align-items: center; gap: 10px; }
.candidate-search > .rounded-select, .candidate-search > input { min-width: 0; width: 100%; }
.candidate-search :deep(.rounded-select-trigger) { min-width: 0; width: 100%; }
@media(max-width:600px) { .candidate-grid { grid-template-columns: repeat(3,minmax(0,1fr)); } }
@media(max-width:760px) { .scrape-toolbar { flex-wrap: wrap; }.scrape-toolbar .search-field { margin-left: 0; width: 160px; }.scrape-head,.scrape-row { grid-template-columns: minmax(0,1fr) 70px 80px; gap: 6px; }.scrape-head > :nth-child(2),.scrape-row > :nth-child(2) { display: none; } }
</style>
