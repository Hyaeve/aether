<script setup>
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {api,notify,state} from '../lib'
import {compactDate} from '../date-time.mjs'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import Modal from './Modal.vue'
import CronInput from './CronInput.vue'

const props=defineProps({target:Object})
const emit=defineEmits(['close'])
const records=ref([]),links=ref([]),page=ref(1),loading=ref(false),busy=ref(false),editing=ref(null),deleting=ref(null),error=ref('')
const pages=computed(()=>Math.max(1,Math.ceil(records.value.length/30)))
const visible=computed(()=>records.value.slice((page.value-1)*30,page.value*30))
const sources={browser:'浏览器转存',bot:'Bot 交互',cron:'活链计划',manual:'活链手动'}
const labels={running:'进行中',completed:'成功',submitted:'已提交',failed:'失败',interrupted:'已中断'}
const statusIcon=status=>({running:'LoaderCircle',completed:'CircleCheck',submitted:'CheckCheck',failed:'CircleAlert',interrupted:'CircleAlert'}[status]||'Clock3')
let alive=true,pending=false,timer,request=0
async function load(){
  if(pending||document.visibilityState==='hidden')return
  pending=true;loading.value=true
  const ticket=++request,target=props.target?.id,owner=state.username
  try{
    const [history,active]=await Promise.all([api('/share-transfers'),target?api(`/share-live?target=${encodeURIComponent(target)}`):Promise.resolve([])])
    if(!Array.isArray(history)||!Array.isArray(active))throw new Error('转存记录响应格式异常')
    if(alive&&ticket===request&&owner===state.username){records.value=history;links.value=active;page.value=Math.min(page.value,pages.value);error.value=''}
  }catch(e){if(alive&&ticket===request)error.value=e.message}
  finally{if(ticket===request){pending=false;if(alive)loading.value=false}}
}
watch(()=>props.target?.id,()=>{request++;links.value=[];pending=false;load()})
watch(()=>state.username,()=>{request++;pending=false;records.value=[];links.value=[];editing.value=null;deleting.value=null})
onMounted(()=>{load();timer=setInterval(load,5000)})
onUnmounted(()=>{alive=false;request++;clearInterval(timer)})
async function action(link,kind,body={}){
  if(busy.value)return
  busy.value=true
  try{await api(`/share-live/${link.id}/${kind}`,'POST',body);notify(kind==='run'?'活链更新已在后台启动':kind==='remove'?'已移除活链记录':'已提交删除');deleting.value=null;await load()}
  catch(e){notify(e.message,true)}finally{busy.value=false}
}
async function save(){
  if(busy.value)return
  busy.value=true
  try{const value=editing.value;await api(`/share-live/${value.id}`,'PUT',{url:value.url,name:value.name,cron:value.cron,directoryName:value.directoryName});editing.value=null;notify('活链设置已保存');await load()}
  catch(e){notify(e.message,true)}finally{busy.value=false}
}
</script>
<template>
  <section class="share-transfer-records" aria-label="转存记录">
    <header><h2><Icon name="ListChecks"/>转存记录</h2><span>{{records.length}} 项</span><button class="icon-btn" aria-label="刷新转存记录" :disabled="loading" @click="load"><Icon name="RefreshCw" :class="{spin:loading}"/></button></header>
    <p v-if="error" role="alert" class="error-message">{{error}}</p>
    <div class="share-record-table">
      <table><colgroup><col style="width:21%"/><col style="width:25%"/><col style="width:12%"/><col style="width:16%"/><col style="width:26%"/></colgroup>
        <thead><tr><th>分享名称</th><th>分享链接</th><th>转存来源</th><th>转存时间</th><th>转存结果</th></tr></thead>
        <tbody><tr v-for="record in visible" :key="record.id"><td><span class="share-record-name"><ProviderIcon :type="record.provider" small unframed/><strong :data-tooltip="record.name" data-tooltip-stationary>{{record.name||'分享资源'}}</strong></span><small v-if="record.directoryName" :data-tooltip="record.directoryName">{{record.directoryName}}</small></td><td><span :data-tooltip="record.url" data-tooltip-stationary>{{record.url}}</span></td><td><span class="share-record-source" :class="record.source">{{sources[record.source]||record.source}}</span></td><td><time>{{compactDate(record.time)}}</time></td><td><span class="share-record-status" :class="record.status"><Icon :name="statusIcon(record.status)" :size="15"/>{{labels[record.status]||record.status}}</span><small :data-tooltip="record.message" data-tooltip-stationary>{{record.message}}</small></td></tr></tbody>
      </table><div v-if="!records.length" class="share-record-empty">{{loading?'正在读取转存记录…':'暂无转存记录'}}</div>
    </div>
    <footer class="share-record-pager"><button class="icon-btn" aria-label="转存记录上一页" :disabled="page<=1" @click="page--"><Icon name="ChevronLeft"/></button><span>{{page}} / {{pages}}</span><button class="icon-btn" aria-label="转存记录下一页" :disabled="page>=pages" @click="page++"><Icon name="ChevronRight"/></button></footer>
  </section>
  <Modal v-if="target" title="活动链接" wide @close="!busy&&emit('close')">
    <div class="modal-body share-live-list">
      <p v-if="error" class="error-message" role="alert">{{error}}</p>
      <div class="share-live-columns"><span>分享名称 / 链接 / 保存目录</span><span>下次执行 / 执行结果</span><span>操作</span></div>
      <article v-for="link in links" :key="link.id" class="share-live-row">
        <div class="share-live-identity"><strong :data-tooltip="link.name" data-tooltip-stationary>{{link.name}}</strong><span :data-tooltip="link.url" data-tooltip-stationary>{{link.url}}</span><span :data-tooltip="link.directoryName"><Icon name="Folder" :size="14"/>{{link.directoryName}}</span></div>
        <div class="share-live-result"><time :aria-label="'下次执行 '+compactDate(link.nextRun)">{{compactDate(link.nextRun,'未启用计划')}}</time><span class="share-record-status" :class="link.status"><Icon :name="statusIcon(link.status)" :size="14"/>{{labels[link.status]||link.status}}</span><small :data-tooltip="link.message">{{link.message}}</small></div>
        <div class="share-live-actions"><button class="icon-btn" :aria-label="'执行活链 '+link.name" :disabled="busy||link.status==='running'" @click="action(link,'run')"><Icon name="Play" :size="17"/></button><button class="icon-btn" :aria-label="'编辑活链 '+link.name" :disabled="busy||link.status==='running'" @click="editing={...link}"><Icon name="Pencil" :size="17"/></button><button class="icon-btn danger-text" :aria-label="'删除活链 '+link.name" :disabled="busy||link.status==='running'" @click="deleting=link"><Icon name="Trash2" :size="17"/></button></div>
      </article>
      <div v-if="!links.length" class="share-record-empty">{{loading?'正在读取活动链接…':'暂无活动链接'}}</div>
    </div>
    <footer class="modal-footer"><button class="btn" :disabled="busy" @click="emit('close')">关闭</button></footer>
  </Modal>
  <Modal v-if="editing" standard title="编辑分享链接" @close="!busy&&(editing=null)">
    <form @submit.prevent="save"><div class="modal-body share-live-form"><label>分享链接<input v-model.trim="editing.url" aria-label="活链分享链接" maxlength="4096" required :disabled="busy"/></label><label>分享名称<input v-model.trim="editing.name" aria-label="活链分享名称" maxlength="300" :disabled="busy"/></label><label>Cron表达式<CronInput v-model="editing.cron" share-live aria-label="活链Cron表达式" :disabled="busy"/></label><label>目录名称<input v-model.trim="editing.directoryName" aria-label="活链目录名称" maxlength="255" required :disabled="busy"/></label></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="editing=null">取消</button><button class="btn primary" :disabled="busy"><Icon name="Check"/>保存</button></footer></form>
  </Modal>
  <Modal v-if="deleting" confirmation title="删除活动链接" @close="!busy&&(deleting=null)">
    <div class="modal-body">移除记录仅停止此活链的更新；确认删除会同时删除云盘中的保存目录及其全部内容：<strong>{{deleting.directoryName}}</strong></div>
    <footer class="modal-footer share-live-delete"><button class="btn share-remove-only" :disabled="busy" @click="action(deleting,'remove')"><Icon name="Link2Off"/>移除记录</button><button class="btn danger" :disabled="busy" @click="action(deleting,'delete',{confirm:true})"><Icon name="Trash2"/>确认删除</button></footer>
  </Modal>
