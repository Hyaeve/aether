<script setup>
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, state, notify, date } from '../lib'
import TaskTabs from '../components/TaskTabs.vue'
import Modal from '../components/Modal.vue'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import NumberInput from '../components/NumberInput.vue'
import ThinScroll from '../components/ThinScroll.vue'
const rules = ref([]), editing = ref(null), modal = ref(false), busy = ref(false), error = ref(''), deleting = ref(null)
const form = reactive({})
const menu = ref(null)
function closeMenu() { menu.value = null }
function showMenu(event, rule) { menu.value = { rule, x:Math.max(8,Math.min(event.currentTarget.getBoundingClientRect().right-160,innerWidth-168)), y:Math.max(8,Math.min(event.currentTarget.getBoundingClientRect().bottom+4,innerHeight-145)) } }
function stepName(step) { return step.kind === 'task' ? state.tasks.find(t=>t.id===step.taskId)?.name || '任务已删除' : step.kind === 'delay' ? `延时 ${step.seconds} 秒` : '刷新目录缓存' }
function lastResult(rule) { const value=rule.lastResult || (rule.status !== 'running' ? rule.status : ''); return value === 'success' ? '成功' : ['error','failed','interrupted','stopped','cancelled'].includes(value) ? '失败' : '无记录' }
function nextExecution(rule) {
  if(rule.trigger !== 'cron' || !rule.enabled || !rule.nextRun || rule.nextRun.startsWith('0001')) return '--'
  const next=new Date(rule.nextRun), now=new Date(), today=new Date(now.getFullYear(),now.getMonth(),now.getDate()), day=new Date(next.getFullYear(),next.getMonth(),next.getDate()), days=Math.round((day-today)/86400000)
  const time=next.toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'})
  if(days===0) return `今天 ${time}`
  if(days===1) return `明天 ${time}`
  const monday=new Date(today); monday.setDate(today.getDate()-((today.getDay()+6)%7))
  const weeks=Math.floor((day-monday)/604800000)
  if(weeks===1) return `下周${'日一二三四五六'[next.getDay()]} ${time}`
  if(next.getFullYear()*12+next.getMonth()===now.getFullYear()*12+now.getMonth()+1) return `下个月 ${next.getDate()}日 ${time}`
  return `${next.getMonth()+1}月${next.getDate()}日 ${time}`
}
const triggers = [{value:'manual',label:'手动执行'},{value:'cron',label:'定时触发'},{value:'task',label:'任务成功后'}]
const actions = [{value:'task',label:'执行任务'},{value:'refresh',label:'刷新目录缓存'},{value:'delay',label:'延时'}]
const conditions = [{value:'success',label:'上一步成功'},{value:'failure',label:'上一步失败'},{value:'always',label:'始终执行'}]
let timer, alive = true
async function load() { try { const value = await api('/automations'); if (alive) rules.value = value } catch(e) { if(alive) notify(e.message,true) } }
onMounted(() => { load(); timer = setInterval(load,5000); document.addEventListener('click',closeMenu) })
onUnmounted(() => { alive = false; clearInterval(timer); document.removeEventListener('click',closeMenu) })
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
    <header class="automation-columns"><span>联动名称</span><span>任务流程</span><span>上次结果</span><span>下次执行</span><span>操作</span></header>
    <ThinScroll class="automation-scroll" :thickness="3"><article v-for="rule in rules" :key="rule.id" class="automation-row" :class="{disabled:!rule.enabled,running:rule.status==='running'}">
      <button class="automation-summary" @click="open(rule)"><strong>{{rule.name}}</strong></button>
      <div class="automation-flow" tabindex="0" :aria-label="rule.steps.map(stepName).join(' → ')"><span class="workflow-chain"><template v-for="(step,index) in rule.steps" :key="index"><Icon v-if="index" name="ArrowRight" :size="14" /><span>{{stepName(step)}}</span></template></span></div>
      <span class="automation-result" :class="lastResult(rule)==='成功'?'success-text':lastResult(rule)==='失败'?'danger-text':'muted'">{{lastResult(rule)}}</span>
      <time :title="date(rule.nextRun)">{{nextExecution(rule)}}</time>
      <div class="automation-actions"><button class="icon-btn" :disabled="!rule.enabled && rule.status!=='running'" :aria-label="rule.status==='running'?'停止联动':'执行联动'" @click="command(rule,rule.status==='running'?'stop':'run')"><Icon :name="rule.status==='running'?'Square':'Play'" /></button><button class="icon-btn" aria-label="联动操作" aria-haspopup="menu" @click.stop="showMenu($event,rule)"><Icon name="Ellipsis" /></button></div>
    </article><p v-if="!rules.length" class="small-empty">暂无联动任务</p></ThinScroll>
  </div>
  <Teleport to="body"><div v-if="menu" class="context-menu" role="menu" :style="{left:menu.x+'px',top:menu.y+'px'}" @click.stop><button role="menuitem" @click="open(menu.rule);closeMenu()"><Icon name="Pencil" />编辑</button><button role="menuitem" :disabled="menu.rule.status==='running'" @click="toggle(menu.rule);closeMenu()"><Icon :name="menu.rule.enabled?'Pause':'Play'" />{{menu.rule.enabled?'停用':'启用'}}</button><button role="menuitem" class="danger-text" :disabled="menu.rule.status==='running'" @click="deleting=menu.rule;closeMenu()"><Icon name="Trash2" />删除</button></div></Teleport>
  <Modal v-if="modal" standard :title="editing?'编辑联动':'添加联动'" @close="modal=false">
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
.automation-list {display:flex;flex-direction:column;flex:1;min-height:0;border:1px solid var(--border);border-radius:8px;background:var(--surface);overflow:hidden}
.automation-columns,.automation-row {display:grid;grid-template-columns:minmax(110px,1fr) minmax(130px,2fr) 80px 150px 80px;gap:12px;align-items:center;padding:12px 16px;min-width:0}
.automation-columns {font-size:12px;color:var(--muted);border-bottom:1px solid var(--border);flex:none}.automation-scroll {flex:1;min-height:0}.automation-row {position:relative;border-bottom:1px solid var(--border);min-height:64px;font-size:13px}.automation-row.disabled {color:var(--muted);opacity:.55}.automation-actions {display:flex}.automation-flow {display:flex;align-items:center;gap:8px;overflow:hidden;white-space:nowrap;min-width:0;padding:8px 0}.automation-flow span,.automation-flow svg {flex:none}.automation-flow::after {content:'';position:absolute;pointer-events:none;right:340px;width:18px;height:30px;background:linear-gradient(90deg,transparent,var(--surface))}
.automation-row:has(.automation-flow:hover) .automation-flow,.automation-row:has(.automation-flow:focus) .automation-flow {grid-column:2 / -1;overflow:auto;scrollbar-width:none}.automation-row:has(.automation-flow:hover) .automation-flow::after,.automation-row:has(.automation-flow:focus) .automation-flow::after {display:none}.automation-row:has(.automation-flow:hover) .automation-result,.automation-row:has(.automation-flow:hover) time,.automation-row:has(.automation-flow:hover) .automation-actions,.automation-row:has(.automation-flow:focus) .automation-result,.automation-row:has(.automation-flow:focus) time,.automation-row:has(.automation-flow:focus) .automation-actions {display:none}
.automation-flow::after { display:none; }.workflow-chain { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.workflow-chain svg { display:inline-block; vertical-align:middle; margin:0 8px; }.automation-flow:hover .workflow-chain,.automation-flow:focus .workflow-chain { overflow:visible; }
.automation-summary {flex:1;min-width:0;text-align:left;background:none;border:0;color:var(--text);display:grid;gap:5px}
.automation-summary strong,.automation-summary small {overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.automation-summary small,.automation-row time {color:var(--muted);font-size:13px}
.automation-row [aria-pressed=true] {color:var(--primary)}
.automation-form {display:grid;gap:16px;max-height:65dvh;overflow:auto;scrollbar-width:none}.automation-form::-webkit-scrollbar {display:none}
.automation-form input, .automation-form :deep(.rounded-select-trigger) {height:36px;min-height:36px;font-size:14px}
.automation-form :deep(.number-input) {height:36px;min-height:36px}
.automation-step {border-bottom:1px solid var(--border);padding-bottom:16px}
.automation-step header {display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;font-size:14px}
.automation-step header span {display:flex}
@media(max-width:800px){.automation-columns,.automation-row {grid-template-columns:minmax(70px,1fr) minmax(80px,1.3fr) 60px 70px;gap:6px;padding-inline:10px}.automation-columns span:nth-child(4),.automation-row time{display:none}.automation-flow::after{right:145px}}
</style>
