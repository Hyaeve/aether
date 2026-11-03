<script setup>
import {computed,nextTick,onMounted,onUnmounted,ref,watch} from 'vue'
import {api,notify,state} from '../lib'
import Icon from './Icon.vue'
import NumberInput from './NumberInput.vue'
import ReplacementRuleList from './ReplacementRuleList.vue'
const empty=()=>({matchRules:[],namingRules:[],blacklist:[],releaseGroups:[],minSizeMB:0})
const config=ref(empty()),blacklistText=ref(''),groupsText=ref(''),ready=ref(false),busy=ref(false),error=ref(''),errorField=ref(''),previewError=ref(''),previewField=ref(''),previewBusy=ref(false),blacklistOpen=ref(false),blacklistInput=ref(),groupsInput=ref(),saved=ref('')
const sample=ref('【高清影视之家发布】www\\.abc.com@蜘蛛侠_2021_国语中字_1080p.WEB-DL.H265-TnT.mkv'),result=ref('')
let alive=true,previewTicket=0,timer
const lines=value=>value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean)
const rules=list=>list.map(({from,to,regex})=>({from,to,regex})).filter(r=>r.from!==''||r.to!=='')
function payload(){return{matchRules:rules(config.value.matchRules),namingRules:rules(config.value.namingRules),blacklist:lines(blacklistText.value),releaseGroups:lines(groupsText.value),minSizeMB:config.value.minSizeMB}}
const sizeValid=computed(()=>Number.isSafeInteger(config.value.minSizeMB)&&config.value.minSizeMB>=0)
const changed=computed(()=>JSON.stringify(payload())!==saved.value)
const blacklistCount=computed(()=>lines(blacklistText.value).length)
function resize(el){if(!el)return;el.style.height='auto';el.style.height=`${Math.max(132,el.scrollHeight)}px`}
function apply(value){config.value={...empty(),...value,matchRules:(value.matchRules||[]).map(r=>({...r,_key:Symbol()})),namingRules:(value.namingRules||[]).map(r=>({...r,_key:Symbol()}))};blacklistText.value=(value.blacklist||[]).join('\n');groupsText.value=(value.releaseGroups||[]).join('\n');saved.value=JSON.stringify(payload());nextTick(()=>{resize(groupsInput.value);resize(blacklistInput.value)})}
async function load(){error.value='';const owner=state.username;try{const value=await api('/recognition-rules');if(!alive||owner!==state.username)return;apply(value);ready.value=true}catch(e){if(alive){error.value=e.message;notify(e.message,true)}}}
async function preview(ticket){const owner=state.username,name=sample.value,rows=payload().matchRules;previewBusy.value=true;try{const value=await api('/recognition-rules/preview','POST',{name,rules:rows});if(alive&&ticket===previewTicket&&owner===state.username){result.value=value.result;previewError.value='';previewField.value=''}}catch(e){if(alive&&ticket===previewTicket){previewError.value=e.message;previewField.value=e.field||''}}finally{if(alive&&ticket===previewTicket)previewBusy.value=false}}
function schedulePreview(){clearTimeout(timer);const ticket=++previewTicket;previewError.value='';previewField.value='';previewBusy.value=true;timer=setTimeout(()=>preview(ticket),250)}
watch(()=>[sample.value,config.value.matchRules],schedulePreview,{deep:true})
async function save(){
 if(!ready.value||busy.value||!sizeValid.value)return
 busy.value=true;error.value='';errorField.value=''
 const owner=state.username;let focusError=false
 try{const value=await api('/recognition-rules','PUT',payload());if(!alive||owner!==state.username)return;apply(value);notify('识别规则已保存')}
 catch(e){if(alive&&owner===state.username){error.value=e.message;errorField.value=e.field||'';focusError=true;notify(e.message,true)}}
 finally{if(alive)busy.value=false}
 if(focusError&&alive){if(errorField.value==='blacklist')blacklistOpen.value=true;await nextTick();document.querySelector('.recognition-settings [aria-invalid=true]')?.focus()}
}
watch(()=>JSON.stringify(payload()),()=>{if(!busy.value){error.value='';errorField.value=''}})
async function closeBlacklist(){blacklistOpen.value=false;await nextTick();document.querySelector('.recognition-blacklist-control .blacklist-trigger')?.focus()}
async function toggleBlacklist(){blacklistOpen.value=!blacklistOpen.value;if(blacklistOpen.value){await nextTick();resize(blacklistInput.value);blacklistInput.value?.focus()}}
function outside(e){if(!e.target.closest('.recognition-blacklist-control'))blacklistOpen.value=false}
onMounted(()=>{load();document.addEventListener('click',outside)})
onUnmounted(()=>{alive=false;previewTicket++;clearTimeout(timer);document.removeEventListener('click',outside)})
</script>
<template>
 <form class="recognition-settings" aria-label="识别规则" @submit.prevent="save">
  <section class="recognition-panel" aria-label="匹配替换"><header><Icon name="ListFilter"/><h2>匹配替换</h2></header><p class="recognition-hint">仅处理送去识别的名称；按顺序替换，替换文本留空即忽略该字段。</p><ReplacementRuleList v-model="config.matchRules" title="匹配替换" field="matchRules" :disabled="busy||!ready" :error-field="previewField||errorField"/>
   <div class="recognition-preview"><h3>预览结果</h3><label>示例名称<input v-model="sample" aria-label="示例名称" maxlength="4096" :disabled="!ready"/></label><small class="recognition-result" :class="{'error-message':previewError}" role="status" :aria-busy="previewBusy">{{previewError||(!previewBusy?(result||'（空）'):'正在预览…')}}</small></div>
  </section>
  <section class="recognition-panel" aria-label="归档黑名单"><header><Icon name="FileX2"/><h2>归档黑名单</h2></header><p class="recognition-hint">区分大小写，一行一条：文件关键词、.扩展名、/目录关键词。</p><div class="recognition-blacklist-control"><button class="blacklist-trigger" type="button" aria-label="归档黑名单" aria-controls="recognition-blacklist" :aria-expanded="blacklistOpen" :disabled="busy||!ready" @click="toggleBlacklist"><span>{{blacklistCount?`${blacklistCount} 条黑名单`:'添加黑名单词条'}}</span><Icon name="ChevronDown" :size="17" :class="{expanded:blacklistOpen}"/></button><div v-if="blacklistOpen" class="blacklist-input-wrap" @keydown.esc.stop.prevent="closeBlacklist"><label for="recognition-blacklist">黑名单词条</label><textarea id="recognition-blacklist" ref="blacklistInput" v-model="blacklistText" aria-label="黑名单词条" :aria-invalid="errorField==='blacklist'" rows="5" maxlength="524288" placeholder="预告&#10;.mkv&#10;/花絮" :disabled="busy" @input="resize($event.target)"/></div></div><p class="recognition-hint blacklist-outcome">目录命中后包含其全部子目录和文件。命中项移至整理任务的失败处理目录；未设置则移入回收站。</p>
  </section>
  <section class="recognition-panel" aria-label="命名替换"><header><Icon name="TextCursorInput"/><h2>命名替换</h2></header><p class="recognition-hint">命名规则生成名称后，再按条目顺序进行替换。</p><ReplacementRuleList v-model="config.namingRules" title="命名替换" field="namingRules" :disabled="busy||!ready" :error-field="errorField"/></section>
  <section class="recognition-panel" aria-label="发布组扩展"><header><Icon name="Tags"/><h2>发布组扩展</h2></header><label class="recognition-groups-label">发布组词条<textarea ref="groupsInput" v-model="groupsText" aria-label="发布组词条" :aria-invalid="errorField==='releaseGroups'" rows="5" maxlength="524288" placeholder="一行一个发布组" :disabled="busy||!ready" @input="resize($event.target)"/></label><p class="recognition-hint">原名称包含词条时，在命名结果的媒体扩展名前添加“-发布组”；保留复合 STRM 后缀。多个词条命中时使用第一条。</p></section>
  <section class="recognition-panel recognition-size" aria-label="文件体积过滤"><header><Icon name="Filter"/><h2>文件体积过滤</h2></header><label>文件体积过滤<NumberInput v-model="config.minSizeMB" aria-label="文件体积过滤" :aria-invalid="!sizeValid||errorField==='minSizeMB'" min="0" max="9007199254740991" step="1" unit="MB" :disabled="busy||!ready"/></label><small v-if="!sizeValid" class="error-message">仅可输入非负整数</small><p class="recognition-hint">0 表示不限制；小于此体积的文件将直接删除。</p></section>
  <footer class="recognition-footer"><p v-if="error" class="error-message" role="alert">{{error}}</p><button v-if="!ready" class="btn" type="button" @click="load">重新读取</button><button class="btn primary" :disabled="busy||!ready||!sizeValid||!changed||!!previewError"><Icon :name="busy?'LoaderCircle':'Check'" :class="{spin:busy}"/>{{busy?'正在保存…':'保存识别规则'}}</button></footer>
 </form>
