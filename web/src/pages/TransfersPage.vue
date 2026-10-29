<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, bytes, notify, state } from '../lib'
import { compactDate } from '../date-time.mjs'
import { readSession, writeSession } from '../session-navigation'
import { useVirtualList } from '../virtual-list'
import { listWheel } from '../nested-scroll'
import { transferStates, transferState, transferSummary, transferPercent, canControlTransfer } from '../transfer-status.mjs'
import Icon from '../components/Icon.vue'
import BackupRules from '../components/BackupRules.vue'
import TransferLocation from '../components/TransferLocation.vue'
import ScrollRail from '../components/ScrollRail.vue'
import SelectionDock from '../components/SelectionDock.vue'
import Modal from '../components/Modal.vue'
const props=defineProps({rules:Boolean})
const backup=ref(null), selected=ref('copy'), items=ref([]), error=ref(''), query=ref(''), busy=ref(false)
const route=useRoute()
const validMode=value=>['copy','upload','download'].includes(value)
selected.value=validMode(route.query.mode)?route.query.mode:readSession(state.username,'transfer-mode')||'copy'
watch(()=>route.query.mode,value=>{if(validMode(value))selected.value=value})
watch(selected,value=>writeSession(state.username,'transfer-mode',value),{immediate:true})
const viewport=ref(null), chosen=ref([]), selecting=ref(false), actionBusy=ref(false), confirm=ref(null), filters=ref([]), filterOpen=ref(false)
let anchor=0, timer, alive=true
const modes=[{id:'copy',name:'复制',icon:'Copy'},{id:'upload',name:'上传',icon:'Upload'},{id:'download',name:'下载',icon:'Download'}]
function clientLocation(source) {
  return source==='WebDAV'?{storage:'WebDAV 客户端',type:'client'}:source==='FUSE'?{storage:'本地挂载',type:'client'}:{storage:source||'浏览器',type:source&&source!=='浏览器'?'client':'browser'}
}
const located=computed(()=>items.value.map(item=>({...item,
  from:item.from||(item.kind==='upload'?clientLocation(item.source):{storage:item.storage}),
  to:item.to||(item.kind==='download'?clientLocation(item.source):item.kind==='upload'?{storage:item.storage}:{})
})))
const summaries=computed(()=>Object.fromEntries(modes.map(mode=>[mode.id,transferSummary(items.value.filter(i=>i.kind===mode.id))])))
const filtered=computed(()=>located.value.filter(i=>i.kind===selected.value&&(!filters.value.length||filters.value.includes(i.status))&&
  [i.name,i.storage,i.source,i.from.storage,i.from.directory,i.from.path,i.to.storage,i.to.directory,i.to.path,transferState(i).label].some(v=>String(v||'').toLowerCase().includes(query.value.trim().toLowerCase()))))
