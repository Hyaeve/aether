<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, notify, state } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import NumberInput from '../components/NumberInput.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import LocalDirectoryPicker from '../components/LocalDirectoryPicker.vue'
import FileTabs from '../components/FileTabs.vue'
import { useCardDrag } from '../useCardDrag'
const mounts = ref([]), modal = ref(false), picker = ref(false), directoryPicker = ref(false), busy = ref(false), error = ref(''), deleting = ref(null)
const form = ref({}), permission = ref('0755')
const menu = ref(null)
function closeMenu() { menu.value = null }
function menuKey(event) { if (event.key === 'Escape') closeMenu() }
function context(event, mount) { menu.value = { mount, x: Math.min(event.clientX, innerWidth - 165), y: Math.min(event.clientY, innerHeight - 180) } }
const { armed, dragging, dropTarget, hold, cardClick } = useCardDrag({ selector: '.mount-card', disabled: () => busy.value, onDrop: moveMount, onStart: closeMenu })
const mountOrderKey = () => `aether:mount-order:${state.username || 'admin'}`
function orderMounts(items) {
  let order = mounts.value.map(mount => mount.id)
  try { const saved = JSON.parse(localStorage.getItem(mountOrderKey()) || 'null'); if (Array.isArray(saved)) order = saved } catch {}
  const positions = new Map(order.map((id, index) => [id, index]))
  return [...items].sort((a, b) => (positions.get(a.id) ?? order.length) - (positions.get(b.id) ?? order.length))
}
function moveMount(id, target) {
  const items = [...mounts.value], from = items.findIndex(m => m.id === id), to = items.findIndex(m => m.id === target)
  if (busy.value || from < 0 || to < 0 || from === to) return
  items.splice(to, 0, items.splice(from, 1)[0])
  mounts.value = items
  try { localStorage.setItem(mountOrderKey(), JSON.stringify(items.map(m => m.id))) } catch {}
}
function cardKey(event, mount) {
  if (event.target !== event.currentTarget) return
  if (event.key === 'Enter') { open(mount); return }
  if (!event.altKey || !['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) return
  event.preventDefault()
  const index = mounts.value.findIndex(m => m.id === mount.id)
  const target = mounts.value[index + (['ArrowUp', 'ArrowLeft'].includes(event.key) ? -1 : 1)]
  if (target) moveMount(mount.id, target.id)
}
const storages = computed(() => state.storages.filter(s => s.enabled))
const sourceName = mount => mount.storageId ? `${state.storages.find(s => s.id === mount.storageId)?.name || '存储不可用'} · ${mount.sourceLabel || '根目录'}` : '所有存储池'
const statusName = mount => ({ mounted: '已挂载', stopped: '未挂载', error: '挂载失败' })[mount.status] || '未挂载'
function open(mount) {
  closeMenu()
  form.value = mount ? JSON.parse(JSON.stringify(mount)) : { name: '', storageId: '', source: '/', sourceLabel: '', sourceTrail: [], mountPoint: '', readOnly: false, automount: true, uid: 0, gid: 0, mode: 0o755 }
  permission.value = form.value.mode.toString(8).padStart(4, '0')
  error.value = ''; modal.value = true
}
async function load() { try { mounts.value = orderMounts(await api('/mounts')) } catch (e) { notify(e.message, true) } }
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
let timer
onMounted(() => { load(); timer = setInterval(load, 5000); document.addEventListener('click', closeMenu); document.addEventListener('keydown', menuKey) })
onUnmounted(() => { clearInterval(timer); document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', menuKey) })
</script>
<template>
  <div class="mount-heading"><FileTabs /><button class="btn primary" @click="open()"><Icon name="Plus" />添加挂载</button></div>
  <div v-if="!mounts.length" class="empty-state workspace-empty"><span class="empty-icon"><Icon name="CloudDownload" :size="36" /></span><h3>还没有挂载</h3></div>
  <div v-else class="mount-grid"><article v-for="mount in mounts" :key="mount.id" class="mount-card" :data-id="mount.id" :class="{ 'drag-armed': armed === mount.id, dragging: dragging === mount.id, 'drop-target': dropTarget === mount.id }" :draggable="false" tabindex="0" :aria-label="`${mount.name}，${statusName(mount)}`" aria-keyshortcuts="Enter Alt+ArrowUp Alt+ArrowDown" @pointerdown="hold($event,mount)" @click="cardClick($event,mount,open)" @keydown="cardKey($event,mount)" @dragstart.prevent>
    <div class="mount-card-top" @contextmenu.prevent.stop="context($event,mount)"><button class="mount-toggle" :disabled="busy" :aria-label="mount.status === 'mounted' ? '停用挂载' : '启用挂载'" :aria-pressed="mount.status === 'mounted'" @click.stop="action(mount, mount.status === 'mounted' ? 'stop' : 'start')"><ProviderIcon type="local" /></button><div class="mount-identity"><h3>{{ mount.name }}</h3><p>{{ mount.mountPoint.replace(/[\\/]$/, '') }}<strong class="mount-path-suffix">/{{ mount.name || 'AetherDrive' }}</strong></p></div><button class="icon-btn" aria-label="挂载操作" @click.stop="context($event,mount)"><Icon name="EllipsisVertical" /></button></div>
    <dl class="mount-details" @contextmenu.prevent.stop="context($event,mount)"><div><dt>源目录</dt><dd>{{ sourceName(mount) }}</dd></div><div><dt>权限</dt><dd class="mount-permissions">UID {{ mount.uid }} · GID {{ mount.gid }} · {{ mount.mode.toString(8).padStart(4, '0') }}</dd></div></dl>
    <p v-if="mount.lastError" class="error-message" @contextmenu.prevent.stop="context($event,mount)">{{ mount.lastError }}</p>
  </article></div>
  <Teleport to="body"><div v-if="menu" class="context-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop><button @click="open(menu.mount)"><Icon name="Pencil" />编辑挂载</button><button :disabled="busy" @click="action(menu.mount,'test')"><Icon name="Activity" />测试连接</button><button :disabled="busy" @click="action(menu.mount,menu.mount.status === 'mounted' ? 'stop' : 'start')"><Icon name="Power" />{{ menu.mount.status === 'mounted' ? '停用挂载' : '启用挂载' }}</button><button class="danger-text" @click="deleting = menu.mount; closeMenu()"><Icon name="Trash2" />删除挂载</button></div></Teleport>
  <Modal v-if="modal" :title="form.id ? '编辑挂载' : '添加挂载'" compact wide @close="!busy && (modal = false)"><form @submit.prevent="save"><div class="modal-body"><div class="form-grid">
    <label class="full">挂载名称<input v-model="form.name" required maxlength="60" /></label>
    <div class="field full"><label>源目录</label><button type="button" class="source-trigger" aria-label="选择挂载源目录" @click="picker = true"><span>{{ sourceName(form) }}</span><Icon name="FolderOpen" /></button></div>
    <div class="field full"><label>挂载点</label><div class="directory-input mount-path-input"><div class="mount-path-scroll"><input v-model="form.mountPoint" :style="{ width: `${Math.max(4, [...form.mountPoint].reduce((n, c) => n + (c.charCodeAt(0) > 255 ? 2 : 1), 0)) + 1}ch` }" required aria-label="挂载点" /><span v-if="form.mountPoint && form.name" class="mount-path-suffix">{{ /[\\/]$/.test(form.mountPoint) ? '' : '/' }}{{ form.name }}</span></div><button type="button" class="icon-btn" aria-label="选择容器目录" @click="directoryPicker = true"><Icon name="FolderOpen" /></button></div></div>
    <div class="full mount-toggles"><label class="toggle-line"><span>只读</span><input v-model="form.readOnly" type="checkbox" role="switch" class="switch" /></label><label class="toggle-line"><span>自动挂载</span><input v-model="form.automount" type="checkbox" role="switch" class="switch" /></label></div>
  </div><details class="link-more"><summary>高级设置</summary><div class="mount-advanced"><label>UID<NumberInput v-model="form.uid" aria-label="UID" min="0" max="4294967295" /></label><label>GID<NumberInput v-model="form.gid" aria-label="GID" min="0" max="4294967295" /></label><label>权限<input v-model="permission" aria-label="权限" inputmode="numeric" pattern="0?[0-7]{3}" required /></label></div></details><p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="modal = false">取消</button><button class="btn primary" :disabled="busy">保存挂载</button></footer></form></Modal>
  <TaskSourcePicker v-if="picker" :storages="storages" :storage="form.storageId" :initial="form.source" :initial-label="form.sourceLabel" :initial-trail="form.sourceTrail" allow-all @select="selectSource" @close="picker = false" />
  <LocalDirectoryPicker v-if="directoryPicker" :initial="form.mountPoint" @close="directoryPicker = false" @select="form.mountPoint = $event; directoryPicker = false" />
  <Modal v-if="deleting" title="删除挂载" @close="deleting = null"><div class="modal-body">删除「{{ deleting.name }}」的配置？不会删除目录或源文件。</div><footer class="modal-footer"><button class="btn" @click="deleting = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除挂载</button></footer></Modal>
</template>
<style scoped>
.mount-toggles { display:flex; flex-wrap:wrap; gap:24px; }.mount-toggles .toggle-line { justify-content:flex-start; gap:10px; }
.mount-grid .mount-card { display: block; }
.mount-card-top { display: flex; align-items: center; gap: 14px; min-width: 0; }
.mount-details { margin: 18px 0 0; padding-top: 15px; border-top: 1px solid var(--border); display: grid; gap: 12px; }
.mount-details > div { display: grid; grid-template-columns: 54px minmax(0,1fr); gap: 10px; align-items: center; }
.mount-details dt { color: var(--muted); font-size: 13px; }
.mount-details dd { margin: 0; padding: 7px 9px; border-radius: 6px; background: var(--bg); font-size: 13px; overflow-wrap: anywhere; }
.mount-details .mount-permissions { font-family: ui-monospace, monospace; }
</style>
