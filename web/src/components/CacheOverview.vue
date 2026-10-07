<script setup>
import { computed } from 'vue'
import { state, bytes } from '../lib'
import Icon from './Icon.vue'
const props = defineProps({ tasks: { type: Array, default: () => [] } })
const total = computed(() => (state.cache.hits || 0) + (state.cache.misses || 0))
const rate = computed(() => total.value ? Math.round(state.cache.hits / total.value * 100) : 0)
const active = computed(() => props.tasks.filter(t => t.status === 'running'))
const metrics = computed(() => [
  ['缓存大小（估算）', bytes(state.cache.bytes || 0), 'Database'],
  ['缓存条目', `${state.cache.entries || 0} / ${state.settings.cacheMaxItems || 10000}`, 'Layers3'],
  ['命中', state.cache.hits || 0, 'CircleCheck'], ['未命中', state.cache.misses || 0, 'Search'],
  ['LRU 淘汰', state.cache.evictions || 0, 'ArrowLeftRight'], ['过期', state.cache.expired || 0, 'FileClock']
])
</script>
<template>
  <section class="cache-overview" aria-label="缓存命中统计">
    <div class="cache-chart">
      <svg viewBox="0 0 100 100" role="img" :aria-label="`缓存命中率 ${rate}%`"><circle class="cache-track" cx="50" cy="50" r="40" /><circle class="cache-hit" cx="50" cy="50" r="40" pathLength="100" :stroke-dasharray="`${rate} 100`" /></svg>
      <span><strong>{{ total ? `${rate}%` : '—' }}</strong><small>缓存命中率</small></span>
    </div>
    <div class="cache-progress"><strong>当前缓存任务</strong><div v-if="!active.length"><small>暂无执行中的任务</small><progress class="idle" value="0" max="100" aria-label="暂无执行中的缓存任务" /></div><div v-for="task in active" :key="task.id"><span>{{ task.name }}</span><small>{{ task.message || `已扫描 ${task.processed || 0} 个目录` }}</small><progress aria-label="正在扫描目录" /></div></div>
    <dl class="cache-metrics"><div v-for="[label, value, icon] in metrics" :key="label"><Icon :name="icon" :size="18" /><span><dt>{{ label }}</dt><dd>{{ value }}</dd></span></div></dl>
  </section>
</template>
<style scoped>
.cache-overview { display: grid; grid-template-columns: 102px minmax(180px, .85fr) minmax(350px, 1.3fr); gap: 26px; align-items: center; padding: 18px 0; border-bottom: 1px solid var(--border); margin-bottom: 20px; }
.cache-chart { width: 96px; height: 96px; position: relative; }
.cache-chart svg { width: 100%; transform: rotate(-90deg); fill: none; stroke-width: 8; }
.cache-track { stroke: color-mix(in srgb, var(--text) 9%, transparent); }
.cache-hit { stroke: #8295d4; stroke-linecap: round; transition: stroke-dasharray .35s; }
.cache-chart span { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; }
.cache-chart strong { font-size: 20px; }
.cache-chart small, dt, .cache-progress small { color: var(--muted); font-size: 12px; }
.cache-metrics { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 20px 16px; margin: 0; }
.cache-metrics > div { display: flex; align-items: center; gap: 9px; min-width: 0; }
.cache-metrics svg { color: var(--primary); flex-shrink: 0; }
dd { margin: 6px 0 0; font-size: 17px; overflow-wrap: anywhere; }
.cache-progress { min-width: 0; }
.cache-progress strong { font-size: 14px; }
.cache-progress span, .cache-progress small { display: block; overflow-wrap: anywhere; margin-top: 8px; }
.cache-progress progress { width: 100%; height: 4px; accent-color: #8295d4; }
.cache-progress progress.idle { appearance: none; border: 0; background: color-mix(in srgb,var(--muted) 20%,transparent); border-radius: 4px; }
.idle::-webkit-progress-bar { background: color-mix(in srgb,var(--muted) 20%,transparent); border-radius: 4px; }
@media(max-width: 1050px) { .cache-overview { grid-template-columns: 96px 1fr; gap: 18px; } .cache-metrics { grid-column: 1 / -1; } }
@media(max-width: 480px) { .cache-metrics { grid-template-columns: repeat(2,minmax(0,1fr)); } }
</style>