const selectedRows=computed(()=>filtered.value.filter(i=>chosen.value.includes(i.id)))
const eligible=action=>selectedRows.value.filter(i=>canControlTransfer(i,action))
const media=window.matchMedia('(max-width:700px)'), narrow=ref(media.matches)
const resize=event=>{narrow.value=event.matches}
const rowHeight=computed(()=>narrow.value?160:64)
const {shown,top,bottom,reset}=useVirtualList(filtered,viewport,{rowHeight})
function clearSelection(){chosen.value=[];selecting.value=false}
function choose(event,item,index){
  if(event.type==='contextmenu'){selecting.value=true;if(!chosen.value.includes(item.id))chosen.value.push(item.id);anchor=index;return}
  if(!selecting.value){chosen.value=[item.id];return}
  if(event.shiftKey){chosen.value=[...new Set([...chosen.value,...filtered.value.slice(Math.min(index,anchor),Math.max(index,anchor)+1).map(i=>i.id)])]}
  else{chosen.value=chosen.value.includes(item.id)?chosen.value.filter(id=>id!==item.id):[...chosen.value,item.id];anchor=index}
}
watch(selected,()=>{filters.value=[];filterOpen.value=false;confirm.value=null;clearSelection()})
watch([selected,query,filters],()=>{reset();chosen.value=chosen.value.filter(id=>filtered.value.some(i=>i.id===id))})
watch(()=>props.rules,()=>{clearSelection();filterOpen.value=false;filters.value=[];clearTimeout(timer);if(!props.rules)load()})
const actionLabel={pause:'暂停',retry:'重试',resume:'恢复',delete:'删除'}
function control(action){
  const rows=eligible(action).map(i=>({id:i.id,name:i.name}))
  if(!rows.length||actionBusy.value)return
  if(action==='delete'||action==='retry')confirm.value={action,rows}
  else execute(action,rows)
}
async function execute(action,rows){
  if(actionBusy.value)return
  actionBusy.value=true
  let done=0, failure=''
  try{
    for(const row of rows){
      if(!alive||!state.authenticated)break
      try{await api('/transfers/action','POST',{id:row.id,action});done++}
      catch(e){failure=e.message;break}
    }
    notify(`${actionLabel[action]}：已处理 ${done}/${rows.length} 项${failure?`；${failure}`:''}`,!!failure)
    confirm.value=null
    if(action==='delete')clearSelection()
    await load()
  }finally{actionBusy.value=false}
}
async function clearFailed(){
  try{items.value=await api('/transfers','DELETE');notify('失败记录已清除')}catch(e){notify(e.message,true)}
}
async function load(){
  if(busy.value)return
  clearTimeout(timer);busy.value=true
  try{const result=await api('/transfers');if(alive){items.value=result;error.value='';chosen.value=chosen.value.filter(id=>result.some(i=>i.id===id))}}
  catch(e){if(alive)error.value=e.message}
  finally{busy.value=false;if(alive&&!props.rules)timer=setTimeout(load,2000)}
}
function key(event){if(event.key==='Escape'&&!confirm.value){clearSelection();filterOpen.value=false}}
function outside(event){
  if(!event.target.closest('.transfer-table,.transfer-dock,.modal'))clearSelection()
  if(!event.target.closest('.transfer-filter'))filterOpen.value=false
}
onMounted(()=>{if(!props.rules)load();document.addEventListener('pointerdown',outside);document.addEventListener('keydown',key);media.addEventListener('change',resize)})
onUnmounted(()=>{alive=false;clearTimeout(timer);document.removeEventListener('pointerdown',outside);document.removeEventListener('keydown',key);media.removeEventListener('change',resize)})
</script>
<template>
  <section class="task-heading" :class="{'backup-heading':rules}">
    <nav v-tab-scroll class="content-tabs"><RouterLink to="/transfer" :class="{active:!rules}"><Icon name="ArrowLeftRight" />传输任务</RouterLink><RouterLink to="/transfer/backup" :class="{active:rules}"><Icon name="ArchiveRestore" />备份规则</RouterLink></nav>
    <div v-if="!rules" class="transfer-actions">
      <button class="icon-btn" aria-label="清除失败记录" :disabled="!items.some(i=>['failed','error'].includes(i.status))||actionBusy" @click="clearFailed"><Icon name="Trash2" /></button>
      <button class="icon-btn" aria-label="刷新传输任务" :disabled="busy" @click="load"><Icon name="RefreshCw" :class="{spin:busy}" /></button>
      <div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索传输任务" placeholder="搜索任务…" /></div>
      <div class="transfer-filter"><button class="icon-btn" :class="{active:filters.length}" aria-label="筛选传输状态" :aria-expanded="filterOpen" @click="filterOpen=!filterOpen"><Icon name="ListFilter" /></button>
        <div v-if="filterOpen" class="transfer-filter-menu" role="group" aria-label="传输状态筛选"><button type="button" @click="filters=[]">所有状态</button><label v-for="option in transferStates" :key="option.value"><input v-model="filters" type="checkbox" :value="option.value" /><Icon :name="option.icon" :size="15" /><span>{{option.label}}</span></label></div>
      </div>
    </div>
    <div v-else class="transfer-actions"><button class="btn" @click="$router.push('/transfer/backup/settings')"><Icon name="Settings2" />备份设置</button><button class="btn primary" @click="backup?.open()"><Icon name="Plus" />添加规则</button></div>
  </section>
  <section class="transfer-content" :class="{'backup-content':rules,'has-transfers':!rules&&filtered.length}">
    <template v-if="!rules">
      <div class="transfer-modes" role="tablist" aria-label="传输类型">
        <button v-for="mode in modes" :key="mode.id" role="tab" :class="mode.id" :aria-selected="selected===mode.id" @click="selected=mode.id">
          <Icon :name="mode.icon" :size="22" /><div class="transfer-mode-summary"><div><strong>{{mode.name}}</strong><span>{{summaries[mode.id].count?summaries[mode.id].count+' 项':'0 空闲'}}</span></div>
          <small v-if="summaries[mode.id].count">{{bytes(summaries[mode.id].done)}} / {{summaries[mode.id].percent===null?'总量待确认':bytes(summaries[mode.id].total)}}<b>{{summaries[mode.id].percent===null?'—':summaries[mode.id].percent+'%'}}</b></small>
          <progress v-if="summaries[mode.id].count" :value="summaries[mode.id].percent??0" max="100" :aria-label="mode.name+'总进度'" /></div>
        </button>
      </div>
      <p v-if="error" class="error-message">{{error}}</p>
      <div class="transfer-table" :class="{'multi-select':selecting}">
        <div class="transfer-header" role="row"><span role="columnheader">文件</span><span role="columnheader">来源 / 目录</span><span role="columnheader">目标 / 目录</span><span role="columnheader">进度状态</span><span role="columnheader">大小</span><span role="columnheader">速度</span><span role="columnheader">开始时间</span></div>
        <div ref="viewport" class="transfer-viewport" @wheel="listWheel"><table aria-label="传输任务"><tbody>
          <tr v-if="top" class="transfer-spacer" :style="{height:top+'px'}" aria-hidden="true"><td colspan="7" /></tr>
          <tr v-for="item in shown" :key="item.id" :class="[item.status,transferState(item).tone,{selected:chosen.includes(item.id)}]" :aria-selected="chosen.includes(item.id)" tabindex="0" @click="choose($event,item,filtered.indexOf(item))" @contextmenu.prevent.stop="choose($event,item,filtered.indexOf(item))" @keydown.space.prevent="selecting=true;choose($event,item,filtered.indexOf(item))">
            <td class="transfer-name" :data-tooltip="[item.name,item.source&&`发起方式：${item.source}`].filter(Boolean).join('\n')" data-tooltip-stationary data-tooltip-delay="300">{{item.name}}</td>
            <td class="transfer-from" data-label="来源"><TransferLocation :location="item.from" /></td>
            <td class="transfer-to" data-label="目标"><TransferLocation :location="item.to" /></td>
            <td class="transfer-progress"><div class="transfer-progress-content"><span class="transfer-status" :data-tooltip="item.message" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300"><Icon :name="transferState(item).icon" :size="14" /><span>{{transferState(item).label}}</span><small>{{transferPercent(item)===null?bytes(item.done):transferPercent(item)+'%'}}</small></span><progress :value="transferPercent(item)??0" max="100" :aria-label="item.name+'进度'" /></div></td>
            <td class="transfer-size" data-label="大小">{{item.total>0?bytes(item.total):'—'}}</td>
            <td class="transfer-speed" data-label="速度">{{item.status==='running'?bytes(item.speed||0)+'/s':'—'}}</td>
            <td class="transfer-started">{{compactDate(item.started)}}</td>
          </tr>
          <tr v-if="bottom" class="transfer-spacer" :style="{height:bottom+'px'}" aria-hidden="true"><td colspan="7" /></tr>
        </tbody></table><p v-if="!filtered.length" class="small-empty">{{filters.length?'没有符合状态的传输任务':'暂无传输任务'}}</p></div><ScrollRail :element="viewport" :thickness="3" />
      </div>
    </template>
    <BackupRules v-else ref="backup" />
  </section>
  <SelectionDock v-if="selecting&&!rules" class="transfer-dock" :count="chosen.length" label="所选传输任务操作">
    <button class="btn" :disabled="actionBusy" @click="chosen=filtered.map(i=>i.id)"><Icon name="CheckCheck" /><span>全选</span></button><button class="btn" :disabled="actionBusy" @click="clearSelection"><Icon name="X" /><span>清空</span></button>
    <button v-for="action in ['pause','retry','resume','delete']" :key="action" class="btn" :class="{'danger-text':action==='delete'}" :disabled="actionBusy||!eligible(action).length" :data-tooltip="action==='retry'&&!eligible(action).length?'仅支持具有安全重试上下文的失败任务；客户端上传或实时下载请从原入口重新发起':undefined" @click="control(action)"><Icon :name="{pause:'Pause',retry:'RotateCcw',resume:'Play',delete:'Trash2'}[action]" /><span>{{actionLabel[action]}}</span></button>
  </SelectionDock>
  <Modal v-if="confirm" compact confirmation :title="'确认'+actionLabel[confirm.action]" @close="!actionBusy&&(confirm=null)"><div class="modal-body">确认{{actionLabel[confirm.action]}} {{confirm.rows.length}} 项传输任务？{{confirm.action==='delete'?'未完成传输会停止；此操作不删除来源或已完成的目标文件。':''}}</div><footer class="modal-footer"><button class="btn" :disabled="actionBusy" @click="confirm=null">取消</button><button class="btn primary" :disabled="actionBusy" @click="execute(confirm.action,confirm.rows)"><Icon name="Check" />确认</button></footer></Modal>
