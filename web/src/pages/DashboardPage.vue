<script setup>
import { computed } from 'vue'
import { state, bytes } from '../lib'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import CacheHitChart from '../components/CacheHitChart.vue'
import { noticeTime } from '../library-notices'
const recent = computed(() => state.logs.slice(-6).reverse())
const modules = {
  audit: ['操作审计','ShieldCheck'], files: ['文件与备份','FolderOpen'], storage: ['存储与服务','HardDrive'],
  tasks: ['任务管理','ListTodo'], links: ['以太链接','Waypoints'], system: ['系统','Server']
}
const levels = { info: '信息', warn: '警告', error: '错误', debug: '调试', success: '完成', cancelled: '取消', interrupted: '中断' }
const moduleOf = log => modules[log.module] || modules.system
const severity = log => ['error','warn','success'].includes(log.level) ? log.level : ['cancelled','interrupted'].includes(log.level) ? 'warn' : 'info'
function scrollShortcuts(event) {
  const area = event.currentTarget
  if (Math.abs(event.deltaX) >= Math.abs(event.deltaY) || event.ctrlKey) return
  const next = Math.max(0, Math.min(area.scrollWidth - area.clientWidth, area.scrollLeft + event.deltaY))
  if (next !== area.scrollLeft) { event.preventDefault(); area.scrollLeft = next }
}
</script>
<template>
  <div class="dashboard-page">
    <section class="page-head"><div><h1>以太概览</h1></div><span class="status success"><i />服务运行中</span></section>
    <div class="metric-strip">
      <div><span class="metric-icon"><Icon name="Layers3" /></span><span><small>已添加存储</small><strong>{{ state.storages.length }}<em>个</em></strong></span></div>
      <div><span class="metric-icon green"><Icon name="Activity" /></span><span><small>运行中任务</small><strong>{{ state.tasks.filter(t => t.status === 'running').length }}<em>项</em></strong></span></div>
      <div><span class="metric-icon amber"><Icon name="Database" /></span><span><small>缓存条目</small><strong>{{ state.cache.entries || 0 }}<em>条</em></strong></span></div>
      <div><span class="metric-icon neutral"><Icon name="Download" /></span><span><small>本次运行下载量</small><strong class="text-metric">{{ bytes(state.traffic.downloaded) }}</strong></span></div>
    </div>
    <div class="dashboard-grid">
      <section class="shortcut-section" aria-label="存储快捷入口">
        <div class="section-label"><h2>存储快捷入口</h2><button class="text-btn" @click="$router.push('/storage')">管理存储<Icon name="ArrowRight" :size="15" /></button></div>
        <div v-if="!state.storages.length" class="empty-state compact"><Icon name="Cloud" :size="36" /><h3>连接你的第一个存储空间</h3><button class="btn primary" @click="$router.push('/storage')"><Icon name="Plus" />添加存储池</button></div>
        <div v-else class="shortcut-viewport"><div class="storage-shortcuts" role="region" aria-label="存储快捷访问" tabindex="0" @wheel="scrollShortcuts">
          <button v-for="s in state.storages" :key="s.id" :disabled="!s.enabled" @click="$router.push({path:'/files',query:{storage:s.id}})"><ProviderIcon :type="s.type" /><span :data-tooltip="s.name">{{ s.name }}</span></button>
        </div></div>
      </section>
      <section class="dashboard-cache" aria-label="缓存命中统计">
        <div class="section-label"><h2>缓存命中</h2><button class="text-btn" @click="$router.push('/tasks/cache')">缓存任务<Icon name="ArrowRight" :size="15" /></button></div>
        <div class="dashboard-cache-body">
          <CacheHitChart :hits="state.cache.hits || 0" :misses="state.cache.misses || 0" />
          <dl class="hit-legend"><div class="hit"><dt><i />命中</dt><dd>{{ state.cache.hits || 0 }}</dd></div><div class="miss"><dt><i />未命中</dt><dd>{{ state.cache.misses || 0 }}</dd></div></dl>
        </div>
        <div class="cache-foot"><span>缓存大小</span><strong>{{ bytes(state.cache.bytes) }}</strong></div>
      </section>
      <section class="runtime-section" aria-label="运行状态">
        <div class="section-label"><h2>运行状态</h2><Icon name="Activity" :size="18" /></div>
        <div class="summary-row"><span>运行时间</span><strong>{{ Math.floor(state.uptime / 3600) }} 时 {{ Math.floor(state.uptime % 3600 / 60) }} 分</strong></div>
        <div class="summary-row"><span>元数据缓存</span><strong>{{ state.cache.entries || 0 }} 条</strong></div>
        <div class="summary-row"><span>WebDAV</span><span class="status" :class="state.settings.webdavEnabled ? 'success' : 'muted'">{{ state.settings.webdavEnabled ? '已启用' : '未启用' }}</span></div>
        <div class="summary-row"><span>服务端口</span><code>15151</code></div>
      </section>
    </div>
    <section class="activity-section" aria-label="最近活动">
      <div class="section-label"><h2>最近活动</h2><button class="text-btn" @click="$router.push('/logs')">全部日志<Icon name="ArrowRight" :size="15" /></button></div>
      <div v-if="!recent.length" class="activity-empty"><Icon name="ScrollText" :size="28" /><span>暂无活动记录</span></div>
      <RouterLink v-for="(log, i) in recent" :key="`${log.time}:${i}`" class="activity-row" :data-level="severity(log)" :to="{path:'/logs',query:{notice:'dashboard',module:log.module || 'system',q:log.message}}">
        <span class="activity-icon"><Icon :name="moduleOf(log)[1]" :size="20" /></span>
        <span class="activity-content"><span class="activity-meta"><strong>{{ moduleOf(log)[0] }}</strong><span class="activity-level">{{ levels[log.level] || '信息' }}</span></span><span class="activity-message" :data-tooltip="log.message">{{ log.message }}</span></span>
        <time :datetime="log.time">{{ noticeTime(log.time) }}</time><Icon class="activity-arrow" name="ChevronRight" :size="16" />
      </RouterLink>
    </section>
  </div>
