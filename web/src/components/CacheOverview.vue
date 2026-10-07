<script setup>
import { computed } from 'vue'
import { state, bytes } from '../lib'
const props = defineProps({ tasks: { type: Array, default: () => [] } })
const total = computed(() => (state.cache.hits || 0) + (state.cache.misses || 0))
const rate = computed(() => total.value ? Math.round(state.cache.hits / total.value * 100) : 0)
const active = computed(() => props.tasks.filter(t => t.status === 'running'))
const metrics = computed(() => [
  ['缓存大小（估算）', bytes(state.cache.bytes || 0)],
  ['缓存条目', `${state.cache.entries || 0} / ${state.settings.cacheMaxItems || 10000}`],
  ['命中', state.cache.hits || 0], ['未命中', state.cache.misses || 0],
  ['LRU 淘汰', state.cache.evictions || 0], ['过期', state.cache.expired || 0]
])
</script>
<template>
  <section class="cache-overview" aria-label="缓存命中统计">
    <div class="cache-chart">
      <svg viewBox="0 0 100 100" role="img" :aria-label="`缓存命中率 ${rate}%`"><circle class="cache-track" cx="50" cy="50" r="40" /><circle class="cache-hit" cx="50" cy="50" r="40" pathLength="100" :stroke-dasharray="`${rate} 100`" /></svg>
      <span><strong>{{ total ? `${rate}%` : '—' }}</strong><small>缓存命中率</small></span>
    </div>
    <dl class="cache-metrics"><div v-for="[label, value] in metrics" :key="label"><dt>{{ label }}</dt><dd>{{ value }}</dd></div></dl>
    <div class="cache-progress"><strong>当前缓存任务</strong><p v-if="!active.length" class="muted">暂无执行中的任务</p><div v-for="task in active" :key="task.id"><span>{{ task.name }}</span><small>{{ task.message || `已扫描 ${task.processed || 0} 个目录` }}</small><progress aria-label="正在扫描目录" /></div></div>
  </section>
</template>
<style scoped>
.cache-overview { display: grid; grid-template-columns: 150px minmax(240px, 1fr) minmax(200px, .8fr); gap: 24px; align-items: center; padding: 22px 0; border-bottom: 1px solid var(--border); margin-bottom: 20px; }
.cache-chart { width: 136px; height: 136px; position: relative; }
.cache-chart svg { width: 100%; transform: rotate(-90deg); fill: none; stroke-width: 8; }
.cache-track { stroke: color-mix(in srgb, var(--text) 9%, transparent); }
.cache-hit { stroke: #8295d4; stroke-linecap: round; transition: stroke-dasharray .35s; }
.cache-chart span { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; }
.cache-chart strong { font-size: 25px; }
.cache-chart small, dt, .cache-progress small { color: var(--muted); font-size: 12px; }
.cache-metrics { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 20px 16px; margin: 0; }
dd { margin: 6px 0 0; font-size: 17px; overflow-wrap: anywhere; }
.cache-progress { min-width: 0; }
.cache-progress strong { font-size: 14px; }
.cache-progress span, .cache-progress small { display: block; overflow-wrap: anywhere; margin-top: 8px; }
.cache-progress progress { width: 100%; height: 4px; accent-color: #8295d4; }
@media(max-width: 900px) { .cache-overview { grid-template-columns: 136px 1fr; gap: 14px; } .cache-progress { grid-column: 1 / -1; } }
@media(max-width: 480px) { .cache-metrics { grid-template-columns: repeat(2,minmax(0,1fr)); } }
</style>
