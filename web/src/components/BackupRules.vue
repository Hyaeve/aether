<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, state, notify, date } from '../lib'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import Modal from './Modal.vue'
import RoundedSelect from './RoundedSelect.vue'
import NumberInput from './NumberInput.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
import ThinScroll from './ThinScroll.vue'
const rules = ref([]), modal = ref(false), editing = ref(''), busy = ref(false), error = ref(''), picker = ref(''), deleting = ref(null), menu = ref('')
const form = reactive({})
const storages = computed(() => state.storages.filter(s => s.enabled))
const storage = id => state.storages.find(s => s.id === id)
const policies = [{value:'skip',label:'跳过同名文件'},{value:'overwrite',label:'覆盖同名文件'}]
const statuses = {idle:'等待执行',running:'正在备份',completed:'备份完成',failed:'备份失败',stopped:'已停止',interrupted:'已中断'}
let timer, alive = true, loading = false
async function load() {
  if (loading) return
  clearTimeout(timer); loading = true
  try { const value = await api('/backup-rules'); if (alive) rules.value = value }
  catch(e) { if (alive) notify(e.message,true) }
  finally { loading = false; if(alive) timer = setTimeout(load,3000) }
}
function closeMenu() { menu.value = '' }
function escape(event) { if(event.key === 'Escape') closeMenu() }
onMounted(() => { load(); document.addEventListener('click',closeMenu); document.addEventListener('keydown',escape) })
onUnmounted(() => { alive = false; clearTimeout(timer); document.removeEventListener('click',closeMenu); document.removeEventListener('keydown',escape) })
function open(rule) {
  closeMenu(); editing.value = rule?.id || ''; error.value = ''
  Object.assign(form,rule ? JSON.parse(JSON.stringify(rule)) : {name:'',enabled:true,sourceId:'',source:'',sourceLabel:'',targetId:'',target:'',targetLabel:'',replace:'skip',extensions:'',exclude:'',minSize:0,maxSize:0,cron:''})
  modal.value = true
}
function choose(value) {
  const prefix = picker.value
  form[prefix+'Id'] = value.storageId; form[prefix] = value.source; form[prefix+'Label'] = value.sourceLabel
  picker.value = ''
}
async function save() {
  if(busy.value) return
  busy.value = true; error.value = ''
  try { await api(editing.value ? `/backup-rules/${editing.value}` : '/backup-rules',editing.value ? 'PUT' : 'POST',form); modal.value=false; await load(); notify('备份规则已保存') }
  catch(e) { error.value=e.message }
  finally { busy.value=false }
}
async function command(rule, action) {
  closeMenu()
  try { await api(`/backup-rules/${rule.id}/${action}`,'POST'); await load(); notify(action==='run'?'备份已开始':'正在停止备份') }
  catch(e) { notify(e.message,true) }
}
async function toggle(rule) {
  closeMenu()
  try { await api(`/backup-rules/${rule.id}`,'PUT',{...rule,enabled:!rule.enabled}); await load(); notify(rule.enabled?'备份规则已停用':'备份规则已启用') }
  catch(e) { notify(e.message,true) }
}
async function remove() {
  busy.value=true
  try { await api(`/backup-rules/${deleting.value.id}`,'DELETE'); deleting.value=null; await load(); notify('备份规则已删除') }
  catch(e) { notify(e.message,true) }
  finally { busy.value=false }
}
const directoryText = (id,label) => id ? `${storage(id)?.name || '存储已移除'} / ${label || '根目录'}` : ''
defineExpose({ open, load })
</script>
<template>
  <div v-if="!rules.length" class="empty-state"><Icon name="ArchiveRestore" :size="32" /><h2>暂无备份规则</h2></div>
  <div v-else class="backup-grid">
    <article v-for="rule in rules" :key="rule.id" class="backup-card" :class="{ disabled:!rule.enabled, failed:rule.status==='failed' }" @contextmenu.prevent.stop="menu=rule.id">
      <header><button class="provider-toggle" :aria-label="`${rule.enabled?'停用':'启用'}备份 ${rule.name}`" :aria-pressed="rule.enabled" :disabled="rule.status==='running'" @click="toggle(rule)"><ProviderIcon :type="storage(rule.sourceId)?.type || 'local'" /></button><button class="backup-name" :disabled="rule.status==='running'" @click="open(rule)"><strong>{{rule.name}}</strong><small>{{statuses[rule.status] || '等待执行'}}</small></button><button class="icon-btn" :aria-label="rule.status==='running'?'停止备份':'执行备份'" :disabled="!rule.enabled" @click="command(rule,rule.status==='running'?'stop':'run')"><Icon :name="rule.status==='running'?'Square':'Play'" /></button><div class="backup-menu" @click.stop><button class="icon-btn" :aria-label="`备份操作 ${rule.name}`" :aria-expanded="menu===rule.id" @click="menu=menu===rule.id?'':rule.id"><Icon name="EllipsisVertical" /></button><div v-if="menu===rule.id" class="storage-menu"><button :disabled="rule.status==='running'" @click="open(rule)"><Icon name="Pencil" />编辑规则</button><button :disabled="rule.status==='running'" @click="toggle(rule)"><Icon name="Power" />{{rule.enabled?'停用规则':'启用规则'}}</button><button class="danger-text" :disabled="rule.status==='running'" @click="closeMenu();deleting=rule"><Icon name="Trash2" />删除规则</button></div></div></header>
      <div class="backup-path" :data-tooltip="directoryText(rule.sourceId,rule.sourceLabel)"><Icon name="FolderOpen" :size="16" /><span>{{directoryText(rule.sourceId,rule.sourceLabel)}}</span></div>
      <div class="backup-path" :data-tooltip="directoryText(rule.targetId,rule.targetLabel)"><Icon name="ArchiveRestore" :size="16" /><span>{{directoryText(rule.targetId,rule.targetLabel)}}</span></div>
      <footer><span :data-tooltip="rule.message">{{rule.message || (rule.cron || '手动执行')}}</span><time>{{rule.lastRun && !rule.lastRun.startsWith('0001') ? date(rule.lastRun) : '尚未执行'}}</time></footer>
      <progress v-if="rule.status==='running'" aria-label="备份进度" :max="Math.max(1,rule.scanned)" :value="rule.copied+rule.skipped" />
    </article>
  </div>
  <Modal v-if="modal" :title="editing?'编辑备份规则':'添加备份规则'" @close="!busy && (modal=false)">
    <form @submit.prevent="save"><ThinScroll class="backup-editor modal-body" content-class="backup-fields">
      <label>备份名称<input v-model="form.name" required maxlength="200" /></label>
      <div class="field"><label>源目录</label><button class="source-trigger" type="button" aria-label="选择备份源目录" @click="picker='source'"><span>{{directoryText(form.sourceId,form.sourceLabel) || '选择源目录'}}</span><Icon name="FolderOpen" /></button></div>
      <div class="field"><label>目标目录</label><button class="source-trigger" type="button" aria-label="选择备份目标目录" @click="picker='target'"><span>{{directoryText(form.targetId,form.targetLabel) || '选择目标目录'}}</span><Icon name="FolderOpen" /></button></div>
      <div class="form-grid"><div class="field"><label>同名文件</label><RoundedSelect v-model="form.replace" label="同名文件策略" :options="policies" /></div><label>Cron 表达式<input v-model="form.cron" aria-label="Cron 表达式" /></label></div>
      <details><summary><Icon name="Settings2" :size="16" />筛选规则<Icon name="ChevronDown" :size="16" /></summary><div class="backup-filters">
        <label>包含扩展名<input v-model="form.extensions" aria-label="包含扩展名" placeholder="英文分号分隔，留空为全部" /></label>
        <label>排除名称关键词<input v-model="form.exclude" aria-label="排除名称关键词" placeholder="英文分号分隔" /></label>
        <div class="form-grid"><div class="field"><label>最小文件大小</label><NumberInput v-model="form.minSize" aria-label="最小文件大小" unit="B" min="0" /></div><div class="field"><label>最大文件大小</label><NumberInput v-model="form.maxSize" aria-label="最大文件大小" unit="B" min="0" /></div></div>
      </div></details>
      <p v-if="error" class="error-message" role="alert">{{error}}</p>
    </ThinScroll><footer class="modal-footer"><button class="btn primary" :disabled="busy || !form.sourceId || !form.targetId"><Icon name="Check" />保存规则</button><button type="button" class="btn" :disabled="busy" @click="modal=false">取消</button></footer></form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="storages" :storage="form[picker+'Id']" :initial="form[picker] || '/'" :initial-label="form[picker+'Label']" @select="choose" @close="picker=''" />
  <Modal v-if="deleting" title="删除备份规则" compact confirmation @close="deleting=null"><div class="modal-body">确认删除「{{deleting.name}}」？</div><footer class="modal-footer"><button class="btn danger" :disabled="busy" @click="remove">确认</button><button class="btn" @click="deleting=null">取消</button></footer></Modal>
