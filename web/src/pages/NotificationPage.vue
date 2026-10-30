<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, notify, state, driverOf } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import TaskSourcePicker from '../components/TaskSourcePicker.vue'
import SecretInput from '../components/SecretInput.vue'
import WeComSettings from '../components/WeComSettings.vue'
import NotificationChannelSettings from '../components/NotificationChannelSettings.vue'
import ShareTransferRecords from '../components/ShareTransferRecords.vue'
import NumberInput from '../components/NumberInput.vue'
import { useCardDrag } from '../useCardDrag'

const props = defineProps({ section: { default: 'telegram' } })
const form = ref({ enabled: false, token: '', chatId: '', userIds: [] }), users = ref(''), targets = ref([])
const wecom = ref(null)
const qqBot = ref(null), feishu = ref(null)
const loaded = ref(false), busy = ref(false), testing = ref(false), editing = ref(null), deleting = ref(null), picker = ref(false)
const resolving = ref(false), pickerTrail = ref([]), pickerInitial = ref('/'), pickerLabel = ref('根目录')
const liveTarget = ref(null)
const maskedTelegram = value => ({ ...value, token: value.hasToken ? '********' : '' })
const providers = [{ id: '115', name: '115 云盘' }, { id: 'mobile', name: '移动云盘' }, { id: 'quark', name: '夸克云盘' }]
const storage = id => state.storages.find(s => s.id === id)
const available = computed(() => state.storages.filter(s => s.enabled && s.type === editing.value?.provider && (s.type !== 'mobile' || s.config?.mode === 'native')))
let alive = true, resolveGeneration = 0
onUnmounted(() => { alive = false; resolveGeneration++ })
watch(editing, () => { resolveGeneration++; resolving.value = false; picker.value = false })
watch(() => props.section, () => { editing.value = null; deleting.value = null; picker.value = false; liveTarget.value = null })
onMounted(async () => {
  try {
    const value = await api('/notification-service')
    if (!alive) return
    form.value = maskedTelegram(value.telegram)
    users.value = (value.telegram.userIds || []).join(';')
    targets.value = value.targets || []
    wecom.value = value.wecom || null
    qqBot.value = value.qqBot || null
    feishu.value = value.feishu || null
    loaded.value = true
  } catch (e) { if (alive) notify(e.message, true) }
})
async function save() {
  busy.value = true
  try {
    const value = await api('/notification-service', 'PUT', { telegram: { ...form.value, token: form.value.token === '********' ? '' : form.value.token, userIds: users.value.split(/[;,\s]+/).filter(Boolean) } })
    if (alive) { form.value = maskedTelegram(value.telegram); users.value = value.telegram.userIds.join(';'); notify('TG Bot 已保存') }
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
function edit(provider, target) {
  if (!busy.value) editing.value = target
    ? { ...target, shareTransfer: target.shareTransfer ?? true, ed2kOffline: !!target.ed2kOffline, updateDays: target.updateDays ?? 7 }
    : { id: '', provider, storageId: '', parent: '', path: '', shareTransfer: true, ed2kOffline: false, ...(provider!=='115'?{updateDays:7}:{}) }
}
async function openPicker() {
  const target = editing.value
  if (!target || resolving.value) return
  const run = ++resolveGeneration
  resolving.value = true
  let parent = '/', label = '根目录', trail = []
  try {
    if (target.storageId && target.path && target.path !== '/') {
      const parts = target.path.split('/').filter(Boolean)
      if (parts.length > 128) throw new Error('目录层级过深，请重新选择')
      for (const part of parts) {
        const entries = await api(`/files?storage=${encodeURIComponent(target.storageId)}&path=${encodeURIComponent(parent)}`)
        if (!alive || run !== resolveGeneration || editing.value !== target) return
        const matches = entries.filter(entry => entry.isDir && entry.name === part)
        if (matches.length !== 1) throw new Error('已保存路径已变化，请重新选择转存目录')
        trail.push({ id: parent, name: label }); parent = matches[0].id; label = matches[0].name
      }
      if (parent !== target.parent) throw new Error('已保存目录身份已变化，请重新选择转存目录')
    }
  } catch (e) {
    if (alive && run === resolveGeneration && editing.value === target) notify(e.message, true)
    parent = '/'; label = '根目录'; trail = []
  } finally { if (alive && run === resolveGeneration) resolving.value = false }
  if (!alive || run !== resolveGeneration || editing.value !== target) return
  pickerInitial.value = parent; pickerLabel.value = label; pickerTrail.value = trail; picker.value = true
}
function choose(value) { Object.assign(editing.value, { storageId: value.storageId, parent: value.source, path: value.sourcePath || '/' }); picker.value = false }
async function submitTarget() {
  const target = { ...editing.value }
  if(target.provider!=='115'&&(!Number.isInteger(target.updateDays)||target.updateDays<0||target.updateDays>365)){notify('更新周期须为0至365天的整数',true);return}
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
  if (event.key === ' ') { event.preventDefault(); edit(item.provider, item); return }
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
      <RouterLink to="/notifications/telegram" :class="{ active: section === 'telegram' }"><Icon name="MessageSquareText" />消息配置</RouterLink>
      <RouterLink to="/notifications/transfers" :class="{ active: section === 'transfers' }"><Icon name="FolderInput" />链接转存</RouterLink>
    </nav>
    <div v-if="section === 'telegram'" class="notification-config-panels">
    <NotificationChannelSettings v-if="loaded" kind="qqBot" :initial="qqBot" @saved="qqBot = $event" />
    <NotificationChannelSettings v-if="loaded" kind="feishu" :initial="feishu" @saved="feishu = $event" />
    <form class="notification-form telegram-form" aria-label="TG Bot" @submit.prevent="save">
      <header><h2><Icon name="Send" />TG Bot</h2><input v-model="form.enabled" type="checkbox" role="switch" class="switch" aria-label="启用 TG Bot" :disabled="!loaded || busy" /></header>
      <label>Bot Token<SecretInput v-model="form.token" secret-path="/notification-service/secret" secret-field="token" aria-label="Bot Token" autocomplete="new-password" :disabled="!loaded || busy" /></label>
      <label>Chat ID<input v-model.trim="form.chatId" aria-label="Chat ID" inputmode="text" autocomplete="off" :disabled="!loaded || busy" /></label>
      <label>授权用户 ID<input v-model="users" aria-label="授权用户 ID" autocomplete="off" :disabled="!loaded || busy" /></label>
      <footer><button type="button" class="btn" :disabled="!loaded || busy || testing || !form.hasToken" @click="test"><Icon name="Send" />测试通知</button><button class="btn primary" :disabled="!loaded || busy || testing"><Icon name="Save" />保存设置</button></footer>
    </form>
    <WeComSettings v-if="loaded" :initial="wecom" @saved="wecom = $event" />
    </div>
    <div v-else class="telegram-pools">
      <section v-for="provider in providers" :key="provider.id" class="telegram-pool" :data-provider="provider.id" :style="{'--pool-accent':driverOf(provider.id).color}" :aria-label="provider.name + '转存目录池'">
        <header><h2><ProviderIcon :type="provider.id" compact unframed />{{ provider.name }}</h2><button class="icon-btn" :aria-label="'添加' + provider.name + '转存目录'" :disabled="!loaded || busy" @click="edit(provider.id)"><Icon name="Plus" /></button></header>
        <div class="telegram-target-list">
          <article v-for="target in targets.filter(item => item.provider === provider.id)" :key="target.id" class="telegram-target-card" :data-id="target.id" :class="{ 'drag-armed': armed === target.id, dragging: dragging === target.id, 'drop-target': dropTarget === target.id }" :aria-label="'转存目录 ' + (storage(target.storageId)?.name || '存储已移除') + ' ' + target.path" tabindex="0" aria-keyshortcuts="Enter Alt+ArrowUp Alt+ArrowDown" @pointerdown="hold($event, target)" @keydown="reorder($event, target)" @click="cardClick($event, target, item => edit(provider.id, item))" @dragstart.prevent>
            <span class="telegram-target-avatar" aria-hidden="true">{{[...(storage(target.storageId)?.name||'已移除')].slice(0,2).join('')}}</span><div><strong>{{ storage(target.storageId)?.name || '存储已移除' }}</strong><span :data-tooltip="target.path" data-tooltip-stationary>{{ target.path }}</span><div v-if="provider.id === '115'" class="telegram-purposes"><span v-if="target.shareTransfer !== false" class="telegram-purpose-share"><Icon name="Share2" :size="12" />分享转存</span><span v-if="target.ed2kOffline" class="telegram-purpose-offline"><Icon name="CloudDownload" :size="12" />ED2K离线</span><span v-if="target.shareTransfer === false && !target.ed2kOffline" class="telegram-purpose-disabled">未启用</span></div></div>
            <button v-if="provider.id!=='115'" class="icon-btn" :aria-label="'活动链接 '+target.path" :disabled="busy" @click.stop="liveTarget=target" data-tooltip="活动链接" data-tooltip-always><Icon name="Link" :size="16"/></button><button class="icon-btn" :aria-label="'编辑转存目录 ' + target.path" :disabled="busy" @click.stop="edit(provider.id, target)"><Icon name="Pencil" :size="16" /></button><button class="icon-btn danger-text" :aria-label="'删除转存目录 ' + target.path" :disabled="busy" @click.stop="deleting = target"><Icon name="Trash2" :size="16" /></button>
          </article>
        </div>
        <button class="add-storage-tile telegram-add" :aria-label="'添加' + provider.name + '目录卡片'" :disabled="!loaded || busy" @click="edit(provider.id)"><Icon name="Plus" /><span>添加转存目录</span></button>
      </section>
    </div>
    <ShareTransferRecords v-if="section==='transfers'" :target="liveTarget" @close="liveTarget=null"/>
    <Modal v-if="editing" standard :title="editing.id ? '编辑转存目录' : '添加转存目录'" @close="!busy && (editing = null)">
      <form @submit.prevent="submitTarget"><div class="modal-body telegram-target-form"><div class="telegram-target-fields" :class="{'has-cycle':editing.provider!=='115'}"><label>转存目录<button type="button" class="source-trigger" aria-label="选择转存目录" :disabled="busy || resolving" @click="openPicker"><span>{{ editing.storageId ? (storage(editing.storageId)?.name || '存储已移除') + ' · ' + editing.path : '' }}</span><Icon :name="resolving ? 'LoaderCircle' : 'FolderOpen'" :class="{ spin: resolving }" /></button></label><label v-if="editing.provider!=='115'">更新周期<NumberInput v-model="editing.updateDays" aria-label="更新周期" unit="天" min="0" max="365" step="1" inputmode="numeric" required :disabled="busy" @keydown="['e','E','+','-','.'].includes($event.key)&&$event.preventDefault()"/></label></div><div v-if="editing.provider === '115'" class="telegram-purpose-settings"><label>分享转存<input v-model="editing.shareTransfer" type="checkbox" role="switch" class="switch" aria-label="分享转存" :disabled="busy" /></label><label>ED2K离线<input v-model="editing.ed2kOffline" type="checkbox" role="switch" class="switch" aria-label="ED2K离线" :disabled="busy" /></label></div></div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="editing = null">取消</button><button class="btn primary" :disabled="busy || !editing.storageId || !editing.parent"><Icon name="Check" />{{ editing.id ? '保存' : '添加' }}</button></footer></form>
    </Modal>
    <TaskSourcePicker v-if="picker && editing" :storages="available" :storage="editing.storageId" :initial="pickerInitial" :initial-label="pickerLabel" :initial-trail="pickerTrail" include-full-path clickable-path @select="choose" @close="picker = false" />
    <Modal v-if="deleting" confirmation title="删除转存目录" @close="!busy && (deleting = null)"><div class="modal-body">确认移除 {{ storage(deleting.storageId)?.name }} 的转存目录 {{ deleting.path }}？</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="deleting = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">确认删除</button></footer></Modal>
  </div>
</template>

<style scoped>
.notification-page{min-width:0}.notification-page>.content-tabs{margin-bottom:24px}.notification-form{width:min(100%,380px);padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface);display:grid;gap:18px}.notification-form header,.notification-form h2,.telegram-pool>header,.telegram-pool h2{display:flex;align-items:center;gap:10px}.notification-form header,.telegram-pool>header{justify-content:space-between}.notification-form h2,.telegram-pool h2{font-size:15px;margin:0}.notification-form label{gap:8px}.notification-form label small{color:var(--muted);font-weight:400}.notification-form :deep(.secret-input),.notification-form :deep(input:not([type=checkbox])){width:100%;height:var(--control-height);min-height:var(--control-height)}.notification-form footer{display:flex;gap:10px}.notification-form footer .btn{flex:1;height:var(--toolbar-height);min-height:var(--toolbar-height);padding-block:0}
.notification-config-panels{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:24px;align-content:start;align-items:stretch;flex:none;min-width:0;width:100%}
.notification-config-panels>.notification-form{width:100%;display:flex;flex-direction:column}.notification-config-panels>.telegram-form footer{margin-top:auto}
.notification-config-panels>:deep(.wecom-form){grid-column:span 2}
@media(max-width:1200px){.notification-config-panels{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:760px){.notification-config-panels{grid-template-columns:minmax(0,1fr)}.notification-config-panels>:deep(.wecom-form){grid-column:auto}}
.telegram-pools{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:24px}.telegram-pool{min-width:0}.telegram-pool>header{padding-bottom:14px;border-bottom:1px solid var(--border);margin-bottom:14px}.telegram-pool h2 :deep(.provider-icon){width:24px;height:24px}.telegram-target-list{display:grid;gap:10px}.telegram-target-card{display:flex;align-items:center;gap:8px;min-width:0;padding:12px 10px;border:1px solid var(--border);border-radius:8px;background:var(--surface);min-height:72px;cursor:grab}.telegram-target-card>div{display:grid;gap:5px;flex:1;min-width:0}.telegram-target-card strong{font-size:13px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.telegram-target-card>div>span{font-size:12px;color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.telegram-target-card>.icon-btn{width:26px;height:30px;flex:none}.telegram-order{color:var(--muted);font-size:12px;min-width:12px}.telegram-add{height:72px;min-height:72px;display:flex;flex-direction:row;justify-content:center;align-items:center;gap:8px;font-size:13px}.telegram-add:disabled{cursor:default}.notification-form .switch{flex:none}
.telegram-pool{padding:16px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}.telegram-pool h2{font-size:17px;gap:10px}.telegram-pool h2 :deep(.provider-icon){width:30px;height:30px}.telegram-pool h2 :deep(.provider-logo){width:28px;height:28px}.telegram-pool h2 :deep([data-provider="115"] .provider-logo){width:24px;height:24px}.telegram-pool h2 :deep([data-provider=mobile] .provider-logo){width:30px;height:30px}.telegram-target-card,.telegram-add{background:var(--input)}.telegram-target-card>:deep(.provider-icon){width:28px;height:28px;flex:none;padding:0;border:0;border-radius:0;background:transparent}.telegram-target-card>:deep(.provider-icon .provider-logo){width:100%;height:100%}.telegram-target-card>:deep([data-provider="115"] .provider-logo){width:22px;height:22px}
@media(max-width:1100px){.telegram-pools{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:680px){.telegram-pools{grid-template-columns:minmax(0,1fr);gap:24px}}
.telegram-purposes{display:flex;flex-wrap:wrap;gap:5px}.telegram-purposes>span{display:inline-flex;align-items:center;gap:4px;white-space:nowrap;font-size:11px;line-height:18px;padding:0 5px;border-radius:4px}.telegram-purpose-share{color:var(--info);background:color-mix(in srgb,var(--info) 12%,var(--input))}.telegram-purpose-offline{color:color-mix(in srgb,var(--amber) 60%,var(--text));background:color-mix(in srgb,var(--amber) 14%,var(--input))}.telegram-purpose-disabled{color:var(--muted);background:var(--bg)}.telegram-target-form{display:grid;gap:20px}.telegram-purpose-settings{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:24px}.telegram-purpose-settings label{flex-direction:row;align-items:center;justify-content:space-between;gap:8px;min-height:40px}.telegram-purpose-settings .switch{flex:none}
:global(.page-content:has(>.notification-page)){min-height:calc(100dvh - 48px);display:flex;flex-direction:column;padding-bottom:var(--workspace-end-gap)}
.notification-page{display:flex;flex-direction:column;flex:1;min-height:0}.notification-page>.content-tabs{flex:none}.telegram-pools{flex:1;min-height:0;align-items:stretch}.telegram-pool{display:flex;flex-direction:column;min-height:0;height:calc(100dvh - 160px)}.telegram-pool>header{flex:none}.telegram-target-list{display:flex;flex-direction:column;gap:10px;overflow:auto;min-height:0;flex:1;padding:1px 2px 2px}.telegram-target-card,.telegram-add{flex:none}.telegram-target-card{cursor:pointer}.telegram-target-card:focus-visible{outline:2px solid var(--primary);outline-offset:-2px}.telegram-pool h2 :deep(.provider-logo),.telegram-pool h2 :deep([data-provider="115"] .provider-logo){width:30px;height:30px}.telegram-target-card>:deep([data-provider="115"] .provider-logo){width:28px;height:28px}
@media(max-width:1100px){.telegram-pools{flex:none}.telegram-pool{height:min(520px,calc(100dvh - 160px));min-height:260px}}
@media(min-width:1101px){:global(.page-content:has(.telegram-pools)){height:calc(100dvh - 48px);min-height:0;overflow:hidden}.telegram-pool{height:100%;overflow:hidden}}
.telegram-pools{flex:none;align-items:stretch}.telegram-pool{height:auto;min-height:0;--pool-accent:#299bce;background:color-mix(in srgb,var(--pool-accent) 7%,var(--surface))}.telegram-pool[data-provider="115"]{--pool-accent:#268be8}.telegram-pool[data-provider="mobile"]{--pool-accent:#3da38d}.telegram-pool[data-provider="quark"]{--pool-accent:#3e7ce8}.telegram-target-list{flex:none;height:284px;overflow:auto;scrollbar-width:none;gap:10px}.telegram-target-list::-webkit-scrollbar{display:none}.telegram-target-card{height:88px;min-height:88px;padding:10px 8px;gap:6px;background:var(--surface)}.telegram-target-avatar{display:grid;place-items:center;flex:none;width:38px;height:42px;border-radius:8px;background:color-mix(in srgb,var(--pool-accent) 13%,var(--surface));color:color-mix(in srgb,var(--pool-accent) 60%,var(--text));font-size:16px;font-weight:600}.telegram-add{margin-top:12px;height:42px;min-height:42px}.telegram-target-fields{display:grid;grid-template-columns:minmax(0,1fr);gap:16px}.telegram-target-fields.has-cycle{grid-template-columns:minmax(0,1fr) 140px}.telegram-target-fields label{min-width:0}.telegram-target-fields .source-trigger{height:40px}.telegram-target-fields .source-trigger span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.telegram-target-fields .source-trigger svg{flex:none}
:global(.page-content:has(.telegram-pools)){height:auto;overflow:visible}
.telegram-target-list{height:287px}
:global(.modal:has(.telegram-target-form)){scrollbar-width:none}
:global(.modal:has(.telegram-target-form)::-webkit-scrollbar){display:none}
@media(max-width:680px){.telegram-target-fields.has-cycle{grid-template-columns:minmax(0,1fr) 110px}.telegram-pool{height:auto;min-height:0}}
</style>
