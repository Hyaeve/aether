<script setup>
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, reload, notify, drivers, driverOf } from '../lib'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import Modal from '../components/Modal.vue'
import NumberInput from '../components/NumberInput.vue'

const query = ref(''), modal = ref(false), step = ref(1), selected = ref(''), editing = ref(''), busy = ref(false), error = ref(''), confirmDelete = ref(null)
const testing = ref('')
const authorization = ref(false), qr = ref(null), authError = ref(''), authBusy = ref(false), authGeneration = ref(0)
const oauthBase = ref('')
let pollTimer
const form = reactive({ name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
const visible = computed(() => state.storages.filter(s => !query.value || s.name.toLowerCase().includes(query.value.toLowerCase())))
const online = computed(() => state.storages.filter(s => s.status === 'connected' && s.enabled).length)
const picked = computed(() => driverOf(selected.value))
function open(storage) {
  error.value = ''; editing.value = storage?.id || ''; step.value = storage ? 2 : 1; selected.value = storage?.type || ''
  Object.assign(form, storage ? JSON.parse(JSON.stringify(storage)) : { name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
  form.config.deleteMode ||= 'trash'
  if (storage?.type === 'tianyi' && storage.config.mode !== 'native') {
    form.config = { root: '-11', deleteMode: form.config.deleteMode, mode: 'native' }
  }
  modal.value = true
}
function next(type) {
  selected.value = type; form.type = type; form.config = { root: driverOf(type).root, deleteMode: 'trash', ...(type === 'mobile' ? { mode: 'native' } : {}) }; step.value = 2
}
function closeAuthorization() { authorization.value = false; clearTimeout(pollTimer); authGeneration.value++; authBusy.value = false }
watch(modal, value => { if (!value) closeAuthorization() })
onUnmounted(closeAuthorization)
async function startAuthorization() {
  closeAuthorization(); authorization.value = true; authError.value = ''; qr.value = null
  if (selected.value !== 'quark') return
  await requestAuthorization()
}
async function requestAuthorization() {
  clearTimeout(pollTimer); authGeneration.value++
  authError.value = ''; qr.value = null
  authBusy.value = true
  const generation = authGeneration.value
  const provider = selected.value
  try {
    const result = await api(`/authorization/${provider}/start`, 'POST', provider === '115' ? { base: oauthBase.value } : undefined)
    if (generation !== authGeneration.value) return
    qr.value = result
    const deadline = Date.now() + result.expiresIn * 1000
    async function poll() {
      if (generation !== authGeneration.value) return
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
  <section class="storage-actions">
    <button class="icon-btn bordered" title="刷新" aria-label="刷新" @click="reload().catch(e => notify(e.message, true))"><Icon name="RefreshCw" /></button>
    <div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="搜索存储池" placeholder="搜索存储池…" /></div>
    <button class="btn primary" @click="open()"><Icon name="Plus" />添加存储池</button>
  </section>
  <div class="metric-strip">
    <div><span class="metric-icon"><Icon name="Layers3" /></span><span><small>全部存储池</small><strong>{{ state.storages.length }}<em>个</em></strong></span></div>
    <div><span class="metric-icon green"><Icon name="CircleCheck" /></span><span><small>已连接</small><strong>{{ online }}<em>个</em></strong></span></div>
    <div><span class="metric-icon amber"><Icon name="Cloud" /></span><span><small>云端存储</small><strong>{{ state.storages.filter(s => s.type !== 'local').length }}<em>个</em></strong></span></div>
    <div><span class="metric-icon neutral"><Icon name="HardDrive" /></span><span><small>本地存储</small><strong>{{ state.storages.filter(s => s.type === 'local').length }}<em>个</em></strong></span></div>
  </div>
  <div class="storage-grid">
    <article v-for="s in visible" :key="s.id" class="storage-card">
      <div class="storage-card-top"><ProviderIcon :type="s.type" /><div class="storage-card-name"><h3>{{ s.name }}</h3><span>{{ driverOf(s.type).name }}</span></div><button class="icon-btn" title="编辑存储池" aria-label="编辑存储池" @click="open(s)"><Icon name="Ellipsis" /></button></div>
      <div class="storage-card-status"><span class="status" :class="!s.enabled ? 'muted' : s.status === 'connected' ? 'success' : s.status === 'error' ? 'danger' : 'pending'"><i />{{ !s.enabled ? '已停用' : s.status === 'connected' ? '连接正常' : s.status === 'error' ? '连接异常' : '待验证' }}</span><span>{{ s.cacheTTL ? `${s.cacheTTL} 分钟缓存` : '跟随全局缓存' }}</span></div>
      <div class="storage-root"><Icon name="Folder" :size="15" /><code>{{ s.config.root || '/' }}</code></div>
      <p v-if="s.lastError" class="card-error">{{ s.lastError }}</p>
      <footer><button class="text-btn" :disabled="testing === s.id || !s.enabled" @click="test(s)"><Icon name="Activity" :size="15" />{{ testing === s.id ? '正在连接…' : '测试连接' }}</button><div><button class="icon-btn" title="浏览文件" aria-label="浏览文件" @click="$router.push(`/files?storage=${s.id}`)"><Icon name="FolderOpen" :size="17" /></button><button class="icon-btn danger-text" title="删除存储池" aria-label="删除存储池" @click="confirmDelete = s"><Icon name="Trash2" :size="16" /></button></div></footer>
    </article>
    <button class="add-storage-tile" @click="open()"><span class="add-tile-icon"><Icon name="Plus" :size="25" /></span><strong>添加存储池</strong><span>连接一个新的存储空间</span></button>
  </div>
  <div v-if="!visible.length && query" class="small-empty">没有匹配的存储池</div>
  <Modal v-if="modal" :title="editing ? '编辑存储池' : step === 1 ? '添加存储池' : '配置存储池'" compact wide @close="!busy && (modal = false)">
    <div v-if="step === 1" class="modal-body">
      <div class="driver-grid"><button v-for="d in drivers" :key="d.id" class="driver-option" @click="next(d.id)"><ProviderIcon :type="d.id" /><strong>{{ d.name }}</strong></button></div>
    </div>
    <form v-else @submit.prevent="save">
      <div class="modal-body">
        <div class="selected-driver"><ProviderIcon :type="selected" small /><h3>{{ picked.name }}</h3></div>
        <div v-if="selected === 'mobile' && form.config.mode !== 'native'" class="inline-note"><Icon name="Info" />此连接器通过 OpenList 网关接入，填写已挂载该网盘的 OpenList 地址和路径。</div>
        <div class="form-grid">
          <label class="full">存储池名称 <span class="required">*</span><input v-model="form.name" required maxlength="60" placeholder="例如：家庭影音库" /></label>
          <template v-if="selected === 'mobile'"><label class="full">接入方式<select :value="form.config.mode || 'gateway'" @change="form.config.mode = $event.target.value; form.config.root = '/'"><option value="native">原生新版个人云（支持 CAS）</option><option value="gateway">OpenList 网关</option></select></label><label v-if="form.config.mode === 'native'" class="full">Authorization<input v-model="form.config.authorization" type="password" required autocomplete="off" /><small>填写移动云盘 Authorization；失效后需更新。不支持旧版个人云、家庭云和群组云的 CAS。</small></label></template>
          <template v-if="selected === '115'"><label>Access Token <span class="required">*</span><input v-model="form.config.accessToken" type="password" required autocomplete="off" /></label><label>Refresh Token<input v-model="form.config.refreshToken" type="password" autocomplete="off" /></label></template>
          <label v-if="selected === 'quark'" class="full">Cookie <span class="required">*</span><textarea v-model="form.config.cookie" required rows="3" autocomplete="off" placeholder="粘贴夸克网页版的完整 Cookie" /></label>
          <template v-if="selected === 'tianyi'"><label>天翼账号<input v-model="form.config.username" required autocomplete="off" /></label><label>天翼密码<input v-model="form.config.password" type="password" required autocomplete="new-password" /></label></template>
          <template v-if="['openlist', 'webdav'].includes(selected) || (selected === 'mobile' && form.config.mode !== 'native')">
            <label class="full">{{ selected === 'webdav' ? 'WebDAV' : 'OpenList' }} 服务地址 <span class="required">*</span><input v-model="form.config.address" required type="url" placeholder="https://storage.example.com" /></label>
            <label v-if="selected !== 'webdav'" class="full">API Token<input v-model="form.config.token" type="password" autocomplete="off" /></label>
            <label v-if="selected === 'webdav'">用户名<input v-model="form.config.username" autocomplete="off" /></label><label :class="{ full: selected !== 'webdav' }">{{ selected === 'webdav' ? '密码' : '目录访问密码（可选）' }}<input v-model="form.config.password" type="password" autocomplete="off" /></label>
          </template>
          <label>{{ selected === 'local' ? '本地根目录' : ['115', 'quark', 'tianyi'].includes(selected) ? '根目录 ID' : '根目录路径' }}<input v-model="form.config.root" :required="selected === 'local'" /></label>
          <div class="field"><label for="storage-cache">缓存时间</label><NumberInput id="storage-cache" v-model="form.cacheTTL" aria-label="缓存时间" unit="分钟" min="0" max="525600" /><small>0 跟随全局设置</small></div>
          <label>删除模式<select v-model="form.config.deleteMode"><option value="trash">移到回收站</option><option value="permanent">永久删除</option></select><small>{{ selected === 'tianyi' || (selected === 'mobile' && form.config.mode === 'native') ? '用于 CAS 临时文件清理。' : '当前文件服务只读，此设置预留。' }}</small></label>
          <label class="toggle-line full"><span>启用此存储池</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" /></label>
        </div>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button v-if="['115', 'quark'].includes(selected)" type="button" class="btn auth-button" :disabled="busy" @click="startAuthorization"><Icon name="ShieldCheck" />{{ selected === '115' ? '获取 TOKEN' : '扫码获取授权' }}</button><button v-if="!editing" type="button" class="btn" :disabled="busy" @click="step = 1"><Icon name="ArrowLeft" />上一步</button><button class="btn primary" :disabled="busy"><Icon name="Check" />{{ busy ? '保存中…' : '保存存储池' }}</button></footer>
    </form>
  </Modal>
  <Modal v-if="authorization" :title="selected === '115' ? '获取 115 Open TOKEN' : '夸克扫码授权'" @close="closeAuthorization">
    <div v-if="selected === 'quark'" class="modal-body qr-authorization"><p v-if="authBusy">正在获取二维码…</p><img v-if="qr && !authError" :src="qr.image" alt="夸克授权二维码" /><p v-if="qr && !authError">请使用夸克网盘 App 扫码确认</p><p v-if="authError" class="error-message" role="alert">{{ authError }}</p><button v-if="authError" class="btn" @click="startAuthorization"><Icon name="RefreshCw" />重新获取</button></div>
    <div v-else class="modal-body oauth-authorization">
      <label>OAuth 代理地址<input v-model="oauthBase" type="url" placeholder="https://oauth.example.com" :disabled="authBusy" /></label>
      <p class="muted">请仅填写你信任且支持 LitePan OAuth 协议的代理。该代理会接收本次授权生成的令牌，Aether 不发送已有凭据。</p>
      <button class="btn primary" :disabled="authBusy || !oauthBase" @click="requestAuthorization">{{ authBusy ? '正在连接…' : '连接授权代理' }}</button>
      <a v-if="qr?.url && !authError" class="btn" :href="qr.url" target="_blank" rel="noopener noreferrer"><Icon name="ArrowUpRight" />打开授权页面</a>
      <p v-if="qr?.url && !authError" role="status">等待授权完成，令牌将自动填入存储表单。</p>
      <p v-if="authError" class="error-message" role="alert">{{ authError }}</p>
    </div>
  </Modal>
  <Modal v-if="confirmDelete" title="删除存储池" @close="confirmDelete = null"><div class="modal-body"><p>确认删除「{{ confirmDelete.name }}」？此操作不会删除存储中的文件。</p></div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除存储池</button></footer></Modal>
</template>
