<script setup>
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, shallowRef, watch } from 'vue'
import { api, date, notify, state } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import { useRoute } from 'vue-router'
const route = useRoute()
const filters = reactive({ query: '', level: 'all', module: 'all', view: 'structured' })
const viewKey = computed(() => state.username ? `aether-log-view:${state.username}` : '')
watch(viewKey, key => {
  try { filters.view = key && localStorage.getItem(key) === 'raw' ? 'raw' : 'structured' }
  catch { filters.view = 'structured' }
}, { immediate: true })
function toggleView() {
  filters.view = filters.view === 'raw' ? 'structured' : 'raw'
  if (viewKey.value) { try { localStorage.setItem(viewKey.value, filters.view) } catch {} }
}
const modules = { audit: '操作审计', files: '文件与备份', transfers: '传输中心', storage: '存储与服务', tasks: '任务中心', links: '以太链接', notifications: '通知服务', system: '系统' }
const levels = { info: '信息', warn: '警告', error: '错误', debug: '调试' }
if (!modules[filters.module]) filters.module = 'all'
if (!levels[filters.level]) filters.level = 'all'
const entries = shallowRef([]), busy = ref(false), viewport = ref(null), scrollHost = ref(null), scroll = ref(0), height = ref(500)
const logs = computed(() => {
  const query = filters.query.toLowerCase()
  return entries.value.filter(l => (filters.level === 'all' || filters.level === l.level) && (filters.module === 'all' || filters.module === l.module) && (!query || `${l.message} ${l.module} ${modules[l.module]} ${l.time}`.toLowerCase().includes(query)))
})
const rowHeights = reactive(new Map())
const rowNodes = new Map()
const offsets = computed(() => {
  if (filters.view !== 'raw') return []
  const result = [0]
  for (let i = 0; i < logs.value.length; i++) result.push(result[i] + (rowHeights.get(i) || 88))
  return result
})
const offsetAt = index => filters.view === 'raw' ? offsets.value[index] : index * 44
const totalHeight = computed(() => offsetAt(logs.value.length))
function indexAt(position) {
  if (filters.view !== 'raw') return Math.min(logs.value.length, Math.max(0, Math.floor(position / 44)))
  let low = 0, high = logs.value.length
  while (low < high) {
    const middle = (low + high) >>> 1
    if (offsets.value[middle + 1] <= position) low = middle + 1
    else high = middle
  }
  return low
}
const start = computed(() => Math.max(0, indexAt(scroll.value) - 5))
const visible = computed(() => logs.value.slice(start.value, indexAt(scroll.value + height.value) + 6))
const showBackTop = computed(() => scroll.value > Math.max(320, height.value * .75))
function backToTop() {
  scrollHost.value?.scrollTo({ top: 0, behavior: 'instant' })
  syncScroll()
  viewport.value?.focus({ preventScroll: true })
}
function setRow(el, index) {
  const previous = rowNodes.get(index)
  if (previous === el) return
  if (previous) rowObserver?.unobserve(previous)
  if (el && filters.view === 'raw') { rowNodes.set(index, el); rowObserver?.observe(el) } else rowNodes.delete(index)
}
function syncScroll() {
  const host = scrollHost.value, list = viewport.value
  if (!host || !list) return
  scroll.value = Math.max(0, host.getBoundingClientRect().top - list.getBoundingClientRect().top)
}
function resetRows() { rowHeights.clear(); scroll.value = 0; if (scrollHost.value) scrollHost.value.scrollTop = 0 }
watch(filters, resetRows)
let alive = true
async function load(background = false) {
  if (busy.value) return
  busy.value = true
  try {
    const incoming = (await api('/logs')).map(l => ({ ...l, module: l.module || 'system', level: l.level === 'success' ? 'info' : ['cancelled', 'interrupted'].includes(l.level) ? 'warn' : l.level })).reverse()
    if (!alive) return
    const anchor = logs.value[indexAt(scroll.value)], offset = scroll.value - offsetAt(indexAt(scroll.value)), wasScrolled = scroll.value > 0
    entries.value = incoming
    if (!background) { applyNotice(); resetRows() }
    else {
      rowHeights.clear()
      if (wasScrolled && anchor) {
        const index = logs.value.findIndex(l => l.time === anchor.time && l.module === anchor.module && l.message === anchor.message)
        await nextTick()
        if (alive && index >= 0 && scrollHost.value && viewport.value) {
          const host = scrollHost.value
          host.scrollTop += viewport.value.getBoundingClientRect().top - host.getBoundingClientRect().top + offsetAt(index) + offset
          syncScroll()
        }
      }
    }
  }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
function applyNotice() {
  if (!route.query.notice) return
  filters.level = 'all'; filters.module = modules[route.query.module] ? route.query.module : 'all'
  const exact = String(route.query.q || '')
  filters.query = entries.value.some(l => l.message.includes(exact)) ? exact : String(route.query.fallback || exact)
}
watch(() => route.fullPath, () => {
  if (route.path !== '/logs') return
  Object.assign(filters, { query: '', level: 'all', module: 'all' })
  applyNotice(); resetRows()
})
let observer, rowObserver, refreshTimer, previousWidth = 0
onMounted(() => {
  rowObserver = new ResizeObserver(rows => {
    const host = scrollHost.value
    const atBottom = host && host.scrollTop > 0 && host.scrollTop + host.clientHeight >= host.scrollHeight - 2
    let changed = false
    for (const row of rows) {
      const index = Number(row.target.dataset.index)
      const size = Math.ceil(row.target.getBoundingClientRect().height)
      if (rowHeights.get(index) !== size) { rowHeights.set(index, size); changed = true }
    }
    if (changed && atBottom) nextTick(() => {
      if (scrollHost.value) scrollHost.value.scrollTop = scrollHost.value.scrollHeight
    })
  })
  scrollHost.value = viewport.value?.closest('.thin-scroll-area')
  observer = new ResizeObserver(() => {
    const host = scrollHost.value
    height.value = host?.clientHeight || 500
    const width = viewport.value.clientWidth
    if (previousWidth !== width) { previousWidth = width; resetRows() }
    syncScroll()
  })
  observer.observe(viewport.value)
  if (scrollHost.value) observer.observe(scrollHost.value)
  scrollHost.value?.addEventListener('scroll', syncScroll, { passive: true })
  syncScroll()
  load(); refreshTimer = setInterval(() => { if (document.visibilityState === 'visible') load(true) }, 5000)
})
onUnmounted(() => { alive = false; clearInterval(refreshTimer); observer?.disconnect(); rowObserver?.disconnect(); scrollHost.value?.removeEventListener('scroll', syncScroll); rowNodes.clear() })
</script>
<template>
  <section class="log-panel" aria-label="运行日志">
    <div class="log-toolbar">
      <button class="icon-btn log-view-toggle" :aria-label="filters.view === 'raw' ? '当前原始列表，切换结构化列表' : '当前结构化列表，切换原始列表'" @click="toggleView"><Icon :name="filters.view === 'raw' ? 'Logs' : 'TableProperties'" :size="21" /></button>
      <RoundedSelect v-model="filters.level" label="日志级别" :options="[{ value: 'all', label: '全部级别' }, ...Object.entries(levels).map(([value, label]) => ({ value, label }))]" />
      <RoundedSelect v-model="filters.module" label="日志模块" :options="[{ value: 'all', label: '全部模块' }, ...Object.entries(modules).map(([value, label]) => ({ value, label }))]" />
      <button class="icon-btn" aria-label="刷新日志" :disabled="busy" @click="load()"><Icon name="RefreshCw" :class="{ spin: busy }" /></button>
      <div class="search-field"><Icon name="Search" :size="16" /><input v-model="filters.query" aria-label="搜索日志" placeholder="搜索日志…" /></div>
    </div>
    <div class="log-list-shell"><div ref="viewport" class="log-viewport" tabindex="0" aria-label="日志记录" :style="{ height: logs.length ? `${totalHeight}px` : undefined }">
      <div :style="{ height: `${totalHeight}px`, position: 'relative' }">
        <div :style="{ transform: `translateY(${offsetAt(start)}px)` }">
          <div v-for="(entry, index) in visible" :key="`${filters.view}:${start + index}`" :ref="el => setRow(el, start + index)" :data-index="start + index" :data-level="entry.level" class="log-entry" :class="{ raw: filters.view === 'raw', striped: (start + index) % 2 === 1 }">
            <template v-if="filters.view === 'raw'"><strong class="raw-level">{{ entry.level.toUpperCase() }}</strong><code><time class="raw-time">{{ entry.time }}</time> {{ JSON.stringify({ module: entry.module, message: entry.message }) }}</code></template>
            <template v-else><time>{{ date(entry.time) }}</time><span class="log-level" :data-level="entry.level">{{ levels[entry.level] || entry.level }}</span><span class="log-module">{{ modules[entry.module] || '系统' }}</span><span class="log-message">{{ entry.message }}</span></template>
          </div>
        </div>
      </div>
      <div v-if="!logs.length" class="small-empty">{{ busy ? '正在读取…' : '暂无匹配日志' }}</div>
    </div></div>
    <footer>{{ logs.length }} 条记录</footer>
  </section>
  <button v-if="showBackTop" class="log-back-top" type="button" aria-label="回到顶部" data-tooltip="回到顶部" data-tooltip-always @click="backToTop"><Icon name="ArrowUp" :size="23" /></button>
</template>
<style scoped>
:root:not([data-theme="dark"]) .log-entry:not(.raw) { background: var(--input); }
:root:not([data-theme="dark"]) .log-entry:not(.raw).striped { background: color-mix(in srgb, var(--text) 2%, var(--input)); }
.log-back-top { position:fixed; right:calc(28px + env(safe-area-inset-right,0px)); bottom:calc(28px + env(safe-area-inset-bottom,0px)); z-index:32; width:46px; height:46px; padding:0; display:grid; place-items:center; border:1px solid var(--border); border-radius:50%; background:var(--surface); color:var(--text); box-shadow:var(--shadow); transition:color .16s,transform .16s; }
.log-back-top:hover { color:var(--primary); transform:translateY(-2px); }
@media(prefers-reduced-motion:reduce) { .log-back-top { transition:none; } }
</style>
