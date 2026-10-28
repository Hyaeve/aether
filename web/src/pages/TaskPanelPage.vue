<script setup>
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {state} from '../lib'
import {compactDate} from '../date-time.mjs'
import TaskTabs from '../components/TaskTabs.vue'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'

const kinds={strm:['STRM 任务','FileVideo'],cas:['CAS 任务','Box'],ed2k:['ED2K 任务','Link'],cache:['缓存任务','Database'],organize:['目录整理','FolderTree'],scrape:['STRM 刮削','ScanSearch'],automation:['联动任务','Workflow']}
const sources={task:'目录任务',generation:'解析生成',scrape:'媒体刮削',automation:'联动流程'}
const items=ref([]),filter=ref('all'),query=ref(''),error=ref(''),loaded=ref(false),refreshing=ref(false)
const active=computed(()=>items.value.filter(item=>item.status==='running').length)
const queued=computed(()=>items.value.filter(item=>item.status==='queued').length)
const grouped=computed(()=>Object.entries(kinds).map(([kind,[name,icon]])=>({kind,name,icon,items:items.value.filter(item=>item.kind===kind&&(filter.value==='all'||filter.value===kind)&&`${item.name} ${item.message}`.toLowerCase().includes(query.value.toLowerCase()))})).filter(group=>group.items.length))
const pool=id=>state.storages.find(storage=>storage.id===id)
const progress=item=>Math.max(0,Math.min(100,Math.floor(item.processed/item.total*100)))
let alive=true,controller,timer,pending=false
async function load(manual=false){
  if(!alive||pending||document.visibilityState==='hidden')return
  pending=true;refreshing.value=manual
  const owner=state.username
  controller=new AbortController()
  const request=controller
  let timedOut=false
  const deadline=setTimeout(()=>{timedOut=true;request.abort()},10000)
  try{
    const response=await fetch('/api/tasks/panel',{credentials:'same-origin',cache:'no-store',signal:request.signal})
    const data=await response.json()
    if(!alive||owner!==state.username||request.signal.aborted)return
    if(!response.ok){if(response.status===401)state.authenticated=false;throw new Error(data.error||'任务状态读取失败')}
    items.value=Array.isArray(data.items)?data.items:[]
    error.value='';loaded.value=true
  }catch(e){if(alive&&owner===state.username&&(timedOut||!request.signal.aborted))error.value=timedOut?'任务状态读取超时':e.message}
  finally{clearTimeout(deadline);pending=false;if(alive)refreshing.value=false}
}
const visibility=()=>{if(document.visibilityState==='visible')load()}
watch(()=>state.username,()=>{items.value=[];loaded.value=false;controller?.abort()})
onMounted(()=>{load();timer=setInterval(load,1500);document.addEventListener('visibilitychange',visibility)})
onUnmounted(()=>{alive=false;clearInterval(timer);controller?.abort();document.removeEventListener('visibilitychange',visibility)})
</script>
<template>
  <section class="task-heading"><TaskTabs/><button class="icon-btn" aria-label="刷新任务面板" :disabled="refreshing" @click="load(true)"><Icon name="RefreshCw" :class="{spin:refreshing}"/></button></section>
  <div class="metric-strip three task-panel-metrics">
    <div><span class="metric-icon green"><Icon name="Activity"/></span><span><small>正在执行</small><strong>{{active}}<em>项</em></strong></span></div>
    <div><span class="metric-icon amber"><Icon name="Clock3"/></span><span><small>排队等待</small><strong>{{queued}}<em>项</em></strong></span></div>
    <div><span class="metric-icon"><Icon name="LayoutGrid"/></span><span><small>活跃栏目</small><strong>{{new Set(items.map(item=>item.kind)).size}}<em>个</em></strong></span></div>
  </div>
  <div class="task-panel-toolbar"><RoundedSelect v-model="filter" label="任务类型" :options="[{value:'all',label:'全部任务'},...Object.entries(kinds).map(([value,[label]])=>({value,label}))]"/><div class="search-field"><Icon name="Search" :size="16"/><input v-model="query" aria-label="搜索运行任务" placeholder="搜索任务"/></div></div>
  <p v-if="error" class="error-message" role="alert">{{error}}</p>
  <section v-for="group in grouped" :key="group.kind" class="task-panel-group" :aria-label="group.name">
    <h2><Icon :name="group.icon" :size="19"/>{{group.name}}<span>{{group.items.length}}</span></h2>
    <article v-for="item in group.items" :key="item.id" :data-id="item.id" :data-processed="item.processed" class="task-panel-row" :class="{queued:item.status==='queued'}">
      <span class="task-panel-symbol"><ProviderIcon v-if="pool(item.storageId)" :type="pool(item.storageId).type"/><Icon v-else :name="group.icon" :size="28"/></span>
      <div class="task-panel-name"><strong :data-tooltip="item.name">{{item.name}}</strong><small>{{sources[item.source]||group.name}}<template v-if="pool(item.storageId)"> · {{pool(item.storageId).name}}</template></small></div>
      <div class="task-panel-detail"><span :data-tooltip="item.message">{{item.message||(item.status==='queued'?'等待执行':'正在执行')}}</span><small v-if="item.unit">{{item.totalKnown?`${item.processed} / ${item.total}`:`已处理 ${item.processed}`}} {{item.unit}}</small><time v-else-if="item.startedAt&&!item.startedAt.startsWith('0001')">{{compactDate(item.startedAt)}}</time></div>
      <div class="task-panel-status"><strong><Icon :name="item.status==='queued'?'Clock3':'Activity'" :size="15"/>{{item.status==='queued'?'排队':'进行中'}}<em v-if="item.totalKnown"> {{progress(item)}}%</em></strong><div class="task-panel-track" role="progressbar" :aria-label="`${item.name}进度`" :aria-valuenow="item.totalKnown?progress(item):undefined" :aria-valuemin="0" :aria-valuemax="100"><span :style="{width:item.totalKnown?`${progress(item)}%`:'0%'}"/></div></div>
      <RouterLink :to="`/tasks/${item.kind}`" class="icon-btn" :aria-label="`前往${group.name}`" :data-tooltip="`前往${group.name}`" data-tooltip-always><Icon name="ArrowUpRight"/></RouterLink>
    </article>
  </section>
  <div v-if="!grouped.length&&!error" class="empty-state task-panel-empty"><Icon name="ListChecks" :size="32"/><p>{{!loaded?'正在读取任务状态…':query||filter!=='all'?'没有匹配的运行任务':'暂无正在进行的任务'}}</p></div>
