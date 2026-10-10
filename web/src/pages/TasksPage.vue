<script setup>
import { computed, reactive, ref, watch, onMounted, onUnmounted } from 'vue'
import { api, state, reload, notify, date, driverOf } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import Modal from '../components/Modal.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import TaskTabs from '../components/TaskTabs.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import NumberInput from '../components/NumberInput.vue'
import CacheOverview from '../components/CacheOverview.vue'
import LocalDirectoryPicker from '../components/LocalDirectoryPicker.vue'
const targetPicker = ref(false)
const resetTask = ref(null)
async function resetOutput() {
  busy.value = true
  try { await api(`/tasks/${resetTask.value.id}/reset`, 'POST', { confirm: true }); resetTask.value = null; await reload(); notify('生成库已清除，全量任务已启动') } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
const props = defineProps({ kind: { default: 'strm' } })
const taskTitle = computed(() => props.kind === 'cas' ? 'CAS' : props.kind === 'strm' ? 'STRM' : props.kind === 'ed2k' ? 'ED2K' : '缓存')
const bindingStorages = computed(() => state.storages.filter(s => s.enabled && ['mobile', 'tianyi'].includes(s.type) && s.config.mode === 'native'))
const ed2kBindings = computed(() => state.storages.filter(s => s.enabled && s.type === '115'))
const validED2KBinding = computed(() => ed2kBindings.value.some(s => s.id === form.ed2kBindingId))
const availableStorages = computed(() => state.storages.filter(s => s.enabled && (props.kind !== 'ed2k' || ['local', '115'].includes(s.type)) && (props.kind !== 'cas' || s.type === 'local' || (s.type === storage(form.casBindingId)?.type && bindingStorages.value.some(b => b.id === s.id)))))
const casStatus = ref(null), casPanel = ref(false)
async function showCAS() {
  try { casStatus.value = await api('/cas/status'); casPanel.value = true } catch (e) { notify(e.message, true) }
}
async function cleanupCAS() {
  busy.value = true
  try { const result = await api('/cas/cleanup', 'POST'); casStatus.value = await api('/cas/status'); notify(`已清理 ${result.removed} 个过期临时文件`) }
  catch (e) { notify(e.message, true) }
  finally { busy.value = false }
}
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
watch(() => form.casBindingId, () => {
  if (props.kind === 'cas' && !availableStorages.value.some(s => s.id === form.storageId)) {
    Object.assign(form, { storageId: '', source: '/', sourceLabel: '', sourceTrail: [] })
  }
})
const tasks = computed(() => state.tasks.filter(t => t.kind === props.kind))
const pending = ref(new Set())
const dragging = ref(''), dropTarget = ref(''), sorting = ref(false)
function dragStart(event, task) {
  if (sorting.value || event.target.closest('button')) { event.preventDefault(); return }
  menu.value = ''; dragging.value = task.id
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', task.id)
}
function dragEnd() { dragging.value = ''; dropTarget.value = '' }
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
  return task.message || ({ running: '正在执行', success: '执行成功', error: '执行失败', failed: '执行失败', cancelled: '已停止' }[task.status]) || '尚未执行'
}
const storage = id => state.storages.find(s => s.id === id)
function closeMenu(event) { if (!event.target.closest('.task-row-menu')) menu.value = '' }
function escapeMenu(event) { if (event.key === 'Escape') menu.value = '' }
onMounted(() => { document.addEventListener('click', closeMenu); document.addEventListener('keydown', escapeMenu) })
onUnmounted(() => { document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', escapeMenu) })
function open(t) {
  menu.value = ''
  editing.value = t?.id || ''; error.value = ''; more.value = false
  Object.keys(form).forEach(k => delete form[k])
  Object.assign(form, t ? JSON.parse(JSON.stringify(t)) : { name: '', kind: props.kind, storageId: '', source: '/', target: '', mode: 'incremental', encodePath: false, apiInterval: 200, cron: '', depth: props.kind === 'cache' ? 4 : 0, interval: 60, cacheTTL: 0, excludeDirs: '', excludeFiles: '', excludeTypes: '', enabled: true })
  if (props.kind !== 'cache') form.retainedExtensions ??= 'iso'
  if (['strm', 'cas', 'ed2k'].includes(props.kind)) form.scrapeExcluded ??= false
  if (props.kind === 'ed2k') form.ed2kBindingId ||= ''
  if (props.kind === 'cas') {
    form.casBindingId ||= t ? bindingStorages.value.find(s => s.id === form.storageId)?.id || '' : ''
    form.retentionHours ||= 12
    form.casOperation = 'generate'
  }
  modal.value = true
}
async function save() {
  if (props.kind === 'ed2k' && !validED2KBinding.value) { error.value = '请选择启用的 115 绑定存储'; return }
  busy.value = true; error.value = ''
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
  <section class="task-heading"><TaskTabs /><div class="toolbar-right"><button v-if="kind === 'cas'" class="btn" @click="showCAS"><Icon name="Database" />临时文件</button><button v-if="kind === 'cache'" class="btn" @click="$router.push('/tasks/cache/settings')"><Icon name="Settings2" />缓存设置</button><button class="btn primary" @click="open()"><Icon name="Plus" />添加任务</button></div></section>
  <CacheOverview v-if="kind === 'cache'" :tasks="tasks" />
  <div v-else class="metric-strip three"><div><span class="metric-icon"><Icon name="ListTodo" /></span><span><small>全部任务</small><strong>{{ tasks.length }}<em>项</em></strong></span></div><div><span class="metric-icon green"><Icon name="Activity" /></span><span><small>正在执行</small><strong>{{ tasks.filter(t => t.status === 'running').length }}<em>项</em></strong></span></div><div><span class="metric-icon amber"><Icon name="FileVideo" /></span><span><small>本次生成文件</small><strong>{{ tasks.reduce((n, t) => n + t.processed, 0) }}<em>条</em></strong></span></div></div>
  <div v-if="!tasks.length" class="empty-state task-empty"><span class="empty-icon"><Icon :name="kind !== 'cache' ? 'FileVideo' : 'Database'" :size="36" /></span><h3>还没有任务</h3><p>从一个存储目录开始。</p><button class="btn" @click="open()"><Icon name="Plus" />添加 {{ taskTitle }} 任务</button></div>
  <div v-else class="task-list"><article v-for="t in tasks" :key="t.id" class="task-row-card" :class="{ dragging: dragging === t.id, 'drop-target': dropTarget === t.id, running: t.status === 'running' }" :draggable="!sorting" tabindex="0" :aria-label="t.name" aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown" @keydown="reorderKey($event, t)" @dragstart="dragStart($event, t)" @dragend="dragEnd" @dragover.prevent="dragging && (dropTarget = t.id)" @dragleave.self="dropTarget = ''" @drop.prevent="moveTask(dragging, t.id)">
    <button class="task-provider-toggle" :aria-label="`${t.enabled ? '停用' : '启用'}任务 ${t.name}`" :aria-pressed="t.enabled" :disabled="t.status === 'running' || pending.has(t.id)" @click="toggle(t)"><ProviderIcon :type="storage(t.storageId)?.type" /></button>
    <div class="task-row-copy"><strong>{{ t.name }}</strong><small>{{ storage(t.storageId)?.name || '存储不可用' }}</small></div>
    <div class="task-last-scan" tabindex="0" :aria-label="`${scanTime(t.lastRun)}，${taskResult(t)}`"><time>{{ scanTime(t.lastRun) }}</time><span class="task-result">{{ taskResult(t) }}</span></div>
    <button class="icon-btn task-run" :class="{stopping:t.status === 'running'}" :aria-label="t.status === 'running' ? '停止任务' : '立即执行'" :disabled="pending.has(t.id)" @click="action(t, t.status === 'running' ? 'stop' : 'run')"><Icon :name="t.status === 'running' ? 'Square' : 'Play'" /></button>
    <div class="task-row-menu"><button class="icon-btn" :aria-label="`任务操作 ${t.name}`" @click.stop="menu = menu === t.id ? '' : t.id"><Icon name="EllipsisVertical" /></button><div v-if="menu === t.id" class="task-menu"><button v-if="kind !== 'cache'" :disabled="t.status === 'running' || pending.has(t.id)" @click="resetTask = t; menu = ''"><Icon name="RotateCcw" />全量重置</button><button :disabled="t.status === 'running' || pending.has(t.id)" @click="open(t)"><Icon name="Pencil" />编辑任务</button><button class="danger-text" :disabled="t.status === 'running' || pending.has(t.id)" @click="confirmDelete = t; menu = ''"><Icon name="Trash2" />删除任务</button></div></div>
  </article></div>
  <Modal v-if="resetTask" title="全量重置" confirmation @close="!busy && (resetTask = null)"><div class="modal-body">将清除任务「{{ resetTask.name }}」对应的生成库及其中元数据，再重新全量生成。此操作不可撤销，不删除源文件。公共根目录和重叠任务目录不能重置。</div><footer class="modal-footer"><button class="btn danger" :disabled="busy" @click="resetOutput">确认重置</button><button class="btn" :disabled="busy" @click="resetTask = null">取消</button></footer></Modal>
  <Modal v-if="modal" :title="`${editing ? '编辑' : '添加'} ${taskTitle} 任务`" compact wide @close="!busy && (modal = false)">
    <form @submit.prevent="save"><div class="modal-body"><div v-if="!availableStorages.length" class="inline-note"><Icon name="Info" />{{ kind === 'cas' ? '需要本地、原生移动或天翼个人云存储池。' : '请先添加并启用一个存储池。' }}<button type="button" class="text-btn" @click="$router.push('/storage')">前往添加</button></div>
      <div class="form-grid">
        <label>任务名称 <span class="required">*</span><input v-model="form.name" required /></label>
        <div v-if="kind === 'cas'" class="field"><label>绑定存储</label><RoundedSelect v-model="form.casBindingId" label="绑定存储" placeholder="选择移动或天翼存储" :options="bindingStorages.map(s => ({ value: s.id, label: s.name }))" /></div>
        <div v-if="kind === 'ed2k'" class="field"><label>绑定存储 <span class="required">*</span></label><RoundedSelect v-model="form.ed2kBindingId" label="绑定存储" placeholder="选择 115 存储" :options="ed2kBindings.map(s => ({ value: s.id, label: s.name }))" /></div>
        <div v-if="kind === 'strm' || kind === 'ed2k'" class="field"><label>生成方式</label><RoundedSelect v-model="form.mode" label="生成方式" :options="[{ value: 'full', label: '全量生成' }, { value: 'incremental', label: '增量生成' }]" /></div>
        <div v-if="kind === 'cache'" class="field"><label for="task-interval">执行间隔</label><NumberInput id="task-interval" v-model="form.interval" aria-label="执行间隔" unit="分钟" min="1" required /></div>
        <div class="field"><label for="task-source">源目录 <span class="required">*</span></label><button id="task-source" type="button" class="source-trigger" aria-label="选择目录" :disabled="kind === 'cas' && !form.casBindingId" @click="picker = true"><span>{{ form.storageId ? `${storage(form.storageId)?.name || '存储不可用'} · ${sourceLabel(form)}` : '选择存储池及源目录' }}</span><Icon name="FolderOpen" /></button></div>
        <div v-if="kind !== 'cache'" class="field"><label for="task-target">生成目录</label><div class="directory-input"><input id="task-target" v-model="form.target" :placeholder="`默认：${state.strmRoot}`" /><button type="button" class="icon-btn" aria-label="选择生成目录" @click="targetPicker = true"><Icon name="FolderOpen" /></button></div></div>
        <div v-else class="field"><label for="task-depth">扫描层级</label><NumberInput id="task-depth" v-model="form.depth" aria-label="扫描层级" unit="层" min="0" max="128" required /></div>
        <div v-if="kind === 'cas'" class="field"><label>生成方式</label><RoundedSelect v-model="form.mode" label="生成方式" :options="[{ value: 'full', label: '全量生成' }, { value: 'incremental', label: '增量生成' }]" /></div>
        <div class="field"><label for="task-api-interval">API 间隔</label><NumberInput id="task-api-interval" v-model="form.apiInterval" aria-label="API 间隔" unit="ms" min="0" max="60000" required /></div>
        <div v-if="kind === 'cas'" class="field"><label for="cas-retention">还原文件保留时间</label><NumberInput id="cas-retention" v-model="form.retentionHours" aria-label="还原文件保留时间" unit="h" min="1" max="8760" required /></div>
        <label v-if="kind !== 'cache'">Cron 表达式<input v-model="form.cron" /></label>
        <div v-else class="field"><label for="task-cache">缓存期</label><NumberInput id="task-cache" v-model="form.cacheTTL" aria-label="缓存期" unit="分钟" min="0" /></div>
        <label v-if="kind === 'strm' && storage(form.storageId)?.type === 'openlist'" class="toggle-line full"><span>编码路径</span><input v-model="form.encodePath" type="checkbox" role="switch" class="switch" /></label>
        <div v-for="field in kind === 'cache' ? [] : [{ key: 'mediaExtensions', id: 'task-media-extensions', label: '媒体扩展名', groups: ['video', 'audio'] }, { key: 'metadataExtensions', id: 'task-metadata-extensions', label: '元数据扩展名', groups: ['image', 'data'] }]" :key="field.key" class="field full extension-field">
          <div class="extension-heading"><label :for="field.id">{{ field.label }}</label><span><button v-for="group in field.groups" :key="group" type="button" class="icon-btn extension-preset" :class="group" :aria-label="`${{ video: '视频', audio: '音频', image: '图片', data: '数据' }[group]}扩展名`" :aria-pressed="groupActive(field.key, group)" @click="toggleExtensions(field.key, group)"><Icon :name="{ video: 'Film', audio: 'Music', image: 'Image', data: 'FileJson' }[group]" /></button></span></div>
          <input :id="field.id" v-model="form[field.key]" />
        </div>
      </div>
      <template v-if="kind !== 'cache'"><button type="button" class="disclosure" :aria-expanded="more" @click="more = !more"><Icon :name="more ? 'ChevronDown' : 'ChevronRight'" :size="16" />更多选项</button><div v-if="more" class="form-grid more-options">
        <div class="field extension-field"><div class="extension-heading"><label for="task-retained-extensions">保留扩展名</label><span><button v-for="group in ['video', 'audio']" :key="group" type="button" class="icon-btn extension-preset" :class="group" :aria-label="`保留${group === 'video' ? '视频' : '音频'}扩展名`" :aria-pressed="groupActive('retainedExtensions', group)" @click="toggleExtensions('retainedExtensions', group)"><Icon :name="group === 'video' ? 'Film' : 'Music'" /></button></span></div><input id="task-retained-extensions" v-model="form.retainedExtensions" /></div>
        <div v-if="['strm', 'cas', 'ed2k'].includes(kind)" class="field extension-field"><div class="extension-heading"><label>STRM 刮削</label></div><RoundedSelect :model-value="form.scrapeExcluded ? 'exclude' : 'include'" @update:model-value="form.scrapeExcluded = $event === 'exclude'" label="STRM 刮削" :options="[{value:'include',label:'参与'},{value:'exclude',label:'不参与'}]" /></div>
        <label>排除目录<input v-model="form.excludeDirs" placeholder="回收站;预告片" /></label><label>排除文件<input v-model="form.excludeFiles" placeholder="sample;trailer" /></label><label>排除类型<input v-model="form.excludeTypes" placeholder="iso;avi" /></label><div class="field"><label for="strm-cache">缓存期</label><NumberInput id="strm-cache" v-model="form.cacheTTL" aria-label="缓存期" unit="分钟" min="0" /></div></div></template>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="modal = false">取消</button><button class="btn primary" :disabled="busy || !form.storageId || (kind === 'cas' && !form.casBindingId) || (kind === 'ed2k' && !validED2KBinding)"><Icon name="Check" />{{ busy ? '保存中…' : '保存任务' }}</button></footer></form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="availableStorages" :storage="form.storageId" :initial="form.source" :initial-label="form.sourceLabel" :initial-trail="form.sourceTrail" @select="selectSource" @close="picker = false" />
  <LocalDirectoryPicker v-if="targetPicker" :initial="form.target" @close="targetPicker = false" @select="form.target = $event; targetPicker = false" />
  <Modal v-if="casPanel" title="CAS 临时文件" wide @close="casPanel = false"><div class="modal-body"><p>按任务保留时间清理，默认 {{ casStatus.defaultRetentionHours }} 小时；播放中的文件不清理。仅处理以太记录的临时文件，遵循存储池删除模式。</p><p v-if="!casStatus.items.length" class="small-empty">暂无临时文件</p><div v-for="item in casStatus.items" :key="item.storageId + item.name" class="settings-row"><span>{{ item.name }}<small>保留 {{ item.retentionHours }} 小时 · 到期 {{ date(item.expiresAt) }}</small></span><span>{{ item.active ? '播放中' : '等待过期' }}</span></div></div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="showCAS"><Icon name="RefreshCw" />刷新</button><button class="btn primary" :disabled="busy" @click="cleanupCAS"><Icon name="Trash2" />清理过期项</button></footer></Modal>
  <Modal v-if="confirmDelete" title="删除任务" @close="confirmDelete = null"><div class="modal-body">确认删除「{{ confirmDelete.name }}」？已生成的文件不会删除。</div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除任务</button></footer></Modal>
</template>
<style scoped>
.extension-heading { display: flex; align-items: center; gap: 12px; min-height: 32px; }
.extension-heading label { margin: 0; }
.extension-heading > span { display: flex; gap: 4px; margin-left: auto; }
.extension-field .extension-preset { color: var(--muted); }
.extension-field .extension-preset[aria-pressed=true], .extension-field .extension-preset:hover { background: transparent; }
.extension-field .extension-preset[aria-pressed=true] { color: var(--preset-color); }
.extension-preset.video { --preset-color: #252a34; }
.extension-preset.video[aria-pressed=true] :deep(svg) { fill:#252a3414; filter:drop-shadow(0 -1px 0 #dce5f1); }
.extension-preset.audio { --preset-color: #c53849; }
.extension-preset.image { --preset-color: #21834f; }
.extension-preset.data { --preset-color: #246bc1; }
[data-theme=dark] .extension-preset.video { --preset-color: #d0d5df; }
[data-theme=dark] .extension-preset.audio { --preset-color: #ff8b98; }
[data-theme=dark] .extension-preset.image { --preset-color: #71d69a; }
[data-theme=dark] .extension-preset.data { --preset-color: #7dbaff; }
.extension-preset { position: relative; }
.extension-preset::after { content: attr(aria-label); position: absolute; bottom: calc(100% + 6px); right: 0; padding: 5px 8px; border: 1px solid var(--border); border-radius: 4px; background: var(--surface); color: var(--text); font-size: 12px; white-space: nowrap; pointer-events: none; opacity: 0; z-index: 2; }
.extension-preset:hover::after, .extension-preset:focus-visible::after { opacity: 1; transition: opacity .15s .6s; }
</style>
