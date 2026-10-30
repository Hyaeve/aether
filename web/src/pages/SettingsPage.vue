<script setup>
import { computed, nextTick, reactive, ref } from 'vue'
import { api, state, reload, notify, bytes } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import NumberInput from '../components/NumberInput.vue'
import AboutPage from './AboutPage.vue'
import WebDAVUsers from '../components/WebDAVUsers.vue'
import AccountFields from '../components/AccountFields.vue'
import TransferSettings from '../components/TransferSettings.vue'
import STRMSettings from '../components/STRMSettings.vue'
import ClipboardConsent from '../components/ClipboardConsent.vue'
import ProxySettings from '../components/ProxySettings.vue'
import { usePageSlide } from '../page-slide'
import { authorizeClipboard, clipboardCapability, clipboardEnabled, clipboardStatus } from '../clipboard-settings'
const props = defineProps({ section: { default: 'cache' } })
const { leaving, back } = usePageSlide('/tasks/cache')
const form = reactive({ ...state.settings })
const account = reactive({ username: state.username, password: '', sessionDays: state.settings.sessionDays || 15 })
const busy = ref(false), error = ref(''), clearConfirm = ref(false)
const clipboardEnvironment = clipboardCapability()
const clipboardConsent = ref(false)
const clipboardLabel = computed(() => !clipboardEnabled.value ? '已关闭' : clipboardEnvironment === 'insecure' ? '当前 HTTP 连接仅支持主动粘贴' : clipboardEnvironment === 'unavailable' ? '当前浏览器仅支持主动粘贴' : ({ granted:'已获读取授权', denied:'读取权限未获授权', unknown:'等待浏览器授权' }[clipboardStatus.value]))
function toggleClipboard(event) {
  if (event.target.checked) { event.target.checked = false; clipboardConsent.value = true }
  else clipboardEnabled.value = false
}
async function acceptClipboard() {
  clipboardConsent.value = false
  clipboardEnabled.value = true
  await nextTick()
  if (clipboardEnvironment === 'available') authorizeClipboard()
}
async function save() {
  busy.value = true; error.value = ''
  try { await api('/settings', 'PUT', form); await reload(); notify('设置已保存') }
  catch (e) { error.value = e.message; if (props.section === 'webdav') { form.webdavEnabled = state.settings.webdavEnabled; notify(e.message, true) } } finally { busy.value = false }
}
async function changeAccount() {
  busy.value = true; error.value = ''
  try { await api('/account', 'PUT', account); account.password = ''; await reload(); notify('账号已更新，其他会话已退出') }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
async function clear() {
  busy.value = true
  try { await api('/cache/clear', 'POST'); await reload(); clearConfirm.value = false; notify('缓存已清空') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
</script>
<template>
  <section v-if="section === 'cache'" class="task-heading jump-heading page-slide" :class="{leaving}"><div class="jump-title"><button class="icon-btn" aria-label="返回缓存任务" @click="back"><Icon name="ChevronRight" :size="24" /></button><h2 class="module-title">缓存设置</h2></div><div class="toolbar-right"><button class="btn danger-outline" @click="clearConfirm = true"><Icon name="Trash2" />清空缓存</button><button class="btn primary" form="cache-settings-form" :disabled="busy"><Icon name="Save" />保存设置</button></div></section>
  <div class="settings-layout" :class="{ 'has-navigation': ['account', 'general', 'about', 'logs', 'transfer','strm'].includes(section), 'page-slide':section==='cache', leaving:section==='cache'&&leaving }">
  <nav v-tab-scroll v-if="['account', 'general', 'about', 'logs', 'transfer','strm'].includes(section)" class="settings-navigation" aria-label="系统设置栏目">
    <RouterLink to="/settings/account" :class="{ active: section === 'account' }"><Icon name="UserRound" />账号与安全</RouterLink>
    <RouterLink to="/settings/transfer" :class="{ active: section === 'transfer' }"><Icon name="Cable" />传输设置</RouterLink>
    <RouterLink to="/settings/strm" :class="{ active: section === 'strm' }"><Icon name="FileSymlink" />STRM 配置</RouterLink>
    <RouterLink to="/settings/logs" :class="{ active: section === 'logs' }"><Icon name="FileClock" />日志管理</RouterLink>
    <RouterLink to="/settings/about" :class="{ active: section === 'about' }"><Icon name="Info" />关于以太</RouterLink>
  </nav>
  <AboutPage v-if="section === 'about'" />
  <TransferSettings v-else-if="section === 'transfer'" mode="transfer" />
  <STRMSettings v-else-if="section === 'strm'" />
  <form v-else-if="section === 'logs'" class="log-settings-panel" @submit.prevent="save">
    <div class="settings-row"><strong>日志保留天数</strong><NumberInput v-model="form.logDays" aria-label="日志保留天数" unit="天" min="1" max="3650" required /></div>
    <div class="settings-row"><strong>日志条数上限</strong><NumberInput v-model="form.logMaxEntries" aria-label="日志条数上限" unit="条" min="100" max="1000000" required /></div>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p><div class="settings-actions"><button class="btn primary" :disabled="busy">保存设置</button></div>
  </form>
  <div v-else-if="section === 'account'" class="account-settings-layout">
  <form class="settings-form account-settings" @submit.prevent="changeAccount">
    <section class="account-panel account-credentials">
      <div class="settings-section-title account-panel-title"><Icon name="UserRound" /><h2>账户配置</h2></div>
      <AccountFields v-model:username="account.username" v-model:password="account.password" />
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      <button class="btn primary account-save" :disabled="busy"><Icon name="Save" />保存修改</button>
    </section>
    <section class="account-panel account-security">
      <div class="settings-row"><strong>会话有效期</strong><NumberInput v-model="account.sessionDays" aria-label="会话有效期" unit="天" min="1" max="365" required /></div>
      <div class="clipboard-setting"><label for="clipboard-listening"><strong>剪贴板监听</strong><small role="status">{{ clipboardLabel }}</small></label><div class="clipboard-controls"><button v-if="clipboardEnabled && clipboardEnvironment === 'available'" type="button" class="icon-btn" aria-label="授权剪贴板" data-tooltip="授权剪贴板" @click="authorizeClipboard"><Icon name="KeyRound" /></button><span v-else class="clipboard-security-mark" aria-hidden="true"><Icon name="KeyRound" /></span><input id="clipboard-listening" aria-label="剪贴板监听" :checked="clipboardEnabled" type="checkbox" role="switch" class="switch" @change="toggleClipboard" /></div></div>
    </section>
  </form>
  <ProxySettings />
  </div>
  <form v-else id="cache-settings-form" class="settings-form" :class="{ 'dav-settings': section === 'webdav' }" @submit.prevent="save">
    <template v-if="section === 'cache'">
      <section class="settings-section"><div class="settings-section-title"><Icon name="Database" /><div><h2>元数据缓存</h2><p>任务级设置优先于存储池设置，其次使用全局设置。</p></div><span class="status success">{{ state.cache.entries || 0 }} 条 · {{ bytes(state.cache.bytes) }}</span></div>
        <label class="settings-row"><span><strong>启用元数据缓存</strong><small>文件浏览、STRM 与缓存任务共用目录缓存</small></span><input v-model="form.cacheEnabled" type="checkbox" role="switch" class="switch" /></label>
        <div class="settings-row"><span><strong>全局缓存时间</strong><small>未单独配置的存储和任务使用此有效期</small></span><NumberInput v-model="form.cacheTTL" aria-label="全局缓存时间" unit="分钟" min="1" required /></div>
        <div class="settings-row"><span><strong>缓存条目上限</strong><small>达到上限后，按 LRU 淘汰最久未使用的条目</small></span><NumberInput v-model="form.cacheMaxItems" aria-label="缓存条目上限" unit="条" min="1" max="1000000" required /></div>
        <div class="settings-row"><span><strong>缓存内存上限</strong><small>按序列化元数据大小估算，不含运行时对象开销</small></span><NumberInput v-model="form.cacheMemoryMB" aria-label="缓存内存上限" unit="MB" min="1" max="4096" required /></div>
      </section>
      <section class="settings-section"><div class="settings-section-title"><Icon name="HardDriveDownload" /><div><h2>持久化</h2><p>将有效缓存写入磁盘，重启后恢复。</p></div></div>
        <label class="settings-row"><span><strong>缓存持久化</strong><small>快照保存在服务数据目录</small></span><input v-model="form.cachePersist" type="checkbox" role="switch" class="switch" /></label>
        <div class="settings-row"><span><strong>持久化快照间隔</strong><small>正常关闭服务时也会保存一次快照</small></span><NumberInput v-model="form.snapshotInterval" aria-label="持久化快照间隔" unit="分钟" min="1" required :disabled="!form.cachePersist" /></div>
      </section>
      <section class="settings-section"><div class="settings-section-title"><Icon name="Network" /><div><h2>WebDAV 缓存</h2><p>使用同一套元数据缓存与容量限制。</p></div></div><label class="settings-row"><span><strong>启用 WebDAV 目录缓存</strong><small>关闭后，每次 PROPFIND 直接读取上游目录</small></span><input v-model="form.webdavCache" type="checkbox" role="switch" class="switch" /></label></section>
    </template>
    <template v-else-if="section === 'webdav'">
      <section class="settings-section"><label>外部访问地址<input v-model="form.publicURL" type="url" required placeholder="http://192.168.1.10:15151" :disabled="busy" @change="save" /></label></section>
      <section class="settings-section">
        <label class="settings-row"><span><strong>启用 WebDAV 服务</strong><small>只读访问 · 与管理后台共用端口</small></span><input v-model="form.webdavEnabled" type="checkbox" role="switch" class="switch" :disabled="busy" @change="save" /></label>
        <div class="settings-row"><span><strong>服务地址</strong><small>使用管理员账号与密码；公网访问请配置 HTTPS</small></span><code>{{ form.publicURL }}/dav/</code></div>
      </section>
    </template>
    <template v-else>
      <section class="settings-section"><div class="settings-section-title"><Icon name="Server" /><div><h2>服务地址</h2><p>STRM 文件中的播放链接使用此地址。</p></div></div><label>外部访问地址<input v-model="form.publicURL" type="url" required placeholder="http://192.168.1.10:15151" /><small>填写媒体服务器可访问的地址；修改后需重新全量生成 STRM。</small></label></section>
      <section class="settings-section"><div class="settings-section-title"><Icon name="Box" /><div><h2>运行环境</h2></div></div><div class="settings-row"><strong>服务版本</strong><code>0.1.0</code></div><div class="settings-row"><strong>容器端口</strong><code>15151</code></div><div class="settings-row"><strong>STRM 根目录</strong><code>{{ state.strmRoot }}</code></div></section>
    </template>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p><div v-if="section !== 'webdav' && section !== 'cache'" class="settings-actions"><button class="btn primary" :disabled="busy"><Icon name="Save" />{{ busy ? '保存中…' : '保存设置' }}</button></div>
  </form>
  </div>
  <WebDAVUsers v-if="section === 'webdav'" />
  <ClipboardConsent v-if="clipboardConsent" @close="clipboardConsent=false" @accept="acceptClipboard" />
  <Modal v-if="clearConfirm" title="清空缓存" @close="clearConfirm = false"><div class="modal-body">确认清空所有目录缓存及磁盘快照？存储中的文件不会受影响。</div><footer class="modal-footer"><button class="btn" @click="clearConfirm = false">取消</button><button class="btn danger" :disabled="busy" @click="clear">清空缓存</button></footer></Modal>
</template>

<style scoped>
#cache-settings-form :deep(.number-control input) { font-size: 16px; }
#cache-settings-form :deep(.number-unit) { font-size: 16px; }
.account-settings.settings-form { display:grid; gap:16px; box-sizing:border-box; background:transparent; padding:0; border:0; }
.account-settings-layout{display:grid;grid-template-columns:repeat(2,minmax(0,380px));gap:16px;align-items:start}
.account-panel-title{gap:10px;margin-bottom:18px}.account-panel-title h2{font-size:16px;margin:0}
.account-panel { min-width:0; padding:20px; border:1px solid var(--border); border-radius:8px; background:var(--surface); }
.account-save { width:100%; margin-top:16px; }
.account-security .settings-row { margin:0; padding:0; border:0; }
.clipboard-setting { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-top:18px; padding-top:18px; border-top:1px solid var(--border); }
.clipboard-setting label { min-width:0; gap:6px; }.clipboard-setting strong { font-size:14px; font-weight:500; }.clipboard-setting small { font-size:12px; overflow-wrap:anywhere; }
.clipboard-controls { display:flex; align-items:center; gap:10px; flex:none; }
.clipboard-security-mark { display:grid; place-items:center; width:33px; height:33px; color:var(--muted); pointer-events:none; }
@media(max-width:900px){.account-settings-layout{grid-template-columns:minmax(0,380px)}}
</style>
