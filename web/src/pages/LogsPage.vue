<script setup>
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, shallowRef, watch } from 'vue'
import { api, state, date, notify } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
const key = `aether-log-filters:${state.username}`
let saved = {}
try { saved = JSON.parse(localStorage.getItem(key) || '{}') || {} } catch {}
const filters = reactive({ query: typeof saved.query === 'string' ? saved.query : '', level: saved.level || 'all', module: saved.module || 'all', view: saved.view === 'raw' ? 'raw' : 'structured' })
const modules = { audit: '操作审计', files: '文件与备份', storage: '存储与服务', tasks: '任务管理', links: '以太链接', system: '系统' }
const levels = { info: '信息', warn: '警告', error: '错误', debug: '调试' }
if (!modules[filters.module]) filters.module = 'all'
if (!levels[filters.level]) filters.level = 'all'
const entries = shallowRef([]), busy = ref(false), viewport = ref(null), scroll = ref(0), height = ref(500)
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
function setRow(el, index) {
  const previous = rowNodes.get(index)
  if (previous === el) return
  if (previous) rowObserver?.unobserve(previous)
  if (el && filters.view === 'raw') { rowNodes.set(index, el); rowObserver?.observe(el) } else rowNodes.delete(index)
}
function resetRows() { rowHeights.clear(); scroll.value = 0; if (viewport.value) viewport.value.scrollTop = 0 }
watch(filters, () => { localStorage.setItem(key, JSON.stringify(filters)); resetRows() })
async function load() {
  if (busy.value) return
  busy.value = true
  try { entries.value = (await api('/logs')).map(l => ({ ...l, module: l.module || 'system', level: l.level === 'success' ? 'info' : ['cancelled', 'interrupted'].includes(l.level) ? 'warn' : l.level })).reverse(); resetRows() }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
let observer, rowObserver, previousWidth = 0
onMounted(() => {
  rowObserver = new ResizeObserver(rows => {
    const atBottom = totalHeight.value > height.value && scroll.value + height.value >= totalHeight.value - 2
    let changed = false
    for (const row of rows) {
      const index = Number(row.target.dataset.index)
      const size = Math.ceil(row.target.getBoundingClientRect().height)
      if (rowHeights.get(index) !== size) { rowHeights.set(index, size); changed = true }
    }
    if (changed && atBottom) nextTick(() => {
      if (viewport.value) viewport.value.scrollTop = Math.max(0, totalHeight.value - height.value)
    })
  })
  observer = new ResizeObserver(([entry]) => {
    height.value = entry.contentRect.height
    if (previousWidth !== entry.contentRect.width) { previousWidth = entry.contentRect.width; resetRows() }
  })
  observer.observe(viewport.value)
  load()
})
onUnmounted(() => { observer?.disconnect(); rowObserver?.disconnect(); rowNodes.clear() })
</script>
<template>
  <section class="log-panel" aria-label="系统日志">
    <div class="log-toolbar">
      <button class="icon-btn log-view-toggle" :aria-label="filters.view === 'raw' ? '当前原始列表，切换结构化列表' : '当前结构化列表，切换原始列表'" @click="filters.view = filters.view === 'raw' ? 'structured' : 'raw'"><Icon :name="filters.view === 'raw' ? 'Logs' : 'TableProperties'" :size="21" /></button>
      <RoundedSelect v-model="filters.level" label="日志级别" :options="[{ value: 'all', label: '全部级别' }, ...Object.entries(levels).map(([value, label]) => ({ value, label }))]" />
      <RoundedSelect v-model="filters.module" label="日志模块" :options="[{ value: 'all', label: '全部模块' }, ...Object.entries(modules).map(([value, label]) => ({ value, label }))]" />
      <button class="icon-btn" aria-label="刷新日志" :disabled="busy" @click="load"><Icon name="RefreshCw" :class="{ spin: busy }" /></button>
      <div class="search-field"><Icon name="Search" :size="16" /><input v-model="filters.query" aria-label="搜索日志" placeholder="搜索日志…" /></div>
    </div>
    <div ref="viewport" class="log-viewport" tabindex="0" aria-label="日志记录" @scroll="scroll = $event.target.scrollTop">
      <div :style="{ height: `${totalHeight}px`, position: 'relative' }">
        <div :style="{ transform: `translateY(${offsetAt(start)}px)` }">
          <div v-for="(entry, index) in visible" :key="`${filters.view}:${start + index}`" :ref="el => setRow(el, start + index)" :data-index="start + index" :data-level="entry.level" class="log-entry" :class="{ raw: filters.view === 'raw' }">
            <template v-if="filters.view === 'raw'"><strong class="raw-level">{{ entry.level.toUpperCase() }}</strong><code>{{ JSON.stringify(entry) }}</code></template>
            <template v-else><time>{{ date(entry.time) }}</time><span class="log-level" :data-level="entry.level">{{ levels[entry.level] || entry.level }}</span><span class="log-module">{{ modules[entry.module] || '系统' }}</span><span class="log-message">{{ entry.message }}</span></template>
          </div>
        </div>
      </div>
      <div v-if="!logs.length" class="small-empty">{{ busy ? '正在读取…' : '暂无匹配日志' }}</div>
    </div>
    <footer>{{ logs.length }} 条记录</footer>
  </section>
</template>
