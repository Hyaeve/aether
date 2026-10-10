<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, bytes, date, notify } from '../lib'
import Icon from '../components/Icon.vue'
import ThinScroll from '../components/ThinScroll.vue'
import BackupRules from '../components/BackupRules.vue'
import { useRoute } from 'vue-router'
import { state } from '../lib'
import { readSession, writeSession } from '../session-navigation'
import { useVirtualList } from '../virtual-list'
import ScrollRail from '../components/ScrollRail.vue'
import { listWheel } from '../nested-scroll'
const props = defineProps({ rules: Boolean })
const backup = ref(null)
const selected = ref('copy'), items = ref([]), error = ref(''), query = ref(''), busy = ref(false)
const route = useRoute()
const validMode = value => ['copy','upload','download'].includes(value)
selected.value = validMode(route.query.mode) ? route.query.mode : readSession(state.username, 'transfer-mode') || 'copy'
watch(() => route.query.mode, value => { if(validMode(value)) selected.value = value })
watch(selected, value => writeSession(state.username, 'transfer-mode', value), { immediate:true })
const viewport = ref(null)
const chosen = ref(''), menu = ref(null)
function context(event, item) { chosen.value = item.id; menu.value = { item, x: Math.max(8, Math.min(event.clientX, innerWidth - 150)), y: Math.max(8, Math.min(event.clientY, innerHeight - 140)) } }
function closeMenu() { menu.value = null }
async function control(action) {
  const id = menu.value?.item.id
  closeMenu()
  try { await api('/transfers/action', 'POST', { id, action }); notify({ pause: '传输已暂停', resume: '传输已继续', delete: '传输已删除' }[action]); await load() } catch (e) { notify(e.message, true) }
}
const modes = [{ id: 'copy', name: '复制', icon: 'Copy' }, { id: 'upload', name: '上传', icon: 'Upload' }, { id: 'download', name: '下载', icon: 'Download' }]
const filtered = computed(() => items.value.filter(i => i.kind === selected.value && [i.name, i.storage, i.source, status(i)].some(v => String(v || '').toLowerCase().includes(query.value.trim().toLowerCase()))))
const { shown, top, bottom, reset } = useVirtualList(filtered, viewport, { rowHeight:64 })
watch([selected,query], reset)
const percent = item => item.status === 'completed' ? 100 : item.total > 0 ? Math.min(100,Math.floor(item.done/item.total*100)) : 0
let timer, alive = true
async function clearFailed() {
  try { items.value = await api('/transfers', 'DELETE'); notify('失败记录已清除') } catch (e) { notify(e.message,true) }
}
async function load() {
  if (busy.value) return
  clearTimeout(timer); busy.value = true
  try { const result = await api('/transfers'); if (alive) { items.value = result; error.value = '' } }
  catch (e) { if (alive) error.value = e.message }
  finally { busy.value = false; if (alive && !props.rules) timer = setTimeout(load, 2000) }
}
watch(() => props.rules, rules => { closeMenu(); clearTimeout(timer); if (!rules) load() })
const status = item => ({ running: '传输中', paused: '已暂停', completed: '成功', failed: '失败', queued:'排队' }[item.status] || item.status)
onMounted(() => { if (!props.rules) load(); document.addEventListener('click', closeMenu) })
onUnmounted(() => { alive = false; clearTimeout(timer); document.removeEventListener('click', closeMenu) })
</script>
<template>
  <section class="task-heading"><nav v-tab-scroll class="content-tabs"><RouterLink to="/transfer" :class="{ active: !rules }"><Icon name="ArrowLeftRight" />传输任务</RouterLink><RouterLink to="/transfer/backup" :class="{ active: rules }"><Icon name="ArchiveRestore" />备份规则</RouterLink></nav><div v-if="!rules" class="transfer-actions"><button class="icon-btn" aria-label="清除失败记录" :disabled="!items.some(i => i.status === 'failed')" @click="clearFailed"><Icon name="Trash2" /></button><button class="icon-btn" aria-label="刷新传输任务" :disabled="busy" @click="load"><Icon name="RefreshCw" :class="{ spin: busy }" /></button><div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索传输任务" placeholder="搜索任务…" /></div></div><div v-else class="transfer-actions"><button class="icon-btn" aria-label="刷新备份规则" @click="backup?.load()"><Icon name="RefreshCw" /></button><button class="btn primary" @click="backup?.open()"><Icon name="Plus" />添加备份</button></div></section>
  <section class="transfer-content" :class="{'backup-content':rules, 'has-transfers':!rules && filtered.length}">
  <template v-if="!rules">
    <div class="transfer-modes" role="tablist" aria-label="传输类型"><button v-for="mode in modes" :key="mode.id" role="tab" :class="mode.id" :aria-selected="selected === mode.id" @click="selected = mode.id"><Icon :name="mode.icon" /><strong>{{ mode.name }}</strong><span>{{ items.filter(i => i.kind === mode.id && i.status === 'running').length }}</span></button></div>
    <p v-if="error" class="error-message">{{ error }}</p>
    <div class="transfer-table"><div class="transfer-header" role="row"><span role="columnheader">文件</span><span role="columnheader">存储 / 来源</span><span role="columnheader">进度</span><span role="columnheader">大小</span><span role="columnheader">速度</span><span role="columnheader">状态</span><span role="columnheader">开始时间</span></div><div ref="viewport" class="transfer-viewport" @wheel="listWheel"><table aria-label="传输任务"><tbody><tr v-if="top" class="transfer-spacer" :style="{height:top+'px'}" aria-hidden="true"><td colspan="7" /></tr><tr v-for="item in shown" :key="item.id" :class="[item.status,{ selected: chosen === item.id }]" @click="chosen = item.id" @contextmenu.prevent.stop="context($event, item)"><td :title="item.name">{{ item.name }}</td><td>{{ item.storage }}<small>{{ item.source }}</small></td><td><span>{{ item.total > 0 || item.status === 'completed' ? percent(item)+'%' : bytes(item.done) }}</span><progress :value="percent(item)" max="100" :aria-label="item.name+'进度'" /></td><td>{{ item.total > 0 ? bytes(item.total) : '—' }}</td><td>{{ item.status === 'running' ? bytes(item.speed || 0)+'/s' : '—' }}</td><td><span class="transfer-status" :class="item.status" :title="item.message">{{ status(item) }}</span></td><td>{{ date(item.started) }}</td></tr><tr v-if="bottom" class="transfer-spacer" :style="{height:bottom+'px'}" aria-hidden="true"><td colspan="7" /></tr></tbody></table><p v-if="!filtered.length" class="small-empty">暂无传输任务</p></div><ScrollRail :element="viewport" :thickness="3" /></div>
  </template>
  <BackupRules v-else ref="backup" />
  </section>
  <Teleport to="body"><div v-if="menu" class="context-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop><button :disabled="menu.item.status !== 'paused' || menu.item.kind === 'copy'" @click="control('resume')"><Icon name="Play" />继续</button><button :disabled="menu.item.status !== 'running' || menu.item.kind === 'copy'" @click="control('pause')"><Icon name="Pause" />暂停</button><button class="danger-text" :disabled="menu.item.kind === 'copy' && menu.item.status === 'running'" @click="control('delete')"><Icon name="Trash2" />删除</button></div></Teleport>
