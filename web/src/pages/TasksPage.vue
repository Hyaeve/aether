<script setup>
import { computed, reactive, ref } from 'vue'
import { api, state, reload, notify, date, driverOf } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import DirectoryPicker from '../components/DirectoryPicker.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import NumberInput from '../components/NumberInput.vue'
const props = defineProps({ kind: { default: 'strm' } })
const taskTitle = computed(() => props.kind === 'cas' ? 'CAS' : props.kind === 'strm' ? 'STRM' : '缓存')
const availableStorages = computed(() => state.storages.filter(s => s.enabled && (props.kind !== 'cas' || (s.type === 'mobile' && s.config.mode === 'native') || (s.type === 'tianyi' && s.config.mode === 'native'))))
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
const query = ref('')
const tasks = computed(() => state.tasks.filter(t => t.kind === props.kind && t.name.toLowerCase().includes(query.value.toLowerCase())))
const storage = id => state.storages.find(s => s.id === id)
const labels = { idle: '等待执行', running: '执行中', success: '已完成', error: '执行失败', cancelled: '已停止', interrupted: '已中断' }
function open(t) {
  editing.value = t?.id || ''; error.value = ''; more.value = false
  Object.keys(form).forEach(k => delete form[k])
  Object.assign(form, t ? JSON.parse(JSON.stringify(t)) : { name: '', kind: props.kind, storageId: availableStorages.value[0]?.id || '', source: '/', target: '', mode: 'incremental', apiInterval: 200, cron: '', depth: 0, interval: 60, cacheTTL: 0, excludeDirs: '', excludeFiles: '', excludeTypes: '', enabled: true })
  if (props.kind === 'cas') form.retentionHours ||= 12
  modal.value = true
}
async function save() {
  busy.value = true; error.value = ''
  try { await api(editing.value ? `/tasks/${editing.value}` : '/tasks', editing.value ? 'PUT' : 'POST', form); await reload(); modal.value = false; notify('任务已保存') }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
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
  <section class="page-head"><h1>{{ taskTitle }} 任务</h1><div class="toolbar-right"><button v-if="kind === 'cas'" class="btn" @click="showCAS"><Icon name="Database" />临时文件</button><button v-if="kind === 'cache'" class="btn" @click="$router.push('/tasks/cache/settings')"><Icon name="Settings2" />缓存设置</button><button class="btn primary" @click="open()"><Icon name="Plus" />添加任务</button></div></section>
  <div class="metric-strip three"><div><span class="metric-icon"><Icon name="ListTodo" /></span><span><small>全部任务</small><strong>{{ tasks.length }}<em>项</em></strong></span></div><div><span class="metric-icon green"><Icon name="Activity" /></span><span><small>正在执行</small><strong>{{ tasks.filter(t => t.status === 'running').length }}<em>项</em></strong></span></div><div><span class="metric-icon amber"><Icon :name="kind !== 'cache' ? 'FileVideo' : 'Database'" /></span><span><small>{{ kind !== 'cache' ? '本次生成文件' : '缓存条目' }}</small><strong>{{ kind !== 'cache' ? tasks.reduce((n, t) => n + t.processed, 0) : state.cache.entries || 0 }}<em>条</em></strong></span></div></div>
  <div class="section-toolbar"><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="搜索任务" placeholder="搜索任务…" /></div></div>
  <div v-if="!tasks.length" class="empty-state"><span class="empty-icon"><Icon :name="kind !== 'cache' ? 'FileVideo' : 'Database'" :size="36" /></span><h3>{{ query ? '没有匹配的任务' : '还没有任务' }}</h3><p>{{ query ? '尝试其他关键词' : '从一个存储目录开始。' }}</p><button v-if="!query" class="btn" @click="open()"><Icon name="Plus" />添加 {{ taskTitle }} 任务</button></div>
  <div v-else class="table-wrap"><table><thead><tr><th>任务名称</th><th>存储 / 源目录</th><th>{{ kind !== 'cache' ? '生成方式' : '扫描层级' }}</th><th>状态</th><th>上次执行</th><th>调度</th><th class="right">操作</th></tr></thead><tbody><tr v-for="t in tasks" :key="t.id"><td><strong>{{ t.name }}</strong><small>{{ t.message }}</small></td><td><div class="inline"><ProviderIcon :type="storage(t.storageId)?.type" small /><span>{{ storage(t.storageId)?.name || '存储不可用' }}<small>{{ t.source }}</small></span></div></td><td>{{ kind !== 'cache' ? t.mode === 'full' ? '全量' : '增量' : t.depth || '全部' }}<small>{{ kind !== 'cache' ? t.target : `${t.interval} 分钟 / 次` }}</small></td><td><span class="status" :class="t.status === 'success' ? 'success' : t.status === 'error' ? 'danger' : 'pending'"><i />{{ labels[t.status] || t.status }}</span></td><td>{{ date(t.lastRun) }}<small v-if="!t.nextRun?.startsWith('0001')">下次 {{ date(t.nextRun) }}</small></td><td><input type="checkbox" class="switch" role="switch" :checked="t.enabled" :disabled="t.status === 'running'" aria-label="启用任务调度" @change="toggle(t)" /></td><td><div class="row-actions"><button class="icon-btn" :title="t.status === 'running' ? '停止' : '立即执行'" :aria-label="t.status === 'running' ? '停止' : '立即执行'" @click="action(t, t.status === 'running' ? 'stop' : 'run')"><Icon :name="t.status === 'running' ? 'Square' : 'Play'" :size="17" /></button><button class="icon-btn" title="编辑任务" aria-label="编辑任务" :disabled="t.status === 'running'" @click="open(t)"><Icon name="Pencil" :size="16" /></button><button class="icon-btn danger-text" title="删除任务" aria-label="删除任务" :disabled="t.status === 'running'" @click="confirmDelete = t"><Icon name="Trash2" :size="16" /></button></div></td></tr></tbody></table></div>
  <Modal v-if="modal" :title="`${editing ? '编辑' : '添加'} ${taskTitle} 任务`" wide @close="!busy && (modal = false)">
    <form @submit.prevent="save"><div class="modal-body"><div v-if="!availableStorages.length" class="inline-note"><Icon name="Info" />{{ kind === 'cas' ? '需要原生移动新版个人云存储池。' : '请先添加并启用一个存储池。' }}<button type="button" class="text-btn" @click="$router.push('/storage')">前往添加</button></div>
      <div class="form-grid">
        <label class="full">任务名称 <span class="required">*</span><input v-model="form.name" required placeholder="例如：电影库每日同步" /></label>
        <label v-if="kind !== 'cache'" class="full">生成方式<div class="segmented"><button type="button" :class="{ active: form.mode === 'full' }" @click="form.mode = 'full'">全量生成</button><button type="button" :class="{ active: form.mode === 'incremental' }" @click="form.mode = 'incremental'">增量生成</button></div></label>
        <label class="full">存储池 <span class="required">*</span><select v-model="form.storageId" required @change="form.source = '/'"><option disabled value="">选择存储池</option><option v-for="s in availableStorages" :key="s.id" :value="s.id">{{ s.name }} · {{ driverOf(s.type).name }}</option></select></label>
        <label class="full">源目录 <span class="required">*</span><div class="input-action"><input v-model="form.source" required /><button type="button" class="icon-btn" :disabled="!form.storageId" title="选择目录" aria-label="选择目录" @click="picker = true"><Icon name="FolderOpen" /></button></div></label>
        <label v-if="kind !== 'cache'" class="full">本地生成目录<input v-model="form.target" :placeholder="`默认：${state.strmRoot}`" /><small>留空使用 {{ state.strmRoot }}；相对目录位于此目录下。</small></label>
        <div class="field"><label for="task-api-interval">API 间隔</label><NumberInput id="task-api-interval" v-model="form.apiInterval" aria-label="API 间隔" unit="ms" min="0" max="60000" required /></div>
        <div v-if="kind === 'cas'" class="field"><label for="cas-retention">还原文件保留时间</label><NumberInput id="cas-retention" v-model="form.retentionHours" aria-label="还原文件保留时间" unit="h" min="1" max="8760" required /></div>
        <label v-if="kind !== 'cache'">Cron 表达式<input v-model="form.cron" placeholder="0 2 * * *" /><small>五字段；留空仅手动执行</small></label>
        <template v-else><div class="field"><label for="task-interval">执行间隔</label><NumberInput id="task-interval" v-model="form.interval" aria-label="执行间隔" unit="分钟" min="1" required /></div><div class="field"><label for="task-depth">扫描层级</label><NumberInput id="task-depth" v-model="form.depth" aria-label="扫描层级" unit="层" min="0" max="128" required /><small>0 为全部；1 为当前目录</small></div><div class="field"><label for="task-cache">缓存有效期</label><NumberInput id="task-cache" v-model="form.cacheTTL" aria-label="缓存有效期" unit="分钟" min="0" /><small>0 跟随存储池或全局设置</small></div></template>
        <label class="toggle-line full"><span>启用定时调度</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" /></label>
      </div>
      <template v-if="kind !== 'cache'"><button type="button" class="disclosure" :aria-expanded="more" @click="more = !more"><Icon :name="more ? 'ChevronDown' : 'ChevronRight'" :size="16" />更多选项</button><div v-if="more" class="form-grid more-options"><label class="full">排除目录<input v-model="form.excludeDirs" placeholder="回收站;预告片" /></label><label class="full">排除文件<input v-model="form.excludeFiles" placeholder="sample;trailer" /></label><label class="full">排除类型<input v-model="form.excludeTypes" placeholder="iso;avi" /><small>英文分号分隔；关键词及后缀不区分大小写。</small></label><div class="field full"><label for="strm-cache">缓存有效期</label><NumberInput id="strm-cache" v-model="form.cacheTTL" aria-label="缓存有效期" unit="分钟" min="0" /><small>0 跟随存储池或全局设置</small></div></div></template>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="modal = false">取消</button><button class="btn primary" :disabled="busy || !form.storageId"><Icon name="Check" />{{ busy ? '保存中…' : '保存任务' }}</button></footer></form>
  </Modal>
  <DirectoryPicker v-if="picker" :storage="form.storageId" :initial="form.source" @select="form.source = $event; picker = false" @close="picker = false" />
  <Modal v-if="casPanel" title="CAS 临时文件" wide @close="casPanel = false"><div class="modal-body"><p>按任务保留时间清理，默认 {{ casStatus.defaultRetentionHours }} 小时；播放中的文件不清理。仅处理以太记录的临时文件，遵循存储池删除模式。</p><p v-if="!casStatus.items.length" class="small-empty">暂无临时文件</p><div v-for="item in casStatus.items" :key="item.storageId + item.name" class="settings-row"><span>{{ item.name }}<small>保留 {{ item.retentionHours }} 小时 · 到期 {{ date(item.expiresAt) }}</small></span><span>{{ item.active ? '播放中' : '等待过期' }}</span></div></div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="showCAS"><Icon name="RefreshCw" />刷新</button><button class="btn primary" :disabled="busy" @click="cleanupCAS"><Icon name="Trash2" />清理过期项</button></footer></Modal>
  <Modal v-if="confirmDelete" title="删除任务" @close="confirmDelete = null"><div class="modal-body">确认删除「{{ confirmDelete.name }}」？已生成的文件不会删除。</div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除任务</button></footer></Modal>
</template>
