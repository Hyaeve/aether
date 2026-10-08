<script setup>
import { computed } from 'vue'
import { state, bytes } from '../lib'
import Icon from './Icon.vue'
const props = defineProps({ tasks: { type: Array, default: () => [] } })
const total = computed(() => (state.cache.hits || 0) + (state.cache.misses || 0))
const rate = computed(() => total.value ? Math.round(state.cache.hits / total.value * 100) : 0)
const active = computed(() => props.tasks.filter(t => t.status === 'running'))
const metrics = computed(() => [
  ['缓存大小', bytes(state.cache.bytes || 0), 'Database'],
  ['缓存条目', `${state.cache.entries || 0} / ${state.settings.cacheMaxItems || 10000}`, 'Layers3'],
  ['命中', state.cache.hits || 0, 'CircleCheck'], ['未命中', state.cache.misses || 0, 'CircleDashed'],
  ['LRU 淘汰', state.cache.evictions || 0, 'LogOut'], ['过期', state.cache.expired || 0, 'FileClock']
])
</script>
<template>
  <section class="cache-overview" aria-label="缓存命中统计">
    <div class="cache-chart">
      <svg viewBox="0 0 100 100" role="img" :aria-label="`缓存命中率 ${rate}%`"><circle class="cache-track" cx="50" cy="50" r="40" /><circle class="cache-hit" cx="50" cy="50" r="40" pathLength="100" :stroke-dasharray="`${rate} 100`" /></svg>
      <span><strong>{{ total ? `${rate}%` : '—' }}</strong><small>命中率</small></span>
    </div>
    <div class="cache-progress"><strong>当前缓存任务</strong><div v-if="!active.length" class="cache-progress-item"><small>暂无执行中的任务</small><div class="cache-progress-track"><progress class="idle" value="0" max="100" aria-label="暂无执行中的缓存任务" /><span class="cache-progress-status">空闲</span></div></div><div v-for="task in active" :key="task.id" class="cache-progress-item"><span>{{ task.name }}</span><small>{{ task.message || `已扫描 ${task.processed || 0} 个目录` }}</small><div class="cache-progress-track running"><progress :aria-label="`${task.name}：正在扫描目录`" /><span class="cache-progress-status">扫描中</span></div></div></div>
    <dl class="cache-metrics"><div v-for="[label, value, icon] in metrics" :key="label"><dt><Icon :name="icon" :size="18" />{{ label }}</dt><dd>{{ value }}</dd></div></dl>
  </section>
</template>
<style scoped>
.cache-overview { display: grid; grid-template-columns: 112px minmax(160px, 1fr) minmax(320px, 1.1fr); gap: 18px; align-items: center; padding: 8px 0; border-bottom: 1px solid var(--border); margin-bottom: 10px; }
.cache-chart { width: 104px; height: 104px; position: relative; justify-self: center; }
.cache-chart svg { width: 100%; transform: rotate(-90deg); fill: none; stroke-width: 8; }
.cache-track { stroke: color-mix(in srgb, var(--text) 9%, transparent); }
.cache-hit { stroke: #8295d4; stroke-linecap: round; transition: stroke-dasharray .35s; }
.cache-chart span { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; }
.cache-chart strong { font-size: 17px; }
.cache-chart small, dt, .cache-progress small { color: var(--muted); font-size: 12px; }
.cache-metrics { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 8px 12px; margin: 0; }
.cache-metrics > div { position: relative; padding-left: 27px; min-width: 0; }
.cache-metrics dt { overflow-wrap: anywhere; }
.cache-metrics dt svg { position: absolute; left: 0; top: 50%; transform: translateY(-50%); }
.cache-metrics svg { color: var(--primary); flex-shrink: 0; }
.cache-metrics > div:nth-child(1) svg { color: #638cc6; }
.cache-metrics > div:nth-child(2) svg { color: #a184bf; }
.cache-metrics > div:nth-child(3) svg { color: #54a88b; }
.cache-metrics > div:nth-child(4) svg { color: #c39453; }
.cache-metrics > div:nth-child(5) svg { color: #c57e85; }
.cache-metrics > div:nth-child(6) svg { color: #799ba3; }
dd { margin: 3px 0 0; font-size: 15px; overflow-wrap: anywhere; }
.cache-progress { min-width: 0; }
.cache-progress strong { font-size: 14px; }
.cache-progress-item > span, .cache-progress small { display: block; overflow-wrap: anywhere; margin-top: 4px; }
.cache-progress-track { position: relative; height: 16px; margin-top: 6px; overflow: hidden; border-radius: 4px; background: color-mix(in srgb,var(--muted) 14%,var(--surface)); }
.cache-progress progress { display: block; appearance: none; width: 100%; height: 100%; border: 0; opacity: .3; accent-color: var(--primary); }
.cache-progress progress::-webkit-progress-bar { background: transparent; }
.cache-progress progress::-webkit-progress-value { background: var(--primary); }
.cache-progress progress::-moz-progress-bar { background: var(--primary); }
.cache-progress-track.running { background: color-mix(in srgb,var(--primary) 18%,var(--surface)); }
.cache-progress-status { position: absolute; inset: 0; display: grid; place-items: center; font-size: 12px; color: var(--text); pointer-events: none; }
@media(max-width: 1050px) { .cache-overview { grid-template-columns: 96px minmax(0,1fr); gap: 12px; } .cache-metrics { grid-column: 1 / -1; } }
@media(max-width: 480px) { .cache-metrics { grid-template-columns: repeat(2,minmax(0,1fr)); } }
</style>
