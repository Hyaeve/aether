<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, reload, notify, drivers, driverOf, bytes } from '../lib'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import Modal from '../components/Modal.vue'
import NumberInput from '../components/NumberInput.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import SecretInput from '../components/SecretInput.vue'
import LocalDirectoryPicker from '../components/LocalDirectoryPicker.vue'
const directoryPicker = ref(false)

const query = ref(''), modal = ref(false), step = ref(1), selected = ref(''), editing = ref(''), busy = ref(false), error = ref(''), confirmDelete = ref(null)
const testing = ref('')
const menu = ref('')
const dragging = ref(''), dropTarget = ref(''), sorting = ref(false)
const armed = ref('')
let holdTimer, suppressClick = false
function hold(event, s) {
  if (event.button !== 0 || event.target.closest('button')) return
  clearTimeout(holdTimer)
  holdTimer = setTimeout(() => { armed.value = s.id; suppressClick = true }, 160)
}
function release() { clearTimeout(holdTimer); setTimeout(() => { armed.value = ''; suppressClick = false }, 100) }
function cardClick(event, s) { if (!suppressClick && !event.target.closest('button')) open(s) }
function dragStart(event, storage) {
  if (sorting.value || armed.value !== storage.id || event.target.closest('button')) { event.preventDefault(); return }
  closeMenu(); dragging.value = storage.id
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', storage.id)
}
function dragEnd() { dragging.value = ''; dropTarget.value = ''; release() }
async function moveStorage(id, target) {
  dragEnd()
  if (!id || id === target || sorting.value) return
  sorting.value = true
  try { await api('/storages/reorder', 'POST', { id, target }); await reload() }
  catch (e) { notify(e.message, true) }
  finally { sorting.value = false }
}
function reorderKey(event, storage) {
  if (event.target !== event.currentTarget || !event.altKey || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return
  event.preventDefault()
  const index = visible.value.findIndex(s => s.id === storage.id)
  const target = visible.value[index + (['ArrowLeft', 'ArrowUp'].includes(event.key) ? -1 : 1)]
  if (target) moveStorage(storage.id, target.id)
}
function closeMenu() { menu.value = '' }
function menuKey(event) { if (event.key === 'Escape') closeMenu() }
onMounted(() => { document.addEventListener('click', closeMenu); document.addEventListener('keydown', menuKey) })
onUnmounted(() => { clearTimeout(holdTimer); document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', menuKey) })
const toggling = ref('')
async function toggle(storage) {
  toggling.value = storage.id
  try { await api(`/storages/${storage.id}`, 'PUT', { ...storage, enabled: !storage.enabled }); await reload() }
  catch (e) { notify(e.message, true) } finally { toggling.value = '' }
}
const authorization = ref(false), qr = ref(null), authError = ref(''), authBusy = ref(false), authGeneration = ref(0)
const devices115 = [
  { value: 'web', label: '网页版' },
  { value: 'android', label: '安卓' },
  { value: 'ios', label: 'iOS' },
  { value: 'tv', label: '电视' },
  { value: 'alipaymini', label: '支付宝小程序' },
  { value: 'wechatmini', label: '微信小程序' },
  { value: 'qandroid', label: 'Android（新版）' }
]
let pollTimer
const form = reactive({ name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
const visible = computed(() => state.storages.filter(s => !query.value || s.name.toLowerCase().includes(query.value.toLowerCase())))
const picked = computed(() => driverOf(selected.value))
const cloudTypes = ['115', 'quark', 'mobile', 'tianyi']
const usages = ref({}), usageRevision = ref(0)
watch(() => JSON.stringify([usageRevision.value, state.authenticated, state.storages.map(s => [s.id, s.type, s.enabled, s.config])]), async (_, __, cleanup) => {
  const controller = new AbortController()
  cleanup(() => controller.abort())
  usages.value = {}
  if (!state.authenticated) return
  const pools = state.storages.filter(s => s.enabled && cloudTypes.includes(s.type))
  let index = 0
  const loadUsage = async () => {
    while (index < pools.length && !controller.signal.aborted) {
      const pool = pools[index++]
      try {
        const response = await fetch(`/api/storages/${encodeURIComponent(pool.id)}/usage`, { credentials:'same-origin', signal:controller.signal })
        if (!response.ok) continue
        const value = await response.json()
        if (!controller.signal.aborted) usages.value[pool.id] = value
      } catch { /* Optional quota lookup must not hide a storage card. */ }
    }
  }
  await Promise.all(Array.from({length:Math.min(3,pools.length)}, loadUsage))
}, { immediate:true })
const quotaPercent = id => Math.min(100, Math.max(0, (usages.value[id]?.used || 0) / usages.value[id].total * 100))
const downloadOptions = computed(() => selected.value === 'quark' ? [{ value: 'proxy', label: '本机代理' }] : [{ value: 'redirect', label: '302 重定向' }, { value: 'proxy', label: '本机代理' }])
function open(storage) {
  closeMenu()
  error.value = ''; editing.value = storage?.id || ''; step.value = storage ? 2 : 1; selected.value = storage?.type || ''
  Object.assign(form, storage ? JSON.parse(JSON.stringify(storage)) : { name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
  form.config.deleteMode ||= 'trash'
  form.config.downloadMode = storage?.type === 'quark' ? 'proxy' : form.config.downloadMode || 'redirect'
  form.config.passUA ||= 'true'
  form.config.refreshList ||= 'false'
  if (storage?.type === 'openlist') form.config.authMode ||= 'token'
  if (storage?.type === 'tianyi') form.config.authMode ||= form.config.accessToken || form.config.refreshToken ? 'token' : 'account'
  if (storage?.type === '115') {
    form.config.cookie ||= form.config.ck || ''
    form.config.device ||= 'web'
    delete form.config.ck
    delete form.config.accessToken
    delete form.config.refreshToken
  }
  if (storage?.type === 'tianyi' && storage.config.mode !== 'native') {
    form.config = { root: '-11', deleteMode: form.config.deleteMode, mode: 'native' }
  }
  if (storage?.type === 'mobile' && storage.config.mode !== 'native') {
    form.config = { root: '/', mode: 'native', deleteMode: form.config.deleteMode }
  }
  modal.value = true
}
function next(type) {
  selected.value = type; form.type = type; form.config = { root: driverOf(type).root, deleteMode: 'trash', downloadMode: type === 'quark' ? 'proxy' : 'redirect', passUA: 'true', refreshList: 'false', ...(type === 'mobile' ? { mode: 'native' } : {}), ...(type === '115' ? { device: 'web', cookie: '' } : {}) }; step.value = 2
}
function changeOpenlistMode(mode) {
  if (mode !== (form.config.authMode || 'token')) {
    form.config.password = ''; form.config.token = ''; form.config.username = ''
  }
  form.config.authMode = mode
}
function closeAuthorization() { authorization.value = false; clearTimeout(pollTimer); authGeneration.value++; authBusy.value = false; qr.value = null }
watch(modal, value => { if (!value) closeAuthorization() })
watch(() => [selected.value, form.config.device], () => { if (authorization.value) closeAuthorization() })
onUnmounted(closeAuthorization)
async function startAuthorization() {
  closeAuthorization(); authorization.value = true; authError.value = ''; qr.value = null
  await requestAuthorization()
}
async function requestAuthorization() {
  clearTimeout(pollTimer); authGeneration.value++
  authError.value = ''; qr.value = null
  authBusy.value = true
  const generation = authGeneration.value
  const provider = selected.value
  const device = provider === '115' ? form.config.device : undefined
  try {
    const result = await api(`/authorization/${provider}/start`, 'POST', provider === '115' ? { device } : undefined)
    if (generation !== authGeneration.value) return
    if (!result.token || !result.image) throw new Error('未获取到二维码，请重新获取')
    qr.value = result
    const token = result.token
    const expiresIn = Number(result.expiresIn)
    const deadline = Date.now() + (Number.isFinite(expiresIn) && expiresIn > 0 ? expiresIn : 300) * 1000
    async function poll() {
      if (generation !== authGeneration.value) return
      if (Date.now() >= deadline) { authError.value = '二维码已过期，请重新获取'; return }
      try {
        const result = await api(`/authorization/${provider}/poll`, 'POST', { token, ...(provider === '115' ? { device } : {}) })
        if (generation !== authGeneration.value) return
        if (result.status === 'expired') { authError.value = '二维码已失效，请重新获取'; return }
        if (['cancelled', 'canceled', 'denied'].includes(result.status)) { authError.value = '扫码授权已取消，请重新获取'; return }
        if (provider === 'mobile' && result.status === 'success') {
          if (typeof result.authorization !== 'string' || !result.authorization.trim()) throw new Error('授权未返回有效 Authorization')
          form.config.authorization = result.authorization
          closeAuthorization(); notify('Authorization 已填入，请保存存储池'); return
        }
        if (provider === 'tianyi' && result.status === 'success') {
          if (!result.accessToken && !result.refreshToken) throw new Error('授权未返回有效令牌')
          form.config.authMode = 'token'; form.config.accessToken = result.accessToken || ''; form.config.refreshToken = result.refreshToken || ''
          form.config.username = ''; form.config.password = ''
          closeAuthorization(); notify('天翼令牌已填入，请保存存储池'); return
        }
        const cookie = result.cookie || result.ck
        if (result.status === 'success' || (!result.status && cookie)) {
          if (typeof cookie !== 'string' || !cookie.trim()) throw new Error('授权未返回有效 CK，请重新获取')
          if (provider === '115' && result.device && !devices115.some(d => d.value === result.device)) throw new Error('授权返回了不支持的设备类型，请重新获取')
          form.config.cookie = cookie
          if (provider === '115') form.config.device = result.device || device
          closeAuthorization(); notify(provider === '115' ? 'CK 已填入，请保存存储池' : '授权已填入，请保存存储池'); return
        }
        if (result.status === 'error') throw new Error(result.error || '扫码授权失败，请重新获取')
        pollTimer = setTimeout(poll, 2000)
      } catch (e) { if (generation === authGeneration.value) authError.value = e.message }
    }
    pollTimer = setTimeout(poll, 2000)
  } catch (e) { if (generation === authGeneration.value) authError.value = e.message }
  finally { if (generation === authGeneration.value) authBusy.value = false }
}
async function save() {
  error.value = ''; busy.value = true
  try {
    if (state.storages.some(s => s.id !== editing.value && s.name.trim().toLowerCase() === form.name.trim().toLowerCase())) throw new Error('存储池名称已存在')
    await api(editing.value ? `/storages/${editing.value}` : '/storages', editing.value ? 'PUT' : 'POST', form)
    await reload(); usageRevision.value++; modal.value = false; notify(editing.value ? '存储池已更新' : '存储池已添加')
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function test(s) {
  testing.value = s.id
  try { await api(`/storages/${s.id}/test`, 'POST'); notify(`${s.name} 连接成功`) }
  catch (e) { notify(e.message, true) }
  finally { testing.value = ''; await reload() }
}
async function remove() {
  busy.value = true
  try { await api(`/storages/${confirmDelete.value.id}`, 'DELETE'); confirmDelete.value = null; await reload(); notify('存储池已删除') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
</script>

<template>
  <div class="storage-grid">
    <article v-for="s in visible" :key="s.id" class="storage-card" :class="{ 'menu-open': menu === s.id, 'drag-armed': armed === s.id, dragging: dragging === s.id, 'drop-target': dropTarget === s.id, 'storage-disabled': !s.enabled, 'storage-unhealthy': s.enabled && s.status === 'error' }" :draggable="armed === s.id && !sorting" tabindex="0" :aria-label="`${s.name}，${s.enabled ? '已启用' : '已停用'}`" aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown" @pointerdown="hold($event,s)" @pointerup="release" @pointerleave="!dragging && release()" @click="cardClick($event,s)" @contextmenu.prevent.stop="menu = s.id" @keydown="reorderKey($event, s)" @dragstart="dragStart($event, s)" @dragend="dragEnd" @dragover.prevent="dragging && (dropTarget = s.id)" @dragleave.self="dropTarget = ''" @drop.prevent="moveStorage(dragging, s.id)">
      <div class="storage-card-top"><button class="provider-toggle" :aria-label="`${s.enabled ? '停用' : '启用'}存储池 ${s.name}`" :aria-pressed="s.enabled" :disabled="!!toggling" @click.stop="toggle(s)"><ProviderIcon :type="s.type" /></button><div class="storage-card-name"><h3>{{ s.name }}</h3><div v-if="cloudTypes.includes(s.type) && usages[s.id]?.total > 0" class="storage-quota"><div class="storage-quota-track" role="meter" aria-label="云盘空间使用量" :aria-valuenow="usages[s.id].used" :aria-valuemax="usages[s.id].total" aria-valuemin="0" :aria-valuetext="`${bytes(usages[s.id].used)} / ${bytes(usages[s.id].total)}`"><i :style="{width:`${quotaPercent(s.id)}%`}" /></div><small>{{ bytes(usages[s.id].used) }} / {{ bytes(usages[s.id].total) }}</small></div><span v-else>{{ usages[s.id]?.username || driverOf(s.type).name }}</span></div><div class="storage-menu-control" @click.stop><button class="icon-btn" :aria-label="`存储操作 ${s.name}`" :aria-expanded="menu === s.id" @click="menu = menu === s.id ? '' : s.id"><Icon name="EllipsisVertical" /></button><div v-if="menu === s.id" class="storage-menu"><button @click="open(s)"><Icon name="Pencil" />编辑存储</button><button :disabled="testing === s.id || !s.enabled" @click="closeMenu(); test(s)"><Icon name="Activity" />测试连接</button><button @click="closeMenu(); toggle(s)"><Icon name="Power" />{{ s.enabled ? '停用存储' : '启用存储' }}</button><button class="danger-text" @click="closeMenu(); confirmDelete = s"><Icon name="Trash2" />删除存储</button></div></div></div>
    </article>
    <button class="add-storage-tile" aria-label="添加存储池" @click="open()"><span class="add-tile-icon"><Icon name="Plus" :size="25" /></span><strong>添加存储池</strong></button>
  </div>
  <div v-if="!visible.length && query" class="small-empty">没有匹配的存储池</div>
  <Modal v-if="modal" :title="editing ? '编辑存储池' : step === 1 ? '添加存储池' : '配置存储池'" compact wide @close="!busy && (modal = false)">
    <div v-if="step === 1" class="modal-body">
      <div class="driver-grid"><button v-for="d in drivers" :key="d.id" class="driver-option" :data-provider="d.id" @click="next(d.id)"><ProviderIcon :type="d.id" /><strong>{{ d.name }}</strong></button></div>
    </div>
    <form v-else @submit.prevent="save">
      <div class="modal-body">
        <div class="selected-driver"><ProviderIcon :type="selected" small /><h3>{{ picked.name }}</h3></div>
        <div class="form-grid">
          <label :class="{ full: !['115', 'tianyi'].includes(selected) }">存储池名称 <span class="required">*</span><input v-model="form.name" required maxlength="60" /></label>
          <div v-if="selected === '115'" class="field"><label>设备类型</label><RoundedSelect v-model="form.config.device" label="设备类型" :options="devices115" /></div>
          <div v-if="selected === 'tianyi'" class="field"><label>接入模式</label><RoundedSelect :model-value="form.config.authMode || 'account'" @update:model-value="form.config.authMode = $event" label="天翼接入模式" :options="[{ value: 'account', label: '账号密码' }, { value: 'token', label: 'Token 令牌' }]" /></div>
          <label v-if="selected === 'mobile'" class="full">Authorization<SecretInput v-model="form.config.authorization" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="authorization" aria-label="Authorization" required autocomplete="off" /><small>新版个人云，支持 CAS；授权失效后需更新。</small></label>
          <template v-if="selected === '115'">
            <label class="full storage-cookie">CK <span class="required">*</span><SecretInput v-model="form.config.cookie" aria-label="CK" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="cookie" required autocomplete="off" /></label>
          </template>
          <label v-if="selected === 'quark'" class="full storage-cookie">CK <span class="required">*</span><SecretInput v-model="form.config.cookie" aria-label="CK" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="cookie" required autocomplete="off" /></label>
          <template v-if="selected === 'tianyi'">
            <template v-if="form.config.authMode !== 'token'"><label>天翼账号<input v-model="form.config.username" required autocomplete="off" /></label><label>天翼密码<SecretInput v-model="form.config.password" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="password" required autocomplete="new-password" /></label></template>
            <template v-else><label>访问令牌<SecretInput v-model="form.config.accessToken" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="accessToken" autocomplete="off" /></label><label>刷新令牌<SecretInput v-model="form.config.refreshToken" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="refreshToken" autocomplete="off" /></label></template>
          </template>
          <template v-if="cloudTypes.includes(selected)">
            <div class="field"><label>下载模式</label><RoundedSelect v-model="form.config.downloadMode" label="下载模式" :options="downloadOptions" /></div>
            <div class="field"><label>删除模式</label><RoundedSelect v-model="form.config.deleteMode" label="删除模式" :options="[{ value: 'trash', label: '移到回收站' }, { value: 'permanent', label: '永久删除' }]" /></div>
          </template>
          <template v-if="['openlist', 'webdav'].includes(selected)">
            <label :class="{ full: selected === 'webdav' }">服务地址 <span class="required">*</span><input v-model="form.config.address" required type="url" placeholder="https://storage.example.com" /></label>
            <div v-if="selected === 'openlist'" class="field"><label>接入模式</label><RoundedSelect :model-value="form.config.authMode || 'token'" @update:model-value="changeOpenlistMode" label="接入模式" :options="[{ value: 'token', label: 'API令牌' }, { value: 'account', label: '账号密码' }]" /></div>
            <label v-if="selected === 'openlist' && form.config.authMode !== 'account'" class="full">API令牌<SecretInput v-model="form.config.token" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="token" autocomplete="off" /></label>
            <template v-if="selected === 'webdav' || form.config.authMode === 'account'"><label>账号<input v-model="form.config.username" autocomplete="off" :required="selected === 'openlist'" /></label><label>密码<SecretInput v-model="form.config.password" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="password" autocomplete="off" :required="selected === 'openlist'" /></label></template>
          </template>
          <div v-if="selected === 'local'" class="field"><label for="storage-local-directory">本地目录</label><div class="directory-input"><input id="storage-local-directory" v-model="form.config.root" required /><button type="button" class="icon-btn" aria-label="选择本地目录" @click="directoryPicker = true"><Icon name="FolderOpen" /></button></div></div>
          <label v-else>{{ ['115', 'quark', 'tianyi'].includes(selected) ? '根目录 ID' : '根目录路径' }}<input v-model="form.config.root" /></label>
          <div v-if="selected === 'openlist'" class="field"><label>透传 UA 给上游</label><RoundedSelect v-model="form.config.passUA" label="透传 UA 给上游" :options="[{value:'true',label:'是'}, {value:'false',label:'否'}]" /></div>
          <div v-else class="field"><label for="storage-cache">缓存时间</label><NumberInput id="storage-cache" v-model="form.cacheTTL" aria-label="缓存时间" unit="分钟" min="0" max="525600" /><small>0 跟随全局设置</small></div>
          <div v-if="selected === 'openlist'" class="field"><label>列目录时刷新上游</label><RoundedSelect v-model="form.config.refreshList" label="列目录时刷新上游" :options="[{value:'true',label:'是'}, {value:'false',label:'否'}]" /></div>
          <div v-if="selected === 'webdav'" class="field"><label for="dav-timeout">请求超时</label><NumberInput id="dav-timeout" :model-value="Number(form.config.timeoutSeconds || 60)" @update:model-value="form.config.timeoutSeconds = String($event)" unit="秒" min="1" max="600" /></div>
          <div v-if="['openlist', 'webdav'].includes(selected)" class="field"><label>下载模式</label><RoundedSelect v-model="form.config.downloadMode" label="下载模式" :options="downloadOptions" /></div>
        </div>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button v-if="['115', 'quark', 'tianyi', 'mobile'].includes(selected)" type="button" class="btn auth-button" :disabled="busy" @click="startAuthorization"><Icon name="ShieldCheck" />{{ selected === '115' ? '扫码获取 CK' : selected === 'tianyi' ? '扫码获取 Token' : selected === 'mobile' ? '扫码获取 Authorization' : '扫码获取授权' }}</button><button v-if="!editing" type="button" class="btn" :disabled="busy" @click="step = 1"><Icon name="ArrowLeft" />上一步</button><button class="btn primary" :disabled="busy"><Icon name="Check" />{{ busy ? '保存中…' : '保存存储池' }}</button></footer>
    </form>
  </Modal>
  <LocalDirectoryPicker v-if="directoryPicker" :initial="form.config.root" @close="directoryPicker = false" @select="form.config.root = $event; directoryPicker = false" />
  <Modal v-if="authorization" :title="selected === '115' ? '115 扫码获取 CK' : selected === 'tianyi' ? '天翼扫码获取 Token' : selected === 'mobile' ? '移动扫码获取 Authorization' : '夸克扫码授权'" @close="closeAuthorization">
    <div class="modal-body qr-authorization">
      <p v-if="authBusy" role="status">正在获取二维码…</p>
      <img v-if="qr && !authError" :src="qr.image" :alt="selected === '115' ? '115 授权二维码' : selected === 'tianyi' ? '天翼授权二维码' : '夸克授权二维码'" />
      <p v-if="qr && !authError" role="status">请使用{{ picked.name }} App 扫码确认</p>
      <p v-if="authError" class="error-message" role="alert">{{ authError }}</p>
      <button v-if="authError" class="btn" :disabled="authBusy" @click="startAuthorization"><Icon name="RefreshCw" />重新获取</button>
    </div>
  </Modal>
  <Modal v-if="confirmDelete" title="删除存储池" @close="confirmDelete = null"><div class="modal-body"><p>确认删除「{{ confirmDelete.name }}」？此操作不会删除存储中的文件。</p></div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除存储池</button></footer></Modal>
</template>
