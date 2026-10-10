<script setup>
import { computed } from 'vue'
import { state, bytes } from '../lib'
import Icon from './Icon.vue'
import CacheHitChart from './CacheHitChart.vue'
const props = defineProps({ tasks: { type: Array, default: () => [] } })
const active = computed(() => props.tasks.filter(t => t.status === 'running'))
function scanDetail(task) {
  return (task.message || '').replace(/^已缓存 \d+ 个目录(?:\s*·\s*)?/, '').replace(/^正在扫描$/, '')
}
const metrics = computed(() => [
  ['缓存大小', bytes(state.cache.bytes || 0), 'Database'],
  ['缓存条目', `${state.cache.entries || 0} / ${state.settings.cacheMaxItems || 10000}`, 'Layers3'],
  ['命中', state.cache.hits || 0, 'CircleCheck'], ['未命中', state.cache.misses || 0, 'CircleDashed'],
  ['LRU 淘汰', state.cache.evictions || 0, 'LogOut'], ['过期', state.cache.expired || 0, 'FileClock']
])
</script>
<template>
  <section class="cache-overview" aria-label="缓存命中统计">
    <CacheHitChart :hits="state.cache.hits || 0" :misses="state.cache.misses || 0" />
    <div class="cache-progress">
      <div v-if="!active.length" class="cache-progress-item">
        <div class="cache-progress-heading"><small>暂无执行中的任务</small><span class="cache-progress-status">空闲</span></div>
        <div class="cache-progress-track"><progress class="idle" value="0" max="100" aria-label="暂无执行中的缓存任务" /></div>
      </div>
      <div v-for="task in active" :key="task.id" class="cache-progress-item">
        <div class="cache-progress-heading"><span class="cache-progress-name">{{ task.name }}</span><span class="cache-progress-status">扫描中</span></div>
        <small class="cache-progress-count" role="status">已缓存 {{ task.processed || 0 }} 个目录</small>
        <div class="cache-progress-track running" role="progressbar" :aria-label="`${task.name}：扫描中，已缓存 ${task.processed || 0} 个目录`" :aria-valuetext="`扫描中，已缓存 ${task.processed || 0} 个目录`"><span class="cache-progress-motion" /></div>
        <small v-if="scanDetail(task)" class="cache-progress-detail">{{ scanDetail(task) }}</small>
      </div>
    </div>
    <dl class="cache-metrics"><div v-for="[label, value, icon] in metrics" :key="label"><dt><Icon :name="icon" :size="18" />{{ label }}</dt><dd>{{ value }}</dd></div></dl>
  </section>
</template>
<style scoped>
.cache-overview { display: grid; grid-template-columns: 112px minmax(160px, 1fr) minmax(320px, 1.1fr); gap: 18px; align-items: center; padding: 8px 0; border-bottom: 1px solid var(--border); margin-bottom: 10px; }
dt, .cache-progress small { color: var(--muted); font-size: 12px; }
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
.cache-progress-item + .cache-progress-item { margin-top: 10px; }
.cache-progress-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-width: 0; }
.cache-progress-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.cache-progress-count, .cache-progress-detail { display: block; margin-top: 4px; }
.cache-progress-detail { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cache-progress-track { position: relative; height: 6px; margin-top: 6px; overflow: hidden; border-radius: 3px; background: color-mix(in srgb,var(--muted) 14%,var(--surface)); }
.cache-progress progress { display: block; appearance: none; width: 100%; height: 100%; border: 0; opacity: .3; accent-color: var(--primary); }
.cache-progress progress::-webkit-progress-bar { background: transparent; }
.cache-progress progress::-webkit-progress-value { background: var(--primary); }
.cache-progress progress::-moz-progress-bar { background: var(--primary); }
.cache-progress-track.running { background: color-mix(in srgb,var(--primary) 18%,var(--surface)); }
.cache-progress-status { flex-shrink: 0; font-size: 12px; color: var(--muted); }
.cache-progress-item:has(.running) .cache-progress-status { color: var(--primary); }
.cache-progress-motion { position: absolute; inset: 0 auto 0 0; width: 35%; border-radius: inherit; background: var(--primary); animation: cache-scan 1.8s ease-in-out infinite; }
@keyframes cache-scan { from { transform: translateX(-100%); } to { transform: translateX(286%); } }
@media(prefers-reduced-motion: reduce) { .cache-progress-motion { animation: none; left: 32.5%; } }
@media(max-width: 1050px) { .cache-overview { grid-template-columns: 96px minmax(0,1fr); gap: 12px; } .cache-metrics { grid-column: 1 / -1; } }
@media(max-width: 480px) { .cache-metrics { grid-template-columns: repeat(2,minmax(0,1fr)); } }
</style>
