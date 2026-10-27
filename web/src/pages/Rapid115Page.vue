<script setup>
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,bytes,date,notify,state} from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import Rapid115Icon from '../components/Rapid115Icon.vue'
import Rapid115Fields from '../components/Rapid115Fields.vue'
import BackupFilterEditor from '../components/BackupFilterEditor.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import LocalDirectoryPicker from '../components/LocalDirectoryPicker.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import ScrollRail from '../components/ScrollRail.vue'
import {usePageSlide} from '../page-slide'
import {compactDate} from '../date-time.mjs'
const {leaving,back}=usePageSlide('/tools')
const empty=()=>({source:'',storageId:'',target:'',targetLabel:'',extensions:'',successAction:'delete',failureAction:'skip',filters:[]})
const filterEditor=ref(null)
const settings=ref({manual:empty(),watches:[]}),draft=ref(null),picker=ref(''),editing=ref(''),loaded=ref(false),busy=ref(false),running=ref(false),confirm=ref(false),records=ref([]),error=ref(''),list=ref(null)
const recordList=ref(null),recordPage=ref(1),recordPages=ref(1),recordTotal=ref(0),jumpPage=ref(1),recordsLoading=ref(false)
const storages=computed(()=>state.storages.filter(s=>s.enabled&&s.type==='115'))
const targetForm=computed(()=>draft.value||settings.value.manual)
const targetPathLabel=computed(()=>{const target=targetForm.value,name=storages.value.find(s=>s.id===target.storageId)?.name||'';const value=target.targetLabel||'';return value.startsWith(name+' ')?value.slice(name.length).trim():value||'根目录'})
const complete=computed(()=>loaded.value&&settings.value.manual.source&&settings.value.manual.storageId&&settings.value.manual.target)
const statuses={running:'处理中',success:'秒传成功',failed:'秒传失败',uploaded:'传输成功',interrupted:'已中断',deleted:'重试3次失败，已删除'}
let alive=true,timer,saving=false,pending,waiters=[],recordRequest=0,recordError=''
async function save(){
  if(!loaded.value)return false
  pending=JSON.parse(JSON.stringify(settings.value))
  const promise=new Promise(resolve=>waiters.push(resolve))
  if(!saving)drain()
  return promise
}
async function drain(){
  saving=true;let ok=true
  while(pending){const value=pending;pending=null;try{await api('/115-rapid','PUT',value);error.value=''}catch(e){ok=false;if(alive){error.value='';notify(e.message,true)}}}
  saving=false;const resolve=waiters;waiters=[];resolve.forEach(done=>done(ok))
}
async function loadRecords(page=recordPage.value){
 const request=++recordRequest;recordsLoading.value=true
 try{
  const result=await api(`/115-rapid/records?page=${page}`)
  if(!alive||request!==recordRequest)return
  const changed=recordPage.value!==(result.page||page)
  records.value=result.items||[];running.value=!!(result.manualRunning??result.running)
  recordTotal.value=result.total??records.value.length;recordPages.value=result.pages||Math.max(1,Math.ceil(recordTotal.value/30))
  recordPage.value=result.page||page;if(changed||document.activeElement?.getAttribute('aria-label')!=='跳转页码')jumpPage.value=recordPage.value
  if(changed&&recordList.value)recordList.value.scrollTop=0
  recordError=''
 }catch(e){if(alive&&request===recordRequest&&recordError!==e.message){recordError=e.message;notify(e.message,true)}}
 finally{if(alive&&request===recordRequest)recordsLoading.value=false}
}
function goPage(page){loadRecords(Math.max(1,Math.min(recordPages.value,Math.trunc(Number(page))||1)))}
async function load(){try{const value=await api('/115-rapid');if(alive){settings.value={manual:{...empty(),...value.manual},watches:value.watches||[]};loaded.value=true;await loadRecords()}}catch(e){if(alive)error.value=e.message}}
function open(watch){editing.value=watch?.id||'';draft.value=watch?{...empty(),...JSON.parse(JSON.stringify(watch))}:{...empty(),storageId:storages.value[0]?.id||'',name:'',mode:'watch',cron:'',enabled:true}}
function selectSource(value){if(draft.value)draft.value.source=value;else{settings.value.manual.source=value;save()}picker.value=''}
function selectTarget(value){const target=targetForm.value;target.storageId=value.storageId;target.target=value.source;target.targetLabel=`${storages.value.find(s=>s.id===value.storageId)?.name||''} ${value.sourcePath||('/'+value.sourceLabel)}`;picker.value='';if(!draft.value)save()}
function inputTarget(value){const target=targetForm.value;target.storageId ||= storages.value[0]?.id||'';target.target='';target.targetLabel=value}
let targetResolution
async function commitTarget(){
 const target=targetForm.value,path=target.targetLabel,storageId=target.storageId
 if(target.target)return
 if(!path){if(!draft.value)await save();return}
 if(targetResolution?.target===target&&targetResolution.path===path&&targetResolution.storageId===storageId)return targetResolution.promise
 const run=async()=>{
  try{
   if(!storageId||!path.startsWith('/')||path.length>2048||path.includes('\\')||path.includes('\0'))throw new Error('目标目录须为115存储内的绝对路径')
   const segments=path.split('/').filter(Boolean);if(segments.length>64||segments.some(s=>s==='.'||s==='..'))throw new Error('目标目录路径无效')
   let directory='/'
   for(const segment of segments){const files=await api(`/files?storage=${encodeURIComponent(storageId)}&path=${encodeURIComponent(directory)}`);const matches=files.filter(f=>f.isDir&&f.name===segment);if(matches.length!==1)throw new Error('目标目录不存在或名称不唯一');directory=matches[0].id}
   if(!alive||targetForm.value!==target||target.targetLabel!==path||target.storageId!==storageId)return
   target.target=directory;target.targetLabel=`${storages.value.find(s=>s.id===storageId)?.name||''} /${segments.join('/')}`;error.value='';if(!draft.value)await save()
  }catch(e){if(alive&&targetForm.value===target&&target.targetLabel===path){error.value='';notify(e.message,true)}}
 }
 const resolution={target,path,storageId,promise:null};targetResolution=resolution;resolution.promise=run();await resolution.promise;if(targetResolution===resolution)targetResolution=null
}
const deleteConfirmation=ref(false)
async function add(confirmed=false){
 if(busy.value||!draft.value)return
 await commitTarget()
 if(busy.value||!draft.value)return
 if(!draft.value?.source||!draft.value.target||!draft.value.storageId){notify('请选择待传目录和115目标目录',true);return}
 if(filterEditor.value&&!filterEditor.value.flush())return
 if(draft.value.failureAction==='delete'&&confirmed!==true){deleteConfirmation.value=true;return}
 busy.value=true
 const previous=JSON.parse(JSON.stringify(settings.value))
 try{
  const key=globalThis.crypto?.randomUUID?.()||`watch-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  const value={...draft.value,id:editing.value||key}
  settings.value.watches=editing.value?settings.value.watches.map(w=>w.id===editing.value?value:w):[...settings.value.watches,value]
  if(await save()){draft.value=null;editing.value='';deleteConfirmation.value=false;notify('监控已保存')}else{settings.value=previous;deleteConfirmation.value=false}
 }catch(e){settings.value=previous;notify(e.message,true)}finally{busy.value=false}
}
async function toggle(w){w.enabled=!w.enabled;if(!await save())w.enabled=!w.enabled}
const removing=ref(null)
async function remove(){const previous=settings.value.watches;settings.value.watches=previous.filter(w=>w.id!==removing.value.id);if(await save())removing.value=null;else settings.value.watches=previous}
async function start(){if(!await save())return;busy.value=true;try{await api('/115-rapid/run','POST',{confirm:true});confirm.value=false;running.value=true;notify('115秒传已开始');await loadRecords()}catch(e){error.value=e.message}finally{busy.value=false}}
async function returnPage(){if(await save())back()}
function compactTime(value){return compactDate(value)}
function recordStatus(record){return record.status==='running'?(record.phase||'处理中'):record.status==='success'?(record.retries?`重试${record.retries}次秒传成功`:'秒传成功'):record.status==='uploaded'?'传输成功':record.status==='retrying'?`等待第${(record.retries||0)+1}次重试`:statuses[record.status]||record.status}
function recordProgress(record){return ['success','uploaded'].includes(record.status)?1:record.total>0?Math.max(0,Math.min(1,(record.done||0)/record.total)):0}
onMounted(()=>{load();timer=setInterval(loadRecords,2000)})
onBeforeUnmount(()=>{alive=false;recordRequest++;clearInterval(timer)})
</script>
<template>
  <div class="rapid115-page page-slide" :class="{leaving}">
    <header class="task-heading"><div class="rapid115-title"><Rapid115Icon /><h2 class="module-title">115秒传中心</h2></div><button class="btn" @click="returnPage"><Icon name="ArrowLeft" />返回页面</button></header>
    <p v-if="error" class="error-message" role="alert">{{error}}</p>
    <div class="rapid115-top">
      <section class="rapid115-section"><header><h3><Icon name="Zap" />手动秒传</h3><button class="btn primary" :disabled="!complete||running" @click="confirm=true"><Icon :name="running?'LoaderCircle':'Zap'" />{{running?'秒传中':'开始秒传'}}</button></header><fieldset :disabled="!loaded"><Rapid115Fields v-model="settings.manual" manual @source="picker='source'" @target="picker='target'" @target-input="inputTarget" @target-commit="commitTarget" @commit="save" /></fieldset></section>
      <section class="rapid115-section monitor-section"><header><h3><Icon name="FolderSearch" />监控秒传</h3><button class="btn" :disabled="!loaded||busy" @click="open()"><Icon name="Plus" />添加监控</button></header><div class="rapid115-watch-frame"><div ref="list" class="rapid115-watch-list" tabindex="0" aria-label="监控目录列表"><article v-for="w in settings.watches" :key="w.id" class="rapid115-watch" :class="{disabled:!w.enabled}"><button class="icon-btn" :aria-label="`${w.enabled?'停用':'启用'} ${w.name}`" :disabled="busy" @click="toggle(w)"><Icon :name="w.mode==='cron'?'Clock':'FolderSearch'" /></button><div><strong>{{w.name}}</strong><small>{{w.source}} → {{w.targetLabel}}</small><small>{{w.mode==='cron'?w.cron:'文件系统监控'}}</small></div><button class="icon-btn" :aria-label="`编辑 ${w.name}`" :disabled="busy" @click="open(w)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :aria-label="`删除 ${w.name}`" :disabled="busy" @click="removing=w"><Icon name="Trash2" /></button></article></div><ScrollRail :element="list" :thickness="3" /></div></section>
    </div>
    <section class="rapid115-section rapid115-records"><header><h3><Icon name="History" />秒传记录</h3><div class="rapid115-record-actions"><span>{{recordTotal}} 项</span><button class="icon-btn" aria-label="刷新秒传记录" :disabled="recordsLoading" @click="loadRecords()"><Icon name="RefreshCw" :class="{spin:recordsLoading}" /></button></div></header>
      <div class="rapid115-record-frame"><div ref="recordList" class="rapid115-record-scroll" tabindex="0" aria-label="秒传记录列表"><div class="rapid115-record-table" role="table" aria-label="秒传记录">
        <div class="rapid115-record-columns" role="row"><span role="columnheader">文件名称</span><span role="columnheader">文件大小</span><span role="columnheader">PickCode</span><span role="columnheader">触发来源</span><span role="columnheader">执行时间</span><span role="columnheader">秒传状态</span></div>
        <article v-for="record in records" :key="record.id" class="rapid115-record" role="row"><span class="rapid115-record-name" role="cell" :data-tooltip="record.name" data-tooltip-stationary data-tooltip-delay="300" tabindex="0">{{record.name}}</span><span role="cell">{{bytes(record.size)}}</span><span class="rapid115-pickcode" role="cell">{{record.pickcode||'--'}}</span><span role="cell" :data-tooltip="record.source" data-tooltip-stationary data-tooltip-delay="300">{{record.source}}</span><time role="cell" :datetime="record.time" :data-tooltip="date(record.time)" data-tooltip-always data-tooltip-stationary>{{compactTime(record.time)}}</time><span class="rapid115-status" role="cell" :class="record.status"><span :data-tooltip="record.message" data-tooltip-stationary data-tooltip-delay="300">{{recordStatus(record)}}</span><progress :aria-label="`${record.name}：${recordStatus(record)}`" :aria-valuetext="record.total>0?`${recordStatus(record)}，${Math.floor(recordProgress(record)*100)}%`:recordStatus(record)" :value="recordProgress(record)" max="1" /></span></article>
      </div><p v-if="!records.length" class="small-empty">暂无秒传记录</p></div><ScrollRail :element="recordList" :thickness="3" /></div>
      <form class="rapid115-pagination" aria-label="秒传记录分页" @submit.prevent="goPage(jumpPage)"><button type="button" class="icon-btn" aria-label="第一页" :disabled="recordsLoading||recordPage===1" @click="goPage(1)"><Icon name="ChevronsLeft" /></button><button type="button" class="icon-btn" aria-label="上一页" :disabled="recordsLoading||recordPage===1" @click="goPage(recordPage-1)"><Icon name="ChevronLeft" /></button><span>{{recordPage}} / {{recordPages}}</span><button type="button" class="icon-btn" aria-label="下一页" :disabled="recordsLoading||recordPage===recordPages" @click="goPage(recordPage+1)"><Icon name="ChevronRight" /></button><button type="button" class="icon-btn" aria-label="最后一页" :disabled="recordsLoading||recordPage===recordPages" @click="goPage(recordPages)"><Icon name="ChevronsRight" /></button><input v-model="jumpPage" type="number" min="1" step="1" aria-label="跳转页码" /><button class="icon-btn" aria-label="跳转到指定页" :disabled="recordsLoading"><Icon name="CornerDownLeft" /></button></form>
    </section>
  </div>
  <Modal v-if="draft" standard :title="editing?'编辑监控':'添加监控'" @close="!busy&&(draft=null)"><form @submit.prevent="add"><div class="modal-body rapid115-editor"><div class="rapid115-name"><label>监控名称<input v-model="draft.name" required maxlength="128" /></label><label>监控模式<RoundedSelect v-model="draft.mode" label="监控模式" :options="[{value:'watch',label:'文件系统监控'},{value:'cron',label:'Cron表达式'}]" /></label></div><label v-if="draft.mode==='cron'">Cron表达式<input v-model="draft.cron" required maxlength="128" /></label><Rapid115Fields v-model="draft" @source="picker='source'" @target="picker='target'" @target-input="inputTarget" @target-commit="commitTarget" /><section class="rapid115-filters"><h3><Icon name="ListFilter" />筛选规则</h3><BackupFilterEditor ref="filterEditor" :form="draft" /></section><p v-if="error" class="error-message" role="alert">{{error}}</p></div><footer class="modal-footer"><button class="btn" type="button" :disabled="busy" @click="draft=null">取消</button><button class="btn primary" :disabled="busy||!draft.source||!draft.target||!draft.storageId"><Icon name="Check" />{{editing?'保存':'添加'}}</button></footer></form></Modal>
  <LocalDirectoryPicker v-if="picker==='source'" :initial="targetForm.source" @select="selectSource" @close="picker=''" />
  <TaskSourcePicker include-full-path v-if="picker==='target'" :storages="storages" :storage="targetForm.storageId||storages[0]?.id" :initial="targetForm.target||'/'" :initial-label="targetPathLabel" @select="selectTarget" @close="picker=''" />
  <Modal v-if="confirm" compact confirmation title="确认开始115秒传" @close="!busy&&(confirm=false)"><div class="modal-body"><p>将处理待传目录中符合扩展名的文件。{{settings.manual.successAction==='delete'?'确认目标文件后清理对应源文件；失败文件保留。':'源文件保留。'}}{{settings.manual.failureAction==='upload'?'未命中秒传时进行本地上传。':'未命中秒传时跳过。'}}</p></div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="confirm=false">取消</button><button class="btn primary" :disabled="busy" @click="start"><Icon name="Check" />确认秒传</button></footer></Modal>
  <Modal v-if="removing" compact confirmation title="删除监控" @close="!busy&&(removing=null)"><div class="modal-body">删除「{{removing.name}}」监控？已生成的文件和秒传记录将保留。</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="removing=null">取消</button><button class="btn danger" :disabled="busy" @click="remove"><Icon name="Trash2" />删除</button></footer></Modal>
  <Modal v-if="deleteConfirmation&&draft" compact confirmation title="确认重试后删除" @close="!busy&&(deleteConfirmation=false)"><div class="modal-body"><p>「{{draft.name}}」监控在秒传未命中后，分别等待10、30、60分钟重试；第三次重试仍明确未命中的源文件将被永久删除，无法撤销。文件变化、取消或云端结果不明时保留源文件。</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="deleteConfirmation=false">返回</button><button type="button" class="btn danger" :disabled="busy" @click="add(true)"><Icon name="Check" />确认保存</button></footer></Modal>
</template>
<style scoped>
.rapid115-page{min-width:0;display:flex;flex-direction:column;flex:1;background:var(--layer-shell)}
.rapid115-title{display:flex;align-items:center;gap:12px}
.rapid115-top{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;margin-bottom:16px}
.rapid115-section{min-width:0;border:1px solid var(--border);border-radius:8px;background:var(--layer-frame);padding:18px}
.rapid115-section>header{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:18px;flex:none}
.rapid115-section h3{font-size:16px;margin:0;display:flex;align-items:center;gap:8px}
.rapid115-section h3 svg,.rapid115-filters h3 svg{width:18px;height:18px;color:var(--muted)}
.rapid115-section fieldset{margin:0;padding:0;border:0;min-width:0}
.rapid115-watch-frame,.rapid115-record-frame{position:relative;min-height:0}
.rapid115-watch-list{height:254px;overflow:auto;scrollbar-width:none}
.rapid115-watch-list::-webkit-scrollbar,.rapid115-record-scroll::-webkit-scrollbar{display:none}
.rapid115-watch{display:flex;align-items:center;gap:6px;border:1px solid var(--border);border-radius:8px;background:var(--input);padding:10px 6px;margin-bottom:8px;min-width:0}
.rapid115-watch:hover{border-color:var(--primary)}.rapid115-watch.disabled{opacity:.5}
.rapid115-watch>div{display:grid;gap:4px;flex:1;min-width:0}
.rapid115-watch strong{font-size:14px}.rapid115-watch small{font-size:12px;color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.rapid115-watch .icon-btn{flex:none;width:30px;height:30px;min-height:30px}
.rapid115-records{flex:none;height:560px;display:flex;flex-direction:column}
.rapid115-records>header{margin-bottom:14px}
.rapid115-record-actions{display:flex;align-items:center;gap:10px;color:var(--muted);font-size:13px}
.rapid115-record-frame{flex:1}
.rapid115-record-scroll{height:100%;overflow:auto;scrollbar-width:none;overscroll-behavior:contain;border:1px solid var(--border);border-radius:6px;background:var(--input)}
.rapid115-record-table{min-width:920px}
.rapid115-record,.rapid115-record-columns{display:grid;grid-template-columns:minmax(150px,1fr) 84px 19ch 110px 126px 154px;gap:12px;align-items:center;font-size:14px;padding:10px 12px}
.rapid115-record-columns{position:sticky;top:0;z-index:1;min-height:42px;color:var(--muted);background:var(--layer-item);border-bottom:1px solid var(--border)}
.rapid115-record{height:56px;border-bottom:1px solid var(--border);background:var(--input)}
.rapid115-record>[role=cell]{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.rapid115-pickcode{font-family:"Cascadia Code",Consolas,monospace;font-size:13px}
.rapid115-record time{font-variant-numeric:tabular-nums}
.rapid115-status{display:grid;gap:7px;--status-color:var(--amber);color:var(--status-color)}
.rapid115-status>span{overflow:hidden;text-overflow:ellipsis}
.rapid115-status.success,.rapid115-status.uploaded{--status-color:var(--green)}
.rapid115-status.failed,.rapid115-status.interrupted,.rapid115-status.deleted{--status-color:var(--red)}
.rapid115-status progress{display:block;appearance:none;width:100%;height:7px;border:1px solid var(--status-color);border-radius:4px;background:var(--input);overflow:hidden;accent-color:var(--status-color)}
.rapid115-status progress::-webkit-progress-bar{background:var(--input)}
.rapid115-status progress::-webkit-progress-value{background:var(--status-color)}
.rapid115-status progress::-moz-progress-bar{background:var(--status-color)}
.rapid115-pagination{display:flex;align-items:center;justify-content:flex-end;gap:6px;padding-top:12px;flex:none;font-variant-numeric:tabular-nums}
.rapid115-pagination>span{min-width:54px;text-align:center;font-size:13px}
.rapid115-pagination input{width:64px;height:32px;min-height:32px;padding:5px 8px;text-align:center}
.rapid115-editor{display:grid;gap:16px;overflow-y:auto;scrollbar-width:none;background:var(--layer-frame)}
.rapid115-editor::-webkit-scrollbar{display:none}.rapid115-editor label{display:grid;gap:8px;min-width:0}
.rapid115-name{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
.rapid115-filters{display:grid;gap:12px}.rapid115-filters h3{display:flex;align-items:center;gap:8px}
.rapid115-page.leaving{animation-timing-function:cubic-bezier(.2,.55,.35,1)}
@media(max-width:1000px){.rapid115-top{grid-template-columns:minmax(0,1fr)}.rapid115-section{padding:14px}}
@media(max-width:440px){.rapid115-pagination{gap:3px;justify-content:center}.rapid115-pagination .icon-btn{width:30px;height:36px}.rapid115-pagination input{width:52px}}
</style>