</template>
<style scoped>
.task-heading>.transfer-actions{align-self:center}
.transfer-content{min-height:0;flex:1;display:flex;flex-direction:column}
.transfer-actions{margin-left:auto;display:flex;gap:8px;align-items:center}.transfer-actions .search-field{width:200px}
.transfer-filter{position:relative}.transfer-filter>.active{color:var(--primary);background:var(--primary-soft)}
.transfer-filter-menu{position:absolute;z-index:20;right:0;top:calc(100% + 6px);min-width:160px;padding:6px;border:1px solid var(--border);border-radius:8px;background:var(--surface);box-shadow:var(--shadow)}
.transfer-filter-menu label,.transfer-filter-menu button{display:flex;align-items:center;gap:8px;min-height:32px;padding:5px 8px;font-size:13px;color:var(--text)}.transfer-filter-menu button{border:0;background:none;width:100%;color:var(--primary)}.transfer-filter-menu input{width:15px;height:15px;margin:0;accent-color:var(--primary)}
.transfer-modes{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px;margin:12px 0 16px;flex:none}
.transfer-modes button{display:flex;align-items:center;gap:12px;min-height:108px;padding:16px;border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text);text-align:left}
.transfer-modes .copy{--accent:#8872be}.transfer-modes .download{--accent:var(--transfer-download)}.transfer-modes .upload{--accent:var(--transfer-upload)}
.transfer-modes button>svg{flex:none;color:var(--accent)}.transfer-modes button[aria-selected=true]{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 10%,var(--surface))}
.transfer-mode-summary{min-width:0;flex:1}.transfer-mode-summary>div,.transfer-mode-summary small{display:flex;align-items:center;justify-content:space-between;gap:8px}.transfer-mode-summary>div>span{font-size:13px;white-space:nowrap}.transfer-mode-summary small{margin-top:10px;font-size:12px;color:var(--muted);font-variant-numeric:tabular-nums;white-space:nowrap}.transfer-mode-summary b{font-weight:500;color:var(--text)}
.transfer-mode-summary progress{display:block;width:100%;height:4px;margin-top:8px;accent-color:var(--accent)}
.transfer-table{display:flex;flex:1;flex-direction:column;position:relative;min-height:240px;border:1px solid var(--border);border-radius:8px;background:var(--surface);overflow:hidden!important}.has-transfers .transfer-table{flex:none;height:calc(100dvh - 64px)}
.transfer-header,.transfer-table tr:not(.transfer-spacer){display:grid;grid-template-columns:minmax(110px,1.6fr) minmax(125px,1.3fr) minmax(125px,1.3fr) 128px 75px 80px 140px;align-items:center}
.transfer-header{flex:none;height:42px;border-bottom:1px solid var(--border);background:var(--layer-item);font-size:12px;color:var(--muted)}
.transfer-header span,.transfer-table td{padding:8px 12px;min-width:0}
.transfer-viewport{flex:1;min-height:0;overflow:auto;scrollbar-width:none;overscroll-behavior:contain}.transfer-viewport::-webkit-scrollbar{display:none}
.transfer-table table,.transfer-table tbody{display:block;width:100%}.transfer-table tr:not(.transfer-spacer){height:64px;border:1px solid transparent;border-bottom-color:var(--border);--progress-color:var(--transfer-blue)}
.transfer-table tr.green{--progress-color:var(--green)}.transfer-table tr.red{--progress-color:var(--red)}.transfer-table tr.orange{--progress-color:var(--transfer-orange)}
.transfer-table td{border:0;max-width:none;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:13px}.transfer-table .transfer-spacer td{padding:0;border:0}.transfer-table .transfer-spacer{display:block}
.transfer-progress-content{width:104px;max-width:100%}.transfer-status{display:flex;align-items:center;gap:4px;font-size:12px;color:var(--progress-color)}.transfer-status>svg{flex:none}.transfer-status small{margin-left:auto;font-size:11px;font-variant-numeric:tabular-nums;color:var(--muted)}
.transfer-table progress{appearance:none;display:block;width:100%;height:4px;margin-top:8px;border:1px solid var(--progress-color);border-radius:3px;overflow:hidden;background:var(--layer-item)}
.transfer-table progress::-webkit-progress-bar{background:var(--layer-item)}.transfer-table progress::-webkit-progress-value{background:var(--progress-color)}.transfer-table progress::-moz-progress-bar{background:var(--progress-color)}
.transfer-table tr.selected{background:var(--selection-bg);border-color:var(--selection-border)}
.transfer-table :deep(.scroll-rail){top:42px}
.transfer-table.multi-select .transfer-viewport{padding-bottom:56px}
.backup-content{padding:16px;border:1px solid var(--border);border-radius:8px;background:var(--surface);min-height:0}.backup-content :deep(.backup-grid){align-content:start}
@media(max-width:1200px){.transfer-header,.transfer-table tr:not(.transfer-spacer){grid-template-columns:minmax(90px,1.4fr) minmax(110px,1fr) minmax(110px,1fr) 118px 68px 72px}.transfer-header span:last-child,.transfer-table .transfer-started{display:none}.transfer-header span,.transfer-table td{padding-inline:7px}}
@media(max-width:700px){
 .task-heading:not(.backup-heading){flex-wrap:wrap}.task-heading:not(.backup-heading)>.transfer-actions{width:100%;justify-content:flex-end}.transfer-actions{gap:5px}.transfer-actions .search-field{width:160px}
 .transfer-modes{gap:6px}.transfer-modes button{padding:10px 8px;min-height:118px;gap:6px;align-items:flex-start;position:relative}.transfer-modes button>svg{position:absolute;top:12px;width:16px;height:16px}.transfer-mode-summary>div{padding-left:22px;flex-wrap:wrap;gap:4px}.transfer-mode-summary strong{font-size:13px}.transfer-mode-summary>div>span{font-size:12px}.transfer-mode-summary small{white-space:normal;flex-wrap:wrap;gap:3px;font-size:11px}
 .transfer-header{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%);border:0}
 .transfer-table tr:not(.transfer-spacer){grid-template-columns:repeat(2,minmax(0,1fr));grid-template-rows:24px 58px 24px 20px;grid-template-areas:"file file" "from to" "progress progress" "size speed";height:160px;padding:9px 10px;column-gap:14px;row-gap:4px}
 .transfer-table td{padding:0;font-size:12px}.transfer-name{grid-area:file}.transfer-from{grid-area:from}.transfer-to{grid-area:to}.transfer-progress{grid-area:progress}.transfer-size{grid-area:size}.transfer-speed{grid-area:speed;text-align:right}
 .transfer-from::before,.transfer-to::before{content:attr(data-label);display:block;color:var(--muted);font-size:11px;line-height:13px;margin-bottom:2px}.transfer-size::before,.transfer-speed::before{content:attr(data-label);color:var(--muted);margin-right:6px}
 .transfer-progress-content{display:flex;align-items:center;gap:12px;width:100%;max-width:none}.transfer-status{width:104px;flex:none}.transfer-table progress{width:104px;max-width:calc(100% - 116px);margin-top:0}.transfer-table :deep(.scroll-rail){top:0}
 .transfer-table.multi-select .transfer-viewport{padding-bottom:96px}
}
</style>
