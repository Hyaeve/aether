<script setup>
import {computed,onMounted,onBeforeUnmount,ref,watch} from 'vue'
import {api,bytes,date,notify,state} from '../lib'
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
const props=defineProps({kind:String})
const pools=computed(()=>state.storages.filter(s=>s.enabled&&(props.kind==='trash'?s.type==='115':props.kind==='shares'?['115','quark'].includes(s.type):s.type==='115')))
const options=computed(()=>[...pools.value.map(s=>({value:s.id,label:s.name,provider:s.type})),...(props.kind==='downloads'?[{value:'cas',label:'CAS 秒传',icon:'Layers3'}]:[])])
const storage=ref(options.value[0]?.value||''),items=ref([]),busy=ref(false),loading=ref(false),selected=ref([]),selecting=ref(false),confirm=ref(null),password=ref(''),editing=ref(null),capabilities=ref({}),nextPage=ref(0),pageError=ref(false),total=ref(0),selectAll=ref(false)
let request=0,alive=true,anchor=0
const pool=computed(()=>pools.value.find(s=>s.id===storage.value))
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
onMounted(()=>{load();document.addEventListener('keydown',key);document.addEventListener('pointerdown',outside)})
onBeforeUnmount(()=>{alive=false;request++;document.removeEventListener('keydown',key);document.removeEventListener('pointerdown',outside)})
</script>
<template>
<header class="task-heading"><StorageTabs /><div class="toolbar-right cloud-toolbar"><button class="icon-btn" aria-label="刷新云盘栏目" :disabled="busy||loading||!storage" @click="refresh"><Icon name="RefreshCw" :class="{spin:loading}" /></button><button v-if="kind==='downloads'" class="btn danger-text" :disabled="busy||loading||!storage" @click="prepareCleanup"><Icon name="Trash2" />清理</button><button v-if="kind==='trash'" class="btn danger-text" :disabled="busy||loading||!items.length" @click="ask('clear',items)"><Icon name="Trash2" />清空</button><span class="cloud-total">{{items.length}} 项</span><RoundedSelect v-model="storage" :disabled="busy||!!confirm||!!editing" label="选择云盘存储池" :options="options" hide-check><template #selected="{option}"><Icon v-if="option?.icon" :name="option.icon" :size="16" /><ProviderIcon v-else-if="option" :type="option.provider" compact /><span>{{option?.label}}</span></template><template #option="{option}"><Icon v-if="option.icon" :name="option.icon" :size="16" /><ProviderIcon v-else :type="option.provider" compact /><span>{{option.label}}</span></template></RoundedSelect></div></header>
<section class="cloud-management-panel" :class="kind" :aria-busy="loading"><div class="cloud-management-columns"><template v-if="kind==='shares'"><span>分享内容</span><span>文件大小</span><span>分享链接</span><span>状态</span><span>操作</span></template><template v-else-if="kind==='trash'"><span>文件名称</span><span>文件大小</span><span>原路径</span><span>删除时间</span><span>操作</span></template><template v-else><span>任务名称</span><span>文件大小</span><span>{{storage==='cas'?'目标目录':'任务链接 / 目标路径'}}</span><span>执行时间 / 状态</span><span>操作</span></template></div>
<VirtualList :items="items" :row-height="72" preserve-scroll window @near-end="more"><template #default="{item,index}"><article class="cloud-management-row" :class="{selected:selected.includes(item.id)}" @contextmenu.prevent="choose($event,item,index)" @click="choose($event,item,index)"><span class="cloud-row-name"><Icon :name="item.isDir?'Folder':entryIcon(item)" :size="18" /><span>{{item.name}}</span></span><button v-if="kind==='shares'" class="cloud-link" :data-tooltip="item.url" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300" @click.stop="selecting?choose($event,item,index):copy(item)">{{item.url}}</button><button v-else-if="kind==='downloads' && item.url" class="cloud-path cloud-copy" :data-tooltip="[item.url,item.path].filter(Boolean).join(' · ')" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300" @click.stop="selecting?choose($event,item,index):copyPath(item)">{{item.url}}<template v-if="item.path"> · {{item.path}}</template></button><span v-else class="cloud-path" :data-tooltip="storage==='cas'?[item.storageName,item.path].filter(Boolean).join(' / '):kind==='trash'?item.path:[item.url,item.path].filter(Boolean).join(' · ')" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300"><template v-if="storage==='cas'"><span class="cloud-target"><ProviderIcon :type="item.provider" compact /><strong>{{item.storageName}}</strong></span><span class="cloud-path-line"><bdi dir="ltr">{{item.path}}</bdi></span></template><template v-else><bdi dir="ltr">{{kind==='trash'?item.path:[item.url,item.path].filter(Boolean).join(' · ')}}</bdi></template></span><span class="cloud-size">{{bytes(item.size)}}</span><span v-if="kind==='shares'" class="cloud-status">{{item.status}}<small v-if="item.expiration">{{item.expiration}}</small></span><span v-else class="cloud-status"><time>{{date(item.time)}}</time><template v-if="kind==='downloads'"><small :class="item.status">{{{success:'成功',error:'失败',running:'进行中',queued:'排队',interrupted:'已中断'}[item.status]||item.status}}</small><progress :class="item.status" :value="item.progress||0" max="1" /></template></span><div class="cloud-row-actions" @click.stop><template v-if="kind==='shares'"><button v-if="capabilities.edit" class="icon-btn" aria-label="修改分享设置" :disabled="busy" @click="editing=item"><Icon name="Pencil" /></button><button class="icon-btn danger-text" aria-label="撤回分享" :disabled="busy" @click="ask('cancel',[item])"><Icon name="X" /></button></template><template v-else><button class="icon-btn" :aria-label="kind==='trash'?'还原':'重试'" :disabled="busy||kind==='downloads'&&(item.status==='success'||storage==='cas'&&item.status==='running')" @click="ask(kind==='trash'?'restore':'retry',[item])"><Icon :name="kind==='trash'?'Undo2':'RotateCcw'" /></button><button class="icon-btn danger-text" :aria-label="kind==='trash'?'彻底删除':'删除记录'" :disabled="busy||storage==='cas'&&item.status==='running'" @click="ask('delete',[item])"><Icon name="Trash2" /></button></template></div></article></template></VirtualList>
<span v-if="loading" class="cloud-loading" role="status"><Icon name="LoaderCircle" class="spin" :size="14" />加载中</span></section>
<Teleport to="body"><div v-if="selecting" class="cloud-management-dock" role="toolbar" aria-label="所选云盘条目操作"><strong>已选 {{selected.length}} 项</strong><button class="btn" :aria-pressed="selectAll" :class="{active:selectAll}" @click="toggleSelectAll"><Icon name="CheckCheck" />全选</button><button class="btn" @click="clearSelection"><Icon name="X" />清空</button><button v-if="kind!=='shares'" class="btn" :disabled="busy||!(kind==='trash'?selected.length:eligible.length)" @click="ask(kind==='trash'?'restore':'retry',kind==='trash'?selectedRows:eligible)"><Icon :name="kind==='trash'?'Undo2':'RotateCcw'" />{{kind==='trash'?'还原':'重试'}}</button><button class="btn danger-text" :disabled="busy||!selected.length||storage==='cas'&&selectedRows.some(i=>i.status==='running')" @click="ask(kind==='shares'?'cancel':'delete',selectedRows)"><Icon name="Trash2" />{{kind==='trash'?'彻底删除':kind==='shares'?'删除分享':'删除'}}</button></div></Teleport>
<Modal v-if="confirm" compact confirmation :title="`确认${actionLabel(confirm.action)}`" @close="if(!busy){confirm=null;password=''}"><form @submit.prevent="execute"><div class="modal-body cloud-confirm"><p>确认{{actionLabel(confirm.action)}} {{confirm.action==='clear'?'全部回收站条目（包括未加载的项目）':confirm.count+' 项'}}？{{kind==='trash'&&confirm.action!=='restore'?'无法撤销。':''}}</p><label v-if="kind==='trash'&&confirm.action!=='restore'&&capabilities.password">115回收站密码<PinInput v-model="password" label="115回收站密码" :disabled="busy" /></label></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="confirm=null;password=''">取消</button><button class="btn primary" :disabled="busy||kind==='trash'&&confirm.action!=='restore'&&capabilities.password&&!/^\d{6}$/.test(password)"><Icon name="Check" />确认</button></footer></form></Modal>
<CloudShare v-if="editing" :storage="storage" :editing="editing" @close="editing=null" @changed="load()" />
</template>
<style scoped>
.cloud-management-panel{position:relative;min-height:calc(100dvh - 190px);border:1px solid var(--border);border-radius:8px;background:var(--surface);overflow:hidden;padding-bottom:22px}
.cloud-management-columns,.cloud-management-row{display:grid;grid-template-columns:minmax(120px,1.6fr) 80px minmax(100px,2fr) minmax(110px,1fr) 68px;align-items:center;gap:12px;padding:10px 14px;font-size:13px}
.cloud-management-columns{color:var(--muted);height:42px;border-bottom:1px solid var(--border)}
.cloud-management-row{height:72px;border:1px solid transparent;border-bottom-color:var(--border);border-radius:6px;background:var(--input);user-select:none}
.cloud-management-panel :deep(.virtual-directory-row:nth-child(odd)) .cloud-management-row{background:var(--layer-item)}
.cloud-management-panel .cloud-management-row.selected{border-color:var(--primary);background:color-mix(in srgb,var(--primary) 13%,var(--input))}
.cloud-row-name{grid-column:1;grid-row:1;display:flex;gap:8px;align-items:center;min-width:0}
.cloud-row-name svg{flex:none}.cloud-row-name>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.cloud-size{grid-column:2;grid-row:1}.cloud-path,.cloud-link{grid-column:3;grid-row:1;min-width:0}
.cloud-status{grid-column:4;grid-row:1;min-width:0}.cloud-row-actions{grid-column:5;grid-row:1;display:flex;gap:4px;flex-wrap:nowrap}
.cloud-management-row>span,.cloud-management-row>.cloud-copy{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}
.cloud-management-row small{display:block;font-size:12px;overflow:hidden;text-overflow:ellipsis}
.cloud-management-panel :deep(.cloud-row-actions .icon-btn){width:30px;height:30px;min-height:30px}
.cloud-management-panel :deep(.cloud-link),.cloud-management-panel :deep(.cloud-copy){height:auto;min-height:0;width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:0;text-align:left;border:0;background:none;color:var(--primary);font-size:13px}
.cloud-path{direction:rtl;text-align:left}.cloud-copy{direction:ltr!important}
.cloud-total{padding:8px 12px;border:1px solid var(--border);border-radius:8px;color:var(--muted);white-space:nowrap;font-size:13px}
.cloud-management-row progress{width:100%;height:4px;accent-color:var(--amber);border:0;border-radius:3px;overflow:hidden;background:var(--layer-item)}
.cloud-management-row progress::-webkit-progress-bar{background:var(--layer-item)}.cloud-management-row progress::-webkit-progress-value{background:var(--amber)}
.cloud-management-row .success{color:var(--green);accent-color:var(--green)}.cloud-management-row .error{color:var(--red);accent-color:var(--red)}.cloud-management-row .running{color:var(--amber)}
.cloud-management-row progress.success::-webkit-progress-value{background:var(--green)}.cloud-management-row progress.error::-webkit-progress-value{background:var(--red)}
.cloud-management-dock{position:fixed;bottom:var(--workspace-end-gap);left:50%;transform:translateX(-50%);display:flex;align-items:center;gap:8px;border:1px solid var(--border);border-radius:999px;padding:7px 12px;background:color-mix(in srgb,var(--surface) 90%,transparent);backdrop-filter:blur(20px);z-index:140;font-size:13px;max-width:calc(100vw - 16px);white-space:nowrap}
.cloud-management-dock .btn{height:30px;padding:3px 8px}.cloud-management-dock .btn.active{background:var(--primary-soft);color:var(--primary)}
.cloud-confirm{display:grid;gap:14px}.cloud-confirm label{display:grid;gap:8px}.cloud-toolbar{align-items:center}
.cloud-loading{position:absolute;right:14px;bottom:3px;display:flex;align-items:center;gap:5px;color:var(--muted);font-size:12px}
.cloud-target{display:flex;align-items:center;gap:6px;min-width:0;direction:ltr}.cloud-target strong{font-weight:500;overflow:hidden;text-overflow:ellipsis}
.cloud-path>span{display:block;overflow:hidden;text-overflow:ellipsis}.cloud-path .cloud-target{display:flex}
@media(max-width:1000px){.cloud-management-columns,.cloud-management-row{grid-template-columns:minmax(80px,1.4fr) 60px minmax(30px,1fr) 82px 76px;gap:7px}}
@media(max-width:760px){.task-heading{flex-wrap:wrap;gap:10px}.task-heading :deep(.content-tabs){width:100%;flex:1 0 100%}.toolbar-right{width:100%;justify-content:flex-end;gap:8px;min-width:0}.toolbar-right :deep(.rounded-select){min-width:0;flex:1}.toolbar-right .btn{flex:none}.cloud-management-panel{min-height:calc(100dvh - 220px)}}
@media(max-width:480px){
 .cloud-management-columns,.cloud-management-row{grid-template-columns:minmax(60px,1fr) 40px minmax(40px,.8fr) 76px;gap:4px;padding:8px;font-size:11px}
 .cloud-management-columns>span:nth-child(4){display:none}
 .cloud-management-row{grid-template-rows:30px 20px}
 .cloud-management-panel :deep(.cloud-row-actions .icon-btn){width:36px;min-width:36px;height:36px;min-height:36px}
 .cloud-row-actions{grid-column:4;grid-row:1/3;gap:2px}
 .cloud-status{grid-column:1/4;grid-row:2;display:flex;align-items:center;gap:6px;overflow:hidden}
 .cloud-status time{overflow:hidden;text-overflow:ellipsis}.cloud-status progress{min-width:20px;flex:1;width:auto}
 .cloud-row-name{gap:4px}.cloud-management-row small{font-size:10px;flex:none}
 .cloud-management-dock{gap:3px;padding:6px}.cloud-management-dock strong{font-size:11px}.cloud-management-dock .btn{padding:3px 5px;font-size:12px}
}
</style>
