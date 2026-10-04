<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, date, notify } from '../lib'
import Icon from '../components/Icon.vue'
const key = `aether-log-filters:${state.username}`
let saved = {}
try { saved = JSON.parse(localStorage.getItem(key) || '{}') || {} } catch {}
const filters = reactive({ query: typeof saved.query === 'string' ? saved.query : '', level: saved.level || 'all', module: saved.module || 'all', view: saved.view === 'raw' ? 'raw' : 'structured' })
const modules = { audit: '操作审计', files: '文件与备份', storage: '存储与服务', tasks: '任务管理', links: '以太链接', system: '系统' }
const levels = { info: '信息', warn: '警告', error: '错误', debug: '调试' }
if (!modules[filters.module]) filters.module = 'all'
if (!levels[filters.level]) filters.level = 'all'
const entries = ref([]), busy = ref(false), viewport = ref(null), scroll = ref(0), height = ref(500)
const logs = computed(() => entries.value.filter(l => (filters.level === 'all' || filters.level === l.level) && (filters.module === 'all' || filters.module === l.module) && `${l.message} ${l.module} ${modules[l.module]} ${l.time}`.toLowerCase().includes(filters.query.toLowerCase())).slice().reverse())
const rowHeights = reactive(new Map())
const rowNodes = new Map()
const offsets = computed(() => {
  const result = [0]
  for (let i = 0; i < logs.value.length; i++) result.push(result[i] + (filters.view === 'raw' ? rowHeights.get(i) || 88 : 44))
  return result
})
function indexAt(position) {
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
  if (el) { rowNodes.set(index, el); rowObserver?.observe(el) } else rowNodes.delete(index)
}
function resetRows() { rowHeights.clear(); scroll.value = 0; if (viewport.value) viewport.value.scrollTop = 0 }
watch(filters, () => { localStorage.setItem(key, JSON.stringify(filters)); resetRows() })
async function load() {
  busy.value = true
  try { entries.value = (await api('/logs')).map(l => ({ ...l, module: l.module || 'system', level: l.level === 'success' ? 'info' : ['cancelled', 'interrupted'].includes(l.level) ? 'warn' : l.level })); resetRows() }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
let observer, rowObserver, previousWidth = 0
onMounted(() => {
  rowObserver = new ResizeObserver(rows => {
    for (const row of rows) {
      const index = Number(row.target.dataset.index)
      const size = Math.ceil(row.target.getBoundingClientRect().height)
      if (rowHeights.get(index) !== size) rowHeights.set(index, size)
    }
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
      <button class="icon-btn log-view-toggle" :aria-label="filters.view === 'raw' ? '当前原始列表，切换结构化列表' : '当前结构化列表，切换原始列表'" :title="filters.view === 'raw' ? '原始列表 · 点击切换结构化列表' : '结构化列表 · 点击切换原始列表'" @click="filters.view = filters.view === 'raw' ? 'structured' : 'raw'"><Icon :name="filters.view === 'raw' ? 'Logs' : 'TableProperties'" :size="21" /></button>
      <div class="search-field"><Icon name="Search" :size="16" /><input v-model="filters.query" aria-label="搜索日志" placeholder="搜索日志…" /></div>
      <select v-model="filters.level" aria-label="日志级别"><option value="all">全部级别</option><option v-for="(label, value) in levels" :key="value" :value="value">{{ label }}</option></select>
      <select v-model="filters.module" aria-label="日志模块"><option value="all">全部模块</option><option v-for="(label, value) in modules" :key="value" :value="value">{{ label }}</option></select>
      <button class="icon-btn" aria-label="刷新日志" title="刷新日志" :disabled="busy" @click="load"><Icon name="RefreshCw" :class="{ spin: busy }" /></button>
    </div>
    <div ref="viewport" class="log-viewport" tabindex="0" aria-label="日志记录" @scroll="scroll = $event.target.scrollTop">
      <div :style="{ height: `${offsets.at(-1)}px`, position: 'relative' }">
        <div :style="{ transform: `translateY(${offsets[start]}px)` }">
          <div v-for="(entry, index) in visible" :key="`${filters.view}:${start + index}`" :ref="el => setRow(el, start + index)" :data-index="start + index" :data-level="entry.level" class="log-entry" :class="{ raw: filters.view === 'raw' }" :title="entry.message">
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
