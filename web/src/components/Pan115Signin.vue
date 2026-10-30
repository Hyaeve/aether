<script setup>
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,notify,state} from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import RoundedSelect from './RoundedSelect.vue'
import CronInput from './CronInput.vue'
import '../plugin-tasks.css'
const emit=defineEmits(['close','changed'])
const settings=ref({enabled:false,rules:[]}),loaded=ref(false),busy=ref(false),error=ref(''),draft=ref(null),original=ref(''),removing=ref(null)
const pools=computed(()=>state.storages.filter(s=>s.type==='115'&&s.enabled))
const name=id=>state.storages.find(s=>s.id===id)?.name||'存储已删除'
const options=computed(()=>pools.value.filter(s=>s.id===original.value||!settings.value.rules.some(r=>r.storageId===s.id)).map(s=>({value:s.id,label:s.name})))
const canAdd=computed(()=>pools.value.some(s=>!settings.value.rules.some(r=>r.storageId===s.id)))
let alive=true
onBeforeUnmount(()=>alive=false)
onMounted(async()=>{try{const value=await api('/115-signin');if(alive){settings.value={...value,rules:value.rules||[]};loaded.value=true}}catch(e){if(alive)error.value=e.message}})
function open(rule){original.value=rule?.storageId||'';draft.value=rule?{...rule}:{storageId:options.value[0]?.value||'',cron:'0 8 * * *'};error.value=''}
async function persist(value){if(busy.value)return false;busy.value=true;error.value='';try{const saved=await api('/115-signin','PUT',value);if(alive){settings.value={...saved,rules:saved.rules||[]};emit('changed',settings.value)}return true}catch(e){if(alive)error.value=e.message;return false}finally{if(alive)busy.value=false}}
async function save(){const rule={...draft.value},rules=original.value?settings.value.rules.map(r=>r.storageId===original.value?rule:r):[...settings.value.rules,rule];if(await persist({...settings.value,enabled:true,rules})){draft.value=null;notify('签到任务已保存')}}
async function remove(){if(await persist({...settings.value,rules:settings.value.rules.filter(r=>r.storageId!==removing.value.storageId)}))removing.value=null}
async function run(rule){if(busy.value)return;busy.value=true;try{const result=await api('/115-signin/run','POST',{storageId:rule.storageId});if(alive)notify(`${name(rule.storageId)}：${result.message}`)}catch(e){if(alive)notify(e.message,true)}finally{if(alive)busy.value=false}}
</script>
<template>
 <Modal standard title="115 自动签到" @close="!busy&&emit('close')"><div class="modal-body plugin-task-settings"><p v-if="error&&!draft" class="error-message" role="alert">{{error}}</p><div class="plugin-task-list"><article v-for="rule in settings.rules" :key="rule.storageId" class="plugin-task-row"><ProviderIcon type="115" /><div class="plugin-task-copy"><strong>{{name(rule.storageId)}}</strong><small>{{rule.cron}}</small></div><button class="btn plugin-task-run" :aria-label="`执行签到 ${name(rule.storageId)}`" :disabled="busy||!settings.enabled" @click="run(rule)"><Icon name="Play" />执行</button><button class="icon-btn" :aria-label="`编辑签到 ${name(rule.storageId)}`" :disabled="busy" @click="open(rule)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :aria-label="`删除签到 ${name(rule.storageId)}`" :disabled="busy" @click="removing=rule"><Icon name="Trash2" /></button></article><p v-if="loaded&&!settings.rules.length" class="small-empty">暂无签到任务</p></div></div><footer class="modal-footer plugin-task-footer"><button class="btn plugin-task-add" :disabled="busy||!loaded||!canAdd" @click="open()"><Icon name="Plus" />添加签到</button></footer></Modal>
 <Modal v-if="draft" standard :title="original?'编辑签到':'添加签到'" @close="!busy&&(draft=null)"><form @submit.prevent="save"><div class="modal-body signin-editor"><label>115存储池<RoundedSelect v-model="draft.storageId" label="签到115存储池" :options="options" :disabled="busy||!!original" /></label><label>Cron表达式<CronInput v-model="draft.cron" aria-label="签到Cron表达式" required :disabled="busy" /></label><p v-if="error" role="alert" class="error-message">{{error}}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="draft=null">取消</button><button class="btn primary" :disabled="busy||!draft.storageId"><Icon name="Check" />保存</button></footer></form></Modal>
 <Modal v-if="removing" compact confirmation title="删除签到任务" @close="!busy&&(removing=null)"><div class="modal-body">删除「{{name(removing.storageId)}}」的签到计划？已完成的签到不受影响。</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="removing=null">取消</button><button class="btn danger" :disabled="busy" @click="remove">确认删除</button></footer></Modal>
</template>
<style scoped>
.signin-editor{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:16px}.signin-editor label{min-width:0}.signin-editor p{grid-column:1/-1}
@media(max-width:480px){.signin-editor{grid-template-columns:minmax(0,1fr)}}
</style>
