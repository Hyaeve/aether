<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, notify, state } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import SecretInput from '../components/SecretInput.vue'
import { useCardDrag } from '../useCardDrag'

const props = defineProps({ section: { default: 'telegram' } })
const form = ref({ enabled: false, token: '', chatId: '', userIds: [] }), users = ref(''), targets = ref([])
const loaded = ref(false), busy = ref(false), testing = ref(false), editing = ref(null), deleting = ref(null), picker = ref(false)
const providers = [{ id: '115', name: '115 云盘' }, { id: 'mobile', name: '移动云盘' }, { id: 'quark', name: '夸克云盘' }]
const storage = id => state.storages.find(s => s.id === id)
const available = computed(() => state.storages.filter(s => s.enabled && s.type === editing.value?.provider && (s.type !== 'mobile' || s.config?.mode === 'native')))
let alive = true
onUnmounted(() => { alive = false })
watch(() => props.section, () => { editing.value = null; deleting.value = null; picker.value = false })
onMounted(async () => {
  try {
    const value = await api('/notification-service')
    if (!alive) return
    form.value = value.telegram
    users.value = (value.telegram.userIds || []).join(';')
    targets.value = value.targets || []
    loaded.value = true
  } catch (e) { if (alive) notify(e.message, true) }
})
async function save() {
  busy.value = true
  try {
    const value = await api('/notification-service', 'PUT', { telegram: { ...form.value, userIds: users.value.split(/[;,\s]+/).filter(Boolean) } })
    if (alive) { form.value = value.telegram; users.value = value.telegram.userIds.join(';'); notify('TG 通知已保存') }
  } catch (e) { if (alive) notify(e.message, true) } finally { if (alive) busy.value = false }
}
async function test() {
  testing.value = true
  try { await api('/notification-service/test', 'POST', {}); if (alive) notify('TG 测试消息已发送') }
  catch (e) { if (alive) notify(e.message, true) } finally { if (alive) testing.value = false }
}
async function saveTargets(next) {
  if (busy.value) return false
  busy.value = true
  try {
    const value = await api('/notification-service/targets', 'PUT', { targets: next })
    if (alive) { targets.value = value.targets || []; notify('转存目录已保存') }
    return true
  } catch (e) { if (alive) notify(e.message, true); return false } finally { if (alive) busy.value = false }
}
function edit(provider, target) { if (!busy.value) editing.value = target ? { ...target } : { id: '', provider, storageId: '', parent: '', path: '' } }
function choose(value) { Object.assign(editing.value, { storageId: value.storageId, parent: value.source, path: value.sourcePath || '/' }); picker.value = false }
async function submitTarget() {
  const target = { ...editing.value }
  const next = target.id ? targets.value.map(item => item.id === target.id ? target : item) : [...targets.value, target]
  if (await saveTargets(next) && alive) editing.value = null
}
async function remove() { if (await saveTargets(targets.value.filter(item => item.id !== deleting.value.id)) && alive) deleting.value = null }
async function move(id, target) {
  dragEnd()
  const from = targets.value.findIndex(item => item.id === id), to = targets.value.findIndex(item => item.id === target)
  if (from < 0 || to < 0 || from === to || targets.value[from].provider !== targets.value[to].provider) return
  const next = [...targets.value]
  next.splice(to, 0, next.splice(from, 1)[0])
  await saveTargets(next)
}
function reorder(event, item) {
  if (event.target !== event.currentTarget) return
  if (event.key === 'Enter') { edit(item.provider, item); return }
  if (!event.altKey || !['ArrowUp', 'ArrowDown'].includes(event.key)) return
  event.preventDefault()
  const group = targets.value.filter(t => t.provider === item.provider), i = group.findIndex(t => t.id === item.id)
  const target = group[i + (event.key === 'ArrowUp' ? -1 : 1)]
  if (target) move(item.id, target.id)
}
const { armed, dragging, dropTarget, hold, cardClick, cancel: dragEnd } = useCardDrag({ selector: '.telegram-target-card', disabled: () => busy.value, onDrop: move })
</script>

