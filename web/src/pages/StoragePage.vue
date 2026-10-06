<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, reload, notify, drivers, driverOf } from '../lib'
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
  holdTimer = setTimeout(() => { armed.value = s.id; suppressClick = true }, 450)
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
const oauthBase = ref(localStorage.getItem('aether-oauth-base') || 'https://oauth.litepan.top')
let authWindow
let pollTimer
const form = reactive({ name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
const visible = computed(() => state.storages.filter(s => !query.value || s.name.toLowerCase().includes(query.value.toLowerCase())))
const online = computed(() => state.storages.filter(s => s.status === 'connected' && s.enabled).length)
const picked = computed(() => driverOf(selected.value))
function open(storage) {
  closeMenu()
  error.value = ''; editing.value = storage?.id || ''; step.value = storage ? 2 : 1; selected.value = storage?.type || ''
  Object.assign(form, storage ? JSON.parse(JSON.stringify(storage)) : { name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
  form.config.deleteMode ||= 'trash'
  if (storage?.type === 'tianyi' && storage.config.mode !== 'native') {
    form.config = { root: '-11', deleteMode: form.config.deleteMode, mode: 'native' }
  }
  if (storage?.type === 'mobile' && storage.config.mode !== 'native') {
    form.config = { root: '/', mode: 'native', deleteMode: form.config.deleteMode }
  }
  modal.value = true
}
function next(type) {
  selected.value = type; form.type = type; form.config = { root: driverOf(type).root, deleteMode: 'trash', ...(type === 'mobile' ? { mode: 'native' } : {}) }; step.value = 2
}
function closeAuthorization() { authorization.value = false; clearTimeout(pollTimer); authGeneration.value++; authBusy.value = false; if (authWindow && !authWindow.closed) authWindow.close(); authWindow = null }
function openAuthorizationWindow() {
  authWindow = window.open('', '_blank')
  if (!authWindow) { authError.value = '请允许弹出窗口后重试'; notify(authError.value, true); return false }
  authWindow.opener = null
  authWindow.document.title = '115 Open 授权'
  authWindow.document.body.textContent = '正在连接授权服务，即将打开 115 账号登录页面…'
  return true
}
watch(modal, value => { if (!value) closeAuthorization() })
onUnmounted(closeAuthorization)
async function startAuthorization() {
  closeAuthorization(); authorization.value = true; authError.value = ''; qr.value = null
  if (selected.value === '115' && !openAuthorizationWindow()) return
  await requestAuthorization()
}
async function requestAuthorization() {
  if (selected.value === '115' && (!authWindow || authWindow.closed) && !openAuthorizationWindow()) return
  clearTimeout(pollTimer); authGeneration.value++
  authError.value = ''; qr.value = null
  authBusy.value = true
  const generation = authGeneration.value
  const provider = selected.value
  try {
    const result = await api(`/authorization/${provider}/start`, 'POST', provider === '115' ? { base: oauthBase.value } : undefined)
    if (generation !== authGeneration.value) return
    qr.value = result
    if (provider === '115') {
      const url = new URL(result.url)
      if (url.protocol !== 'https:' || url.username || url.password) throw new Error('授权地址无效')
      localStorage.setItem('aether-oauth-base', oauthBase.value)
      if (authWindow && !authWindow.closed) authWindow.location.replace(url.href)
    }
    const deadline = Date.now() + result.expiresIn * 1000
    async function poll() {
      if (generation !== authGeneration.value) return
      if (provider === '115' && authWindow?.closed) { authError.value = '授权窗口已关闭，请重试'; return }
      if (Date.now() >= deadline) { authError.value = '二维码已过期，请重新获取'; return }
      try {
        const result = await api(`/authorization/${provider}/poll`, 'POST', { token: qr.value.token })
        if (generation !== authGeneration.value) return
        if (result.status === 'success') {
          if (provider === 'quark') form.config.cookie = result.cookie
          else { form.config.accessToken = result.accessToken; form.config.refreshToken = result.refreshToken }
          closeAuthorization(); notify('授权已填入，请保存存储池'); return
        }
        if (result.status === 'expired') { authError.value = '二维码已失效，请重新获取'; return }
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
    await api(editing.value ? `/storages/${editing.value}` : '/storages', editing.value ? 'PUT' : 'POST', form)
    await reload(); modal.value = false; notify(editing.value ? '存储池已更新' : '存储池已添加')
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
  <div class="metric-strip">
    <div><span class="metric-icon"><Icon name="Layers3" /></span><span><small>全部存储池</small><strong>{{ state.storages.length }}<em>个</em></strong></span></div>
    <div><span class="metric-icon green"><Icon name="CircleCheck" /></span><span><small>已连接</small><strong>{{ online }}<em>个</em></strong></span></div>
    <div><span class="metric-icon amber"><Icon name="Cloud" /></span><span><small>云端存储</small><strong>{{ state.storages.filter(s => s.type !== 'local').length }}<em>个</em></strong></span></div>
    <div><span class="metric-icon neutral"><Icon name="HardDrive" /></span><span><small>本地存储</small><strong>{{ state.storages.filter(s => s.type === 'local').length }}<em>个</em></strong></span></div>
  </div>
  <div class="storage-grid">
    <article v-for="s in visible" :key="s.id" class="storage-card" :class="{ 'menu-open': menu === s.id, 'drag-armed': armed === s.id, dragging: dragging === s.id, 'drop-target': dropTarget === s.id, 'storage-disabled': !s.enabled }" :draggable="armed === s.id && !sorting" tabindex="0" :aria-label="`${s.name}，${s.enabled ? '已启用' : '已停用'}`" aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown" @pointerdown="hold($event,s)" @pointerup="release" @pointerleave="!dragging && release()" @click="cardClick($event,s)" @contextmenu.prevent.stop="menu = s.id" @keydown="reorderKey($event, s)" @dragstart="dragStart($event, s)" @dragend="dragEnd" @dragover.prevent="dragging && (dropTarget = s.id)" @dragleave.self="dropTarget = ''" @drop.prevent="moveStorage(dragging, s.id)">
      <div class="storage-card-top"><button class="provider-toggle" :aria-label="`${s.enabled ? '停用' : '启用'}存储池 ${s.name}`" :aria-pressed="s.enabled" :disabled="!!toggling" @click.stop="toggle(s)"><ProviderIcon :type="s.type" /></button><div class="storage-card-name"><h3>{{ s.name }}</h3><span>{{ driverOf(s.type).name }}</span></div><div class="storage-menu-control" @click.stop><button class="icon-btn" :aria-label="`存储操作 ${s.name}`" :aria-expanded="menu === s.id" @click="menu = menu === s.id ? '' : s.id"><Icon name="EllipsisVertical" /></button><div v-if="menu === s.id" class="storage-menu"><button @click="open(s)"><Icon name="Pencil" />编辑存储</button><button :disabled="testing === s.id || !s.enabled" @click="closeMenu(); test(s)"><Icon name="Activity" />测试连接</button><button @click="closeMenu(); toggle(s)"><Icon name="Power" />{{ s.enabled ? '停用存储' : '启用存储' }}</button><button class="danger-text" @click="closeMenu(); confirmDelete = s"><Icon name="Trash2" />删除存储</button></div></div></div>
    </article>
    <button class="add-storage-tile" aria-label="添加存储池" @click="open()"><span class="add-tile-icon"><Icon name="Plus" :size="25" /></span><strong>添加存储池</strong></button>
  </div>
  <div v-if="!visible.length && query" class="small-empty">没有匹配的存储池</div>
  <Modal v-if="modal" :title="editing ? '编辑存储池' : step === 1 ? '添加存储池' : '配置存储池'" compact wide @close="!busy && (modal = false)">
    <div v-if="step === 1" class="modal-body">
      <div class="driver-grid"><button v-for="d in drivers" :key="d.id" class="driver-option" @click="next(d.id)"><ProviderIcon :type="d.id" /><strong>{{ d.name }}</strong></button></div>
    </div>
    <form v-else @submit.prevent="save">
      <div class="modal-body">
        <div class="selected-driver"><ProviderIcon :type="selected" small /><h3>{{ picked.name }}</h3></div>
        <div class="form-grid">
          <label>存储池名称 <span class="required">*</span><input v-model="form.name" required maxlength="60" placeholder="例如：家庭影音库" /></label>
          <div class="field"><label>删除模式</label><RoundedSelect v-model="form.config.deleteMode" label="删除模式" :options="[{ value: 'trash', label: '移到回收站' }, { value: 'permanent', label: '永久删除' }]" /></div>
          <label v-if="selected === 'mobile'" class="full">Authorization<SecretInput v-model="form.config.authorization" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="authorization" required autocomplete="off" /><small>新版个人云，支持 CAS；授权失效后需更新。</small></label>
          <template v-if="selected === '115'"><label>Access Token <span class="required">*</span><SecretInput v-model="form.config.accessToken" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="accessToken" required autocomplete="off" /></label><label>Refresh Token<SecretInput v-model="form.config.refreshToken" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="refreshToken" autocomplete="off" /></label></template>
          <small v-if="selected === '115'" class="full muted">获取 TOKEN 将通过第三方 OAuth 服务打开 115 登录授权；授权服务会接收本次生成的令牌。</small>
          <label v-if="selected === 'quark'" class="full storage-cookie">Cookie <span class="required">*</span><SecretInput v-model="form.config.cookie" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="cookie" required autocomplete="off" placeholder="粘贴夸克网页版的完整 Cookie" /></label>
          <template v-if="selected === 'tianyi'"><label>天翼账号<input v-model="form.config.username" required autocomplete="off" /></label><label>天翼密码<SecretInput v-model="form.config.password" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="password" required autocomplete="new-password" /></label></template>
          <template v-if="['openlist', 'webdav'].includes(selected)">
            <label class="full">{{ selected === 'webdav' ? 'WebDAV' : 'OpenList' }} 服务地址 <span class="required">*</span><input v-model="form.config.address" required type="url" placeholder="https://storage.example.com" /></label>
            <label v-if="selected !== 'webdav'" class="full">API Token<SecretInput v-model="form.config.token" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="token" autocomplete="off" /></label>
            <label v-if="selected === 'webdav'">用户名<input v-model="form.config.username" autocomplete="off" /></label><label :class="{ full: selected !== 'webdav' }">{{ selected === 'webdav' ? '密码' : '目录访问密码（可选）' }}<SecretInput v-model="form.config.password" :secret-path="editing ? `/storages/${editing}/secret` : ''" secret-field="password" autocomplete="off" /></label>
          </template>
          <div v-if="selected === 'local'" class="field"><label for="storage-local-directory">本地目录</label><div class="directory-input"><input id="storage-local-directory" v-model="form.config.root" required /><button type="button" class="icon-btn" aria-label="选择本地目录" @click="directoryPicker = true"><Icon name="FolderOpen" /></button></div></div>
          <label v-else>{{ ['115', 'quark', 'tianyi'].includes(selected) ? '根目录 ID' : '根目录路径' }}<input v-model="form.config.root" /></label>
          <div class="field"><label for="storage-cache">缓存时间</label><NumberInput id="storage-cache" v-model="form.cacheTTL" aria-label="缓存时间" unit="分钟" min="0" max="525600" /><small>0 跟随全局设置</small></div>
          <label class="toggle-line full"><span>启用此存储池</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" /></label>
        </div>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button v-if="['115', 'quark'].includes(selected)" type="button" class="btn auth-button" :disabled="busy" @click="startAuthorization"><Icon name="ShieldCheck" />{{ selected === '115' ? '获取 TOKEN' : '扫码获取授权' }}</button><button v-if="!editing" type="button" class="btn" :disabled="busy" @click="step = 1"><Icon name="ArrowLeft" />上一步</button><button class="btn primary" :disabled="busy"><Icon name="Check" />{{ busy ? '保存中…' : '保存存储池' }}</button></footer>
    </form>
  </Modal>
  <LocalDirectoryPicker v-if="directoryPicker" :initial="form.config.root" @close="directoryPicker = false" @select="form.config.root = $event; directoryPicker = false" />
  <Modal v-if="authorization" :title="selected === '115' ? '获取 115 Open TOKEN' : '夸克扫码授权'" @close="closeAuthorization">
    <div v-if="selected === 'quark'" class="modal-body qr-authorization"><p v-if="authBusy">正在获取二维码…</p><img v-if="qr && !authError" :src="qr.image" alt="夸克授权二维码" /><p v-if="qr && !authError">请使用夸克网盘 App 扫码确认</p><p v-if="authError" class="error-message" role="alert">{{ authError }}</p><button v-if="authError" class="btn" @click="startAuthorization"><Icon name="RefreshCw" />重新获取</button></div>
    <div v-else class="modal-body oauth-authorization">
      <p v-if="authBusy" role="status">正在打开 115 登录授权…</p>
      <details><summary>授权服务设置</summary><label>OAuth 代理地址<input v-model="oauthBase" type="url" placeholder="https://oauth.example.com" :disabled="authBusy" /></label><p class="muted">默认使用 LitePan 的第三方授权服务。仅更换为你信任的兼容代理，Aether 不发送已有凭据。</p></details>
      <button class="btn primary" :disabled="authBusy || !oauthBase" @click="requestAuthorization">{{ authBusy ? '正在连接…' : '重新打开授权' }}</button>
      <a v-if="qr?.url && !authError" class="btn" :href="qr.url" target="_blank" rel="noopener noreferrer"><Icon name="ArrowUpRight" />打开授权页面</a>
      <p v-if="qr?.url && !authError" role="status">等待授权完成，令牌将自动填入存储表单。</p>
      <p v-if="authError" class="error-message" role="alert">{{ authError }}</p>
    </div>
  </Modal>
  <Modal v-if="confirmDelete" title="删除存储池" @close="confirmDelete = null"><div class="modal-body"><p>确认删除「{{ confirmDelete.name }}」？此操作不会删除存储中的文件。</p></div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除存储池</button></footer></Modal>
</template>
