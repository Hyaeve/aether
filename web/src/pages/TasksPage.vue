<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { api, state, reload, notify, date, driverOf } from '../lib'
import Icon from '../components/Icon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import Modal from '../components/Modal.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import TaskTabs from '../components/TaskTabs.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import NumberInput from '../components/NumberInput.vue'
const props = defineProps({ kind: { default: 'strm' } })
const taskTitle = computed(() => props.kind === 'cas' ? 'CAS' : props.kind === 'strm' ? 'STRM' : props.kind === 'ed2k' ? 'ED2K' : '缓存')
const bindingStorages = computed(() => state.storages.filter(s => s.enabled && ['mobile', 'tianyi'].includes(s.type) && s.config.mode === 'native'))
const availableStorages = computed(() => state.storages.filter(s => s.enabled && (props.kind !== 'ed2k' || s.type === 'local') && (props.kind !== 'cas' || s.type === 'local' || (s.type === storage(form.casBindingId)?.type && bindingStorages.value.some(b => b.id === s.id)))))
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
const modal = ref(false), busy = ref(false), error = ref(''), editing = ref(''), more = ref(false), picker = ref(false), confirmDelete = ref(null)
const form = reactive({})
watch(() => form.casBindingId, () => {
  if (props.kind === 'cas' && !availableStorages.value.some(s => s.id === form.storageId)) {
    Object.assign(form, { storageId: '', source: '/', sourceLabel: '', sourceTrail: [] })
  }
})
const tasks = computed(() => state.tasks.filter(t => t.kind === props.kind))
const storage = id => state.storages.find(s => s.id === id)
const labels = { idle: '等待执行', running: '执行中', success: '已完成', error: '执行失败', cancelled: '已停止', interrupted: '已中断' }
function open(t) {
  editing.value = t?.id || ''; error.value = ''; more.value = false
  Object.keys(form).forEach(k => delete form[k])
  Object.assign(form, t ? JSON.parse(JSON.stringify(t)) : { name: '', kind: props.kind, storageId: props.kind === 'cas' ? '' : availableStorages.value[0]?.id || '', source: '/', target: '', mode: 'incremental', apiInterval: 200, cron: '', depth: 0, interval: 60, cacheTTL: 0, excludeDirs: '', excludeFiles: '', excludeTypes: '', enabled: true })
  if (props.kind === 'cas') {
    form.casBindingId ||= t ? bindingStorages.value.find(s => s.id === form.storageId)?.id || '' : ''
    form.retentionHours ||= 12
    form.casOperation = 'generate'
  }
  modal.value = true
}
async function save() {
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
  try { await api(`/tasks/${t.id}/${action}`, 'POST'); await reload(); notify(action === 'run' ? '任务已启动' : '正在停止任务') } catch (e) { notify(e.message, true) }
}
async function remove() {
  busy.value = true
  try { await api(`/tasks/${confirmDelete.value.id}`, 'DELETE'); confirmDelete.value = null; await reload(); notify('任务已删除') } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
async function toggle(t) {
  try { await api(`/tasks/${t.id}`, 'PUT', { ...t, enabled: !t.enabled }); await reload() } catch (e) { notify(e.message, true) }
}
</script>
<template>
  <section class="task-heading"><TaskTabs /><div class="toolbar-right"><button v-if="kind === 'cas'" class="btn" @click="showCAS"><Icon name="Database" />临时文件</button><button v-if="kind === 'cache'" class="btn" @click="$router.push('/tasks/cache/settings')"><Icon name="Settings2" />缓存设置</button><button class="btn primary" @click="open()"><Icon name="Plus" />添加任务</button></div></section>
  <div class="metric-strip three"><div><span class="metric-icon"><Icon name="ListTodo" /></span><span><small>全部任务</small><strong>{{ tasks.length }}<em>项</em></strong></span></div><div><span class="metric-icon green"><Icon name="Activity" /></span><span><small>正在执行</small><strong>{{ tasks.filter(t => t.status === 'running').length }}<em>项</em></strong></span></div><div><span class="metric-icon amber"><Icon :name="kind !== 'cache' ? 'FileVideo' : 'Database'" /></span><span><small>{{ kind !== 'cache' ? '本次生成文件' : '缓存条目' }}</small><strong>{{ kind !== 'cache' ? tasks.reduce((n, t) => n + t.processed, 0) : state.cache.entries || 0 }}<em>条</em></strong></span></div></div>
  <div v-if="!tasks.length" class="empty-state"><span class="empty-icon"><Icon :name="kind !== 'cache' ? 'FileVideo' : 'Database'" :size="36" /></span><h3>还没有任务</h3><p>从一个存储目录开始。</p><button class="btn" @click="open()"><Icon name="Plus" />添加 {{ taskTitle }} 任务</button></div>
  <div v-else class="table-wrap"><table><thead><tr><th>任务名称</th><th>存储 / 源目录</th><th>{{ kind !== 'cache' ? '生成方式' : '扫描层级' }}</th><th>状态</th><th>上次执行</th><th>调度</th><th class="right">操作</th></tr></thead><tbody><tr v-for="t in tasks" :key="t.id"><td><strong>{{ t.name }}</strong><small>{{ t.message }}</small></td><td><div class="inline"><ProviderIcon :type="storage(t.storageId)?.type" small /><span>{{ storage(t.storageId)?.name || '存储不可用' }}<small>{{ sourceLabel(t) }}</small></span></div></td><td>{{ kind !== 'cache' ? t.mode === 'full' ? '全量' : '增量' : t.depth || '全部' }}<small>{{ kind !== 'cache' ? t.target : `${t.interval} 分钟 / 次` }}</small></td><td><span class="status" :class="t.status === 'success' ? 'success' : t.status === 'error' ? 'danger' : 'pending'"><i />{{ labels[t.status] || t.status }}</span></td><td>{{ date(t.lastRun) }}<small v-if="!t.nextRun?.startsWith('0001')">下次 {{ date(t.nextRun) }}</small></td><td><input type="checkbox" class="switch" role="switch" :checked="t.enabled" :disabled="t.status === 'running'" aria-label="启用任务调度" @change="toggle(t)" /></td><td><div class="row-actions"><button class="icon-btn" :title="t.status === 'running' ? '停止' : '立即执行'" :aria-label="t.status === 'running' ? '停止' : '立即执行'" @click="action(t, t.status === 'running' ? 'stop' : 'run')"><Icon :name="t.status === 'running' ? 'Square' : 'Play'" :size="17" /></button><button class="icon-btn" title="编辑任务" aria-label="编辑任务" :disabled="t.status === 'running'" @click="open(t)"><Icon name="Pencil" :size="16" /></button><button class="icon-btn danger-text" title="删除任务" aria-label="删除任务" :disabled="t.status === 'running'" @click="confirmDelete = t"><Icon name="Trash2" :size="16" /></button></div></td></tr></tbody></table></div>
  <Modal v-if="modal" :title="`${editing ? '编辑' : '添加'} ${taskTitle} 任务`" compact wide @close="!busy && (modal = false)">
    <form @submit.prevent="save"><div class="modal-body"><div v-if="!availableStorages.length" class="inline-note"><Icon name="Info" />{{ kind === 'cas' ? '需要本地、原生移动或天翼个人云存储池。' : '请先添加并启用一个存储池。' }}<button type="button" class="text-btn" @click="$router.push('/storage')">前往添加</button></div>
      <div class="form-grid">
        <label>任务名称 <span class="required">*</span><input v-model="form.name" required /></label>
        <div v-if="kind === 'cas'" class="field"><label>绑定存储</label><RoundedSelect v-model="form.casBindingId" label="绑定存储" placeholder="选择移动或天翼存储" :options="bindingStorages.map(s => ({ value: s.id, label: s.name }))" /></div>
        <div v-if="kind === 'strm' || kind === 'ed2k'" class="field"><label>生成方式</label><RoundedSelect v-model="form.mode" label="生成方式" :options="[{ value: 'full', label: '全量生成' }, { value: 'incremental', label: '增量生成' }]" /></div>
        <div v-if="kind === 'cache'" class="field"><label for="task-interval">执行间隔</label><NumberInput id="task-interval" v-model="form.interval" aria-label="执行间隔" unit="分钟" min="1" required /></div>
        <div class="field"><label for="task-source">源目录 <span class="required">*</span></label><button id="task-source" type="button" class="source-trigger" aria-label="选择目录" :disabled="kind === 'cas' && !form.casBindingId" @click="picker = true"><span>{{ form.storageId ? `${storage(form.storageId)?.name || '存储不可用'} · ${sourceLabel(form)}` : '选择存储池及源目录' }}</span><Icon name="FolderOpen" /></button></div>
        <label v-if="kind !== 'cache'">生成目录<input v-model="form.target" :placeholder="`默认：${state.strmRoot}`" /></label>
        <div v-else class="field"><label for="task-depth">扫描层级</label><NumberInput id="task-depth" v-model="form.depth" aria-label="扫描层级" unit="层" min="0" max="128" required /></div>
        <div v-if="kind === 'cas'" class="field"><label>生成方式</label><RoundedSelect v-model="form.mode" label="生成方式" :options="[{ value: 'full', label: '全量生成' }, { value: 'incremental', label: '增量生成' }]" /></div>
        <div class="field"><label for="task-api-interval">API 间隔</label><NumberInput id="task-api-interval" v-model="form.apiInterval" aria-label="API 间隔" unit="ms" min="0" max="60000" required /></div>
        <div v-if="kind === 'cas'" class="field"><label for="cas-retention">还原文件保留时间</label><NumberInput id="cas-retention" v-model="form.retentionHours" aria-label="还原文件保留时间" unit="h" min="1" max="8760" required /></div>
        <label v-if="kind !== 'cache'">Cron 表达式<input v-model="form.cron" placeholder="0 2 * * *" /></label>
        <div v-else class="field"><label for="task-cache">缓存有效期</label><NumberInput id="task-cache" v-model="form.cacheTTL" aria-label="缓存有效期" unit="分钟" min="0" /></div>
        <label class="toggle-line full"><span>启用定时调度</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" /></label>
      </div>
      <template v-if="kind !== 'cache'"><button type="button" class="disclosure" :aria-expanded="more" @click="more = !more"><Icon :name="more ? 'ChevronDown' : 'ChevronRight'" :size="16" />更多选项</button><div v-if="more" class="form-grid more-options"><label>排除目录<input v-model="form.excludeDirs" placeholder="回收站;预告片" /></label><label>排除文件<input v-model="form.excludeFiles" placeholder="sample;trailer" /></label><label>排除类型<input v-model="form.excludeTypes" placeholder="iso;avi" /></label><div class="field"><label for="strm-cache">缓存有效期</label><NumberInput id="strm-cache" v-model="form.cacheTTL" aria-label="缓存有效期" unit="分钟" min="0" /></div></div></template>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="modal = false">取消</button><button class="btn primary" :disabled="busy || !form.storageId || (kind === 'cas' && !form.casBindingId)"><Icon name="Check" />{{ busy ? '保存中…' : '保存任务' }}</button></footer></form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="availableStorages" :storage="form.storageId" :initial="form.source" :initial-label="form.sourceLabel" :initial-trail="form.sourceTrail" @select="selectSource" @close="picker = false" />
  <Modal v-if="casPanel" title="CAS 临时文件" wide @close="casPanel = false"><div class="modal-body"><p>按任务保留时间清理，默认 {{ casStatus.defaultRetentionHours }} 小时；播放中的文件不清理。仅处理以太记录的临时文件，遵循存储池删除模式。</p><p v-if="!casStatus.items.length" class="small-empty">暂无临时文件</p><div v-for="item in casStatus.items" :key="item.storageId + item.name" class="settings-row"><span>{{ item.name }}<small>保留 {{ item.retentionHours }} 小时 · 到期 {{ date(item.expiresAt) }}</small></span><span>{{ item.active ? '播放中' : '等待过期' }}</span></div></div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="showCAS"><Icon name="RefreshCw" />刷新</button><button class="btn primary" :disabled="busy" @click="cleanupCAS"><Icon name="Trash2" />清理过期项</button></footer></Modal>
  <Modal v-if="confirmDelete" title="删除任务" @close="confirmDelete = null"><div class="modal-body">确认删除「{{ confirmDelete.name }}」？已生成的文件不会删除。</div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除任务</button></footer></Modal>
</template>
