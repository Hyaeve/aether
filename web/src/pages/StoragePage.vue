<script setup>
import { computed, reactive, ref } from 'vue'
import { api, state, reload, notify, drivers, driverOf } from '../lib'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import Modal from '../components/Modal.vue'

const query = ref(''), filter = ref('all'), modal = ref(false), step = ref(1), selected = ref(''), editing = ref(''), busy = ref(false), error = ref(''), confirmDelete = ref(null)
const testing = ref('')
const form = reactive({ name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
const visible = computed(() => state.storages.filter(s => (!query.value || s.name.toLowerCase().includes(query.value.toLowerCase())) && (filter.value === 'all' || (filter.value === 'local' ? s.type === 'local' : s.type !== 'local'))))
const online = computed(() => state.storages.filter(s => s.status === 'connected' && s.enabled).length)
const picked = computed(() => driverOf(selected.value))
function open(storage) {
  error.value = ''; editing.value = storage?.id || ''; step.value = storage ? 2 : 1; selected.value = storage?.type || ''
  Object.assign(form, storage ? JSON.parse(JSON.stringify(storage)) : { name: '', type: '', enabled: true, cacheTTL: 0, config: {} })
  modal.value = true
}
function next(type) {
  selected.value = type; form.type = type; form.config = { root: driverOf(type).root }; step.value = 2
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
  <section class="page-head">
    <div><div class="eyebrow">STORAGE WORKSPACE</div><h1>存储管理<span class="title-dot">.</span></h1><p>连接云端与本地，让每一份文件各有所归。</p></div>
    <button class="btn primary" @click="open()"><Icon name="Plus" />添加存储池</button>
  </section>
  <div class="metric-strip">
    <div><span class="metric-icon"><Icon name="Layers3" /></span><span><small>全部存储池</small><strong>{{ state.storages.length }}<em>个</em></strong></span></div>
    <div><span class="metric-icon green"><Icon name="CircleCheck" /></span><span><small>已连接</small><strong>{{ online }}<em>个</em></strong></span></div>
    <div><span class="metric-icon amber"><Icon name="Cloud" /></span><span><small>云端存储</small><strong>{{ state.storages.filter(s => s.type !== 'local').length }}<em>个</em></strong></span></div>
    <div><span class="metric-icon neutral"><Icon name="HardDrive" /></span><span><small>本地存储</small><strong>{{ state.storages.filter(s => s.type === 'local').length }}<em>个</em></strong></span></div>
  </div>
  <div class="section-toolbar">
    <div class="tabs"><button :class="{ active: filter === 'all' }" @click="filter = 'all'">全部存储 <span>{{ state.storages.length }}</span></button><button :class="{ active: filter === 'cloud' }" @click="filter = 'cloud'">云端</button><button :class="{ active: filter === 'local' }" @click="filter = 'local'">本地</button></div>
    <div class="toolbar-right"><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="搜索存储池" placeholder="搜索存储池…" /></div><button class="icon-btn bordered" title="刷新" aria-label="刷新" @click="reload().catch(e => notify(e.message, true))"><Icon name="RefreshCw" /></button></div>
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
  <section class="supported-section"><div class="section-label"><h2>存储连接器</h2><span>{{ drivers.length }} 种存储类型</span></div><div class="connectors"><button v-for="d in drivers" :key="d.id" @click="open(); next(d.id)"><ProviderIcon :type="d.id" small /><span>{{ d.name }}<small>{{ d.auth }}</small></span><Icon name="ArrowUpRight" :size="15" /></button></div></section>
  <div class="page-foot"><span><Icon name="ShieldCheck" :size="15" />凭据加密保存于本机</span><span>Aether · 存储空间，无缝相连</span></div>

  <Modal v-if="modal" :title="editing ? '编辑存储池' : step === 1 ? '添加存储池' : '配置存储池'" eyebrow="STORAGE CONNECTION" wide @close="!busy && (modal = false)">
    <div class="wizard-steps"><span :class="{ current: step === 1 }"><b>1</b>选择存储类型</span><i /><span :class="{ current: step === 2 }"><b>2</b>配置存储信息</span></div>
    <div v-if="step === 1" class="modal-body">
      <template v-for="group in ['云端存储', '协议与本地']" :key="group"><h3 class="group-heading">{{ group }}</h3><div class="driver-grid"><button v-for="d in drivers.filter(d => d.kind === group)" :key="d.id" class="driver-option" @click="next(d.id)"><ProviderIcon :type="d.id" /><strong>{{ d.name }}</strong><small>{{ d.auth }}</small><div><span v-for="tag in d.tags" :key="tag">{{ tag }}</span></div></button></div></template>
    </div>
    <form v-else @submit.prevent="save">
      <div class="modal-body">
        <div class="selected-driver"><ProviderIcon :type="selected" /><div><h3>{{ picked.name }}</h3><span>{{ picked.subtitle }}</span></div><button v-if="!editing" type="button" class="text-btn" @click="step = 1">更换类型</button></div>
        <div v-if="['mobile', 'tianyi'].includes(selected)" class="inline-note"><Icon name="Info" />此连接器通过 OpenList 网关接入，填写已挂载该网盘的 OpenList 地址和路径。</div>
        <div class="form-grid">
          <label class="full">存储池名称 <span class="required">*</span><input v-model="form.name" required maxlength="60" placeholder="例如：家庭影音库" /></label>
          <template v-if="selected === '115'"><label class="full">访问令牌 Access Token <span class="required">*</span><input v-model="form.config.accessToken" type="password" required autocomplete="off" /></label><label class="full">刷新令牌 Refresh Token<input v-model="form.config.refreshToken" type="password" autocomplete="off" /><small>已保存备用；当前版本需手动更新过期的访问令牌。</small></label></template>
          <label v-if="selected === 'quark'" class="full">Cookie <span class="required">*</span><textarea v-model="form.config.cookie" required rows="3" autocomplete="off" placeholder="粘贴夸克网页版的完整 Cookie" /></label>
          <template v-if="['openlist', 'mobile', 'tianyi', 'webdav'].includes(selected)">
            <label class="full">{{ selected === 'webdav' ? 'WebDAV' : 'OpenList' }} 服务地址 <span class="required">*</span><input v-model="form.config.address" required type="url" placeholder="https://storage.example.com" /></label>
            <label v-if="selected !== 'webdav'" class="full">API Token<input v-model="form.config.token" type="password" autocomplete="off" /></label>
            <label v-if="selected === 'webdav'">用户名<input v-model="form.config.username" autocomplete="off" /></label><label :class="{ full: selected !== 'webdav' }">{{ selected === 'webdav' ? '密码' : '目录访问密码（可选）' }}<input v-model="form.config.password" type="password" autocomplete="off" /></label>
          </template>
          <label>{{ selected === 'local' ? '本地根目录' : ['115', 'quark'].includes(selected) ? '根目录 ID' : '根目录路径' }}<input v-model="form.config.root" :required="selected === 'local'" /></label>
          <label>缓存时间（分钟）<input v-model.number="form.cacheTTL" type="number" min="0" max="525600" placeholder="0" /><small>0 表示跟随全局设置</small></label>
          <label class="toggle-line full"><span>启用此存储池</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" /></label>
        </div>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="modal = false">取消</button><button class="btn primary" :disabled="busy"><Icon name="Check" />{{ busy ? '保存中…' : '保存存储池' }}</button></footer>
    </form>
  </Modal>
  <Modal v-if="confirmDelete" title="删除存储池" @close="confirmDelete = null"><div class="modal-body"><p>确认删除「{{ confirmDelete.name }}」？此操作不会删除存储中的文件。</p></div><footer class="modal-footer"><button class="btn" @click="confirmDelete = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除存储池</button></footer></Modal>
</template>
