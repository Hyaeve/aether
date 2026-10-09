<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, notify } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import TaskTabs from '../components/TaskTabs.vue'
import Modal from '../components/Modal.vue'
import ScrapeScopePicker from '../components/ScrapeScopePicker.vue'
import { useVirtualList } from '../virtual-list'
const task = ref(''), items = ref([]), progress = ref({}), busy = ref(false), settingsOpen = ref(false), matching = ref(null), candidates = ref([])
const query = ref(''), status = ref('all')
const scopeOpen = ref(false)
const excludedScopes = ref([]), selectionMode = ref(false), selection = ref([]), selectionAnchor = ref(''), confirmation = ref(null)
function exitSelection() { selectionMode.value = false; selection.value = []; selectionAnchor.value = '' }
function choose(event, item) {
  if (event.target.closest('.scrape-actions') || progress.value.running) return
  if (event.type === 'contextmenu') selectionMode.value = true
  if (!selectionMode.value) { rematch(item); return }
  if (event.shiftKey && selectionAnchor.value) {
    const a = filtered.value.findIndex(i => i.path === selectionAnchor.value), b = filtered.value.indexOf(item)
    if (a >= 0) selection.value = [...new Set([...selection.value, ...filtered.value.slice(Math.min(a,b),Math.max(a,b)+1).map(i=>i.path)])]
  } else {
    selection.value = selection.value.includes(item.path) ? selection.value.filter(p=>p!==item.path) : [...selection.value,item.path]
    selectionAnchor.value = item.path
  }
}
function selectionKey(event) { if(event.key === 'Escape' && !document.querySelector('.modal')) exitSelection() }
function overlayFocus(event) { if (selectionMode.value && event.target.closest?.('.modal')) exitSelection() }
async function confirmAction() {
  const command = confirmation.value
  busy.value = true
  try {
    const result = await api(`/strm-scrape/${command.action}`, 'POST', { taskId: task.value, paths: command.paths || [], excludedScopes: excludedScopes.value, group:true, confirmed:true, deleteFiles: command.action === 'reset', scrape:true, reidentify:command.action === 'identify' })
    confirmation.value = null
    if(command.action === 'reset') { await load(); notify('所选作品已重置') } else progress.value = result
  } catch(e) { notify(e.message,true) } finally { busy.value = false }
}
const libraryRoot = ref('')
let directoryRequest = 0
async function loadDirectories(dir = '') {
  const run = ++directoryRequest, taskId = task.value
  try {
    const result = await api(`/strm-scrape/directories?taskId=${encodeURIComponent(taskId)}&path=${encodeURIComponent(dir)}`)
    if (!alive || run !== directoryRequest || taskId !== task.value) return
    if (typeof result.root === 'string') libraryRoot.value = result.root
  } catch { /* A missing library remains visible as an empty workspace. */ }
}
const workPath = computed(() => {
  const target = state.tasks.find(t => t.id === task.value)?.target || '/data/strm'
  const base = libraryRoot.value || (/^(\/|[a-z]:[\\/])/i.test(target) ? target : `/data/strm/${target}`)
  return `${base.replace(/[\\/]+$/, '')}/${matching.value?.path?.split('/').slice(0, -1).join('/') || ''}`
})
const settings = reactive({ writeMode: 'missing', episodes: true, fanart: false, actors: false, excluded: '' })
const match = reactive({ tmdb: '', kind: 'movie' })
const resetting = ref(null)
watch([matching, scopeOpen, settingsOpen, resetting, confirmation], values => { if (values.some(Boolean)) exitSelection() }, { flush: 'sync' })
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
const filtered = computed(() => items.value.filter(i => (i.directories || [i.path.split('/').slice(0,-1).join('/')]).some(d=>!excludedScopes.value.some(s=>s==='.' || d===s || d.startsWith(s+'/'))) && (status.value === 'all' || itemStatus(i) === status.value) && `${i.title} ${i.path}`.toLowerCase().includes(query.value.toLowerCase())))
const viewport = ref(null)
const { shown, top, bottom, columns, reset } = useVirtualList(filtered, viewport, { rowHeight: 314, columnWidth: 155, maxColumns: 6, grid: ref(true), window: true })
watch([query, status, task, excludedScopes], reset)
const candidateQuery = ref(''), candidateBusy = ref(false), candidateError = ref('')
let alive = true, timer, request = 0, candidateRequest = 0
async function load(cached = false) {
  const id = task.value, run = ++request
  if (!id) { items.value = []; return }
  try { const result = await api(`/strm-scrape/items?taskId=${encodeURIComponent(id)}&group=true&cached=${cached}`); if (alive && run === request) { items.value = result; const paths = new Set(result.map(i=>i.path)); selection.value = selection.value.filter(p=>paths.has(p)) } }
  catch (e) { if (alive) notify(e.message, true) }
}
async function poll() {
  try {
    const result = await api('/strm-scrape/status')
    if (!alive) return
    const finished = progress.value.running && !result.running
    progress.value = result
    if (finished && result.taskId === task.value) { await load(); notify(result.message || '执行结束') }
  } catch (e) { if (alive) notify(e.message, true) }
  finally { if (alive) timer = setTimeout(poll, 2000) }
}
async function action(name, item) {
  busy.value = true
  try {
    const result = await api(`/strm-scrape/${name}`, 'POST', { taskId: task.value, path: item?.path || '', excludedScopes: excludedScopes.value, group: true })
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
    const result = await api('/strm-scrape/match', 'POST', { taskId: task.value, path: matching.value.path, tmdb: Number(match.tmdb), kind: match.kind, group: true, scrape:true })
    matching.value = null; progress.value = result; await load(); notify('匹配已应用，正在刮削')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
const preferenceKey = computed(() => `aether-scrape-task:${state.username || ''}`)
watch(task, value => {
  excludedScopes.value = []; exitSelection()
  libraryRoot.value = ''; directoryRequest++
  if (value) { try { localStorage.setItem(preferenceKey.value, value) } catch {} }
  load(true)
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
  document.addEventListener('keydown', selectionKey)
  document.addEventListener('focusin', overlayFocus)
  poll()
  try { Object.assign(settings, await api('/strm-scrape/settings')) } catch (e) { notify(e.message, true) }
})
onUnmounted(() => { alive = false; clearTimeout(timer); request++; candidateRequest++; directoryRequest++; document.removeEventListener('keydown',selectionKey); document.removeEventListener('focusin',overlayFocus) })
</script>
<template>
  <section class="task-heading"><TaskTabs /><div class="toolbar-right"><button class="btn" @click="settingsOpen = true"><Icon name="Settings2" />刮削设置</button><button v-if="progress.running" class="btn" :disabled="busy" @click="action('stop')"><Icon name="Square" />停止</button></div></section>
  <section class="scrape-panel" :class="{'selecting':selectionMode}">
    <div class="scrape-toolbar">
      <RoundedSelect v-model="task" label="STRM 任务" placeholder="选择 STRM 任务" :disabled="progress.running" :options="tasks" />
      <button class="btn" aria-label="筛选库目录" :disabled="!task || progress.running" @click="scopeOpen = true"><Icon name="Folder" />{{ excludedScopes.length ? `已排除 ${excludedScopes.length} 个目录` : '全部目录' }}</button>
      <RoundedSelect v-model="status" label="刮削状态" :options="[{ value: 'all', label: '全部状态' }, ...Object.entries(statuses).map(([value,label]) => ({value,label}))]" />
      <button class="btn" :disabled="!task || busy || progress.running" @click="confirmation = {action:'identify'}"><Icon name="ScanSearch" />识别 STRM 库</button>
      <div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索刮削记录" placeholder="搜索名称或路径" /></div>
      <button class="icon-btn" aria-label="刷新刮削索引" :disabled="!task || busy || progress.running" @click="exitSelection(); action('scan')"><Icon name="RefreshCw" /></button>
    </div>
    <div class="scrape-progress"><span>{{ progress.running ? progress.message : progress.message || '等待执行' }}</span><small>{{ progress.done || 0 }} / {{ progress.total || 0 }}</small><progress :value="progress.done || 0" :max="progress.total || 1" /></div>
    <div ref="viewport" class="scrape-body">
      <div v-if="filtered.length" class="scrape-wall" :style="{gridTemplateColumns: `repeat(${columns}, minmax(0,1fr))`}">
        <div v-if="top" :style="{height: `${top}px`, gridColumn: '1 / -1'}" aria-hidden="true" />
        <article v-for="item in shown" :key="item.path" class="scrape-card" :class="{selected:selection.includes(item.path)}" @click="selectionMode && choose($event,item)" @contextmenu.prevent="choose($event,item)">
          <button class="scrape-poster" :aria-label="`匹配 ${item.title}`" :aria-pressed="selectionMode ? selection.includes(item.path) : undefined" :disabled="progress.running || busy" @click.stop="choose($event,item)"><img v-if="item.poster" :src="item.poster" :alt="item.title" loading="lazy" referrerpolicy="no-referrer" /><template v-else><Icon :name="item.kind === 'tv' ? 'Tv' : 'Film'" :size="32" /><span>{{ item.kind === 'tv' ? '电视剧' : '电影' }}</span></template><span v-if="selectionMode" class="poster-selection"><Icon :name="selection.includes(item.path) ? 'CircleCheck' : 'Circle'" :size="22" /></span></button>
          <div class="scrape-card-body"><div class="scrape-title"><strong :data-tooltip="item.title">{{ item.title }}</strong><span class="work-star" :class="itemStatus(item)" :aria-label="statuses[itemStatus(item)]" :data-tooltip="statuses[itemStatus(item)]" data-tooltip-always /></div><small :data-tooltip="item.path">{{ item.kind === 'tv' ? '剧集' : '电影' }}<template v-if="item.year"> · {{ item.year }}</template><template v-if="item.kind === 'tv'"> · {{ item.count || 1 }} 集</template></small></div>
          <div v-if="!selectionMode" class="scrape-actions"><button class="icon-btn" aria-label="重置元数据" :disabled="progress.running || busy" @click="resetting = item"><Icon name="RotateCcw" :size="16" />重置</button><button class="icon-btn" aria-label="识别作品" :disabled="progress.running || busy" @click="rematch(item)"><Icon name="ScanSearch" :size="16" />识别</button></div>
        </article>
        <div v-if="bottom" :style="{height: `${bottom}px`, gridColumn: '1 / -1'}" aria-hidden="true" />
      </div>
      <div v-else class="small-empty">{{ task ? '暂无刮削记录' : '请选择 STRM 任务' }}</div>
    </div>
    <footer>{{ filtered.length }} 部作品</footer>
  </section>
  <Teleport to="body"><div v-if="selectionMode" class="scrape-dock" role="toolbar" aria-label="所选作品操作"><strong>已选 {{ selection.length }} 项</strong><button class="btn" @click="selection = filtered.map(i=>i.path)"><Icon name="CheckCheck" />全选</button><button class="btn" @click="exitSelection"><Icon name="X" />清空</button><button class="btn" :disabled="!selection.length || busy || progress.running" @click="confirmation = {action:'reset',paths:[...selection]}"><Icon name="RotateCcw" />重置</button><button class="btn primary" :disabled="!selection.length || busy || progress.running" @click="confirmation = {action:'identify',paths:[...selection]}"><Icon name="ScanSearch" />识别</button></div></Teleport>
  <Modal v-if="confirmation" :title="confirmation.action === 'reset' ? '重置所选作品' : '确认识别刮削'" compact confirmation @close="!busy && (confirmation = null)"><div class="modal-body"><p v-if="confirmation.action === 'reset'">将删除所选 {{ confirmation.paths.length }} 部作品目录下的所有非 STRM 文件，包括封面、NFO 和其他文件，并清除匹配记录。STRM 与目录保留，此操作不可撤销。</p><p v-else>将按刮削设置重新识别并刮削{{ confirmation.paths?.length ? `所选 ${confirmation.paths.length} 部作品` : '当前范围内的 STRM 库' }}，确认继续？</p></div><footer class="modal-footer"><button class="btn primary" :disabled="busy" @click="confirmAction">{{ confirmation.action === 'reset' ? '确认重置' : '确认识别' }}</button><button class="btn" :disabled="busy" @click="confirmation = null">取消</button></footer></Modal>
  <Modal v-if="resetting" title="重置作品元数据" compact confirmation @close="!busy && (resetting = null)"><div class="modal-body"><p>重置「{{ resetting.title }}」的识别结果、匹配信息和刮削状态？磁盘上的 NFO、封面和 STRM 文件会保留。</p></div><footer class="modal-footer"><button class="btn primary" :disabled="busy" @click="resetMetadata">确认重置</button><button class="btn" :disabled="busy" @click="resetting = null">取消</button></footer></Modal>
  <Modal v-if="settingsOpen" title="STRM 刮削设置" compact wide @close="settingsOpen = false"><form @submit.prevent="saveSettings"><div class="modal-body scrape-settings">
    <div class="field"><label>写入策略</label><RoundedSelect v-model="settings.writeMode" label="写入策略" :options="[{value:'missing',label:'仅补缺'}, {value:'overwrite',label:'覆盖已有'}]" /></div>
    <label class="toggle-line"><span>分集 NFO 与预览图</span><input v-model="settings.episodes" type="checkbox" /></label>
    <label class="toggle-line"><span>背景图</span><input v-model="settings.fanart" type="checkbox" /></label>
    <label class="toggle-line"><span>演员信息</span><input v-model="settings.actors" type="checkbox" /></label>
    <label>排除目录<input v-model="settings.excluded" placeholder="英文分号分隔" /></label>
  </div><footer class="modal-footer"><button class="btn primary" :disabled="busy"><Icon name="Save" />保存设置</button></footer></form></Modal>
  <ScrapeScopePicker v-if="scopeOpen" :task="task" :excluded="excludedScopes" @close="scopeOpen = false" @save="excludedScopes = $event; scopeOpen = false" />
  <Modal v-if="matching" title="选择媒体匹配" compact wide @close="matching = null; candidateRequest++"><form @submit.prevent="saveMatch"><div class="modal-body scrape-settings">
    <div class="match-source-path" :data-tooltip="workPath"><small>作品目录</small>{{ workPath }}</div>
    <div class="candidate-search"><RoundedSelect v-model="match.kind" label="媒体类型" :options="[{value:'movie',label:'电影'},{value:'tv',label:'电视剧'}]" @update:model-value="match.tmdb = ''; candidates = []; candidateRequest++; candidateBusy = false" /><input v-model="candidateQuery" aria-label="候选名称" @keydown.enter.prevent="searchCandidates" /><button type="button" class="btn" aria-label="搜索候选媒体" :disabled="candidateBusy" @click="searchCandidates"><Icon name="Search" />搜索</button></div>
    <p v-if="candidateError" class="error-message" role="alert">{{ candidateError }}</p><p v-else-if="candidateBusy" role="status">正在搜索候选媒体…</p>
    <div class="candidate-grid"><button v-for="candidate in candidates" :key="candidate.id" type="button" class="candidate-card" :aria-pressed="Number(match.tmdb) === candidate.id" :class="{ selected: Number(match.tmdb) === candidate.id }" @click="match.tmdb = candidate.id"><img v-if="candidate.poster" :src="candidate.poster" :alt="candidate.title || candidate.name" loading="lazy" referrerpolicy="no-referrer" /><span v-else class="candidate-poster"><Icon name="Film" /></span><span class="candidate-info"><strong :data-tooltip="candidate.title || candidate.name">{{ candidate.title || candidate.name }}</strong><span><em>{{ match.kind === 'tv' ? '电视剧' : '电影' }}</em><small>{{ candidate.year?.slice(0,4) || '年份未知' }}</small></span><small>TMDB {{ candidate.id }}</small></span></button></div>
    <label>TMDB ID<input v-model="match.tmdb" type="number" min="1" required /></label></div><footer class="modal-footer"><button class="btn primary" :disabled="busy || !match.tmdb">确认匹配</button></footer></form></Modal>
</template>
<style scoped>
.scrape-panel { min-height: 320px; display: flex; flex-direction: column; background: var(--surface); }
.scrape-panel.selecting { padding-bottom:86px; }
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
.scrape-card { position: relative; min-width: 0; height: 302px; margin-bottom: 12px; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; background: var(--surface); }
.scrape-poster { width: 100%; height: 238px; padding: 0; border: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; background: var(--bg); color: var(--muted); }
.scrape-poster img { width: 100%; height: 100%; object-fit: cover; }
.scrape-poster span { font-size: 12px; }
.scrape-card-body { min-width: 0; padding: 9px 10px; font-size: 14px; }
.scrape-title { display:flex; gap:7px; align-items:flex-start; }.scrape-title strong { min-width:0; flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.work-star { --status-color:#8b93a4; width:6px; height:6px; flex:none; margin-top:3px; border-radius:50%; background:var(--status-color); box-shadow:0 0 5px 1px color-mix(in srgb,var(--status-color) 55%,transparent); }
.work-star.pending { --status-color:#6e78d4; }.work-star.ok { --status-color:var(--green); }.work-star.miss,.work-star.doubt { --status-color:var(--amber); }.work-star.error { --status-color:var(--red); }
.scrape-card-body > small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.scrape-card-body > small { margin-top: 5px; color: var(--muted); font-size: 12px; }
.scrape-status { display: inline-flex; gap: 8px; margin-top: 7px; font-size: 12px; color: var(--muted); }
.scrape-status.ok { color: var(--success); }.scrape-status.error { color: var(--danger); }
.scrape-status em { font-style: normal; color: var(--muted); }
.scrape-row small { display: block; color: var(--muted); font-size: 12px; margin-top: 4px; }
.scrape-name, .scrape-status { min-width: 0; }
.scrape-name strong, .scrape-row small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; }
.scrape-status.ok strong { color: var(--success); }
.scrape-status.error strong { color: var(--danger); }
.scrape-actions { display: flex; gap:6px; position: absolute; left: 6px; right: 6px; top: 194px; opacity:0; pointer-events:none; transform:translateY(5px); transition:opacity .18s,transform .18s; }
.scrape-card:hover .scrape-actions, .scrape-card:focus-within .scrape-actions { opacity:1; pointer-events:auto; transform:none; }
.scrape-actions :deep(button) { background:var(--surface); border-radius:10px; box-shadow:0 2px 8px #0002; }
@media(hover:none) { .scrape-actions { opacity:1; pointer-events:auto; transform:none; } }
.scrape-actions :deep(button) { display:flex; gap:6px; justify-content:center; width: 50%; height: 38px; border-radius:8px; color:#fff; font-size:12px; background:#222633ba; backdrop-filter:blur(12px); box-shadow:0 2px 8px #0002; transition:transform .18s,background .18s; }
.scrape-actions :deep(button:last-child) { background:#4c438cbd; }
.scrape-actions :deep(button:hover:not(:disabled)) { transform:translateY(-3px); background:#323947df; }.scrape-actions :deep(button:last-child:hover:not(:disabled)) { background:#6457a9e6; }
.scrape-card.selected { border-color:#8585e1; box-shadow:inset 0 0 0 1px #8585e1,0 0 12px #7872d84d; }
.selecting .scrape-card { transition:transform .2s ease, border-color .2s ease; overflow:visible; }
.selecting .scrape-card:hover { transform:scale(1.025); z-index:2; }
.scrape-poster { position:relative; border-radius:7px 7px 0 0; overflow:hidden; }.poster-selection { position:absolute; left:9px; top:9px; color:var(--primary); background:var(--surface); border-radius:50%; width:24px; height:24px; }
.scrape-dock { position:fixed; z-index:140; bottom:20px; left:50%; transform:translateX(-50%); display:flex; align-items:center; gap:6px; max-width:calc(100vw - 24px); padding:8px 16px; border:1px solid color-mix(in srgb,var(--primary) 18%,var(--border)); border-radius:999px; background:color-mix(in srgb,var(--primary) 12%,var(--surface)); box-shadow:var(--shadow); font-size:13px; }
.scrape-dock strong { white-space:nowrap; font-size:13px; }.scrape-dock .btn { font-size:13px; border:0; background:transparent; color:var(--text); box-shadow:none; padding:8px; border-radius:0; }
.scrape-dock .btn:hover:not(:disabled), .scrape-dock .btn:focus-visible { background:transparent; color:var(--primary); }
.match-source-path { padding:10px 12px; background:var(--bg); border:1px solid var(--border); border-radius:8px; font-size:14px; overflow-wrap:anywhere; }.match-source-path small { display:block; color:var(--muted); font-size:12px; margin-bottom:7px; }
.scrape-body { min-height: 0; overflow-anchor: none; }
.scrape-body::-webkit-scrollbar { display: none; }
.scrape-panel > footer { padding: 8px 14px; color: var(--muted); font-size: 12px; }
.scrape-settings { display: grid; gap: 16px; }
.candidate-grid { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 12px; max-height: 360px; overflow: auto; padding: 2px; }
.candidate-card { display: flex; align-items:center; gap: 12px; min-width: 0; padding: 10px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); color: var(--text); text-align: left; }
.candidate-card:hover, .candidate-card.selected { border-color: var(--primary); background: var(--primary-soft); }
.candidate-card img, .candidate-poster { width: 72px; height:108px; flex:none; object-fit: cover; border-radius: 6px; background: var(--bg); display: grid; place-items: center; color: var(--muted); }
.candidate-info { min-width:0; display:flex; flex-direction:column; gap:7px; }.candidate-info > span { display:flex; gap:8px; align-items:center; }.candidate-info em { font-style:normal; font-size:12px; color:var(--primary); background:var(--primary-soft); padding:2px 6px; border-radius:5px; }
.candidate-card strong, .candidate-card small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.candidate-card small { color: var(--muted); font-size: 13px; }
.candidate-search { display: grid; grid-template-columns: 100px minmax(0,1fr) 90px; align-items: center; gap: 10px; }
.candidate-search > .rounded-select, .candidate-search > input { min-width: 0; width: 100%; }
.candidate-search :deep(.rounded-select-trigger) { min-width: 0; width: 100%; }
@media(max-width:900px) { .candidate-grid { grid-template-columns: repeat(2,minmax(0,1fr)); } }
@media(max-width:600px) { .candidate-grid { grid-template-columns: minmax(0,1fr); }.scrape-dock { left:50%; flex-wrap:wrap; justify-content:center; width:calc(100% - 24px); bottom:10px; }.candidate-search { grid-template-columns:90px minmax(0,1fr); }.candidate-search > button { grid-column:2; justify-self:end; }.scrape-actions { gap:4px; left:4px; right:4px; } }
@media(max-width:600px) { .scrape-dock { gap:2px; padding:8px; }.scrape-dock strong, .scrape-dock .btn { font-size:12px; }.scrape-dock .btn { padding:6px; gap:5px; } }
@media(prefers-reduced-motion:reduce) { .scrape-actions, .scrape-actions :deep(button), .selecting .scrape-card { transition:none; } }
@media(max-width:760px) { .scrape-toolbar { flex-wrap: wrap; }.scrape-toolbar .search-field { margin-left: 0; width: 160px; }.scrape-head,.scrape-row { grid-template-columns: minmax(0,1fr) 70px 80px; gap: 6px; }.scrape-head > :nth-child(2),.scrape-row > :nth-child(2) { display: none; } }
</style>