</template>
<style scoped>
.task-panel-toolbar{display:flex;align-items:center;gap:12px;margin-bottom:18px}.task-panel-toolbar>.rounded-select{width:160px}.task-panel-toolbar .search-field{width:240px;margin-left:auto}
.task-panel-group{margin-bottom:22px}.task-panel-group h2{display:flex;align-items:center;gap:9px;font-size:15px;margin:0 0 10px;font-weight:600}.task-panel-group h2>span{font-size:12px;color:var(--muted);font-weight:400}
.task-panel-row{display:grid;grid-template-columns:40px minmax(110px,1fr) minmax(130px,1.4fr) 120px 36px;align-items:center;gap:14px;min-height:82px;padding:12px 14px;border:1px solid var(--border);border-radius:8px;background:var(--surface);margin-bottom:8px}
.task-panel-symbol{display:grid;place-items:center;width:40px;height:40px;color:var(--primary)}.task-panel-symbol:deep(.provider-symbol){max-width:36px;max-height:36px}
.task-panel-name,.task-panel-detail{display:grid;gap:5px;min-width:0}.task-panel-name strong{font-size:15px;font-weight:600}.task-panel-detail>span{font-size:13px}.task-panel-name strong,.task-panel-name small,.task-panel-detail>span{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.task-panel-row small,.task-panel-row time{font-size:12px;color:var(--muted)}
.task-panel-status{color:var(--transfer-blue,var(--primary));display:grid;gap:8px}.task-panel-status strong{display:flex;align-items:center;gap:5px;font-size:13px;font-weight:500;white-space:nowrap}.task-panel-status em{font-style:normal;margin-left:auto;font-size:12px}.queued .task-panel-status{color:var(--transfer-orange,var(--amber))}
.task-panel-track{height:4px;width:104px;border:1px solid currentColor;border-radius:3px;overflow:hidden}.task-panel-track>span{display:block;height:100%;background:currentColor}.task-panel-empty{min-height:240px}
@media(max-width:700px){.task-panel-row{grid-template-columns:34px minmax(0,1fr) 112px 30px;gap:10px;padding:12px;min-height:108px}.task-panel-symbol{grid-row:1 / 3;width:34px}.task-panel-name{grid-column:2 / 4}.task-panel-row>.icon-btn{grid-column:4;grid-row:1 / 3}.task-panel-detail{grid-column:2;grid-row:2}.task-panel-status{grid-column:3;grid-row:2}.task-panel-toolbar .search-field{width:160px;min-width:0;flex:1}.task-panel-toolbar>.rounded-select{width:130px;flex:none}}
</style>
