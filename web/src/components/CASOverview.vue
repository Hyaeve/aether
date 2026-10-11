<script setup>
import { computed } from 'vue'
import CacheHitChart from './CacheHitChart.vue'
import Icon from './Icon.vue'
import { BadgeCheck, FileWarning } from 'lucide-vue-next'

const props = defineProps({ tasks: { type: Array, default: () => [] } })
const casTasks = computed(() => props.tasks.filter(t => !t.kind || t.kind === 'cas'))
const active = computed(() => casTasks.value.filter(t => t.status === 'running'))
// One latest terminal result per executed task, including cancelled/interrupted runs.
const executed = computed(() => casTasks.value.filter(t =>
  ['success', 'error', 'cancelled', 'interrupted'].includes(t.status) &&
  typeof t.lastRun === 'string' && !t.lastRun.startsWith('0001') && Number.isFinite(Date.parse(t.lastRun))
))
const successes = computed(() => executed.value.filter(t => t.status === 'success').length)
function count(value) { return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0 }
const generated = computed(() => casTasks.value.reduce((sum, t) => sum + count(t.casGenerated), 0))
const failed = computed(() => casTasks.value.reduce((sum, t) => sum + count(t.casFailed), 0))
</script>

<template>
  <div class="cas-overview-area">
    <section class="cas-overview" aria-label="CAS 生成统计">
      <CacheHitChart label="成功率" :hits="successes" :misses="executed.length - successes" title="按各任务最近一次执行结果统计：成功 /（成功 + 失败 + 取消 + 中断），仅计入有有效执行时间的已结束任务" />
      <div class="cas-progress">
        <div v-if="!active.length" class="cas-progress-item">
          <div class="cas-progress-heading"><small>暂无执行中的任务</small><span class="cas-progress-status">空闲</span></div>
          <div class="cas-progress-track"><progress class="idle" value="0" max="100" aria-label="暂无执行中的 CAS 任务" /></div>
        </div>
        <div v-for="task in active" :key="task.id" class="cas-progress-item">
          <div class="cas-progress-heading"><span class="cas-progress-name" :title="task.name">{{ task.name }}</span><span class="cas-progress-status">扫描中</span></div>
          <small class="cas-progress-count" role="status">已处理 {{ count(task.processed) }} 项 · 已生成 {{ count(task.casGenerated) }} · 异常 {{ count(task.casFailed) }}</small>
          <div class="cas-progress-track running" role="progressbar" :aria-label="`${task.name}：扫描中，已处理 ${count(task.processed)} 项`" :aria-valuetext="`扫描中，已处理 ${count(task.processed)} 项`"><span class="cas-progress-motion" /></div>
          <small v-if="task.message" class="cas-progress-detail" :title="task.message">{{ task.message }}</small>
        </div>
      </div>
      <dl class="cas-metrics">
        <div><dt><BadgeCheck :size="18" />成功</dt><dd>{{ generated }}</dd></div>
        <div><dt><FileWarning :size="18" />异常</dt><dd>{{ failed }}</dd></div>
      </dl>
      <button type="button" class="icon-btn cas-pool-trigger" aria-label="打开存储绑定池" title="存储绑定池" @click="$router.push('/tasks/cas/pool')"><Icon name="ChevronLeft" :size="28" /></button>
    </section>
  </div>
</template>

<style scoped>
.cas-overview-area { min-width: 0; margin-bottom: 10px; overflow-x: clip; }
.cas-overview { display: grid; grid-template-columns: 112px minmax(160px,1fr) minmax(180px,.6fr) 44px; align-items: center; gap: 18px; padding: 12px 12px 12px 24px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); --cache-hit-color: var(--green); --cache-miss-color: color-mix(in srgb,var(--red) 55%,var(--surface)); }
.cas-progress { min-width: 0; }
.cas-progress-item + .cas-progress-item { margin-top: 10px; }
.cas-progress-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-width: 0; }
.cas-progress-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.cas-progress small, .cas-progress-status, .cas-metrics dt { color: color-mix(in srgb,var(--text) 72%,var(--surface)); font-size: 12px; }
.cas-progress-status { flex: none; }
.cas-progress-count, .cas-progress-detail { display: block; margin-top: 4px; }
.cas-progress-count { overflow-wrap: anywhere; }
.cas-progress-detail { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cas-progress-track { position: relative; height: 6px; margin-top: 6px; overflow: hidden; border-radius: 3px; background: color-mix(in srgb,var(--text) 10%,var(--surface)); }
.cas-progress progress { display: block; appearance: none; width: 100%; height: 100%; border: 0; opacity: .3; }
.cas-progress progress::-webkit-progress-bar { background: transparent; }
.cas-progress progress::-webkit-progress-value, .cas-progress progress::-moz-progress-bar { background: var(--primary); }
.cas-progress-track.running { background: color-mix(in srgb,var(--primary) 18%,var(--surface)); }
.cas-progress-item:has(.running) .cas-progress-status { color: var(--primary); }
.cas-progress-motion { position: absolute; inset: 0 auto 0 0; width: 35%; border-radius: inherit; background: var(--primary); animation: cas-scan 1.8s ease-in-out infinite; }
.cas-metrics { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 16px; margin: 0; min-width: 0; }
.cas-metrics > div { min-width: 0; }
.cas-metrics dt { display: flex; align-items: center; gap: 6px; overflow-wrap: anywhere; }
.cas-metrics > div:first-child svg { color: var(--green); }.cas-metrics > div:last-child svg { color: var(--red); }
.cas-metrics dd { margin: 6px 0 0 24px; font-size: 17px; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.cas-pool-trigger { width: 44px; height: 44px; color: var(--primary); }
.cas-pool-trigger :deep(svg) { transform: scaleX(.7); }
@keyframes cas-scan { from { transform: translateX(-100%); } to { transform: translateX(286%); } }
@media(max-width:1050px) { .cas-overview { grid-template-columns: 104px minmax(0,1fr) 44px; gap: 12px; }.cas-metrics { grid-column: 1 / 3; grid-row: 2; }.cas-pool-trigger { grid-column: 3; grid-row: 1 / 3; } }
@media(max-width:480px) { .cas-overview { grid-template-columns: 92px minmax(0,1fr) 44px; padding: 12px; gap: 12px 8px; --cache-chart-size:92px; }.cas-metrics { gap: 12px; } }
@media(prefers-reduced-motion:reduce) { .cas-progress-motion { animation: none; left: 32.5%; } }
</style>
