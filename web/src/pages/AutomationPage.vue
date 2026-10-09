<script setup>
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, state, notify, date } from '../lib'
import TaskTabs from '../components/TaskTabs.vue'
import Modal from '../components/Modal.vue'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import NumberInput from '../components/NumberInput.vue'
const rules = ref([]), editing = ref(null), modal = ref(false), busy = ref(false), error = ref(''), deleting = ref(null)
const form = reactive({})
const triggers = [{value:'manual',label:'手动执行'},{value:'cron',label:'定时触发'},{value:'task',label:'任务成功后'}]
const actions = [{value:'task',label:'执行任务'},{value:'refresh',label:'刷新目录缓存'},{value:'delay',label:'延时'}]
const conditions = [{value:'success',label:'上一步成功'},{value:'failure',label:'上一步失败'},{value:'always',label:'始终执行'}]
let timer, alive = true
async function load() { try { const value = await api('/automations'); if (alive) rules.value = value } catch(e) { if(alive) notify(e.message,true) } }
onMounted(() => { load(); timer = setInterval(load,5000) })
onUnmounted(() => { alive = false; clearInterval(timer) })
function open(rule) { editing.value = rule?.id || null; Object.assign(form, rule ? JSON.parse(JSON.stringify(rule)) : {name:'',enabled:true,trigger:'manual',cron:'',sourceTask:'',steps:[{kind:'task',taskId:'',condition:'success',seconds:10}]}); error.value=''; modal.value=true }
async function save() { busy.value=true; error.value=''; try { await api(editing.value ? `/automations/${editing.value}` : '/automations',editing.value ? 'PUT' : 'POST',form); modal.value=false; await load(); notify('联动已保存') } catch(e) { error.value=e.message } finally {busy.value=false} }
async function command(rule, action) { try { await api(`/automations/${rule.id}/${action}`,'POST'); await load(); notify(action==='run'?'联动已启动':'正在停止联动') } catch(e) {notify(e.message,true)} }
async function toggle(rule) { try { await api(`/automations/${rule.id}`,'PUT',{...rule,enabled:!rule.enabled}); await load(); notify(rule.enabled?'联动已停用':'联动已启用') } catch(e) {notify(e.message,true)} }
async function remove() { busy.value=true; try {await api(`/automations/${deleting.value.id}`,'DELETE');deleting.value=null;await load();notify('联动已删除')} catch(e) {notify(e.message,true)} finally {busy.value=false} }
const taskOptions = () => state.tasks.map(t => ({value:t.id,label:t.name}))
</script>
<template>
  <section class="task-heading"><TaskTabs /><div class="toolbar-right"><button class="btn primary" @click="open()"><Icon name="Plus" />添加联动</button></div></section>
  <div class="automation-list">
    <article v-for="rule in rules" :key="rule.id" class="automation-row">
      <button class="icon-btn" :aria-label="rule.enabled?'停用联动':'启用联动'" :aria-pressed="rule.enabled" :disabled="rule.status==='running'" @click="toggle(rule)"><Icon name="Waypoints" /></button>
      <button class="automation-summary" @click="open(rule)"><strong>{{rule.name}}</strong><small>{{rule.steps.length}} 个动作 · {{triggers.find(t=>t.value===rule.trigger)?.label}} · {{rule.message || '等待执行'}}</small></button>
      <time>{{ date(rule.lastRun) }}</time>
      <button class="icon-btn" :aria-label="rule.status==='running'?'停止联动':'执行联动'" @click="command(rule,rule.status==='running'?'stop':'run')"><Icon :name="rule.status==='running'?'Square':'Play'" /></button>
      <button class="icon-btn" aria-label="删除联动" :disabled="rule.status==='running'" @click="deleting=rule"><Icon name="Trash2" /></button>
    </article>
  </div>
  <Modal v-if="modal" :title="editing?'编辑联动':'添加联动'" @close="modal=false">
    <form @submit.prevent="save"><div class="modal-body automation-form">
      <label>联动名称<input v-model="form.name" required maxlength="200" /></label>
      <div class="form-grid"><div class="field"><label>触发方式</label><RoundedSelect v-model="form.trigger" label="触发方式" :options="triggers" /></div>
      <label v-if="form.trigger==='cron'">Cron 表达式<input v-model="form.cron" required /></label>
      <div v-if="form.trigger==='task'" class="field"><label>触发任务</label><RoundedSelect v-model="form.sourceTask" label="触发任务" :options="taskOptions()" /></div></div>
      <div v-for="(step,index) in form.steps" :key="index" class="automation-step">
        <header><strong>动作 {{index+1}}</strong><span><button type="button" class="icon-btn" aria-label="上移动作" :disabled="index===0" @click="form.steps.splice(index-1,2,step,form.steps[index-1])"><Icon name="ArrowUp" /></button><button type="button" class="icon-btn" aria-label="下移动作" :disabled="index===form.steps.length-1" @click="form.steps.splice(index,2,form.steps[index+1],step)"><Icon name="ArrowDown" /></button><button type="button" class="icon-btn" aria-label="删除动作" :disabled="form.steps.length===1" @click="form.steps.splice(index,1)"><Icon name="Trash2" /></button></span></header>
        <div class="form-grid"><div class="field"><label>动作</label><RoundedSelect v-model="step.kind" label="联动动作" :options="actions" /></div><div v-if="index" class="field"><label>执行条件</label><RoundedSelect v-model="step.condition" label="执行条件" :options="conditions" /></div>
        <div v-if="step.kind==='task'" class="field full"><label>任务</label><RoundedSelect v-model="step.taskId" label="动作任务" :options="taskOptions()" /></div><div v-if="step.kind==='delay'" class="field"><label>延时</label><NumberInput v-model="step.seconds" unit="秒" min="1" max="3600" /></div></div>
      </div>
      <button type="button" class="btn" :disabled="form.steps.length>=50" @click="form.steps.push({kind:'task',taskId:'',condition:'success',seconds:10})"><Icon name="Plus" />添加动作</button>
      <p v-if="error" class="error-message" role="alert">{{error}}</p>
    </div><footer class="modal-footer"><button type="button" class="btn" @click="modal=false">取消</button><button class="btn primary" :disabled="busy"><Icon name="Check" />保存联动</button></footer></form>
  </Modal>
  <Modal v-if="deleting" title="删除联动" compact @close="deleting=null"><div class="modal-body">确认删除「{{deleting.name}}」？</div><footer class="modal-footer"><button class="btn danger" :disabled="busy" @click="remove">确认</button><button class="btn" @click="deleting=null">取消</button></footer></Modal>
</template>
<style scoped>
.automation-list {display:grid;gap:12px}
.automation-row {display:flex;align-items:center;gap:12px;padding:14px;background:var(--surface);border:1px solid var(--border);border-radius:8px;min-width:0}
.automation-summary {flex:1;min-width:0;text-align:left;background:none;border:0;color:var(--text);display:grid;gap:5px}
.automation-summary strong,.automation-summary small {overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.automation-summary small,.automation-row time {color:var(--muted);font-size:13px}
.automation-row [aria-pressed=true] {color:var(--primary)}
.automation-form {display:grid;gap:16px;max-height:65dvh;overflow:auto;scrollbar-width:thin}
.automation-form input, .automation-form :deep(.rounded-select-trigger) {height:36px;min-height:36px;font-size:14px}
.automation-form :deep(.number-input) {height:36px;min-height:36px}
.automation-step {border-bottom:1px solid var(--border);padding-bottom:16px}
.automation-step header {display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;font-size:14px}
.automation-step header span {display:flex}
@media(max-width:600px){.automation-row time{display:none}}
</style>