<template>
  <div class="notification-page">
    <nav v-tab-scroll class="content-tabs" aria-label="通知服务栏目">
      <RouterLink to="/notifications/telegram" :class="{ active: section === 'telegram' }"><Icon name="Send" />TG 通知</RouterLink>
      <RouterLink to="/notifications/transfers" :class="{ active: section === 'transfers' }"><Icon name="FolderInput" />链接转存</RouterLink>
    </nav>
    <form v-if="section === 'telegram'" class="notification-form" @submit.prevent="save">
      <header><h2><Icon name="Send" />TG 通知</h2><input v-model="form.enabled" type="checkbox" role="switch" class="switch" aria-label="启用 TG 通知" :disabled="!loaded || busy" /></header>
      <label>Bot Token <small v-if="form.hasToken">已保存</small><SecretInput v-model="form.token" aria-label="Bot Token" autocomplete="new-password" :disabled="!loaded || busy" /></label>
      <label>Chat ID<input v-model.trim="form.chatId" aria-label="Chat ID" inputmode="text" autocomplete="off" :disabled="!loaded || busy" /></label>
      <label>授权用户 ID<input v-model="users" aria-label="授权用户 ID" autocomplete="off" :disabled="!loaded || busy" /></label>
      <footer><button type="button" class="btn" :disabled="!loaded || busy || testing || !form.hasToken" @click="test"><Icon name="Send" />测试通知</button><button class="btn primary" :disabled="!loaded || busy || testing"><Icon name="Save" />保存设置</button></footer>
    </form>
    <div v-else class="telegram-pools">
      <section v-for="provider in providers" :key="provider.id" class="telegram-pool" :aria-label="provider.name + '转存目录池'">
        <header><h2><ProviderIcon :type="provider.id" compact />{{ provider.name }}</h2><button class="icon-btn" :aria-label="'添加' + provider.name + '转存目录'" :disabled="!loaded || busy" @click="edit(provider.id)"><Icon name="Plus" /></button></header>
        <div class="telegram-target-list">
          <article v-for="(target, index) in targets.filter(item => item.provider === provider.id)" :key="target.id" class="telegram-target-card" :data-id="target.id" :class="{ 'drag-armed': armed === target.id, dragging: dragging === target.id, 'drop-target': dropTarget === target.id }" :aria-label="'转存目录 ' + (storage(target.storageId)?.name || '存储已移除') + ' ' + target.path" tabindex="0" aria-keyshortcuts="Enter Alt+ArrowUp Alt+ArrowDown" @pointerdown="hold($event, target)" @keydown="reorder($event, target)" @click="cardClick($event, target, item => edit(provider.id, item))" @dragstart.prevent>
            <span class="telegram-order">{{ index + 1 }}</span><ProviderIcon :type="provider.id" small /><div><strong>{{ storage(target.storageId)?.name || '存储已移除' }}</strong><span :data-tooltip="target.path" data-tooltip-stationary>{{ target.path }}</span></div>
            <button class="icon-btn" :aria-label="'编辑转存目录 ' + target.path" :disabled="busy" @click="edit(provider.id, target)"><Icon name="Pencil" :size="16" /></button><button class="icon-btn danger-text" :aria-label="'删除转存目录 ' + target.path" :disabled="busy" @click="deleting = target"><Icon name="Trash2" :size="16" /></button>
          </article>
          <button class="add-storage-tile telegram-add" :aria-label="'添加' + provider.name + '目录卡片'" :disabled="!loaded || busy" @click="edit(provider.id)"><Icon name="Plus" /><span>添加转存目录</span></button>
        </div>
      </section>
    </div>
    <Modal v-if="editing" standard :title="editing.id ? '编辑转存目录' : '添加转存目录'" @close="!busy && (editing = null)">
      <form @submit.prevent="submitTarget"><div class="modal-body"><label>转存目录<button type="button" class="source-trigger" aria-label="选择转存目录" :disabled="busy" @click="picker = true"><span>{{ editing.storageId ? (storage(editing.storageId)?.name || '存储已移除') + ' · ' + editing.path : '' }}</span><Icon name="FolderOpen" /></button></label></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="editing = null">取消</button><button class="btn primary" :disabled="busy || !editing.storageId || !editing.parent"><Icon name="Check" />{{ editing.id ? '保存' : '添加' }}</button></footer></form>
    </Modal>
    <TaskSourcePicker v-if="picker && editing" :storages="available" :storage="editing.storageId" :initial="editing.parent || '/'" :initial-label="editing.path === '/' ? '根目录' : editing.path?.replace(/^\/+/, '')" include-full-path @select="choose" @close="picker = false" />
    <Modal v-if="deleting" confirmation title="删除转存目录" @close="!busy && (deleting = null)"><div class="modal-body">确认移除 {{ storage(deleting.storageId)?.name }} 的转存目录 {{ deleting.path }}？</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="deleting = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">确认删除</button></footer></Modal>
  </div>
</template>

<style scoped>
.notification-page{min-width:0}.notification-page>.content-tabs{margin-bottom:24px}.notification-form{width:min(100%,380px);padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface);display:grid;gap:18px}.notification-form header,.notification-form h2,.telegram-pool>header,.telegram-pool h2{display:flex;align-items:center;gap:10px}.notification-form header,.telegram-pool>header{justify-content:space-between}.notification-form h2,.telegram-pool h2{font-size:15px;margin:0}.notification-form label{gap:8px}.notification-form label small{color:var(--muted);font-weight:400}.notification-form :deep(.secret-input),.notification-form :deep(input:not([type=checkbox])){width:100%;height:var(--control-height);min-height:var(--control-height)}.notification-form footer{display:flex;gap:10px}.notification-form footer .btn{flex:1;height:var(--toolbar-height);min-height:var(--toolbar-height);padding-block:0}
.telegram-pools{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:24px}.telegram-pool{min-width:0}.telegram-pool>header{padding-bottom:14px;border-bottom:1px solid var(--border);margin-bottom:14px}.telegram-pool h2 :deep(.provider-icon){width:24px;height:24px}.telegram-target-list{display:grid;gap:10px}.telegram-target-card{display:flex;align-items:center;gap:8px;min-width:0;padding:12px 10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);min-height:72px;cursor:grab}.telegram-target-card>div{display:grid;gap:5px;flex:1;min-width:0}.telegram-target-card strong{font-size:13px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.telegram-target-card>div>span{font-size:12px;color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.telegram-target-card>.icon-btn{width:26px;height:30px;flex:none}.telegram-order{color:var(--muted);font-size:12px;min-width:12px}.telegram-add{height:72px;min-height:72px;display:flex;flex-direction:row;justify-content:center;align-items:center;gap:8px;font-size:13px}.telegram-add:disabled{cursor:default}.notification-form .switch{flex:none}
@media(max-width:1100px){.telegram-pools{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:680px){.telegram-pools{grid-template-columns:minmax(0,1fr);gap:24px}}
</style>