</template>
<style scoped>
.share-transfer-records{margin-top:24px;min-width:0}.share-transfer-records>header{display:flex;align-items:center;gap:12px;margin-bottom:12px}.share-transfer-records h2{display:flex;align-items:center;gap:9px;font-size:17px;margin:0}.share-transfer-records>header>span{margin-left:auto;font-size:13px;color:var(--muted)}
.share-record-table{border:1px solid var(--border);border-radius:8px;background:var(--surface);min-height:280px}.share-record-table table{width:100%;table-layout:fixed;border-collapse:collapse}.share-record-table th{text-align:left;color:var(--muted);font-size:13px;font-weight:500;padding:12px}.share-record-table td{padding:10px 12px;border-top:1px solid var(--border);font-size:14px;vertical-align:middle}.share-record-table td>span:not(.share-record-name),.share-record-table small{display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.share-record-table small{margin-top:5px;font-size:12px;color:var(--muted)}.share-record-name{display:flex;align-items:center;gap:8px;min-width:0}.share-record-name strong{font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.share-record-name :deep(.provider-icon){flex:none;width:25px;height:25px}.share-record-table time{font-size:13px;white-space:nowrap}.share-record-status{display:inline-flex!important;align-items:center;gap:5px;color:var(--muted)}.share-record-status.completed,.share-record-status.submitted{color:var(--green)}.share-record-status.failed,.share-record-status.interrupted{color:var(--red)}.share-record-status.running{color:var(--info)}.share-record-source.bot{color:var(--info)}.share-record-source.cron{color:var(--primary)}.share-record-source.browser,.share-record-source.manual{color:var(--amber)}.share-record-empty{display:grid;place-items:center;min-height:180px;color:var(--muted);font-size:14px}.share-record-pager{display:flex;align-items:center;justify-content:flex-end;gap:12px;margin-top:10px;font-size:13px}
.share-live-list{height:min(480px,60dvh);overflow:auto;scrollbar-width:none}.share-live-list::-webkit-scrollbar{display:none}.share-live-row{display:grid;grid-template-columns:minmax(0,1fr) minmax(140px,.7fr) 96px;gap:12px;align-items:center;padding:13px 0;border-bottom:1px solid var(--border)}.share-live-identity,.share-live-result{display:grid;gap:6px;min-width:0}.share-live-identity strong{font-size:15px;font-weight:600}.share-live-identity span,.share-live-result small{font-size:12px;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.share-live-identity span:last-child{display:flex;align-items:center;gap:5px}.share-live-identity svg{flex:none}.share-live-identity strong{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}.share-live-result time{font-size:13px;white-space:nowrap}.share-live-result .share-record-status{font-size:12px}.share-live-actions{display:flex;align-items:center}.share-live-actions .icon-btn{width:32px;height:34px}.share-live-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px 16px}.share-live-form label{min-width:0}.share-live-form input{width:100%}.share-live-delete{justify-content:space-between}.share-remove-only{background:color-mix(in srgb,var(--amber) 16%,var(--surface));border-color:color-mix(in srgb,var(--amber) 40%,var(--border));color:color-mix(in srgb,var(--amber) 65%,var(--text))}
:global(.modal:has(.share-live-list)),:global(.modal:has(.share-live-form)){scrollbar-width:none}
:global(.modal:has(.share-live-list)::-webkit-scrollbar),:global(.modal:has(.share-live-form)::-webkit-scrollbar){display:none}
.share-live-delete .btn.danger{color:var(--red);background:color-mix(in srgb,var(--red) 12%,var(--surface));border-color:color-mix(in srgb,var(--red) 35%,var(--border))}.share-live-delete .btn.danger:hover{background:color-mix(in srgb,var(--red) 20%,var(--surface))}.share-remove-only:hover{background:color-mix(in srgb,var(--amber) 23%,var(--surface));border-color:var(--amber)}
.share-live-columns{display:grid;grid-template-columns:minmax(0,1fr) minmax(140px,.7fr) 96px;gap:12px;color:var(--muted);font-size:12px;padding-bottom:12px;border-bottom:1px solid var(--border)}
@media(max-width:680px){.share-live-columns{display:none}}
@media(max-width:680px){.share-record-table table,.share-record-table tbody,.share-record-table tr,.share-record-table td{display:block}.share-record-table colgroup,.share-record-table thead{display:none}.share-record-table tr{display:grid;grid-template-columns:1fr 1fr;padding:10px;border-top:1px solid var(--border);gap:9px}.share-record-table td{border:0;padding:0;min-width:0}.share-record-table td:first-child,.share-record-table td:nth-child(2),.share-record-table td:last-child{grid-column:1/-1}.share-live-row{grid-template-columns:minmax(0,1fr) 96px}.share-live-identity{grid-column:1/-1}.share-live-actions{grid-column:2}.share-live-form{gap:16px 10px}.share-live-form input{font-size:14px}}
</style>
