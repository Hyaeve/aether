<script setup>
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,notify,state} from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import SecretInput from './SecretInput.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
import PageJumpInput from './PageJumpInput.vue'
import '../plugin-tasks.css'

const emit=defineEmits(['close'])
const cfg=ref({targets:[],active:[],connected:false}),tab=ref('targets'),loaded=ref(false),busy=ref(false),error=ref('')
const extensions=ref(''),savedExtensions=ref(''),token=ref(''),picker=ref(false),removing=ref(null),rapid=ref(null),link=ref(''),requestId=ref('')
const history=ref(null),records=ref([]),page=ref(1),recordBusy=ref(false),qr=ref(null),qrStatus=ref('')
const pools=computed(()=>state.storages.filter(s=>s.type==='115'&&s.enabled))
const poolName=id=>state.storages.find(s=>s.id===id)?.name||'存储不可用'
const active=id=>cfg.value.active?.find(r=>r.target===id)
const running=computed(()=>!!cfg.value.active?.length)
const pages=computed(()=>Math.max(1,Math.ceil(records.value.length/20)))
const rows=computed(()=>records.value.slice((Math.max(1,Number(page.value)||1)-1)*20,(Math.max(1,Number(page.value)||1))*20))
const statusName=value=>({running:'进行中',completed:'已完成',failed:'失败',interrupted:'已中断',success:'成功',skipped:'已存在'}[value]||value)
let alive=true,timer,qrTimer,qrGeneration=0
async function load(initial=false){
 try{const data=await api('/ali115');if(!alive)return;cfg.value={...data,targets:data.targets||[],active:data.active||[]};loaded.value=true;if(initial){extensions.value=data.extensions;savedExtensions.value=data.extensions}}
 catch(e){if(alive&&initial){error.value=e.message;notify(e.message,true)}}
}
async function poll(){await load();if(history.value)await loadRecords();if(alive)timer=setTimeout(poll,2000)}
onMounted(async()=>{await load(true);if(alive)timer=setTimeout(poll,2000)})
onBeforeUnmount(()=>{alive=false;clearTimeout(timer);stopQR()})
async function saveExtensions(){
 if(busy.value||extensions.value===savedExtensions.value)return
 busy.value=true;error.value=''
 try{const data=await api('/ali115','PUT',{extensions:extensions.value});if(alive){extensions.value=data.extensions;savedExtensions.value=data.extensions;notify('秒传文件扩展名已保存')}}catch(e){if(alive){error.value=e.message;notify(e.message,true)}}finally{if(alive)busy.value=false}
}
async function connect(){
 if(busy.value||(!token.value.trim()&&(!cfg.value.hasToken||cfg.value.connected)))return
 busy.value=true;error.value=''
 try{await api('/ali115/account','POST',{refreshToken:token.value.trim()});if(alive){token.value='';await load();notify('阿里云盘账号已连接')}}catch(e){if(alive){await load();if(cfg.value.hasToken&&!cfg.value.connected)token.value='';error.value=e.message;notify(e.message,true)}}finally{if(alive)busy.value=false}
}
async function selectDirectory(value){
 if(busy.value)return;busy.value=true
 try{cfg.value=await api('/ali115','POST',{storageId:value.storageId,directory:value.source,path:value.sourcePath||'/'});if(alive){picker.value=false;notify('115秒传接收目录已添加')}}catch(e){if(alive)notify(e.message,true)}finally{if(alive)busy.value=false}
}
async function remove(){busy.value=true;try{cfg.value=await api(`/ali115/targets/${encodeURIComponent(removing.value.id)}`,'DELETE');if(alive){removing.value=null;notify('接收目录已移除')}}catch(e){if(alive)notify(e.message,true)}finally{if(alive)busy.value=false}}
function openRapid(target){rapid.value=target;link.value='';requestId.value=Array.from(crypto.getRandomValues(new Uint8Array(24)),v=>v.toString(16).padStart(2,'0')).join('');error.value=''}
async function run(){
 if(busy.value||!link.value.trim())return;busy.value=true;error.value=''
 const target=rapid.value
 try{await api('/ali115/run','POST',{target:target.id,link:link.value,requestId:requestId.value});if(alive){rapid.value=null;await load();await openHistory(target);notify('阿里云盘秒传已开始')}}catch(e){if(alive){error.value=e.message;notify(e.message,true)}}finally{if(alive)busy.value=false}
}
async function loadRecords(){
 if(recordBusy.value||!history.value)return;recordBusy.value=true;const key=history.value.id
 try{const data=await api(`/ali115/records?target=${encodeURIComponent(key)}`);if(alive&&history.value?.id===key){records.value=data;page.value=Math.min(Number(page.value)||1,pages.value)}}catch(e){if(alive)notify(e.message,true)}finally{if(alive)recordBusy.value=false}
}
async function openHistory(target){history.value=target;page.value=1;records.value=[];await loadRecords()}
function stopQR(){qrGeneration++;clearTimeout(qrTimer);qr.value=null}
async function scan(){
 if(busy.value)return;stopQR();const generation=qrGeneration;busy.value=true;error.value=''
 try{const value=await api('/ali115/qr','POST',{});if(alive&&generation===qrGeneration){qr.value=value;qrStatus.value='请使用阿里云盘App扫码';qrTimer=setTimeout(()=>queryQR(generation),2000)}}catch(e){if(alive)notify(e.message,true)}finally{if(alive)busy.value=false}
}
async function queryQR(generation){
 if(!alive||generation!==qrGeneration||!qr.value)return
 try{const value=await api(`/ali115/qr?id=${encodeURIComponent(qr.value.id)}`);if(!alive||generation!==qrGeneration)return
  if(value.status==='CONFIRMED'){stopQR();await load();notify('阿里云盘账号已连接');return}
  qrStatus.value=({NEW:'请使用阿里云盘App扫码',SCANED:'已扫码，请在手机确认',EXPIRED:'二维码已过期，请重新获取',CANCELED:'扫码已取消'}[value.status]||'等待扫码确认')
  if(['EXPIRED','CANCELED'].includes(value.status))return
  qrTimer=setTimeout(()=>queryQR(generation),2000)
 }catch(e){if(alive&&generation===qrGeneration){qrStatus.value=e.message;notify(e.message,true)}}
}
</script>
<template>
 <Modal standard title="阿里云盘秒传" @close="!busy&&emit('close')">
  <div class="ali-tabs" role="tablist" aria-label="阿里云盘秒传栏目"><button role="tab" :aria-selected="tab==='targets'" :class="{active:tab==='targets'}" @click="tab='targets'"><Icon name="FolderInput"/>接收目录</button><button role="tab" :aria-selected="tab==='account'" :class="{active:tab==='account'}" @click="tab='account'"><Icon name="UserRound"/>账号配置</button></div>
  <div v-if="tab==='targets'" class="modal-body plugin-task-settings ali-targets" role="tabpanel" aria-label="接收目录"><div class="plugin-task-list">
   <article v-for="target in cfg.targets" :key="target.id" :data-config-path="`/ali115/targets/${target.id}`" class="plugin-task-row ali-target"><ProviderIcon type="115"/><div class="plugin-task-copy"><strong>{{poolName(target.storageId)}}</strong><small :data-tooltip="target.path" data-tooltip-stationary data-tooltip-delay="300">{{target.path}}</small><small v-if="active(target.id)" role="status">{{active(target.id).done}} / {{active(target.id).total}} · {{active(target.id).message}}</small></div><button class="btn plugin-task-run" :aria-label="`秒传到 ${target.path}`" :disabled="busy||running||!cfg.connected" @click="openRapid(target)"><Icon :name="active(target.id)?'LoaderCircle':'Zap'" :class="{spin:active(target.id)}"/>秒传</button><button class="icon-btn" :aria-label="`秒传记录 ${target.path}`" @click="openHistory(target)"><Icon name="ListChecks"/></button><button class="icon-btn danger-text" :aria-label="`删除接收目录 ${target.path}`" :disabled="busy||running" @click="removing=target"><Icon name="Trash2"/></button></article>
   <p v-if="loaded&&!cfg.targets.length" class="ali-empty">添加115接收目录后，可从网页或TG Bot发送阿里云盘分享链接。</p>
   <p v-if="error" role="alert" class="danger-text">{{error}}</p>
  </div></div>
  <div v-else class="modal-body ali-account" role="tabpanel" aria-label="账号配置">
   <div class="ali-account-status"><img src="/providers/aliyun.png" alt=""/><div><strong>{{cfg.connected?(cfg.nick||'阿里云盘账号'):'尚未连接阿里云盘'}}</strong><small>{{cfg.connected?`账号 ${cfg.userId}`:'扫码登录或使用refresh token连接'}}</small></div><button class="btn" :disabled="busy||running" @click="scan"><Icon name="QrCode"/>扫码登录</button></div>
   <form class="ali-token" @submit.prevent="connect"><label>refresh token<SecretInput v-model="token" aria-label="refresh token" autocomplete="off" placeholder="粘贴refresh token" :disabled="busy||running"/></label><button class="btn" :disabled="busy||running||(!token.trim()&&(!cfg.hasToken||cfg.connected))"><Icon :name="busy?'LoaderCircle':'LogIn'" :class="{spin:busy}"/>{{!cfg.connected&&cfg.hasToken&&!token.trim()?'重试连接':'连接账号'}}</button></form>
   <label>秒传文件扩展名<textarea v-model="extensions" aria-label="秒传文件扩展名" rows="3" :disabled="busy||running" @keydown.enter.prevent="saveExtensions" @blur="saveExtensions"/></label>
   <small class="ali-hint">回车或离开输入框自动保存。分享文件临时转存到 /Aether，115文件核验通过后清理临时副本。</small>
   <p v-if="error" class="danger-text" role="alert">{{error}}</p>
  </div>
  <footer class="modal-footer plugin-task-footer"><button v-if="tab==='targets'" class="btn plugin-task-add" :disabled="busy||running||!loaded||!pools.length" @click="picker=true"><Icon name="FolderPlus"/>添加115秒传接收目录</button><span v-else class="ali-hint">默认仅秒传视频与字幕，保留分享目录结构。</span></footer>
 </Modal>
 <TaskSourcePicker v-if="picker" include-full-path :storages="pools" :storage="pools[0]?.id||''" initial="/" @select="selectDirectory" @close="!busy&&(picker=false)"/>
 <Modal v-if="rapid" standard title="阿里云盘分享秒传" @close="!busy&&(rapid=null)"><form @submit.prevent="run"><div class="modal-body ali-form"><p>{{poolName(rapid.storageId)}} · {{rapid.path}}</p><label>阿里云盘分享链接<textarea v-model="link" aria-label="阿里云盘分享链接" rows="3" placeholder="粘贴阿里云盘分享链接，支持提取码" :disabled="busy" required/></label><p v-if="error" role="alert" class="danger-text">{{error}}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="rapid=null">取消</button><button class="btn primary" :disabled="busy||!link.trim()"><Icon :name="busy?'LoaderCircle':'Zap'" :class="{spin:busy}"/>开始秒传</button></footer></form></Modal>
 <Modal v-if="history" standard title="阿里云盘秒传记录" @close="history=null"><div class="modal-body ali-history"><p class="ali-history-target">{{poolName(history.storageId)}} · {{history.path}}</p><div class="ali-records"><article v-for="record in rows" :key="record.id" class="ali-record"><header><strong :data-tooltip="record.name" data-tooltip-stationary data-tooltip-delay="300">{{record.name}}</strong><span class="status" :class="record.status==='completed'?'success':record.status==='running'?'pending':'error'">{{statusName(record.status)}}</span></header><small>{{record.source}} · {{new Date(record.created).toLocaleString()}}</small><small class="ali-record-link" :data-tooltip="record.link" data-tooltip-stationary data-tooltip-delay="300">{{record.link}}</small><p>{{record.message}}</p><small>秒传 {{record.success}} 项 · 已存在 {{record.skipped}} 项 · 失败 {{record.failed}} 项</small><progress v-if="record.status==='running'" :value="record.success+record.skipped+record.failed" :max="Math.max(1,record.total)"/><details v-if="record.files?.length"><summary>文件记录</summary><div v-for="(file,i) in record.files" :key="i" class="ali-file"><span :data-tooltip="file.name" data-tooltip-stationary data-tooltip-delay="300">{{file.name}}</span><small :data-tooltip="file.message" data-tooltip-stationary data-tooltip-delay="300">{{statusName(file.status)}}</small></div></details></article><p v-if="!records.length">{{recordBusy?'正在加载…':'暂无秒传记录'}}</p></div></div><footer class="modal-footer ali-pagination"><button class="icon-btn" aria-label="上一页" :disabled="Number(page)<=1" @click="page=Number(page)-1"><Icon name="ChevronLeft"/></button><PageJumpInput v-model="page" :pages="pages"/><span>/ {{pages}}</span><button class="icon-btn" aria-label="下一页" :disabled="Number(page)>=pages" @click="page=Number(page)+1"><Icon name="ChevronRight"/></button></footer></Modal>
 <Modal v-if="removing" compact confirmation title="删除接收目录" @close="!busy&&(removing=null)"><div class="modal-body">移除「{{poolName(removing.storageId)}} · {{removing.path}}」秒传接收目录？</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="removing=null">取消</button><button class="btn danger" :disabled="busy" @click="remove">确认删除</button></footer></Modal>
 <Modal v-if="qr" compact title="阿里云盘扫码登录" @close="stopQR"><div class="modal-body ali-qr"><img :src="qr.image" alt="阿里云盘登录二维码"/><p role="status">{{qrStatus}}</p></div><footer class="modal-footer"><button class="btn" :disabled="busy||running" @click="scan"><Icon name="RefreshCw"/>重新获取</button></footer></Modal>