</template>
<style scoped>
.dashboard-page { min-width:0; max-width:100%; }
.dashboard-page .page-head { margin-bottom:22px; align-items:center; }
.dashboard-page .page-head h1 { font-size:24px; letter-spacing:0; }
.dashboard-grid { grid-template-columns:minmax(0,1.25fr) minmax(0,1fr) minmax(0,.95fr); gap:20px; margin-top:20px; }
.dashboard-grid > section { background:var(--surface); border:1px solid var(--border); border-radius:8px; overflow:hidden; }
.section-label { min-height:56px; padding:14px 18px; margin:0; border-bottom:1px solid color-mix(in srgb,var(--border) 65%,transparent); }
.section-label h2 { font-size:14px; font-weight:600; }
.section-label .text-btn { font-size:12px; gap:5px; white-space:nowrap; }
.section-label > svg { color:var(--muted); }
.shortcut-viewport { padding:12px 12px 16px; }
.storage-shortcuts { display:grid; grid-template-rows:repeat(2,84px); grid-auto-flow:column; grid-auto-columns:calc((100% - 16px) / 3); gap:8px; overflow-x:auto; overflow-y:hidden; max-width:100%; scrollbar-width:none; overscroll-behavior-x:contain; scroll-snap-type:x proximity; border-radius:6px; }
.storage-shortcuts::-webkit-scrollbar { display:none; }
.storage-shortcuts button { display:flex; flex-direction:column; gap:8px; align-items:center; justify-content:center; padding:8px; border:0; border-radius:6px; background:transparent; color:var(--text); min-width:0; scroll-snap-align:start; transition:background .18s; }
.storage-shortcuts button:hover:not(:disabled) { background:var(--bg); }
.storage-shortcuts button:disabled { opacity:.4; }
.storage-shortcuts span { max-width:100%; font-size:12px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.storage-shortcuts :deep(.provider-icon) { flex-shrink:0; width:40px; height:40px; padding:0; border:0; border-radius:0; background:none; }
.storage-shortcuts :deep(.provider-logo),.storage-shortcuts :deep(.provider-icon > svg) { width:40px; height:40px; }
.storage-shortcuts:focus-visible, .storage-shortcuts button:focus-visible { outline:2px solid var(--primary); outline-offset:-2px; }
.dashboard-cache { --cache-chart-size:112px; --cache-hit-color:#55a78d; --cache-miss-color:#d5b578; }
.dashboard-cache-body { display:flex; align-items:center; justify-content:center; gap:22px; min-height:156px; padding:18px; }
.hit-legend { display:grid; gap:15px; margin:0; min-width:0; }
.hit-legend dt { display:flex; align-items:center; gap:7px; color:var(--muted); font-size:12px; white-space:nowrap; }
.hit-legend i { width:7px; height:7px; border-radius:50%; background:var(--cache-hit-color); }
.hit-legend .miss i { background:var(--cache-miss-color); }
.hit-legend dd { margin:4px 0 0 14px; font-size:17px; font-weight:600; font-variant-numeric:tabular-nums; overflow-wrap:anywhere; }
.cache-foot { display:flex; justify-content:space-between; gap:12px; margin:0 18px; padding:14px 0; border-top:1px solid color-mix(in srgb,var(--border) 65%,transparent); font-size:12px; }
.cache-foot > span { color:var(--muted); }
.cache-foot strong { font-weight:500; }
.dashboard-grid .summary-row { margin:0 18px; padding:11px 0; min-height:46px; border-bottom-color:color-mix(in srgb,var(--border) 65%,transparent); }
.dashboard-grid .summary-row:last-child { border-bottom:0; }
.summary-row > span:first-child, .summary-row strong { min-width:0; overflow-wrap:anywhere; }
.summary-row .status { flex-shrink:0; }
.activity-section { margin-top:24px; }
.activity-section .section-label { padding:0 0 12px; }
.activity-section .activity-row { display:grid; grid-template-columns:38px minmax(0,1fr) auto 16px; gap:14px; align-items:center; margin:0; padding:14px 8px; border-bottom:1px solid color-mix(in srgb,var(--border) 60%,transparent); border-radius:6px; color:var(--text); text-decoration:none; transition:background .18s; }
.activity-section .activity-row:hover { background:color-mix(in srgb,var(--surface) 75%,transparent); }
.activity-section .activity-row:focus-visible { outline:2px solid var(--primary); outline-offset:-2px; }
.activity-section .activity-row:last-child { border-bottom:0; }
.activity-icon { display:grid; place-items:center; width:36px; height:36px; border-radius:8px; color:var(--primary); background:color-mix(in srgb,var(--primary) 8%,transparent); }
.activity-content { min-width:0; }
.activity-meta { display:flex; align-items:center; gap:10px; margin-bottom:5px; font-size:12px; }
.activity-meta strong { font-weight:500; }
.activity-level { font-size:11px; color:var(--muted); }
.activity-row[data-level=error] .activity-level { color:#c05b70; }
.activity-row[data-level=warn] .activity-level { color:#a47a38; }
.activity-row[data-level=success] .activity-level { color:#40957c; }
.activity-message { display:-webkit-box; -webkit-box-orient:vertical; -webkit-line-clamp:2; overflow:hidden; font-size:13px; line-height:1.6; overflow-wrap:anywhere; white-space:pre-wrap; }
.activity-section .activity-row time { margin:0; font-size:12px; color:var(--muted); font-variant-numeric:tabular-nums; white-space:nowrap; }
.activity-arrow { color:var(--muted); opacity:.5; }
.activity-empty { display:flex; align-items:center; justify-content:center; gap:10px; min-height:116px; color:var(--muted); font-size:13px; }
.metric-strip { background:transparent; border:0; padding:0; gap:14px; margin-bottom:0; }
.metric-strip > div { padding:20px; min-height:106px; border:1px solid var(--border); border-radius:8px; background:var(--surface); }
.metric-strip > div:last-child, .metric-strip > div:nth-child(2) { border-right:1px solid var(--border); }
.metric-strip > div:nth-child(1) { border-top:3px solid #657cc0; }
.metric-strip > div:nth-child(2) { border-top:3px solid #479d85; }
.metric-strip > div:nth-child(3) { border-top:3px solid #c99d50; }
.metric-strip > div:nth-child(4) { border-top:3px solid #699db6; }
.metric-strip > div, .metric-strip > div > span:last-child { min-width:0; }
.metric-strip strong { overflow-wrap:anywhere; }
.metric-icon { flex-shrink:0; }
.page-head, .section-label { flex-wrap:wrap; }
:global([data-theme=dark] .dashboard-page .dashboard-cache) { --cache-hit-color:#78c5a7; --cache-miss-color:#bb9e6d; }
:global([data-theme=dark] .dashboard-page .activity-row[data-level=error] .activity-level) { color:#f09aa9; }
:global([data-theme=dark] .dashboard-page .activity-row[data-level=warn] .activity-level) { color:#dfbc7a; }
:global([data-theme=dark] .dashboard-page .activity-row[data-level=success] .activity-level) { color:#78c5a7; }
@media(max-width:1250px) { .dashboard-grid { grid-template-columns:repeat(2,minmax(0,1fr)); }.shortcut-section { grid-column:1 / -1; }.storage-shortcuts { grid-template-rows:84px; grid-auto-columns:calc((100% - 40px) / 6); }.metric-strip { grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px; } }
@media(max-width:600px) { .dashboard-grid { grid-template-columns:minmax(0,1fr); gap:16px; }.storage-shortcuts { grid-template-rows:repeat(2,84px); grid-auto-columns:calc((100% - 16px) / 3); }.activity-section .activity-row { grid-template-columns:36px minmax(0,1fr) 16px; gap:10px; }.activity-section .activity-row time { grid-column:2; grid-row:2; padding:0; width:auto; }.activity-arrow { grid-column:3; grid-row:1 / 3; }.activity-icon { align-self:start; }.metric-strip > div { flex-wrap:wrap; padding:14px; gap:10px; }.metric-strip strong { font-size:23px; }.section-label { padding:12px 14px; } }
@media(prefers-reduced-motion:reduce) { .storage-shortcuts button, .activity-section .activity-row { transition:none; } }
</style>