</template>
<style scoped>
.recognition-settings{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;align-items:start;min-width:0}.recognition-panel{min-width:0;padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}.recognition-panel>header{display:flex;align-items:center;gap:10px;margin-bottom:12px}.recognition-panel>header>svg{width:21px;height:21px;flex:none;color:var(--primary)}.recognition-panel h2{font-size:16px;font-weight:500;line-height:24px;margin:0}.recognition-hint{font-size:12px;color:var(--muted);line-height:1.7;margin:10px 0 16px;overflow-wrap:anywhere}.recognition-preview{margin-top:20px;padding-top:16px;border-top:1px solid var(--border)}.recognition-preview h3{font-size:14px;font-weight:500;margin:0 0 12px}.recognition-panel label{display:grid;gap:8px;font-size:13px;min-width:0;margin:0}.recognition-panel input,.recognition-panel textarea{width:100%;min-width:0}.recognition-preview input{height:40px}.recognition-result{display:block;min-height:20px;margin-top:10px;font-size:12px;color:var(--muted);overflow-wrap:anywhere;white-space:pre-wrap;line-height:1.7}.recognition-result.error-message{color:var(--red)}.blacklist-trigger{height:40px;width:100%;border:1px solid var(--border);background:var(--input);color:var(--text);border-radius:8px;display:flex;align-items:center;justify-content:space-between;padding:8px 12px;font-size:13px;cursor:pointer}.blacklist-trigger:focus-visible{outline:2px solid var(--primary);outline-offset:2px}.blacklist-trigger .expanded{transform:rotate(180deg)}.blacklist-input-wrap{margin-top:10px}.blacklist-input-wrap>label{margin-bottom:8px}.recognition-panel textarea{min-height:132px;resize:none;line-height:22px;font-size:13px;overflow:hidden;padding:10px 12px}.recognition-groups-label{margin-top:16px}.recognition-panel [aria-invalid=true]{border-color:var(--red)}.blacklist-outcome{margin-bottom:0}.recognition-size{grid-column:1/-1;display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:0 16px}.recognition-size>header,.recognition-size>small{grid-column:1/-1}.recognition-size>p{align-self:end;margin-bottom:0}.recognition-size :deep(.number-control){width:100%;height:40px}.recognition-size :deep(.number-control input){padding-right:72px}.recognition-size :deep(.number-accessory){width:66px;justify-items:start;padding-left:4px}.recognition-size :deep(.number-unit){opacity:1}.recognition-size :deep(.number-steppers){left:30px}.recognition-footer{grid-column:1/-1;display:flex;gap:12px;align-items:center;justify-content:flex-end;padding:0 0 16px;min-width:0}.recognition-footer>p{margin:0 auto 0 0;font-size:13px;line-height:1.7;overflow-wrap:anywhere}.recognition-footer .btn{flex:none}.recognition-footer .spin{animation:recognition-spin 1s linear infinite}@keyframes recognition-spin{to{transform:rotate(360deg)}}
@media(max-width:760px){.recognition-settings{grid-template-columns:minmax(0,1fr)}.recognition-panel{padding:16px}.recognition-size{display:block}.recognition-footer{flex-wrap:wrap}.recognition-footer>p{flex-basis:100%}}
@media(prefers-reduced-motion:reduce){.recognition-footer .spin{animation:none}}
</style>
