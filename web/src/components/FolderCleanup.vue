<script setup>
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,notify,state} from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import NumberInput from './NumberInput.vue'
import CronInput from './CronInput.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
import '../plugin-tasks.css'
const emit=defineEmits(['close','changed'])
const settings=ref({enabled:false,rules:[]}),loaded=ref(false),draft=ref(null),picker=ref(false),busy=ref(false),confirmation=ref(null),removing=ref(null),running=ref(null)
const pools=computed(()=>state.storages.filter(s=>s.type==='115'&&s.enabled))
const poolName=id=>pools.value.find(s=>s.id===id)?.name||'存储不可用'
let alive=true
onMounted(async()=>{try{const value=await api('/115-folder-cleanup');if(alive){settings.value={...value,rules:value.rules||[]};loaded.value=true}}catch(e){if(alive)notify(e.message,true)}})
onBeforeUnmount(()=>alive=false)
function open(rule){draft.value=rule?JSON.parse(JSON.stringify(rule)):{id:'',storageId:pools.value[0]?.id||'',directory:'',label:'',cron:'0 3 * * *',days:0,trail:[]}}
function select(value){draft.value.storageId=value.storageId;draft.value.directory=value.source;draft.value.label=value.sourcePath||value.sourceLabel;picker.value=false}
function numericKey(event){if(['e','E','+','-','.'].includes(event.key))event.preventDefault()}
function ask(){if(draft.value.directory==='/'||draft.value.directory==='0'){notify('请选择115存储的子文件夹，不能清理根目录',true);return}confirmation.value=JSON.parse(JSON.stringify(draft.value))}
async function persist(value){busy.value=true;try{const result=await api('/115-folder-cleanup','PUT',{...value,confirmed:true});if(alive){settings.value={...result,rules:result.rules||[]};emit('changed',settings.value);return true}}catch(e){if(alive)notify(e.message,true)}finally{if(alive)busy.value=false}return false}
async function save(){const rule=confirmation.value,rules=rule.id?settings.value.rules.map(r=>r.id===rule.id?rule:r):[...settings.value.rules,rule];if(await persist({...settings.value,enabled:true,rules})){confirmation.value=null;draft.value=null;notify('文件夹清理规则已保存')}}
async function remove(){if(await persist({...settings.value,rules:settings.value.rules.filter(r=>r.id!==removing.value.id)}))removing.value=null}
async function run(){if(busy.value||!running.value)return;busy.value=true;try{const result=await api('/115-folder-cleanup/run','POST',{id:running.value.id,confirmed:true});notify(`文件夹清理完成，已处理 ${Number(result.count)||0} 项`)}catch(e){notify(e.message,true)}finally{busy.value=false;running.value=null}}
</script>
<template>
 <Modal standard title="115 文件夹清理" @close="!busy&&emit('close')"><div class="modal-body plugin-task-settings"><div class="plugin-task-list">
  <article v-for="rule in settings.rules" :key="rule.id" class="folder-cleanup-rule plugin-task-row"><ProviderIcon type="115" /><div class="plugin-task-copy"><strong>{{poolName(rule.storageId)}}</strong><small :data-tooltip="`${rule.label} · ${rule.cron} · ${rule.days===0?'清理全部文件':`保留${rule.days}天`}`" data-tooltip-stationary data-tooltip-delay="300">{{rule.label}} · {{rule.cron}} · {{rule.days===0?'清理全部文件':`保留${rule.days}天`}}</small></div><button class="btn plugin-task-run" :aria-label="`执行文件夹清理 ${rule.label}`" :disabled="busy||!settings.enabled" @click="running=rule"><Icon name="Play" />执行</button><button class="icon-btn" :aria-label="`编辑文件夹清理 ${rule.label}`" :disabled="busy" @click="open(rule)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :aria-label="`删除文件夹清理 ${rule.label}`" :disabled="busy" @click="removing=rule"><Icon name="Trash2" /></button></article>
 </div></div><footer class="modal-footer plugin-task-footer"><button class="btn plugin-task-add" :disabled="busy||!loaded||!pools.length" @click="open()"><Icon name="FolderPlus" />添加文件夹</button></footer></Modal>
 <Modal v-if="draft" standard :title="draft.id?'编辑清理文件夹':'添加清理文件夹'" @close="!busy&&(draft=null)"><form @submit.prevent="ask"><div class="modal-body folder-cleanup-editor">
  <div class="folder-cleanup-schedule"><label>Cron表达式<CronInput v-model="draft.cron" aria-label="文件夹清理Cron表达式" required /></label><label>清理阈值<NumberInput v-model="draft.days" aria-label="清理阈值" unit="天" min="0" max="3650" step="1" inputmode="numeric" required @keydown="numericKey" /></label></div>
  <label>清理文件夹<button type="button" class="source-trigger" aria-label="选择清理文件夹" @click="picker=true"><span>{{draft.directory?`${poolName(draft.storageId)} · ${draft.label}`:''}}</span><Icon name="FolderOpen" /></button></label>
 </div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="draft=null">取消</button><button class="btn primary" :disabled="busy||!draft.directory"><Icon name="Check" />保存</button></footer></form></Modal>
 <TaskSourcePicker v-if="picker&&draft" include-full-path :storages="pools" :storage="draft.storageId" :initial="draft.directory||'/'" :initial-label="draft.label" @select="select" @close="picker=false" />
 <Modal v-if="confirmation" compact confirmation title="确认文件夹清理计划" @close="!busy&&(confirmation=null)"><div class="modal-body">保存并启用「{{poolName(confirmation.storageId)}} · {{confirmation.label}}」清理计划？{{confirmation.days===0?'每次计划执行将清理该文件夹下全部文件。':`每次计划执行将清理存在超过${confirmation.days}天的文件。时间未知的文件保留。`}}随后清理空子目录，保留所选文件夹。文件移入115回收站，不会清空回收站。<p v-if="!settings.enabled&&settings.rules.some(r=>r.id!==confirmation.id)">同时启用其余 {{settings.rules.filter(r=>r.id!==confirmation.id).length}} 条已保存清理计划。</p></div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="confirmation=null">返回</button><button class="btn danger" :disabled="busy" @click="save">确认保存</button></footer></Modal>
 <Modal v-if="running" compact confirmation title="执行文件夹清理" @close="!busy&&(running=null)"><div class="modal-body">立即清理「{{poolName(running.storageId)}} · {{running.label}}」？{{running.days===0?'清理全部文件。':`清理存在超过${running.days}天的文件，时间未知的文件保留。`}}随后清理空子目录，保留所选文件夹，移入115回收站。</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="running=null">取消</button><button class="btn danger" :disabled="busy" @click="run">确认清理</button></footer></Modal>
 <Modal v-if="removing" compact confirmation title="删除文件夹清理规则" @close="!busy&&(removing=null)"><div class="modal-body">删除「{{removing.label}}」清理规则？不清理任何云端文件。</div><footer class="modal-footer"><button class="btn" @click="removing=null">取消</button><button class="btn danger" :disabled="busy" @click="remove">确认删除</button></footer></Modal>
</template>
<style scoped>
.folder-cleanup,.folder-cleanup-editor{display:grid;grid-template-columns:minmax(0,1fr);gap:16px}
.folder-cleanup-editor label{min-width:0}
.folder-cleanup-schedule{display:grid;grid-template-columns:minmax(0,2fr) minmax(0,1fr);gap:16px}
.folder-cleanup-schedule :deep(.number-control){width:100%}
.folder-cleanup-editor .source-trigger{width:100%;min-width:0;max-width:100%;height:var(--control-height);min-height:var(--control-height);justify-content:space-between;background:var(--input)}
.source-trigger>span{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
</style>
