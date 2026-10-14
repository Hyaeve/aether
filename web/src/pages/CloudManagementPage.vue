<script setup>
import {computed,onMounted,onBeforeUnmount,ref,watch} from 'vue'
import {api,bytes,date,notify,state} from '../lib'
import {copyOwnShare} from '../own-shares'
import StorageTabs from '../components/StorageTabs.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import SecretInput from '../components/SecretInput.vue'
import CloudShare from '../components/CloudShare.vue'
import VirtualList from '../components/VirtualList.vue'
const props=defineProps({kind:String})
const pools=computed(()=>state.storages.filter(s=>s.enabled&&['115','quark','mobile','tianyi'].includes(s.type)))
const storage=ref(pools.value[0]?.id||''),items=ref([]),busy=ref(false),error=ref(''),selected=ref([]),selecting=ref(false),confirm=ref(null),password=ref(''),editing=ref(null),capabilities=ref({})
let request=0,alive=true,anchor=0,timer
const pool=computed(()=>pools.value.find(s=>s.id===storage.value)),url=computed(()=>`/cloud/manage?kind=${props.kind}&storage=${encodeURIComponent(storage.value)}`)
const selectedRows=computed(()=>items.value.filter(i=>selected.value.includes(i.id)))
const eligible=computed(()=>selectedRows.value.filter(i=>i.status!=='success'))
async function load(){const run=++request;busy.value=true;error.value='';try{const value=await api(url.value);if(alive&&run===request){items.value=value.items||[];capabilities.value=value;selected.value=selected.value.filter(id=>items.value.some(i=>i.id===id))}}catch(e){if(alive&&run===request){items.value=[];error.value=e.message}}finally{if(alive&&run===request)busy.value=false}}
watch(storage,()=>{confirm.value=null;editing.value=null;password.value='';clearSelection();load()})
function clearSelection(){selected.value=[];selecting.value=false}
function choose(event,item,index){if(event.button===2||event.type==='contextmenu')selecting.value=true;if(!selecting.value)return;if(event.shiftKey){const range=items.value.slice(Math.min(anchor,index),Math.max(anchor,index)+1).map(i=>i.id);selected.value=[...new Set([...selected.value,...range])]}else{selected.value=selected.value.includes(item.id)?selected.value.filter(id=>id!==item.id):[...selected.value,item.id];anchor=index}}
function ask(action,rows){if(busy.value)return;password.value='';confirm.value={action,ids:rows.map(i=>i.id),count:rows.length,storage:storage.value}}
async function execute(){
 const value=confirm.value;if(busy.value||!value||value.storage!==storage.value)return
 busy.value=true;error.value='';let completed=0
 const batches=value.action==='clear'?[[]]:Array.from({length:Math.ceil(value.ids.length/499)},(_,i)=>value.ids.slice(i*499,(i+1)*499))
 try{
  for(const ids of batches){
   if(!alive||!state.authenticated||value.storage!==storage.value)return
   const result=await api(`/cloud/manage?kind=${props.kind}&storage=${encodeURIComponent(value.storage)}`,'POST',{action:value.action,ids,password:password.value,confirm:true})
   completed+=result.count
  }
  if(!alive||value.storage!==storage.value)return
  confirm.value=null;clearSelection();notify(`操作完成：${completed}项`);await load()
 }catch(e){if(alive&&value.storage===storage.value){confirm.value=null;clearSelection();error.value=`${completed?`已完成 ${completed} 项。`:''}${e.message}；请刷新核对结果，未自动重试`}}
 finally{if(alive){password.value='';busy.value=false}}
}
function key(event){if(event.key==='Escape')clearSelection()}
function outside(event){if(!event.target.closest('.cloud-management-panel,.cloud-management-dock,.modal'))clearSelection()}
const actionLabel=action=>({restore:'还原',delete:props.kind==='trash'?'彻底删除':'删除记录',clear:'清空回收站',retry:'重试',cancel:'撤回分享'})[action]
async function copy(item){try{await copyOwnShare(item,pool.value.type,true);notify('分享内容已复制')}catch(e){notify('复制失败，请检查浏览器权限',true)}}
onMounted(()=>{load();document.addEventListener('keydown',key);document.addEventListener('pointerdown',outside);if(props.kind==='downloads')timer=setInterval(()=>{if(!busy.value&&!confirm.value)load()},5000)})
onBeforeUnmount(()=>{alive=false;request++;clearInterval(timer);document.removeEventListener('keydown',key);document.removeEventListener('pointerdown',outside)})
</script>
<template>
<header class="task-heading"><StorageTabs /><div class="toolbar-right"><RoundedSelect v-model="storage" :disabled="busy||!!confirm||!!editing" label="选择云盘存储池" :options="pools.map(s=>({value:s.id,label:s.name,provider:s.type}))" hide-check><template #selected="{option}"><ProviderIcon v-if="option" :type="option.provider" compact /><span>{{option?.label}}</span></template><template #option="{option}"><ProviderIcon :type="option.provider" compact /><span>{{option.label}}</span></template></RoundedSelect><button v-if="kind==='trash'" class="btn danger-text" :disabled="busy||!items.length" @click="ask('clear',items)"><Icon name="Trash2" />清空回收站</button><button class="icon-btn" aria-label="刷新云盘栏目" :disabled="busy||!storage" @click="load"><Icon name="RefreshCw" /></button></div></header>
<p v-if="error&&!confirm" class="error-message" role="alert">{{error}}</p>
<section class="cloud-management-panel" :class="kind"><div class="cloud-management-columns"><template v-if="kind==='shares'"><span>分享内容</span><span>分享链接</span><span>文件大小</span><span>状态</span><span>操作</span></template><template v-else-if="kind==='trash'"><span>文件名称</span><span>原路径</span><span>文件大小</span><span>删除时间</span><span>操作</span></template><template v-else><span>任务名称</span><span>任务链接 / 目标路径</span><span>文件大小</span><span>执行时间 / 状态</span><span>操作</span></template></div>
<VirtualList :items="items" :row-height="72"><template #default="{item,index}"><article class="cloud-management-row" :class="{selected:selected.includes(item.id)}" @contextmenu.prevent="choose($event,item,index)" @click="choose($event,item,index)"><span class="cloud-row-name"><Icon :name="item.isDir?'Folder':'File'" :size="18" /><span>{{item.name}}</span></span><button v-if="kind==='shares'" class="cloud-link" @click.stop="copy(item)">{{item.url}}</button><span v-else class="cloud-path">{{kind==='trash'?item.path:[item.url,item.path].filter(Boolean).join(' · ')}}</span><span>{{bytes(item.size)}}</span><span v-if="kind==='shares'">{{item.status}}<small v-if="item.expiration">{{item.expiration}}</small></span><span v-else><time>{{date(item.time)}}</time><template v-if="kind==='downloads'"><small :class="item.status">{{{success:'成功',error:'失败',running:'进行中',queued:'排队'}[item.status]||item.status}}</small><progress :class="item.status" :value="item.progress||0" max="1" /></template></span><div class="cloud-row-actions" @click.stop><template v-if="kind==='shares'"><button v-if="capabilities.edit" class="icon-btn" aria-label="修改分享设置" :disabled="busy" @click="editing=item"><Icon name="Pencil" /></button><button class="icon-btn danger-text" aria-label="撤回分享" :disabled="busy" @click="ask('cancel',[item])"><Icon name="X" /></button></template><template v-else><button class="icon-btn" :aria-label="kind==='trash'?'还原':'重试'" :disabled="busy||kind==='downloads'&&item.status==='success'" @click="ask(kind==='trash'?'restore':'retry',[item])"><Icon :name="kind==='trash'?'ArchiveRestore':'RotateCcw'" /></button><button class="icon-btn danger-text" :aria-label="kind==='trash'?'彻底删除':'删除记录'" :disabled="busy" @click="ask('delete',[item])"><Icon name="Trash2" /></button></template></div></article></template></VirtualList><span class="cloud-item-count">{{items.length}} 项</span></section>
<Teleport to="body"><div v-if="selecting&&kind!=='shares'" class="cloud-management-dock" role="toolbar" aria-label="所选云盘条目操作"><strong>已选 {{selected.length}} 项</strong><button class="btn" @click="selected=items.map(i=>i.id)"><Icon name="CheckCheck" />全选</button><button class="btn" @click="clearSelection"><Icon name="X" />清空</button><button class="btn" :disabled="busy||!(kind==='trash'?selected.length:eligible.length)" @click="ask(kind==='trash'?'restore':'retry',kind==='trash'?selectedRows:eligible)"><Icon :name="kind==='trash'?'ArchiveRestore':'RotateCcw'" />{{kind==='trash'?'还原':'重试'}}</button><button class="btn danger-text" :disabled="busy||!selected.length" @click="ask('delete',selectedRows)"><Icon name="Trash2" />{{kind==='trash'?'彻底删除':'删除'}}</button></div></Teleport>
<Modal v-if="confirm" compact confirmation :title="`确认${actionLabel(confirm.action)}`" @close="if(!busy){confirm=null;password=''}"><form @submit.prevent="execute"><div class="modal-body cloud-confirm"><p>确认{{actionLabel(confirm.action)}} {{confirm.count}} 项？{{kind==='trash'&&confirm.action!=='restore'?'无法撤销。':''}}</p><label v-if="kind==='trash'&&confirm.action!=='restore'&&capabilities.password">115回收站密码<SecretInput v-model="password" aria-label="115回收站密码" maxlength="4" required autocomplete="off" /></label><p v-if="error" class="error-message" role="alert">{{error}}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="confirm=null;password=''">取消</button><button class="btn primary" :disabled="busy"><Icon name="Check" />确认</button></footer></form></Modal>
<CloudShare v-if="editing" :storage="storage" :editing="editing" @close="editing=null" @changed="load" />
</template>
<style scoped>.cloud-management-panel{position:relative;display:flex;flex-direction:column;height:calc(100dvh - 160px);min-height:260px;border:1px solid var(--border);border-radius:8px;background:var(--surface);overflow:hidden;padding-bottom:22px}.cloud-management-columns,.cloud-management-row{display:grid;grid-template-columns:minmax(150px,2fr) minmax(180px,2fr) 85px minmax(115px,1fr) 80px;align-items:center;gap:12px;padding:10px 14px;font-size:13px}.cloud-management-columns{color:var(--muted);flex:none;height:42px;border-bottom:1px solid var(--border)}.cloud-management-row{height:72px;border:1px solid transparent;border-bottom-color:var(--border);background:var(--input)}.cloud-management-row:nth-child(even){background:var(--layer-item)}.cloud-management-row.selected{border-color:var(--primary)}.cloud-management-row>span{min-width:0;overflow:hidden;white-space:nowrap;text-overflow:ellipsis}.cloud-row-name{display:flex;gap:8px;align-items:center}.cloud-row-name svg{flex:none}.cloud-row-name>span{overflow:hidden;text-overflow:ellipsis}.cloud-management-row small{display:block;font-size:12px}.cloud-row-actions{display:flex;gap:4px}.cloud-management-panel :deep(.cloud-row-actions .icon-btn){width:30px;height:30px;min-height:30px}.cloud-management-panel :deep(.cloud-link){height:auto;min-height:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:0;text-align:left;border:0;background:none;color:var(--primary);font-size:13px}.cloud-item-count{position:absolute;bottom:3px;left:14px;font-size:12px;color:var(--muted)}.cloud-management-row progress{width:100%;height:4px;accent-color:var(--amber)}.cloud-management-row .success{color:var(--green);accent-color:var(--green)}.cloud-management-row .error{color:var(--red);accent-color:var(--red)}.cloud-management-row .running{color:var(--amber)}.cloud-management-dock{position:fixed;bottom:12px;left:calc(50% + 118px);transform:translateX(-50%);display:flex;align-items:center;gap:8px;border:1px solid var(--border);border-radius:999px;padding:7px 12px;background:color-mix(in srgb,var(--surface) 90%,transparent);backdrop-filter:blur(20px);z-index:140;font-size:13px}.cloud-management-dock .btn{height:30px;padding:3px 8px}.cloud-confirm{display:grid;gap:14px}.cloud-confirm label{display:grid;gap:8px}@media(max-width:1000px){.cloud-management-columns,.cloud-management-row{grid-template-columns:minmax(90px,1fr) minmax(90px,1fr) 65px 80px;gap:7px}.cloud-management-columns>span:nth-child(4),.cloud-management-row>span:nth-child(4){display:none}.cloud-management-dock{left:50%;max-width:calc(100vw - 16px);gap:4px;padding:6px}.cloud-management-dock strong{font-size:11px}}@media(max-width:600px){.cloud-management-columns,.cloud-management-row{grid-template-columns:minmax(70px,1fr) 65px 68px}.cloud-management-columns>span:nth-child(2),.cloud-management-row>:nth-child(2){display:none}}
.cloud-management-row{user-select:none}
.cloud-management-row progress{border:0;border-radius:3px;overflow:hidden;background:var(--layer-item)}
.cloud-management-row progress::-webkit-progress-bar{background:var(--layer-item)}
.cloud-management-row progress::-webkit-progress-value{background:var(--amber)}
.cloud-management-row progress.success::-webkit-progress-value{background:var(--green)}
.cloud-management-row progress.error::-webkit-progress-value{background:var(--red)}
@media(max-width:760px){
 .task-heading{flex-wrap:wrap;gap:10px}
 .task-heading :deep(.content-tabs){width:100%;flex:1 0 100%}
 .toolbar-right{width:100%;justify-content:flex-end;gap:8px;min-width:0}
 .toolbar-right :deep(.rounded-select){min-width:0;flex:1}
 .toolbar-right .btn{flex:none}
 .cloud-management-panel{height:calc(100dvh - 205px)}
}
</style>
