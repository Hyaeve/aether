<script setup>
import {computed,onMounted,onBeforeUnmount,ref,watch} from 'vue'
import {api,bytes,notify,state} from '../lib'
import {copyOwnShare} from '../own-shares'
import {copyText} from '../clipboard'
import {entryIcon} from '../file-icon'
import StorageTabs from '../components/StorageTabs.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import PinInput from '../components/PinInput.vue'
import CloudShare from '../components/CloudShare.vue'
import VirtualList from '../components/VirtualList.vue'
import {compactDate} from '../date-time.mjs'
const props=defineProps({kind:String})
const pools=computed(()=>state.storages.filter(s=>s.enabled&&(props.kind==='trash'?s.type==='115':props.kind==='shares'?['115','quark'].includes(s.type):s.type==='115')))
const options=computed(()=>[...pools.value.map(s=>({value:s.id,label:s.name,provider:s.type})),...(props.kind==='downloads'?[{value:'cas',label:'CAS 秒传',icon:'Layers3'}]:[])])
const storage=ref(options.value[0]?.value||''),items=ref([]),busy=ref(false),loading=ref(false),selected=ref([]),selecting=ref(false),confirm=ref(null),password=ref(''),editing=ref(null),capabilities=ref({}),nextPage=ref(0),pageError=ref(false),total=ref(0),selectAll=ref(false)
let request=0,alive=true,anchor=0
const pool=computed(()=>pools.value.find(s=>s.id===storage.value))
const compact=ref(window.innerWidth<=600)
function resize(){compact.value=window.innerWidth<=600}
const progressValue=item=>item.status==='success'?1:Math.max(0,Math.min(1,Number(item.progress)||0))
const statusLabel=item=>({success:'成功',error:'失败',running:'进行中',queued:'排队',interrupted:'已中断'}[item.status]||item.status)
const selectedRows=computed(()=>items.value.filter(i=>selected.value.includes(i.id)))
const cleanable=computed(()=>selectedRows.value.filter(i=>['success','error','interrupted'].includes(i.status)))
const eligible=computed(()=>selectedRows.value.filter(i=>i.status!=='success'&&!(storage.value==='cas'&&i.status==='running')))
async function load(page=1){
 if(!storage.value||loading.value||busy.value)return
 const run=++request,sid=storage.value;loading.value=true;pageError.value=false
 try{
  const value=await api(`/cloud/manage?kind=${props.kind}&storage=${encodeURIComponent(sid)}&page=${page}`)
  if(!alive||run!==request||sid!==storage.value)return
  // Use the request snapshot here. The selection can change while an async
  // response is settling; rendering from the live ref can misclassify CAS
  // rows as ordinary cloud-download rows.
  const incoming=(value.items||[]).map(item=>sid==='cas'?{...item,url:''}:item)
  if(page===1)items.value=incoming
  else { const known=new Set(items.value.map(i=>i.id)),added=incoming.filter(i=>!known.has(i.id));if(incoming.length&&!added.length)throw new Error('云盘分页内容重复，请刷新核对');items.value=[...items.value,...added] }
  nextPage.value=Number.isInteger(value.nextPage)&&value.nextPage>page?value.nextPage:0
  total.value=Number.isFinite(value.total)?value.total:items.value.length
  capabilities.value=value;selected.value=selected.value.filter(id=>items.value.some(i=>i.id===id))
  if(selectAll.value) selected.value=[...new Set([...selected.value,...incoming.map(i=>i.id)])]
 }catch(e){if(alive&&run===request&&sid===storage.value){pageError.value=true;notify(e.message,true)}}
 finally{if(alive&&run===request)loading.value=false}
}
function more(){if(nextPage.value&&!pageError.value)load(nextPage.value)}
function refresh(){load(pageError.value&&items.value.length&&nextPage.value?nextPage.value:1)}
watch(storage,()=>{request++;loading.value=false;items.value=[];nextPage.value=0;total.value=0;confirm.value=null;editing.value=null;password.value='';clearSelection();load()})
watch(options,values=>{if(!values.some(o=>o.value===storage.value))storage.value=values[0]?.value||''})
function clearSelection(){selected.value=[];selecting.value=false;selectAll.value=false}
function toggleSelectAll(){selectAll.value=!selectAll.value;selected.value=selectAll.value?items.value.map(i=>i.id):[]}
function choose(event,item,index){selectAll.value=false;if(event.button===2||event.type==='contextmenu')selecting.value=true;if(!selecting.value)return;if(event.shiftKey){const range=items.value.slice(Math.min(anchor,index),Math.max(anchor,index)+1).map(i=>i.id);selected.value=[...new Set([...selected.value,...range])]}else{selected.value=selected.value.includes(item.id)?selected.value.filter(id=>id!==item.id):[...selected.value,item.id];anchor=index}}
function ask(action,rows){if(busy.value||!rows.length)return;password.value='';confirm.value={action,rows:rows.map(i=>({...i})),count:rows.length,storage:storage.value}}
async function prepareCleanup(){
 if(busy.value||loading.value)return
 const sid=storage.value,run=++request;busy.value=true
 try{const value=await api(`/cloud/manage?kind=downloads&storage=${encodeURIComponent(sid)}&prepare=cleanup`);if(!alive||run!==request||sid!==storage.value)return;if(value.items?.length){busy.value=false;ask('cleanup',value.items)}else notify('没有可清理的已结束任务记录')}
 catch(e){if(alive&&run===request)notify(e.message,true)}finally{if(alive)busy.value=false}
}
async function execute(){
 const value=confirm.value;if(busy.value||!value||value.storage!==storage.value)return
 busy.value=true;let completed=0
 const groups=new Map()
 for(const row of value.rows){const sid=value.storage==='cas'?row.storageId:value.storage;if(!sid)continue;if(!groups.has(sid))groups.set(sid,[]);groups.get(sid).push(row.id)}
 try{
  for(const [sid,ids] of groups){
   const batches=value.action==='clear'?[[]]:Array.from({length:Math.ceil(ids.length/499)},(_,i)=>ids.slice(i*499,(i+1)*499))
   for(const batch of batches){
    if(!alive||!state.authenticated||value.storage!==storage.value)return
    const result=await api(`/cloud/manage?kind=${props.kind}&storage=${encodeURIComponent(sid)}`,'POST',{action:value.action,ids:batch,password:password.value,confirm:true})
    completed+=result.count
   }
  }
  if(!alive||value.storage!==storage.value)return
  confirm.value=null;clearSelection();notify(`操作完成：${completed}项`);busy.value=false;await load()
 }catch(e){if(alive&&value.storage===storage.value){confirm.value=null;clearSelection();notify(`${completed?`已完成 ${completed} 项。`:''}${e.message}；请刷新核对结果，未自动重试`,true)}}
 finally{if(alive){password.value='';busy.value=false}}
}
function key(event){if(event.key==='Escape')clearSelection()}
function outside(event){if(!event.target.closest('.cloud-management-row,.cloud-management-dock,.modal'))clearSelection()}
const actionLabel=action=>({restore:'还原',delete:props.kind==='trash'?'彻底删除':props.kind==='downloads'?'清理记录':'删除记录',clear:'清空',cleanup:'清理记录',retry:'重试',cancel:'撤回分享'})[action]
async function copy(item){try{await copyOwnShare(item,pool.value.type,true);notify('分享内容已复制')}catch{notify('复制失败，请检查浏览器权限',true)}}
async function copyPath(item){try{await copyText(item.url);notify('任务链接已复制')}catch{notify('复制失败，请检查浏览器权限',true)}}
onMounted(()=>{load();window.addEventListener('resize',resize);document.addEventListener('keydown',key);document.addEventListener('pointerdown',outside)})
onBeforeUnmount(()=>{alive=false;request++;window.removeEventListener('resize',resize);document.removeEventListener('keydown',key);document.removeEventListener('pointerdown',outside)})
</script>
<template>
<header class="task-heading"><StorageTabs /><div class="toolbar-right cloud-toolbar">
<RoundedSelect v-model="storage" :disabled="busy||!!confirm||!!editing" label="选择云盘存储池" :options="options" hide-check><template #selected="{option}"><Icon v-if="option?.icon" :name="option.icon" :size="16" /><ProviderIcon v-else-if="option" :type="option.provider" compact /><span>{{option?.label}}</span></template><template #option="{option}"><Icon v-if="option.icon" :name="option.icon" :size="16" /><ProviderIcon v-else :type="option.provider" compact /><span>{{option.label}}</span></template></RoundedSelect>
<button v-if="kind==='downloads'" class="btn danger-text cloud-clean" :disabled="busy||loading||!storage" @click="prepareCleanup"><Icon name="Trash2" />清理</button><button v-if="kind==='trash'" class="btn danger-text cloud-clean" :disabled="busy||loading||!items.length" @click="ask('clear',items)"><Icon name="Trash2" />清空</button>
<span class="cloud-total">{{items.length}} 项</span><button class="icon-btn" aria-label="刷新云盘栏目" :disabled="busy||loading||!storage" @click="refresh"><Icon name="RefreshCw" :class="{spin:loading}" /></button></div></header>
<section class="cloud-management-panel" :class="[kind,{cas:storage==='cas'}]" :aria-busy="loading"><div class="cloud-management-columns">
<span>{{kind==='shares'?'分享内容':kind==='trash'?'文件名称':'任务名称'}}</span><span>文件大小</span>
<span v-if="kind==='shares'">分享链接</span><span v-else-if="kind==='downloads'&&storage!=='cas'">任务链接</span>
<span v-if="kind!=='shares'">{{kind==='trash'?'原路径':'目标路径'}}</span>
<span>{{kind==='downloads'?'执行状态':kind==='trash'?'删除时间':'时间'}}</span><span v-if="kind==='shares'">状态</span><span>操作</span></div>
<VirtualList :items="items" :row-height="compact?112:72" preserve-scroll window @near-end="more"><template #default="{item,index}">
<article class="cloud-management-row" :class="{selected:selected.includes(item.id)}" @contextmenu.prevent="choose($event,item,index)" @click="choose($event,item,index)">
<span class="cloud-row-name"><Icon :name="item.isDir?'Folder':entryIcon(item)" :size="18" /><span>{{item.name}}</span></span><span class="cloud-size">{{bytes(item.size)}}</span>
<button v-if="kind==='shares'||kind==='downloads'&&storage!=='cas'" class="cloud-link" :data-tooltip="item.url" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300" @click.stop="selecting?choose($event,item,index):kind==='shares'?copy(item):copyPath(item)">{{item.url||'--'}}</button>
<span v-if="kind!=='shares'" class="cloud-path" :data-tooltip="storage==='cas'?[item.storageName,item.path].filter(Boolean).join(' / '):item.path" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300"><span v-if="storage==='cas'" class="cloud-target"><ProviderIcon :type="item.provider" compact /><strong>{{item.storageName}}</strong><bdi dir="ltr">{{item.path||'目录名称不可用'}}</bdi></span><bdi v-else dir="ltr">{{item.path||'--'}}</bdi></span>
<span class="cloud-execution"><time :datetime="item.time">{{compactDate(item.time)}}</time><progress v-if="kind==='downloads'" :aria-label="statusLabel(item)" :class="item.status" :value="progressValue(item)" max="1" /></span>
<span v-if="kind==='shares'" class="cloud-status">{{item.status}}<small v-if="item.expiration">{{item.expiration}}</small></span>
<div class="cloud-row-actions" @click.stop><template v-if="kind==='shares'"><button v-if="capabilities.edit" class="icon-btn" aria-label="修改分享设置" :disabled="busy" @click="editing=item"><Icon name="Pencil" /></button><button class="icon-btn danger-text" aria-label="撤回分享" :disabled="busy" @click="ask('cancel',[item])"><Icon name="X" /></button></template><template v-else><button class="icon-btn" :aria-label="kind==='trash'?'还原':'重试'" :disabled="busy||kind==='downloads'&&(item.status==='success'||storage==='cas'&&item.status==='running')" @click="ask(kind==='trash'?'restore':'retry',[item])"><Icon :name="kind==='trash'?'Undo2':'RotateCcw'" /></button><button class="icon-btn danger-text" :aria-label="kind==='trash'?'彻底删除':'删除记录'" :disabled="busy||storage==='cas'&&item.status==='running'" @click="ask('delete',[item])"><Icon name="Trash2" /></button></template></div></article></template></VirtualList>
<span v-if="loading" class="cloud-loading" role="status"><Icon name="LoaderCircle" class="spin" :size="14" />加载中</span></section>
<Teleport to="body"><div v-if="selecting" class="cloud-management-dock" role="toolbar" aria-label="所选云盘条目操作"><strong>已选 {{selected.length}} 项</strong><button class="btn" :aria-pressed="selectAll" :class="{active:selectAll}" @click="toggleSelectAll"><Icon name="CheckCheck" />全选</button><button class="btn" @click="clearSelection"><Icon name="X" />清空</button><button v-if="kind!=='shares'" class="btn" :disabled="busy||!(kind==='trash'?selected.length:eligible.length)" @click="ask(kind==='trash'?'restore':'retry',kind==='trash'?selectedRows:eligible)"><Icon :name="kind==='trash'?'Undo2':'RotateCcw'" />{{kind==='trash'?'还原':'重试'}}</button><button class="btn danger-text" :disabled="busy||!selected.length||storage==='cas'&&selectedRows.some(i=>i.status==='running')" @click="ask(kind==='shares'?'cancel':'delete',selectedRows)"><Icon name="Trash2" />{{kind==='trash'?'彻底删除':kind==='shares'?'删除分享':'删除'}}</button></div></Teleport>
<Modal v-if="confirm" compact confirmation :title="`确认${actionLabel(confirm.action)}`" @close="if(!busy){confirm=null;password=''}"><form @submit.prevent="execute"><div class="modal-body cloud-confirm"><p>确认{{actionLabel(confirm.action)}} {{confirm.action==='clear'?'全部回收站条目（包括未加载的项目）':confirm.count+' 项'}}？{{kind==='trash'&&confirm.action!=='restore'?'无法撤销。':''}}</p><label v-if="kind==='trash'&&confirm.action!=='restore'&&capabilities.password">115回收站密码<PinInput v-model="password" label="115回收站密码" :disabled="busy" /></label></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="confirm=null;password=''">取消</button><button class="btn primary" :disabled="busy||kind==='trash'&&confirm.action!=='restore'&&capabilities.password&&!/^\d{6}$/.test(password)"><Icon name="Check" />确认</button></footer></form></Modal>
<CloudShare v-if="editing" :storage="storage" :editing="editing" @close="editing=null" @changed="load()" />
</template>
<style scoped>
.cloud-management-panel{position:relative;min-height:calc(100dvh - 190px);border:1px solid var(--border);border-radius:8px;background:var(--surface);overflow:hidden;padding-bottom:22px}
.cloud-management-columns,.cloud-management-row{display:grid;grid-template-columns:minmax(90px,1.06fr) 84px minmax(120px,2fr) 132px 68px;align-items:center;gap:12px;padding:10px 14px;font-size:14px}
.downloads:not(.cas) .cloud-management-columns,.downloads:not(.cas) .cloud-management-row{grid-template-columns:minmax(90px,1.06fr) 84px minmax(100px,1.2fr) minmax(100px,1.4fr) 132px 68px}
.shares .cloud-management-columns,.shares .cloud-management-row{grid-template-columns:minmax(90px,1.06fr) 84px minmax(120px,2fr) 132px minmax(70px,.65fr) 68px}
.cloud-management-columns{color:var(--muted);height:42px;border-bottom:1px solid var(--border)}
.cloud-management-row{height:72px;border:1px solid transparent;border-bottom-color:var(--border);border-radius:6px;background:var(--input);user-select:none}
.cloud-management-panel :deep(.virtual-directory-row:nth-child(odd)) .cloud-management-row{background:var(--layer-item)}
.cloud-management-panel .cloud-management-row.selected{border-color:var(--primary);background:color-mix(in srgb,var(--primary) 13%,var(--input))}
.cloud-row-name{grid-column:1;grid-row:1;display:flex;gap:8px;align-items:center;min-width:0}
.cloud-row-name svg{flex:none}.cloud-row-name>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.cloud-size{grid-column:2;grid-row:1}.cloud-path,.cloud-link{grid-column:3;grid-row:1;min-width:0}
.cloud-execution{grid-column:4;grid-row:1;display:grid;gap:7px;min-width:0;font-variant-numeric:tabular-nums}
.downloads:not(.cas) .cloud-path{grid-column:4}.downloads:not(.cas) .cloud-execution{grid-column:5}
.cloud-status{grid-column:5;grid-row:1;min-width:0}.cloud-row-actions{grid-column:-2;grid-row:1;display:flex;gap:4px;flex-wrap:nowrap}
.cloud-management-row>span,.cloud-management-row>.cloud-copy{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}
.cloud-management-row small{display:block;font-size:12px;overflow:hidden;text-overflow:ellipsis}
.cloud-management-panel :deep(.cloud-row-actions .icon-btn){width:30px;height:30px;min-height:30px}
.cloud-management-panel :deep(.cloud-link),.cloud-management-panel :deep(.cloud-copy){height:auto;min-height:0;width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:0;text-align:left;border:0;background:none;color:var(--primary);font-size:14px}
.cloud-path{direction:rtl;text-align:left}.cloud-copy{direction:ltr!important}
.cloud-total{padding:8px 12px;border:1px solid color-mix(in srgb,var(--primary) 24%,var(--border));border-radius:8px;color:var(--primary);background:var(--primary-soft);white-space:nowrap;font-size:14px}
.cloud-clean:hover:not(:disabled){color:var(--red);border-color:color-mix(in srgb,var(--red) 40%,var(--border));background:color-mix(in srgb,var(--red) 12%,var(--surface))}
.cloud-management-row progress{width:100%;height:7px;accent-color:var(--amber);border:1px solid var(--amber);border-radius:4px;overflow:hidden;background:var(--layer-item)}
.cloud-management-row progress::-webkit-progress-bar{background:var(--layer-item)}.cloud-management-row progress::-webkit-progress-value{background:var(--amber)}
.cloud-management-row .success{color:var(--green);accent-color:var(--green)}.cloud-management-row .error{color:var(--red);accent-color:var(--red)}.cloud-management-row .running{color:var(--amber)}
.cloud-management-row progress.success{border-color:var(--green)}.cloud-management-row progress.error,.cloud-management-row progress.interrupted{border-color:var(--red);accent-color:var(--red)}
.cloud-management-row progress.success::-webkit-progress-value{background:var(--green)}.cloud-management-row progress.error::-webkit-progress-value,.cloud-management-row progress.interrupted::-webkit-progress-value{background:var(--red)}
.cloud-management-row progress::-moz-progress-bar{background:var(--amber)}.cloud-management-row progress.success::-moz-progress-bar{background:var(--green)}.cloud-management-row progress.error::-moz-progress-bar,.cloud-management-row progress.interrupted::-moz-progress-bar{background:var(--red)}
.cloud-management-dock{position:fixed;bottom:var(--workspace-end-gap);left:50%;transform:translateX(-50%);display:flex;align-items:center;gap:8px;border:1px solid var(--border);border-radius:999px;padding:7px 12px;background:color-mix(in srgb,var(--surface) 90%,transparent);backdrop-filter:blur(20px);z-index:140;font-size:13px;max-width:calc(100vw - 16px);white-space:nowrap}
.cloud-management-dock .btn{height:30px;padding:3px 8px}.cloud-management-dock .btn.active{background:var(--primary-soft);color:var(--primary)}
.cloud-confirm{display:grid;gap:14px}.cloud-confirm label{display:grid;gap:8px}.cloud-toolbar{align-items:center}
.cloud-loading{position:absolute;right:14px;bottom:3px;display:flex;align-items:center;gap:5px;color:var(--muted);font-size:12px}
.cloud-target{display:flex;align-items:center;gap:6px;min-width:0;direction:ltr}.cloud-target strong{font-weight:500;max-width:40%;overflow:hidden;text-overflow:ellipsis;flex:none}.cloud-target bdi{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.cloud-target :deep(.provider-icon){flex:none}
.cloud-path>span{display:block;overflow:hidden;text-overflow:ellipsis}.cloud-path .cloud-target{display:flex}
@media(min-width:601px) and (max-width:1100px){.cloud-management-columns,.cloud-management-row{gap:6px;padding-inline:8px}.cloud-management-panel{overflow-x:auto}.cloud-management-columns,.cloud-management-row{min-width:710px}.downloads:not(.cas) .cloud-management-columns,.downloads:not(.cas) .cloud-management-row{min-width:850px}}
@media(max-width:760px){.task-heading{flex-wrap:wrap;gap:10px}.task-heading :deep(.content-tabs){width:100%;flex:1 0 100%}.toolbar-right{width:100%;justify-content:flex-end;gap:8px;min-width:0}.toolbar-right :deep(.rounded-select){min-width:0;flex:1}.toolbar-right .btn{flex:none}.cloud-management-panel{min-height:calc(100dvh - 220px)}}
@media(max-width:600px){
 .cloud-management-panel .cloud-management-columns,.cloud-management-panel .cloud-management-row{grid-template-columns:minmax(60px,1fr) 72px 76px;gap:4px;padding:8px;font-size:13px;min-width:0}
 .cloud-management-columns>span{display:none}.cloud-management-columns>span:first-child,.cloud-management-columns>span:nth-child(2),.cloud-management-columns>span:last-child{display:block}
 .cloud-management-panel .cloud-management-row{grid-template-rows:repeat(4,21px);height:112px}
 .cloud-management-panel :deep(.cloud-row-actions .icon-btn){width:36px;min-width:36px;height:36px;min-height:36px}
 .cloud-row-actions{grid-column:3;grid-row:1/3;gap:2px;align-self:start}
 .cloud-management-panel .cloud-link{grid-column:1/3;grid-row:2;font-size:13px}
 .cloud-management-panel .cloud-path,.cloud-management-panel.downloads .cloud-path{grid-column:1/4;grid-row:3}
 .cloud-management-panel .cloud-execution,.cloud-management-panel.downloads .cloud-execution{grid-column:1/4;grid-row:4;display:flex;align-items:center;gap:10px}
 .cloud-execution time{flex:none}.cloud-execution progress{min-width:20px;flex:1;width:auto}
 .cloud-status{grid-column:1/4;grid-row:3;display:flex;gap:8px}
 .cloud-row-name{gap:4px}.cloud-management-row small{font-size:12px;flex:none}
 .cloud-management-dock{gap:3px;padding:6px}.cloud-management-dock strong{font-size:11px}.cloud-management-dock .btn{padding:3px 5px;font-size:12px}
}
</style>