</template>
<style scoped>
.backup-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
.backup-card{position:relative;padding:16px;background:var(--surface);border:1px solid var(--border);border-radius:8px;min-width:0;transition:border-color .2s}
.backup-card:hover{border-color:color-mix(in srgb,var(--primary) 30%,var(--border))}.backup-card.failed{border-color:color-mix(in srgb,var(--danger) 45%,var(--border))}.backup-card.disabled{opacity:.65}
.backup-card header{display:flex;align-items:center;gap:10px;margin-bottom:14px}.provider-toggle{flex:none}.backup-name{flex:1;min-width:0;background:none;border:0;text-align:left;color:var(--text);display:grid;gap:5px}.backup-name strong{font-size:15px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.backup-name small{font-size:12px;color:var(--muted)}
.backup-path{display:flex;gap:8px;align-items:center;margin:7px 0;font-size:13px;color:var(--muted)}.backup-path span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.backup-menu{position:relative}.backup-card footer{display:flex;gap:10px;justify-content:space-between;font-size:12px;color:var(--muted);margin-top:14px}.backup-card footer span{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}.backup-card footer time{flex:none}.backup-card progress{width:100%;height:6px;margin-top:10px;accent-color:var(--primary)}
.backup-editor{height:min(65dvh,520px)}.backup-editor :deep(.backup-fields){display:grid;gap:14px;align-content:start}.backup-editor :deep(input),.backup-editor :deep(.rounded-select-trigger),.backup-editor :deep(.source-trigger){height:36px;min-height:36px;font-size:14px}.backup-editor :deep(.form-grid){gap:14px}.backup-editor :deep(label){font-size:14px}.backup-editor :deep(summary){display:flex;align-items:center;gap:8px;color:var(--muted);cursor:pointer;font-size:14px;list-style:none}.backup-editor :deep(summary:hover){color:var(--primary)}.backup-editor :deep(summary svg:last-child){margin-left:auto}.backup-filters{display:grid;gap:14px;margin-top:14px}
@media(max-width:760px){.backup-grid{grid-template-columns:1fr}.backup-card footer{flex-wrap:wrap}.backup-editor{max-height:60dvh}}
</style>
