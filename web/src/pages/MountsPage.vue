<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, notify, state } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import NumberInput from '../components/NumberInput.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
const mounts = ref([]), modal = ref(false), picker = ref(false), directoryPicker = ref(false), busy = ref(false), error = ref(''), deleting = ref(null)
const form = ref({}), permission = ref('0755')
const menu = ref(null)
function closeMenu() { menu.value = null }
function menuKey(event) { if (event.key === 'Escape') closeMenu() }
function context(event, mount) { menu.value = { mount, x: Math.min(event.clientX, innerWidth - 165), y: Math.min(event.clientY, innerHeight - 180) } }
function cardClick(event, mount) { if (!event.target.closest('button')) open(mount) }
const directories = ref({ path: '', parent: '', items: [] }), directoryError = ref(''), directoryBusy = ref(false)
const storages = computed(() => state.storages.filter(s => s.enabled))
const sourceName = mount => mount.storageId ? `${state.storages.find(s => s.id === mount.storageId)?.name || '存储不可用'} · ${mount.sourceLabel || '根目录'}` : '所有存储池'
const statusName = mount => ({ mounted: '已挂载', stopped: '未挂载', error: '挂载失败' })[mount.status] || '未挂载'
function open(mount) {
  closeMenu()
  form.value = mount ? JSON.parse(JSON.stringify(mount)) : { name: '', storageId: '', source: '/', sourceLabel: '', sourceTrail: [], mountPoint: '', readOnly: false, automount: true, uid: 0, gid: 0, mode: 493 }
  permission.value = form.value.mode.toString(8).padStart(4, '0')
  error.value = ''; modal.value = true
}
async function load() { try { mounts.value = await api('/mounts') } catch (e) { notify(e.message, true) } }
async function save() {
  if (!/^0?[0-7]{3}$/.test(permission.value)) { error.value = '权限须为八进制，例如 0755'; return }
  busy.value = true; error.value = ''
  try {
    await api(form.value.id ? `/mounts/${form.value.id}` : '/mounts', form.value.id ? 'PUT' : 'POST', { ...form.value, mode: parseInt(permission.value, 8) })
    modal.value = false; await load(); notify('挂载配置已保存')
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function action(mount, action) {
  closeMenu()
  busy.value = true
  try { await api(`/mounts/${mount.id}/${action}`, 'POST'); notify(action === 'test' ? '挂载连接正常' : action === 'start' ? '挂载成功' : '已卸载') }
  catch (e) { notify(e.message, true) } finally { await load(); busy.value = false }
}
async function remove() {
  busy.value = true
  try { await api(`/mounts/${deleting.value.id}`, 'DELETE'); deleting.value = null; await load(); notify('挂载已删除') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
function selectSource(value) { Object.assign(form.value, value); picker.value = false }
async function browse(path = '') {
  directoryPicker.value = true; directoryBusy.value = true; directoryError.value = ''
  try { directories.value = await api(`/local-directories?path=${encodeURIComponent(path)}`) }
  catch (e) { directoryError.value = e.message } finally { directoryBusy.value = false }
}
let timer
onMounted(() => { load(); timer = setInterval(load, 5000); document.addEventListener('click', closeMenu); document.addEventListener('keydown', menuKey) })
onUnmounted(() => { clearInterval(timer); document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', menuKey) })
</script>
<template>
  <div class="toolbar mount-toolbar"><span /><button class="btn primary" @click="open()"><Icon name="Plus" />添加挂载</button></div>
  <div v-if="!mounts.length" class="empty-state"><span class="empty-icon"><Icon name="CloudDownload" :size="36" /></span><h3>还没有挂载</h3></div>
  <div v-else class="mount-grid"><article v-for="mount in mounts" :key="mount.id" class="mount-card" tabindex="0" :aria-label="`${mount.name}，${statusName(mount)}`" @click="cardClick($event,mount)" @keydown.enter.self="open(mount)" @contextmenu.prevent.stop="context($event,mount)"><button class="mount-toggle" :disabled="busy" :aria-label="mount.status === 'mounted' ? '停用挂载' : '启用挂载'" :aria-pressed="mount.status === 'mounted'" @click.stop="action(mount, mount.status === 'mounted' ? 'stop' : 'start')"><ProviderIcon type="local" /></button><div class="mount-identity"><h3>{{ mount.name }}</h3><p>{{ mount.mountPoint }}</p><p v-if="mount.lastError" class="error-message">{{ mount.lastError }}</p></div><button class="icon-btn" aria-label="挂载操作" @click.stop="context($event,mount)"><Icon name="EllipsisVertical" /></button></article></div>
  <Teleport to="body"><div v-if="menu" class="context-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop><button @click="open(menu.mount)"><Icon name="Pencil" />编辑挂载</button><button :disabled="busy" @click="action(menu.mount,'test')"><Icon name="Activity" />测试连接</button><button :disabled="busy" @click="action(menu.mount,menu.mount.status === 'mounted' ? 'stop' : 'start')"><Icon name="Power" />{{ menu.mount.status === 'mounted' ? '停用挂载' : '启用挂载' }}</button><button class="danger-text" @click="deleting = menu.mount; closeMenu()"><Icon name="Trash2" />删除挂载</button></div></Teleport>
  <Modal v-if="modal" :title="form.id ? '编辑挂载' : '添加挂载'" compact wide @close="!busy && (modal = false)"><form @submit.prevent="save"><div class="modal-body"><div class="form-grid">
    <label>挂载名称<input v-model="form.name" required maxlength="60" /></label>
    <div class="field"><label>源目录</label><button type="button" class="source-trigger" aria-label="选择挂载源目录" @click="picker = true"><span>{{ sourceName(form) }}</span><Icon name="FolderOpen" /></button></div>
    <div class="field full"><label>挂载点</label><div class="mount-point-input"><input v-model="form.mountPoint" required placeholder="/mnt/media" aria-label="挂载点" /><button type="button" class="icon-btn" aria-label="选择容器目录" @click="browse(form.mountPoint)"><Icon name="FolderOpen" /></button></div></div>
    <label class="toggle-line"><span>只读</span><input v-model="form.readOnly" type="checkbox" role="switch" class="switch" /></label>
    <label class="toggle-line"><span>自动挂载</span><input v-model="form.automount" type="checkbox" role="switch" class="switch" /></label>
  </div><details class="link-more"><summary>高级设置</summary><div class="mount-advanced"><label>UID<NumberInput v-model="form.uid" aria-label="UID" min="0" max="4294967295" /></label><label>GID<NumberInput v-model="form.gid" aria-label="GID" min="0" max="4294967295" /></label><label>权限<input v-model="permission" aria-label="权限" inputmode="numeric" pattern="0?[0-7]{3}" required /></label></div></details><p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="modal = false">取消</button><button class="btn primary" :disabled="busy">保存挂载</button></footer></form></Modal>
  <TaskSourcePicker v-if="picker" :storages="storages" :storage="form.storageId" :initial="form.source" :initial-label="form.sourceLabel" :initial-trail="form.sourceTrail" allow-all @select="selectSource" @close="picker = false" />
  <Modal v-if="directoryPicker" title="选择容器目录" @close="directoryPicker = false"><div class="modal-body"><div class="path-bar"><button class="icon-btn" aria-label="上级容器目录" :disabled="directoryBusy || directories.path === directories.parent" @click="browse(directories.parent)"><Icon name="ArrowUp" /></button><span>{{ directories.path }}</span></div><p v-if="directoryError" class="error-message" role="alert">{{ directoryError }}</p><div class="container-directory-list"><button v-for="entry in directories.items" :key="entry.path" type="button" class="directory-row" :disabled="directoryBusy" @click="browse(entry.path)"><Icon name="Folder" /><span>{{ entry.name }}</span><Icon name="ChevronRight" /></button></div></div><footer class="modal-footer"><button class="btn" @click="directoryPicker = false">取消</button><button class="btn primary" :disabled="directoryBusy || !!directoryError || !directories.path" @click="form.mountPoint = directories.path; directoryPicker = false">选择当前目录</button></footer></Modal>
  <Modal v-if="deleting" title="删除挂载" @close="deleting = null"><div class="modal-body">删除「{{ deleting.name }}」的配置？不会删除目录或源文件。</div><footer class="modal-footer"><button class="btn" @click="deleting = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除挂载</button></footer></Modal>
</template>
