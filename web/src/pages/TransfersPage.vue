<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, bytes, date, notify } from '../lib'
import Icon from '../components/Icon.vue'
const props = defineProps({ rules: Boolean })
const selected = ref('copy'), items = ref([]), error = ref(''), query = ref(''), busy = ref(false)
const modes = [{ id: 'copy', name: '复制', icon: 'Copy' }, { id: 'upload', name: '上传', icon: 'ArrowUp' }, { id: 'download', name: '下载', icon: 'Download' }]
const filtered = computed(() => items.value.filter(i => i.kind === selected.value && [i.name, i.storage, i.source, status(i)].some(v => String(v || '').toLowerCase().includes(query.value.trim().toLowerCase()))))
let timer, alive = true
async function clearFailed() {
  try { items.value = await api('/transfers', 'DELETE'); notify('失败记录已清除') } catch (e) { notify(e.message,true) }
}
async function load() {
  if (busy.value) return
  clearTimeout(timer); busy.value = true
  try { const result = await api('/transfers'); if (alive) { items.value = result; error.value = '' } }
  catch (e) { if (alive) error.value = e.message }
  finally { busy.value = false; if (alive) timer = setTimeout(load, 2000) }
}
const status = item => ({ running: '传输中', completed: '已结束', failed: '失败' }[item.status] || item.status)
onMounted(load)
onUnmounted(() => { alive = false; clearTimeout(timer) })
</script>
<template>
  <nav class="content-tabs"><RouterLink to="/transfer" :class="{ active: !rules }"><Icon name="ArrowLeftRight" />传输任务</RouterLink><RouterLink to="/transfer/backup" :class="{ active: rules }"><Icon name="ArchiveRestore" />备份规则</RouterLink><div v-if="!rules" class="transfer-actions"><button class="icon-btn" aria-label="清除失败记录" :disabled="!items.some(i => i.status === 'failed')" @click="clearFailed"><Icon name="Trash2" /></button><button class="icon-btn" aria-label="刷新传输任务" :disabled="busy" @click="load"><Icon name="RefreshCw" :class="{ spin: busy }" /></button><div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索传输任务" placeholder="搜索任务…" /></div></div></nav>
  <template v-if="!rules">
    <div class="transfer-modes" role="tablist" aria-label="传输类型"><button v-for="mode in modes" :key="mode.id" role="tab" :class="mode.id" :aria-selected="selected === mode.id" @click="selected = mode.id"><Icon :name="mode.icon" /><strong>{{ mode.name }}</strong><span>{{ items.filter(i => i.kind === mode.id && i.status === 'running').length }}</span></button></div>
    <p v-if="error" class="error-message">{{ error }}</p>
    <div class="transfer-table table-wrap"><table><thead><tr><th>文件</th><th>存储 / 来源</th><th>进度</th><th>状态</th><th>开始时间</th></tr></thead><tbody><tr v-for="item in filtered" :key="item.id"><td :data-tooltip="item.name">{{ item.name }}</td><td>{{ item.storage }}<small>{{ item.source }}</small></td><td><span>{{ bytes(item.done) }}<template v-if="item.total > 0"> / {{ bytes(item.total) }}</template></span><progress v-if="item.status === 'running'" :value="item.total > 0 ? Math.min(item.done, item.total) : undefined" :max="item.total > 0 ? item.total : 1" /></td><td :data-tooltip="item.message" data-tooltip-always>{{ status(item) }}</td><td>{{ date(item.started) }}</td></tr></tbody></table><p v-if="!filtered.length" class="small-empty">暂无传输任务</p></div>
  </template>
  <div v-else class="empty-state"><Icon name="ArchiveRestore" :size="32" /><h2>暂无备份规则</h2><p>备份规则引擎尚未接入。</p></div>
</template>
<style scoped>
.transfer-modes { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 14px; margin: 22px 0; }
.transfer-actions { margin-left: auto; display: flex; gap: 8px; align-items: center; }.transfer-actions .search-field { width: 200px; }
.transfer-modes button { display:flex; align-items:center; gap:12px; padding:20px; border:1px solid var(--border); border-radius:8px; background:var(--surface); color:var(--text); }
.transfer-modes .copy { --accent:#8872be; }.transfer-modes .download { --accent:var(--transfer-download); }.transfer-modes .upload { --accent:var(--transfer-upload); }
.transfer-modes button svg { color:var(--accent); }.transfer-modes button[aria-selected=true] { border-color:var(--accent); background:color-mix(in srgb,var(--accent) 12%,var(--surface)); }
.transfer-modes span { margin-left:auto; }.transfer-table td { max-width:280px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:13px; }.transfer-table small { display:block; color:var(--muted); }.transfer-table progress { display:block; width:100%; height:5px; margin-top:8px; accent-color:var(--primary); }
@media(max-width:600px) { .transfer-modes { gap:6px; }.transfer-modes button { padding:12px 8px; gap:5px; font-size:13px; }.transfer-modes svg { width:18px; } }
</style>
