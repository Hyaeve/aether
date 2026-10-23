<script setup>
import { computed, reactive, ref, onMounted, onUnmounted } from 'vue'
import { api, state, reload, notify, date, driverOf } from '../lib'
import Icon from '../components/Icon.vue'
import ExtensionPresets from '../components/ExtensionPresets.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import Modal from '../components/Modal.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import TaskTabs from '../components/TaskTabs.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import NumberInput from '../components/NumberInput.vue'
import CacheOverview from '../components/CacheOverview.vue'
import CASOverview from '../components/CASOverview.vue'
import LocalDirectoryPicker from '../components/LocalDirectoryPicker.vue'
import { useCardDrag } from '../useCardDrag'
import { revealOptions } from '../expanded-options'
import GenerationImport from '../components/GenerationImport.vue'
import {extensionError} from '../extension-rules'
const importer=ref(false),generationJobs=ref([])
let generationTimer,alive=true
async function loadGeneration(){if(!['cas','ed2k'].includes(props.kind))return;try{const result=await api('/generation/jobs');if(alive)generationJobs.value=Array.isArray(result)?result.filter(j=>j.kind===props.kind):[]}catch(e){if(alive)notify(e.message,true)}}
const targetPicker = ref(false)
const resetTask = ref(null)
async function resetOutput() {
  busy.value = true
  try { await api(`/tasks/${resetTask.value.id}/reset`, 'POST', { confirm: true }); resetTask.value = null; await reload(); notify('生成库已清除，全量任务已启动') } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
const props = defineProps({ kind: { default: 'strm' } })
const taskTitle = computed(() => props.kind === 'cas' ? 'CAS' : props.kind === 'strm' ? 'STRM' : props.kind === 'ed2k' ? 'ED2K' : '缓存')
const ed2kBindings = computed(() => state.storages.filter(s => s.enabled && s.type === '115'))
const validED2KBinding = computed(() => ed2kBindings.value.some(s => s.id === form.ed2kBindingId))
const availableStorages = computed(() => state.storages.filter(s => s.enabled && (props.kind !== 'ed2k' || ['local', '115'].includes(s.type))))
const modal = ref(false), busy = ref(false), error = ref(''), editing = ref(''), more = ref(false), picker = ref(false), confirmDelete = ref(null), menu = ref('')
const form = reactive({})
const extensionGroups = {
  video: 'mp4;mkv;avi;mov;wmv;flv;webm;m4v;ts;m2ts;mkvb;rm;3gp;iso',
  audio: 'mp3;flac;wav;aac;m4a;ogg;wma;ape;alac;opus',
  image: 'jpg;jpeg;png;webp', data: 'nfo;ass;srt'
}
const extensionTokens = value => [...new Set((value || '').toLowerCase().split(';').map(v => v.trim().replace(/^\./, '')).filter(Boolean))]
function groupActive(field, group) { return extensionGroups[group].split(';').every(v => extensionTokens(form[field]).includes(v)) }
function toggleExtensions(field, group) {
  const set = new Set(extensionTokens(form[field])), values = extensionGroups[group].split(';'), remove = groupActive(field, group)
  values.forEach(v => remove ? set.delete(v) : set.add(v))
  form[field] = [...set].join(';')
}
const tasks = computed(() => state.tasks.filter(t => t.kind === props.kind))
const pending = ref(new Set())
const sorting = ref(false)
const { armed, dragging, dropTarget, hold, cardClick, cancel: dragEnd } = useCardDrag({ selector: '.task-row-card', disabled: () => sorting.value, onDrop: moveTask, onStart: () => { menu.value = '' } })
function editCard(task) { if (task.status !== 'running' && !pending.value.has(task.id)) open(task) }
function toggleMore(event) {
  more.value = !more.value
  if (more.value) revealOptions(event.currentTarget)
}
async function moveTask(id, target) {
  dragEnd()
  if (!id || !target || id === target || sorting.value) return
  sorting.value = true
  try { await api('/tasks/reorder', 'POST', { id, target }); await reload() }
  catch (e) { notify(e.message, true) }
  finally { sorting.value = false }
}
function moveBy(task, direction) {
  const index = tasks.value.findIndex(t => t.id === task.id)
  const target = tasks.value[index + direction]
  menu.value = ''
  if (target) moveTask(task.id, target.id)
}
function reorderKey(event, task) {
  if (event.target === event.currentTarget && event.key === 'Enter') { editCard(task); return }
  if (event.target !== event.currentTarget || !event.altKey || !['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) return
  event.preventDefault()
  moveBy(task, ['ArrowUp', 'ArrowLeft'].includes(event.key) ? -1 : 1)
}
function scanTime(value) {
  if (!value || value.startsWith('0001-')) return '尚未扫描'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '尚未扫描'
  const pad = n => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
function taskResult(task) {
	if (task.healthPause) return task.healthPause
  return task.message || ({ running: '正在执行', success: '执行成功', error: '执行失败', failed: '执行失败', cancelled: '已停止' }[task.status]) || '尚未执行'
}
const storage = id => state.storages.find(s => s.id === id)
function closeMenu(event) { if (!event.target.closest('.task-row-menu')) menu.value = '' }
function escapeMenu(event) { if (event.key === 'Escape') menu.value = '' }
onMounted(() => { document.addEventListener('click', closeMenu); document.addEventListener('keydown', escapeMenu);loadGeneration();generationTimer=setInterval(loadGeneration,1500) })
onUnmounted(() => { alive=false;clearInterval(generationTimer);document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', escapeMenu) })
function open(t) {
  menu.value = ''
  editing.value = t?.id || ''; error.value = ''; more.value = false
  Object.keys(form).forEach(k => delete form[k])
  Object.assign(form, t ? JSON.parse(JSON.stringify(t)) : { name: '', kind: props.kind, storageId: '', source: '/', target: '', mode: 'incremental', encodePath: false, apiInterval: 200, cron: '', depth: props.kind === 'cache' ? 4 : 0, interval: 60, cacheTTL: 0, excludeDirs: '', excludeFiles: '', excludeTypes: '', enabled: true })
  if (props.kind === 'strm') form.retainedExtensions ??= 'iso'
  if (['cas', 'ed2k'].includes(props.kind)) delete form.retainedExtensions
  if (['cas', 'ed2k'].includes(props.kind)) form.completionAction ||= 'keep'
  if (['strm', 'cas', 'ed2k'].includes(props.kind)) form.scrapeExcluded ??= !t && props.kind === 'cas'
  if (props.kind === 'ed2k') form.ed2kBindingId ||= ''
  if (props.kind === 'cas') {
    delete form.casBindingId
    delete form.retentionHours
    form.casOperation = 'generate'
  }
  modal.value = true
}
async function save() {
  busy.value = true; error.value = ''
  for (const [key,label] of [['mediaExtensions','媒体扩展名'],['metadataExtensions','元数据扩展名']]) { const invalid=extensionError(label,form[key]); if(invalid){ notify(invalid,true); busy.value=false; return } }
  try { await api(editing.value ? `/tasks/${editing.value}` : '/tasks', editing.value ? 'PUT' : 'POST', form); await reload(); modal.value = false; notify('任务已保存') }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
function selectSource(value) {
  Object.assign(form, value)
  if (props.kind === 'cas' && storage(value.storageId)?.type === 'local') form.casOperation = 'generate'
  picker.value = false
}
function sourceLabel(task) { return task.sourceLabel || (task.source === '/' ? '根目录' : '已选目录') }
async function action(t, action) {
  menu.value = ''
  if (pending.value.has(t.id)) return
  pending.value.add(t.id)
  try { await api(`/tasks/${t.id}/${action}`, 'POST'); await reload(); notify(action === 'run' ? '任务已启动' : '正在停止任务') } catch (e) { notify(e.message, true) }
  finally { pending.value.delete(t.id) }
}
async function remove() {
  busy.value = true
  try { await api(`/tasks/${confirmDelete.value.id}`, 'DELETE'); confirmDelete.value = null; await reload(); notify('任务已删除') } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
async function toggle(t) {
  if (pending.value.has(t.id)) return
  pending.value.add(t.id)
  const enabled = !t.enabled
  try { await api(`/tasks/${t.id}`, 'PUT', { ...t, enabled }); await reload(); notify(`已${enabled ? '启用' : '停用'}任务 ${t.name}`) } catch (e) { notify(e.message, true) }
  finally { pending.value.delete(t.id) }
}
</script>
<template>
  <section class="task-heading"><TaskTabs /><div class="toolbar-right"><button class="btn primary" @click="['cas','ed2k'].includes(kind)?importer=true:open()"><Icon name="Plus" />添加任务</button></div></section>
  <CacheOverview v-if="kind === 'cache'" :tasks="tasks" />
  <CASOverview v-else-if="['cas','ed2k'].includes(kind)" :kind="kind" :tasks="tasks" :jobs="generationJobs" />
  <div v-else class="metric-strip three"><div><span class="metric-icon"><Icon name="ListTodo" /></span><span><small>全部任务</small><strong>{{ tasks.length }}<em>项</em></strong></span></div><div><span class="metric-icon green"><Icon name="Activity" /></span><span><small>正在执行</small><strong>{{ tasks.filter(t => t.status === 'running').length }}<em>项</em></strong></span></div><div><span class="metric-icon amber"><Icon name="FileVideo" /></span><span><small>本次生成文件</small><strong>{{ tasks.reduce((n, t) => n + t.processed, 0) }}<em>条</em></strong></span></div></div>
  <div class="task-list"><article v-for="t in tasks" :key="t.id" class="task-row-card" :data-id="t.id" :class="{ 'drag-armed': armed === t.id, dragging: dragging === t.id, 'drop-target': dropTarget === t.id, running: t.status === 'running' && !t.healthPause, 'health-paused': !!t.healthPause }" :draggable="false" tabindex="0" :aria-label="t.name" aria-keyshortcuts="Enter Alt+ArrowUp Alt+ArrowDown" @keydown="reorderKey($event, t)" @pointerdown="hold($event, t)" @click="cardClick($event, t, editCard)" @dragstart.prevent>
    <button class="task-provider-toggle" :aria-label="`${t.enabled ? '停用' : '启用'}任务 ${t.name}`" :aria-pressed="t.enabled" :disabled="t.status === 'running' || pending.has(t.id)" @click="toggle(t)"><ProviderIcon :type="storage(t.storageId)?.type" /></button>
    <div class="task-row-copy"><strong>{{ t.name }}</strong><small>{{ storage(t.storageId)?.name || '存储不可用' }}</small></div>
    <div class="task-last-scan" tabindex="0" :aria-label="`最近执行：${scanTime(t.lastRun)}，${taskResult(t)}`"><time>最近执行：{{ scanTime(t.lastRun) }}</time><span class="task-result">{{ taskResult(t) }}</span></div>
    <button class="icon-btn task-run" :class="{stopping:t.status === 'running'}" :aria-label="t.healthPause ? '健康异常，暂时停止' : t.status === 'running' ? '停止任务' : '立即执行'" :disabled="pending.has(t.id) || !!t.healthPause" @click="action(t, t.status === 'running' ? 'stop' : 'run')"><Icon :name="t.healthPause ? 'Pause' : t.status === 'running' ? 'Square' : 'Play'" /></button>
    <div class="task-row-menu"><button class="icon-btn" :aria-label="`任务操作 ${t.name}`" @click.stop="menu = menu === t.id ? '' : t.id"><Icon name="EllipsisVertical" /></button><div v-if="menu === t.id" class="task-menu"><button :disabled="t.status === 'running' || pending.has(t.id)" @click="open(t)"><Icon name="Pencil" />编辑任务</button><button v-if="kind !== 'cache'" :disabled="t.status === 'running' || pending.has(t.id)" @click="resetTask = t; menu = ''"><Icon name="RotateCcw" />全量重置</button><button class="danger-text" :disabled="t.status === 'running' || pending.has(t.id)" @click="confirmDelete = t; menu = ''"><Icon name="Trash2" />删除任务</button></div></div>
  </article><button class="add-storage-tile add-task-tile" :aria-label="`添加 ${taskTitle} 任务卡片`" @click="open()"><Icon name="Plus" :size="25" /><strong>添加 {{taskTitle}} 任务</strong></button></div>
  <section v-if="generationJobs.length" class="generation-job-list" aria-label="解析生成队列"><article v-for="job in generationJobs" :key="job.id" :class="{running:job.status==='running',failed:job.status==='error'}"><div><strong>{{job.name}}</strong><small>{{job.processed}} / {{job.total}} · {{job.status==='queued'?'排队中':job.message}}</small></div><progress :value="job.processed" :max="job.total||1" :aria-label="`${job.name}生成进度`" /></article></section>
  <GenerationImport v-if="importer" :kind="kind" @close="importer=false" @queued="loadGeneration" />
  <Modal v-if="resetTask" title="全量重置" confirmation @close="!busy && (resetTask = null)"><div class="modal-body">将清除任务「{{ resetTask.name }}」对应的生成库及其中元数据，再重新全量生成。此操作不可撤销，不删除源文件。公共根目录和重叠任务目录不能重置。</div><footer class="modal-footer"><button class="btn danger" :disabled="busy" @click="resetOutput">确认重置</button><button class="btn" :disabled="busy" @click="resetTask = null">取消</button></footer></Modal>
  <Modal v-if="modal" :title="`${editing ? '编辑' : '添加'} ${taskTitle} 任务`" compact wide @close="!busy && (modal = false)">
    <form @submit.prevent="save"><div class="modal-body"><div v-if="!availableStorages.length" class="inline-note"><Icon name="Info" />请先添加并启用一个存储池。<button type="button" class="text-btn" @click="$router.push('/storage')">前往添加</button></div>
      <div class="form-grid">
        <label>任务名称 <span class="required">*</span><input v-model="form.name" required /></label>
        <div v-if="kind === 'cas'" class="field"><label>生成方式</label><RoundedSelect v-model="form.mode" label="生成方式" :options="[{ value: 'full', label: '全量生成' }, { value: 'incremental', label: '增量生成' }]" /></div>
        <div v-if="kind === 'strm' || kind === 'ed2k'" class="field"><label>生成方式</label><RoundedSelect v-model="form.mode" label="生成方式" :options="[{ value: 'full', label: '全量生成' }, { value: 'incremental', label: '增量生成' }]" /></div>
        <div v-if="kind === 'cache'" class="field"><label for="task-interval">执行间隔</label><NumberInput id="task-interval" v-model="form.interval" aria-label="执行间隔" unit="分钟" min="1" required /></div>
        <div class="field"><label for="task-source">源目录 <span class="required">*</span></label><button id="task-source" type="button" class="source-trigger" aria-label="选择目录" @click="picker = true"><span>{{ form.storageId ? `${storage(form.storageId)?.name || '存储不可用'} · ${sourceLabel(form)}` : '' }}</span><Icon name="FolderOpen" /></button></div>
        <div v-if="kind !== 'cache'" class="field"><label for="task-target">生成目录</label><div class="directory-input"><input id="task-target" v-model="form.target" :placeholder="`默认：${state.strmRoot}`" @keydown.enter.prevent /><button type="button" class="icon-btn" aria-label="选择生成目录" @click="targetPicker = true"><Icon name="FolderOpen" /></button></div></div>
        <div v-else class="field"><label for="task-depth">扫描层级</label><NumberInput id="task-depth" v-model="form.depth" aria-label="扫描层级" unit="层" min="0" max="128" required /></div>
        <div class="field"><label for="task-api-interval">API 间隔</label><NumberInput id="task-api-interval" v-model="form.apiInterval" aria-label="API 间隔" unit="ms" min="0" max="60000" required /></div>
        <label v-if="kind !== 'cache'">Cron 表达式<input v-model="form.cron" /></label>
        <div v-else class="field"><label for="task-cache">缓存期</label><NumberInput id="task-cache" v-model="form.cacheTTL" aria-label="缓存期" unit="分钟" min="0" /></div>
        <label v-if="kind === 'strm' && storage(form.storageId)?.type === 'openlist'" class="toggle-line full"><span>编码路径</span><input v-model="form.encodePath" type="checkbox" role="switch" class="switch" /></label>
        <div v-for="field in kind === 'cache' ? [] : [{ key: 'mediaExtensions', id: 'task-media-extensions', label: '媒体扩展名', groups: ['video', 'audio', 'comic'] }, { key: 'metadataExtensions', id: 'task-metadata-extensions', label: '元数据扩展名', groups: ['image', 'data'] }]" :key="field.key" class="field full extension-field">
          <div class="extension-heading"><label :for="field.id">{{ field.label }}</label><ExtensionPresets v-model="form[field.key]" :groups="field.groups" /></div>
          <input :id="field.id" v-model="form[field.key]" />
        </div>
      </div>
      <template v-if="kind !== 'cache'"><button type="button" class="disclosure" :aria-expanded="more" @click="toggleMore"><Icon :name="more ? 'ChevronDown' : 'ChevronRight'" :size="16" />更多选项</button><div v-if="more" class="form-grid more-options">
        <div v-if="['cas', 'ed2k'].includes(kind)" class="field extension-field"><div class="extension-heading"><label>完成动作</label></div><RoundedSelect v-model="form.completionAction" label="完成动作" :options="[{value:'keep',label:'保留源文件'},{value:'delete',label:'删除源文件'}]" /></div>
        <div v-if="kind === 'strm'" class="field extension-field"><div class="extension-heading"><label for="task-retained-extensions">保留扩展名</label><span><button v-for="group in ['video', 'audio']" :key="group" type="button" class="icon-btn extension-preset" :class="group" :aria-label="`保留${group === 'video' ? '视频' : '音频'}扩展名`" :aria-pressed="groupActive('retainedExtensions', group)" @click="toggleExtensions('retainedExtensions', group)"><Icon :name="group === 'video' ? 'Film' : 'Music'" /></button></span></div><input id="task-retained-extensions" v-model="form.retainedExtensions" /></div>
        <div v-if="['strm', 'cas', 'ed2k'].includes(kind)" class="field extension-field"><div class="extension-heading"><label>STRM 刮削</label></div><RoundedSelect :model-value="form.scrapeExcluded ? 'exclude' : 'include'" @update:model-value="form.scrapeExcluded = $event === 'exclude'" label="STRM 刮削" :options="[{value:'include',label:'参与'},{value:'exclude',label:'不参与'}]" /></div>
        <label>排除目录<input v-model="form.excludeDirs" placeholder="回收站;预告片" /></label><label>排除文件<input v-model="form.excludeFiles" placeholder="sample;trailer" /></label><label>排除类型<input v-model="form.excludeTypes" placeholder="iso;avi" /></label><div class="field"><label for="strm-cache">缓存期</label><NumberInput id="strm-cache" v-model="form.cacheTTL" aria-label="缓存期" unit="分钟" min="0" /></div></div></template>
      <p v-if="form.completionAction === 'delete'" class="inline-note">将删除已成功生成的媒体、已复制的元数据及其他未纳入扩展名范围的文件，只清理本地空子目录，远程空目录保留；未生成、未复制或执行期间新增及变化的文件保留。源目录不能是存储池根目录。</p><p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="modal = false">取消</button><button class="btn primary" :disabled="busy || !form.storageId"><Icon name="Check" />{{ busy ? '保存中…' : '保存任务' }}</button></footer></form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="availableStorages" :storage="form.storageId" :initial="form.source" :initial-label="form.sourceLabel" :initial-trail="form.sourceTrail" @select="selectSource" @close="picker = false" />
  <LocalDirectoryPicker v-if="targetPicker" :initial="form.target" @close="targetPicker = false" @select="form.target = $event; targetPicker = false" />
  <Modal v-if="confirmDelete" title="删除任务" @close="confirmDelete = null"><div class="modal-body">确认删除「{{ confirmDelete.name }}」？已生成的文件不会删除。</div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除任务</button></footer></Modal>
</template>
<style scoped>
.task-row-card.health-paused{border-color:var(--red);background:color-mix(in srgb,var(--red) 4%,var(--surface))}.health-paused .task-result{color:var(--red)}
.health-paused .task-last-scan time{visibility:hidden}.health-paused .task-last-scan .task-result{visibility:visible}
.add-task-tile{min-height:72px;align-self:stretch;flex-direction:row;border-radius:8px;padding:12px 16px}.add-task-tile strong{font-size:13px;font-weight:500}@media(max-width:480px){.add-task-tile{min-height:98px}}.generation-job-list{display:grid;gap:10px;margin-top:16px}.generation-job-list article{padding:14px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}.generation-job-list article.running{border-color:var(--green)}.generation-job-list article.failed{border-color:var(--red)}.generation-job-list article>div{display:flex;justify-content:space-between;gap:16px}.generation-job-list strong{font-size:14px}.generation-job-list small{color:var(--muted);font-size:12px;overflow-wrap:anywhere}.generation-job-list progress{width:100%;height:6px;margin-top:10px;accent-color:var(--primary)}
.extension-heading { display: flex; align-items: center; gap: 12px; min-height: 32px; }
.extension-heading label { margin: 0; }
.extension-heading > span { display: flex; gap: 4px; margin-left: auto; }
</style>
