<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, notify } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import TaskTabs from '../components/TaskTabs.vue'
import Modal from '../components/Modal.vue'
import VirtualList from '../components/VirtualList.vue'
const task = ref(''), items = ref([]), progress = ref({}), busy = ref(false), settingsOpen = ref(false), matching = ref(null)
const query = ref(''), status = ref('all')
const settings = reactive({ writeMode: 'missing', episodes: true, fanart: false, actors: false, excluded: '' })
const match = reactive({ tmdb: '', kind: 'movie' })
const tasks = computed(() => state.tasks.filter(t => t.kind === 'strm').map(t => ({ value: t.id, label: t.name })))
const statuses = { pending: '待刮削', ok: '已完成', miss: '未匹配', doubt: '待确认', error: '失败' }
const filtered = computed(() => items.value.filter(i => (status.value === 'all' || i.status === status.value) && `${i.title} ${i.path}`.toLowerCase().includes(query.value.toLowerCase())))
let alive = true, timer, request = 0
async function load() {
  const id = task.value, run = ++request
  if (!id) { items.value = []; return }
  try { const result = await api(`/strm-scrape/items?taskId=${encodeURIComponent(id)}`); if (alive && run === request) items.value = result }
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
    const result = await api(`/strm-scrape/${name}`, 'POST', { taskId: task.value, path: item?.path || '' })
    if (name !== 'stop') progress.value = result
    else notify('正在停止刮削')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
async function saveSettings() {
  busy.value = true
  try { await api('/strm-scrape/settings', 'PUT', settings); settingsOpen.value = false; notify('刮削设置已保存') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
function rematch(item) { matching.value = item; Object.assign(match, { tmdb: item.tmdb || '', kind: item.kind }) }
async function saveMatch() {
  busy.value = true
  try {
    await api('/strm-scrape/match', 'POST', { taskId: task.value, path: matching.value.path, tmdb: Number(match.tmdb), kind: match.kind })
    matching.value = null; await load(); notify('匹配已保存')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
watch(task, load)
onMounted(async () => {
  poll()
  try { Object.assign(settings, await api('/strm-scrape/settings')) } catch (e) { notify(e.message, true) }
})
onUnmounted(() => { alive = false; clearTimeout(timer); request++ })
</script>
<template>
  <section class="task-heading"><TaskTabs /><div class="toolbar-right"><button class="btn" @click="settingsOpen = true"><Icon name="Settings2" />刮削设置</button><button v-if="progress.running" class="btn" :disabled="busy" @click="action('stop')"><Icon name="Square" />停止</button><button v-else class="btn primary" :disabled="!task || busy" @click="action('run')"><Icon name="Play" />开始刮削</button></div></section>
  <section class="scrape-panel">
    <div class="scrape-toolbar">
      <RoundedSelect v-model="task" label="STRM 任务" placeholder="选择 STRM 任务" :disabled="progress.running" :options="tasks" />
      <RoundedSelect v-model="status" label="刮削状态" :options="[{ value: 'all', label: '全部状态' }, ...Object.entries(statuses).map(([value,label]) => ({value,label}))]" />
      <div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索刮削记录" placeholder="搜索名称或路径" /></div>
      <button class="icon-btn" aria-label="刷新刮削索引" :disabled="!task || busy || progress.running" @click="action('scan')"><Icon name="RefreshCw" /></button>
    </div>
    <div class="scrape-progress"><span>{{ progress.running ? progress.message : progress.message || '等待执行' }}</span><small>{{ progress.done || 0 }} / {{ progress.total || 0 }}</small><progress :value="progress.done || 0" :max="progress.total || 1" /></div>
    <div class="scrape-head"><span>名称 / 路径</span><span>类型</span><span>状态</span><span>操作</span></div>
    <div class="scrape-body">
      <VirtualList v-if="filtered.length" :items="filtered" :row-height="66">
        <template #default="{ item }"><div class="scrape-row">
          <div class="scrape-name"><strong :data-tooltip="item.title">{{ item.title }}</strong><small :data-tooltip="item.path">{{ item.path }}</small></div>
          <span>{{ item.kind === 'tv' ? '电视剧' : '电影' }}<small v-if="item.tmdb">TMDB {{ item.tmdb }}</small></span>
          <span class="scrape-status" :class="item.status"><strong>{{ statuses[item.status] }}</strong><small :data-tooltip="item.message">{{ item.message }}</small></span>
          <div class="scrape-actions"><button class="icon-btn" aria-label="重新匹配" :disabled="progress.running || busy" @click="rematch(item)"><Icon name="ScanSearch" /></button><button class="icon-btn" aria-label="重新刮削" :disabled="progress.running || busy" @click="action('run', item)"><Icon name="RefreshCw" /></button></div>
        </div></template>
      </VirtualList>
      <div v-else class="small-empty">{{ task ? '暂无刮削记录' : '请选择 STRM 任务' }}</div>
    </div>
    <footer>{{ filtered.length }} 条记录</footer>
  </section>
  <Modal v-if="settingsOpen" title="STRM 刮削设置" compact @close="settingsOpen = false"><form @submit.prevent="saveSettings"><div class="modal-body scrape-settings">
    <div class="field"><label>写入策略</label><RoundedSelect v-model="settings.writeMode" label="写入策略" :options="[{value:'missing',label:'仅补缺'}, {value:'overwrite',label:'覆盖已有'}]" /></div>
    <label class="toggle-line"><span>分集 NFO 与预览图</span><input v-model="settings.episodes" type="checkbox" /></label>
    <label class="toggle-line"><span>背景图</span><input v-model="settings.fanart" type="checkbox" /></label>
    <label class="toggle-line"><span>演员信息</span><input v-model="settings.actors" type="checkbox" /></label>
    <label>排除目录<input v-model="settings.excluded" placeholder="英文分号分隔" /></label>
  </div><footer class="modal-footer"><button class="btn primary" :disabled="busy"><Icon name="Save" />保存设置</button></footer></form></Modal>
  <Modal v-if="matching" title="重新匹配" compact @close="matching = null"><form @submit.prevent="saveMatch"><div class="modal-body scrape-settings"><p>{{ matching.title }}</p><div class="field"><label>媒体类型</label><RoundedSelect v-model="match.kind" label="媒体类型" :options="[{value:'movie',label:'电影'},{value:'tv',label:'电视剧'}]" /></div><label>TMDB ID<input v-model="match.tmdb" type="number" min="1" required /></label></div><footer class="modal-footer"><button class="btn primary" :disabled="busy">确认匹配</button></footer></form></Modal>
</template>
<style scoped>
.scrape-panel { height: calc(100dvh - 180px); min-height: 320px; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); overflow: hidden; }
.scrape-toolbar { display: flex; align-items: center; gap: 10px; padding: 12px; }
.scrape-toolbar > .rounded-select:first-child { width: 220px; }
.scrape-toolbar > .rounded-select:nth-child(2) { width: 130px; }
.scrape-toolbar .search-field { width: 200px; margin-left: auto; }
.scrape-progress { display: flex; align-items: center; gap: 12px; padding: 0 14px 12px; font-size: 13px; color: var(--muted); }
.scrape-progress span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 50%; }
.scrape-progress progress { height: 4px; flex: 1; accent-color: var(--primary); }
.scrape-head, .scrape-row { display: grid; grid-template-columns: minmax(0,2fr) 100px minmax(140px,1fr) 84px; gap: 12px; align-items: center; padding: 8px 14px; }
.scrape-head { background: var(--bg); color: var(--muted); font-size: 13px; }
.scrape-row { height: 66px; box-sizing: border-box; border-bottom: 1px solid color-mix(in srgb,var(--border) 50%,transparent); font-size: 14px; }
.scrape-row small { display: block; color: var(--muted); font-size: 12px; margin-top: 4px; }
.scrape-name, .scrape-status { min-width: 0; }
.scrape-name strong, .scrape-row small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; }
.scrape-status.ok strong { color: var(--success); }
.scrape-status.error strong { color: var(--danger); }
.scrape-actions { display: flex; gap: 6px; }
.scrape-actions :deep(button) { width: 30px; height: 30px; }
.scrape-body { flex: 1; min-height: 0; }
.scrape-panel > footer { padding: 8px 14px; color: var(--muted); font-size: 12px; }
.scrape-settings { display: grid; gap: 16px; }
@media(max-width:760px) { .scrape-toolbar { flex-wrap: wrap; }.scrape-toolbar .search-field { margin-left: 0; width: 160px; }.scrape-head,.scrape-row { grid-template-columns: minmax(0,1fr) 70px 80px; gap: 6px; }.scrape-head > :nth-child(2),.scrape-row > :nth-child(2) { display: none; } }
</style>