</template>
<style scoped>
.ali-tabs{display:flex;gap:8px;padding:0 24px 12px;border-bottom:1px solid var(--border)}
.ali-tabs button{display:flex;align-items:center;gap:7px;border:0;border-radius:10px;padding:9px 14px;background:transparent;color:var(--muted)}
.ali-tabs button.active{background:var(--layer-item);color:var(--primary)}
.ali-account,.ali-form{display:grid;gap:18px;min-width:0;max-height:min(500px,calc(100dvh - 230px));overflow:auto;scrollbar-width:none}
.ali-account::-webkit-scrollbar,.ali-history::-webkit-scrollbar,.ali-records::-webkit-scrollbar{display:none}
.ali-account label,.ali-form label{display:grid;gap:8px;min-width:0}
.ali-account textarea,.ali-form textarea{width:100%;resize:vertical;min-height:80px;background:var(--input)}
.ali-account-status{display:flex;align-items:center;gap:10px;min-width:0}.ali-account-status>img{width:38px;height:38px;flex:none}.ali-account-status>div{display:grid;gap:5px;flex:1;min-width:0}.ali-account-status strong,.ali-account-status small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.ali-account-status small,.ali-hint,.ali-record>small{color:var(--muted);font-size:13px;line-height:1.7}
.ali-token{display:flex;gap:10px;align-items:end;min-width:0}.ali-token label{flex:1;min-width:0}.ali-token .btn{flex:none}.ali-hint{overflow-wrap:anywhere}.ali-empty{color:var(--muted);font-size:14px;line-height:1.8}
.ali-history{min-width:0}.ali-history-target{margin:0 0 14px;overflow-wrap:anywhere;color:var(--muted)}.ali-records{max-height:min(470px,calc(100dvh - 280px));overflow:auto;scrollbar-width:none;overscroll-behavior:contain}.ali-record{display:grid;gap:7px;padding:14px;border-radius:12px;background:var(--layer-item);margin-bottom:12px;min-width:0}.ali-record header{display:flex;align-items:center;gap:12px}.ali-record strong{flex:1;min-width:0;white-space:nowrap;text-overflow:ellipsis;overflow:hidden}.ali-record p{margin:0;font-size:14px;overflow-wrap:anywhere}.ali-record-link{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.ali-record progress{width:100%;accent-color:var(--primary)}.ali-file{display:flex;gap:10px;padding:6px 0;min-width:0}.ali-file span{flex:1;min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.ali-file small{flex:none}.ali-pagination{align-items:center;gap:8px}.ali-pagination input{width:66px}.ali-qr{text-align:center}.ali-qr img{width:min(256px,100%);aspect-ratio:1;background:white;border-radius:12px;padding:10px}
@media(max-width:480px){.ali-tabs{padding-inline:18px}.ali-tabs button{padding-inline:10px}.ali-account-status{flex-wrap:wrap}.ali-account-status>.btn{margin-left:48px}.ali-token{flex-wrap:wrap}.ali-token label{flex-basis:100%}.ali-token>.btn{margin-left:auto}.ali-account{max-height:calc(100dvh - 240px)}}
</style>
