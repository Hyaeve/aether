<script setup>
import { computed } from 'vue'
import { state, bytes, date } from '../lib'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
const hitRate = computed(() => { const n = (state.cache.hits || 0) + (state.cache.misses || 0); return n ? Math.round(state.cache.hits / n * 100) : 0 })
</script>
<template>
  <div class="dashboard-page">
  <section class="page-head"><div><h1>以太概览</h1></div><span class="status success"><i />服务运行中</span></section>
  <div class="metric-strip"><div><span class="metric-icon"><Icon name="Layers3" /></span><span><small>已添加存储</small><strong>{{ state.storages.length }}<em>个</em></strong></span></div><div><span class="metric-icon green"><Icon name="Activity" /></span><span><small>运行中任务</small><strong>{{ state.tasks.filter(t => t.status === 'running').length }}<em>项</em></strong></span></div><div><span class="metric-icon amber"><Icon name="Database" /></span><span><small>缓存命中率</small><strong>{{ hitRate }}<em>%</em></strong></span></div><div><span class="metric-icon neutral"><Icon name="Download" /></span><span><small>本次运行下载量</small><strong class="text-metric">{{ bytes(state.traffic.downloaded) }}</strong></span></div></div>
  <div class="dashboard-grid"><section><div class="section-label"><h2>存储快捷入口</h2><button class="text-btn" @click="$router.push('/storage')">管理存储<Icon name="ArrowRight" :size="15" /></button></div><div v-if="!state.storages.length" class="empty-state compact"><Icon name="Cloud" :size="36" /><h3>连接你的第一个存储空间</h3><button class="btn primary" @click="$router.push('/storage')"><Icon name="Plus" />添加存储池</button></div><div class="storage-shortcuts"><button v-for="s in state.storages" :key="s.id" :disabled="!s.enabled" @click="$router.push({path:'/files',query:{storage:s.id}})"><ProviderIcon :type="s.type" /><span>{{ s.name }}</span></button></div></section><section><div class="section-label"><h2>运行状态</h2><Icon name="Activity" /></div><div class="summary-row"><span>服务运行时间</span><strong>{{ Math.floor(state.uptime / 3600) }} 时 {{ Math.floor(state.uptime % 3600 / 60) }} 分</strong></div><div class="summary-row"><span>元数据缓存</span><strong>{{ state.cache.entries || 0 }} 条 / {{ bytes(state.cache.bytes) }}</strong></div><div class="summary-row"><span>WebDAV 服务</span><span class="status" :class="state.settings.webdavEnabled ? 'success' : 'muted'">{{ state.settings.webdavEnabled ? '已启用' : '未启用' }}</span></div><div class="summary-row"><span>服务端口</span><code>15151</code></div></section></div>
  <section class="activity-section"><div class="section-label"><h2>最近活动</h2><button class="text-btn" @click="$router.push('/logs')">全部日志<Icon name="ArrowRight" :size="15" /></button></div><p v-if="!state.logs.length" class="small-empty">暂无活动记录</p><div v-for="(log, i) in state.logs.slice(-6).reverse()" :key="i" class="activity-row"><span class="activity-dot" /><span>{{ log.message }}</span><time>{{ date(log.time) }}</time></div></section>
  </div>
</template>
<style scoped>
.dashboard-page { min-width: 0; max-width: 100%; }
.storage-shortcuts { display:grid; grid-template-columns:repeat(auto-fill,minmax(125px,1fr)); gap:12px; padding:18px; }.storage-shortcuts button { display:flex; gap:10px; align-items:center; padding:10px; border:0; border-radius:8px; background:transparent; color:var(--text); min-width:0; text-align:left; }.storage-shortcuts button:hover { background:var(--bg); }.storage-shortcuts span { overflow-wrap:anywhere; }.storage-shortcuts :deep(.provider-icon) { flex-shrink:0; width:32px; height:32px; }
.dashboard-page .page-head { margin-bottom: 22px; align-items: center; }
.dashboard-page .page-head h1 { font-size: 24px; letter-spacing: 0; }
.dashboard-grid { grid-template-columns: minmax(0,1.2fr) minmax(0,1fr); gap: 20px; margin-top: 20px; }
.dashboard-grid > section, .activity-section { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
.section-label { min-height: 58px; padding: 14px 20px; margin: 0; border-bottom: 1px solid var(--border); background: color-mix(in srgb,var(--bg) 45%,var(--surface)); }
.section-label h2 { font-size: 15px; }
.dashboard-grid .summary-row { margin: 0 20px; min-height: 58px; }
.dashboard-grid .summary-row:last-child { border-bottom: 0; }
.activity-section { margin-top: 20px; }
.activity-section .activity-row { margin: 0 20px; padding: 16px 0; }
.activity-section .activity-row:last-child { border-bottom: 0; }
.metric-strip { background: transparent; border: 0; padding: 0; gap: 14px; margin-bottom: 0; }
.metric-strip > div { padding: 20px; min-height: 106px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); }
.metric-strip > div:last-child, .metric-strip > div:nth-child(2) { border-right: 1px solid var(--border); }
.metric-strip > div:nth-child(1) { border-top: 3px solid #657cc0; }
.metric-strip > div:nth-child(2) { border-top: 3px solid #479d85; }
.metric-strip > div:nth-child(3) { border-top: 3px solid #c99d50; }
.metric-strip > div:nth-child(4) { border-top: 3px solid #699db6; }
.metric-strip > div, .metric-strip > div > span:last-child { min-width: 0; }
.metric-strip strong { overflow-wrap: anywhere; }
.metric-icon, .activity-dot, .summary-row .status { flex-shrink: 0; }
.summary-row > span:first-child, .summary-row strong { min-width: 0; overflow-wrap: anywhere; }
.activity-row { display: grid; grid-template-columns: 6px minmax(0,1fr) auto; }
.activity-row > span:nth-child(2) { min-width: 0; overflow-wrap: anywhere; white-space: pre-wrap; }
.page-head, .section-label { flex-wrap: wrap; }
@media (max-width: 1100px) {
  .metric-strip { grid-template-columns: repeat(2,minmax(0,1fr)); gap: 12px; }
}
@media (max-width: 960px) {
  .dashboard-grid { grid-template-columns: minmax(0,1fr); }
}
@media (max-width: 600px) {
  .activity-row { grid-template-columns: 6px minmax(0,1fr); }
  .activity-row time { grid-column: 2; width: auto; padding-left: 0; margin-left: 0; }
  .metric-strip > div { flex-wrap: wrap; }
  .metric-strip > div { padding: 14px; gap: 10px; }
  .metric-strip strong { font-size: 23px; }
  .section-label { padding: 12px 14px; }
  .dashboard-grid .summary-row, .activity-section .activity-row { margin-inline: 14px; }
}
</style>
