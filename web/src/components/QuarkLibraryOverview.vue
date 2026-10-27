<script setup>
import { computed } from 'vue'
import { BadgeCheck, FileWarning } from 'lucide-vue-next'
import CacheHitChart from './CacheHitChart.vue'
import SettingsEntry from './SettingsEntry.vue'
import AnimatedNumber from './AnimatedNumber.vue'

const props = defineProps({ cards: { type: Array, default: () => [] }, stats: { type: Object, default: () => ({}) }, running: { default: '' } })
const active = computed(() => props.cards.filter(card => card.result?.status === 'running' || card.id === props.running))
const executed = computed(() => props.cards.filter(card => card.id !== props.running &&
  ['completed', 'failed', 'invalid', 'cancelled', 'interrupted'].includes(card.result?.status) &&
  typeof card.result?.lastRun === 'string' && !card.result.lastRun.startsWith('0001') && Number.isFinite(Date.parse(card.result.lastRun))))
const successes = computed(() => executed.value.filter(card => card.result.status === 'completed').length)
const invalid = computed(() => props.cards.filter(card => card.result?.invalidSince && !card.result.invalidSince.startsWith('0001')).length)
</script>

<template>
  <section class="quark-library-stats" aria-label="夸克链库统计">
    <CacheHitChart task label="成功率" :hits="successes" :misses="executed.length-successes" title="按各分享卡片最近一次已结束执行统计成功率，未执行和执行中不计入" />
    <div class="quark-overview-progress">
      <div v-if="!active.length" class="quark-overview-item">
        <div class="quark-overview-heading"><small>暂无执行中的任务</small><span>空闲</span></div>
        <div class="quark-overview-track"><progress value="0" max="100" aria-label="暂无执行中的夸克链库任务" /></div>
      </div>
      <div v-for="card in active" :key="card.id" class="quark-overview-item">
        <div class="quark-overview-heading"><strong :title="card.name">{{ card.name }}</strong><span>生成中</span></div>
        <div class="quark-overview-track running" role="progressbar" :aria-label="`${card.name}生成进度`" :aria-valuetext="card.result?.message||'正在生成分享 STRM'" />
        <small class="quark-overview-detail" :title="card.result?.message">{{ card.result?.message||'正在生成分享 STRM' }}</small>
      </div>
      <p class="quark-overview-totals"><span>链接 <AnimatedNumber :value="cards.length" /></span><span>STRM <AnimatedNumber :value="stats.strm||0" /></span><span>元数据 <AnimatedNumber :value="stats.metadata||0" /></span><span>失效 <AnimatedNumber :value="invalid" /></span></p>
    </div>
    <dl class="quark-overview-metrics">
      <div><dt><BadgeCheck :size="18" />成功</dt><dd><AnimatedNumber :value="successes" /></dd></div>
      <div><dt><FileWarning :size="18" />异常</dt><dd><AnimatedNumber :value="executed.length-successes" /></dd></div>
    </dl>
    <SettingsEntry class="quark-library-settings-entry" to="/virtual/quark/settings" label="夸克链库设置" hint="进入夸克链库设置" />
  </section>
</template>

<style scoped>
.quark-library-stats { display:grid;grid-template-columns:112px minmax(160px,1fr) minmax(180px,.6fr) 44px;align-items:center;gap:18px;padding:12px 12px 12px 24px;margin:0 0 10px;min-width:0;flex:none;border:1px solid var(--border);border-radius:8px;background:var(--surface); }
.quark-overview-progress { min-width:0; }
.quark-overview-item + .quark-overview-item { margin-top:10px; }
.quark-overview-heading { display:flex;align-items:center;justify-content:space-between;gap:10px;min-width:0; }
.quark-overview-heading strong { min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px;font-weight:400; }
.quark-overview-heading span { flex:none; }
.quark-overview-heading small,.quark-overview-heading span,.quark-overview-detail,.quark-overview-totals,.quark-overview-metrics dt { font-size:12px;color:color-mix(in srgb,var(--text) 72%,var(--surface)); }
.quark-overview-track { height:6px;margin-top:6px;overflow:hidden;border-radius:3px;background:color-mix(in srgb,var(--text) 10%,var(--surface)); }
.quark-overview-track progress { display:block;appearance:none;width:100%;height:100%;border:0;background:transparent; }
.quark-overview-track progress::-webkit-progress-bar { background:transparent; }
.quark-overview-track progress::-webkit-progress-value { background:var(--primary); }
.quark-overview-track progress::-moz-progress-bar { background:var(--primary); }
.quark-overview-track.running { background:color-mix(in srgb,var(--primary) 18%,var(--surface)); }
.quark-overview-item:has(.running) .quark-overview-heading span { color:var(--primary); }
.quark-overview-detail { display:block;margin-top:4px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap; }
.quark-overview-totals { display:flex;flex-wrap:wrap;gap:4px 12px;margin:8px 0 0;font-variant-numeric:tabular-nums; }
.quark-overview-totals > span { white-space:nowrap; }
.quark-overview-metrics { display:grid;gap:20px;margin:0;min-width:0; }
.quark-overview-metrics > div { display:flex;align-items:center;gap:12px;min-width:0; }
.quark-overview-metrics dt { display:flex;align-items:center;gap:6px; }
.quark-overview-metrics > div:first-child svg { color:var(--green); }
.quark-overview-metrics > div:last-child svg { color:var(--red); }
.quark-overview-metrics dd { margin:0;font-size:17px;font-variant-numeric:tabular-nums;overflow-wrap:anywhere; }
@media(max-width:1050px) { .quark-library-stats { grid-template-columns:104px minmax(0,1fr) 44px;gap:12px; }.quark-overview-metrics { grid-column:1/3;grid-row:2; }.quark-library-settings-entry { grid-column:3;grid-row:1/3; } }
@media(max-width:480px) { .quark-library-stats { grid-template-columns:92px minmax(0,1fr) 44px;padding:12px;gap:12px 8px;--cache-chart-size:92px; } }
</style>