</template>
<style scoped>
.transfer-table { display:flex; flex-direction:column; position:relative; border:1px solid var(--border); border-radius:8px; background:var(--surface); overflow:hidden !important; }
.transfer-header,.transfer-table tr:not(.transfer-spacer) { display:grid; grid-template-columns:minmax(120px,2fr) minmax(100px,1.2fr) minmax(100px,1.1fr) 85px 90px 72px 150px; align-items:center; }
.transfer-header { flex:none; height:42px; border-bottom:1px solid var(--border); background:var(--surface-soft,var(--surface)); font-size:12px; color:var(--muted); }
.transfer-header span,.transfer-table td { padding:8px 12px; min-width:0; }
.transfer-viewport { flex:1; min-height:0; overflow:auto; scrollbar-width:none; overscroll-behavior:contain; }.transfer-viewport::-webkit-scrollbar { display:none; }
.transfer-table table,.transfer-table tbody { display:block; width:100%; }.transfer-table tr:not(.transfer-spacer) { height:64px; border-bottom:1px solid var(--border); --progress-color:var(--transfer-download); }.transfer-table tr.completed { --progress-color:var(--green); }.transfer-table tr.failed { --progress-color:var(--red); }.transfer-table tr.paused,.transfer-table tr.queued { --progress-color:var(--amber); }
.transfer-table td { border:0; }.transfer-table .transfer-spacer td { padding:0; border:0; }.transfer-table .transfer-spacer { display:block; }
.transfer-table progress { appearance:none; border:0; border-radius:3px; overflow:hidden; background:var(--border); }.transfer-table progress::-webkit-progress-bar { background:var(--border); }.transfer-table progress::-webkit-progress-value { background:var(--progress-color); }.transfer-table progress::-moz-progress-bar { background:var(--progress-color); }
.transfer-status { font-size:12px; }.transfer-status.running { color:var(--transfer-download); }.transfer-status.completed { color:var(--green); }.transfer-status.failed { color:var(--red); }.transfer-status.queued,.transfer-status.paused { color:var(--amber); }
.transfer-table :deep(.scroll-rail) { top:42px; }.transfer-table tr.running { --progress-color:var(--transfer-upload); }.transfer-status.running { color:var(--transfer-upload); }
@media(max-width:1000px) { .transfer-header,.transfer-table tr:not(.transfer-spacer) { grid-template-columns:minmax(90px,1.4fr) minmax(70px,1fr) 85px 70px 75px 60px; }.transfer-header span:last-child,.transfer-table td:last-child { display:none; }.transfer-header span,.transfer-table td { padding-inline:7px; } }
@media(max-width:600px) { .transfer-header,.transfer-table tr:not(.transfer-spacer) { grid-template-columns:minmax(60px,1fr) 65px 65px 58px 57px; }.transfer-header span:nth-child(2),.transfer-table td:nth-child(2) { display:none; }.transfer-table td { font-size:11px !important; }.transfer-header { font-size:11px; }.transfer-header span,.transfer-table td { padding-inline:4px; } }
.transfer-modes { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 14px; margin: 22px 0; }
.transfer-table { max-height: calc(100dvh - 270px); overflow: auto; }
.transfer-table tr.selected { background: color-mix(in srgb,var(--primary) 8%,transparent); }
.transfer-actions { margin-left: auto; display: flex; gap: 8px; align-items: center; }.transfer-actions .search-field { width: 200px; }
.transfer-modes button { display:flex; align-items:center; gap:12px; padding:20px; border:1px solid var(--border); border-radius:8px; background:var(--surface); color:var(--text); }
.transfer-modes .copy { --accent:#8872be; }.transfer-modes .download { --accent:var(--transfer-download); }.transfer-modes .upload { --accent:var(--transfer-upload); }
.transfer-modes button svg { color:var(--accent); }.transfer-modes button[aria-selected=true] { border-color:var(--accent); background:color-mix(in srgb,var(--accent) 12%,var(--surface)); }
.transfer-modes span { margin-left:auto; }.transfer-table td { max-width:280px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:13px; }.transfer-table small { display:block; color:var(--muted); }.transfer-table progress { display:block; width:100%; height:5px; margin-top:8px; accent-color:var(--primary); }
@media(max-width:600px) { .transfer-modes { gap:6px; }.transfer-modes button { padding:12px 8px; gap:5px; font-size:13px; }.transfer-modes svg { width:18px; } }
@media(max-width:600px) { .transfer-actions { gap:4px; }.transfer-actions .search-field { width:90px; padding-inline:7px; }.transfer-actions > .icon-btn { width:30px; min-width:30px; }.task-heading { gap:6px; }.task-heading .content-tabs { min-width:82px; } }
.transfer-content { min-height:0; flex:1; display:flex; flex-direction:column; }.transfer-table { flex:1; min-height:180px; max-height:none; height:auto; }.has-transfers .transfer-table { flex:none; height:calc(100dvh - 48px); }.transfer-modes { margin:12px 0 16px; flex:none; }.backup-content { padding:16px; border:1px solid var(--border); border-radius:8px; background:var(--surface); min-height:calc(100dvh - 150px); }.backup-content :deep(.backup-grid) { align-content:start; }
</style>
