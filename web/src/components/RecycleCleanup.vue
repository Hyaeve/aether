<script setup>
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,notify,state} from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import RoundedSelect from './RoundedSelect.vue'
import SecretInput from './SecretInput.vue'
import NumberInput from './NumberInput.vue'
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
async function save(){busy.value=true;error.value='';try{const rules=editing.value?value.value.rules.map(r=>r.storageId===editing.value?draft.value:r):[...value.value.rules,draft.value];await api('/115-recycle-cleanup','PUT',{...value.value,rules});draft.value=null;await load();emit('changed',value.value);notify('清空任务已保存')}catch(e){error.value=e.message}finally{busy.value=false}}
const removing=ref(null)
async function remove(){busy.value=true;try{await api('/115-recycle-cleanup','PUT',{...value.value,rules:value.value.rules.filter(r=>r.storageId!==removing.value.storageId)});removing.value=null;await load();emit('changed',value.value)}catch(e){error.value=e.message}finally{busy.value=false}}
</script>
<template>
<Modal standard title="115回收站清空" @close="!busy&&emit('close')"><div class="modal-body recycle-cleanup"><p v-if="error&&!draft" class="error-message" role="alert">{{error}}</p><div class="cleanup-list"><article v-for="rule in value.rules" :key="rule.storageId"><ProviderIcon type="115" compact /><div><strong>{{name(rule.storageId)}}</strong><small>{{rule.cron}} · {{rule.days}}天</small></div><button class="icon-btn" :aria-label="`编辑清空任务 ${name(rule.storageId)}`" @click="open(rule)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :aria-label="`删除清空任务 ${name(rule.storageId)}`" @click="removing=rule"><Icon name="Trash2" /></button></article></div><button class="btn cleanup-add" :disabled="busy||!loaded||!available.length" @click="open()"><Icon name="Plus" />添加清空任务</button></div></Modal>
<Modal v-if="draft" standard :title="editing?'编辑清空任务':'添加清空任务'" @close="!busy&&(draft=null)"><form @submit.prevent="save"><div class="modal-body cleanup-editor"><label>115存储池<RoundedSelect v-model="draft.storageId" label="清空任务存储池" :disabled="!!editing" :options="available.map(s=>({value:s.id,label:s.name}))" /></label><label>回收站密码<SecretInput v-model="draft.recyclePIN" aria-label="回收站密码" maxlength="4" :required="!editing" autocomplete="off" /></label><label>Cron表达式<input v-model="draft.cron" aria-label="清空任务Cron表达式" required /></label><label>保留天数<NumberInput v-model="draft.days" aria-label="保留天数" :min="1" :max="3650" unit="天" required /></label><p v-if="error" role="alert" class="error-message">{{error}}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="draft=null">取消</button><button class="btn primary" :disabled="busy||!draft.storageId"><Icon name="Check" />保存</button></footer></form></Modal>
<Modal v-if="removing" compact confirmation title="删除清空任务" @close="!busy&&(removing=null)"><div class="modal-body">删除「{{name(removing.storageId)}}」清空任务？</div><footer class="modal-footer"><button class="btn" @click="removing=null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除</button></footer></Modal>
</template>
<style scoped>.recycle-cleanup{display:grid;gap:14px}.cleanup-list{height:300px;overflow:auto;scrollbar-width:none}.cleanup-list article{display:flex;align-items:center;gap:10px;padding:12px;border:1px solid var(--border);border-radius:8px;margin-bottom:8px;background:var(--input)}.cleanup-list article>div{flex:1;min-width:0;display:grid;gap:5px}.cleanup-list small{font-size:13px;color:var(--muted)}.cleanup-list strong{font-size:14px}.cleanup-add{width:100%;border-style:dashed}.cleanup-editor{display:grid;grid-template-columns:1fr 1fr;gap:16px}.cleanup-editor label{display:grid;gap:8px;min-width:0}.cleanup-editor>p{grid-column:1/-1}</style>
