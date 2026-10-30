<script setup>
import CronInput from './CronInput.vue'
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,notify,state} from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import RoundedSelect from './RoundedSelect.vue'
import PinInput from './PinInput.vue'
import NumberInput from './NumberInput.vue'
import '../plugin-tasks.css'
const emit=defineEmits(['close','changed'])
const value=ref({enabled:false,rules:[]}),draft=ref(null),editing=ref(''),busy=ref(false),loaded=ref(false),error=ref('')
const pools=computed(()=>state.storages.filter(s=>s.type==='115'))
const available=computed(()=>pools.value.filter(s=>!value.value.rules.some(r=>r.storageId===s.id)||editing.value===s.id))
const name=id=>pools.value.find(s=>s.id===id)?.name||'存储已删除'
let alive=true
onBeforeUnmount(()=>{alive=false})
async function load(){try{const saved=await api('/115-recycle-cleanup');if(alive){value.value=saved;value.value.rules ||= [];loaded.value=true}}catch(e){if(alive)error.value=e.message}}
onMounted(load)
function open(rule){editing.value=rule?.storageId||'';draft.value=rule?{...rule,recyclePIN:''}:{storageId:available.value[0]?.id||'',recyclePIN:'',cron:'0 3 * * *',days:10};error.value=''}
async function save(){busy.value=true;error.value='';try{const rules=editing.value?value.value.rules.map(r=>r.storageId===editing.value?draft.value:r):[...value.value.rules,draft.value];await api('/115-recycle-cleanup','PUT',{...value.value,rules});draft.value=null;await load();emit('changed',value.value);notify('清理任务已保存')}catch(e){error.value=e.message}finally{busy.value=false}}
const running=ref(null)
async function run(){const rule=running.value;if(busy.value||!rule)return;busy.value=true;error.value='';try{const result=await api('/115-recycle-cleanup/run','POST',{storageId:rule.storageId,confirm:true});running.value=null;notify(`${name(rule.storageId)}：已清理回收站 ${Number(result.count)||0} 项`)}catch(e){running.value=null;notify(e.message,true)}finally{busy.value=false}}
const removing=ref(null)
async function remove(){busy.value=true;try{await api('/115-recycle-cleanup','PUT',{...value.value,rules:value.value.rules.filter(r=>r.storageId!==removing.value.storageId)});removing.value=null;await load();emit('changed',value.value)}catch(e){error.value=e.message}finally{busy.value=false}}
</script>
<template>
<Modal standard title="115 回收站清理" @close="!busy&&emit('close')"><div class="modal-body plugin-task-settings"><p v-if="error&&!draft" class="error-message" role="alert">{{error}}</p><div class="plugin-task-list"><article v-for="rule in value.rules" :key="rule.storageId" class="plugin-task-row"><ProviderIcon type="115" /><div class="plugin-task-copy"><strong>{{name(rule.storageId)}}</strong><small>{{rule.cron}} · {{rule.days}}天<template v-if="rule.needsPassword"> · 需更新密码</template></small></div><button class="btn plugin-task-run" :disabled="busy||rule.needsPassword||!value.enabled" @click="running=rule"><Icon name="Play" />执行</button><button class="icon-btn" :disabled="busy" :aria-label="`编辑清理任务 ${name(rule.storageId)}`" @click="open(rule)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :disabled="busy" :aria-label="`删除清理任务 ${name(rule.storageId)}`" @click="removing=rule"><Icon name="Trash2" /></button></article></div></div><footer class="modal-footer plugin-task-footer"><button class="btn plugin-task-add" :disabled="busy||!loaded||!available.length" @click="open()"><Icon name="Plus" />添加清理任务</button></footer></Modal>
<Modal v-if="draft" standard :title="editing?'编辑清理任务':'添加清理任务'" @close="!busy&&(draft=null)"><form @submit.prevent="save"><div class="modal-body cleanup-editor"><label>115存储池<RoundedSelect v-model="draft.storageId" label="清理任务存储池" :disabled="!!editing" :options="available.map(s=>({value:s.id,label:s.name}))" /></label><div class="cleanup-password"><span>回收站密码</span><PinInput v-model="draft.recyclePIN" :disabled="busy" /></div><label>Cron表达式<CronInput v-model="draft.cron" aria-label="清理任务Cron表达式" required /></label><label>保留天数<NumberInput v-model="draft.days" aria-label="保留天数" :min="1" :max="3650" unit="天" required /></label><p v-if="error" role="alert" class="error-message">{{error}}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="draft=null">取消</button><button class="btn primary" :disabled="busy||!draft.storageId||(!editing||draft.needsPassword?!/^\d{6}$/.test(draft.recyclePIN):draft.recyclePIN.length>0&&!/^\d{6}$/.test(draft.recyclePIN))"><Icon name="Check" />保存</button></footer></form></Modal>
<Modal v-if="running" compact confirmation title="执行回收站清理" @close="!busy&&(running=null)"><div class="modal-body">立即清理「{{name(running.storageId)}}」回收站中超过 {{running.days}} 天的项目？未到期和删除时间未知的项目保留，永久删除无法撤销。</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="running=null">取消</button><button class="btn danger" :disabled="busy" @click="run">确认清理</button></footer></Modal>
<Modal v-if="removing" compact confirmation title="删除清空任务" @close="!busy&&(removing=null)"><div class="modal-body">删除「{{name(removing.storageId)}}」清空任务？</div><footer class="modal-footer"><button class="btn" @click="removing=null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除</button></footer></Modal>
</template>
<style scoped>.recycle-cleanup{display:grid;gap:14px}.cleanup-list{height:300px;overflow:auto;scrollbar-width:none}.cleanup-list::-webkit-scrollbar{display:none}.cleanup-list article{display:flex;align-items:center;gap:8px;padding:12px 0;border-bottom:1px solid var(--border)}.cleanup-list article>div{flex:1;min-width:0;display:grid;gap:5px}.cleanup-list :deep(.provider-icon){width:40px;height:40px;flex:0 0 40px;background:none}.cleanup-list small{font-size:13px;color:var(--muted)}.cleanup-list strong{font-size:14px}.cleanup-run{height:36px;padding:0 8px}.cleanup-add{width:100%;border-style:dashed}.cleanup-editor{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:16px}.cleanup-editor label,.cleanup-password{display:grid;gap:8px;min-width:0}.cleanup-editor>p{grid-column:1/-1}@media(max-width:480px){.cleanup-editor{grid-template-columns:1fr}.cleanup-list article{gap:5px}.cleanup-list .icon-btn{width:28px}}</style>
