<script setup>
import { computed, useId } from 'vue'
const props = defineProps({ hits: { type: Number, default: 0 }, misses: { type: Number, default: 0 }, label: { type: String, default: '命中率' }, showValue: { type: Boolean, default: true }, task: Boolean })
const gradient = `task-rate-${useId()}`
const total = computed(() => props.hits + props.misses)
const rate = computed(() => total.value > 0 ? Math.max(0, Math.min(100, Math.round(props.hits / total.value * 100))) : 0)
const chartLabel = computed(() => total.value ? `${props.label === '命中率' ? '缓存命中率' : props.label} ${rate.value}%` : props.label === '命中率' ? '缓存暂无访问记录' : `${props.label}暂无记录`)
</script>
<template>
  <div class="cache-chart">
    <svg viewBox="0 0 100 100" role="img" :aria-label="chartLabel">
      <defs v-if="task"><linearGradient :id="gradient" x1="0" y1="0" x2="1" y2="1"><stop class="rate-start" offset="0" /><stop class="rate-end" offset="1" /></linearGradient></defs>
      <circle class="cache-track" :class="{ sampled: total > 0 }" cx="50" cy="50" r="40" />
      <circle v-show="rate > 0" class="cache-hit" :style="task ? {stroke:`url(#${gradient})`} : undefined" cx="50" cy="50" r="40" pathLength="100" :stroke-dasharray="`${rate} 100`" />
    </svg>
    <span :class="{'label-only':!showValue}"><strong v-if="showValue">{{ total ? `${rate}%` : '—' }}</strong><small>{{ label }}</small></span>
  </div>
</template>
<style scoped>
.cache-chart { width: var(--cache-chart-size,104px); height: var(--cache-chart-size,104px); position: relative; justify-self: center; flex-shrink: 0; }
.cache-chart svg { width: 100%; transform: rotate(-90deg); fill: none; stroke-width: 8; }
.cache-track { stroke: color-mix(in srgb,var(--text) 9%,transparent); }
.cache-track.sampled { stroke: var(--cache-miss-color,color-mix(in srgb,var(--text) 9%,transparent)); }
.cache-hit { stroke: var(--cache-hit-color,#8295d4); stroke-linecap: round; transition: stroke-dasharray .35s; }
.rate-start { stop-color:#5368a9; }.rate-end { stop-color:#6b5a93; }
[data-theme=dark] .rate-start { stop-color:#9baae6; }[data-theme=dark] .rate-end { stop-color:#b39bdc; }
.cache-chart span { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; }
.cache-chart strong { font-size: 17px; font-weight: 600; font-variant-numeric: tabular-nums; }
.cache-chart small { color: var(--muted); font-size: 12px; margin-top: 4px; }
.cache-chart .label-only small { margin:0; font-size:14px; color:var(--text); }
@media(prefers-reduced-motion:reduce) { .cache-hit { transition:none; } }
</style>
