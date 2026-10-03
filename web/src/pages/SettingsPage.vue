<script setup>
import { reactive, ref } from 'vue'
import { api, state, reload, notify, bytes } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import NumberInput from '../components/NumberInput.vue'
import AboutPage from './AboutPage.vue'
import WebDAVUsers from '../components/WebDAVUsers.vue'
const props = defineProps({ section: { default: 'cache' } })
const form = reactive({ ...state.settings })
const account = reactive({ username: state.username, password: '', sessionDays: state.settings.sessionDays || 7 })
const busy = ref(false), error = ref(''), clearConfirm = ref(false)
async function save() {
  busy.value = true; error.value = ''
  try { await api('/settings', 'PUT', form); await reload(); notify('设置已保存') }
  catch (e) { error.value = e.message } finally { busy.value = false }
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
  <section v-if="section === 'cache'" class="page-head"><h1>缓存设置</h1><button class="btn" @click="$router.push('/tasks/cache')"><Icon name="ArrowLeft" />返回缓存任务</button></section>
  <div class="settings-layout" :class="{ 'has-navigation': ['account', 'general', 'about'].includes(section) }">
  <nav v-if="['account', 'general', 'about'].includes(section)" class="settings-navigation" aria-label="系统设置栏目">
    <RouterLink to="/settings/account" :class="{ active: section === 'account' }"><Icon name="UserRound" />账号与安全</RouterLink>
    <RouterLink to="/settings/about" :class="{ active: section === 'about' }"><Icon name="Info" />关于以太</RouterLink>
  </nav>
  <AboutPage v-if="section === 'about'" />
  <form v-else-if="section === 'account'" class="settings-form" @submit.prevent="changeAccount">
    <section class="settings-section"><div class="form-grid"><label>账号<input v-model="account.username" required maxlength="150" autocomplete="username" /></label><label>密码<input v-model="account.password" type="password" required maxlength="72" autocomplete="new-password" /></label></div></section>
    <section class="settings-section"><div class="settings-row"><strong>会话有效期</strong><NumberInput v-model="account.sessionDays" aria-label="会话有效期" unit="天" min="1" max="365" required /></div></section>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p><div class="settings-actions"><button class="btn primary" :disabled="busy"><Icon name="Save" />保存账号</button></div>
  </form>
  <form v-else class="settings-form" @submit.prevent="save">
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
      <section class="settings-section"><label>外部访问地址<input v-model="form.publicURL" type="url" required placeholder="http://192.168.1.10:15151" /></label></section>
      <section class="settings-section"><div class="settings-section-title"><Icon name="Network" /><div><h2>聚合 WebDAV</h2><p>与主服务共用 15151 端口，当前提供只读访问。</p></div></div>
        <label class="settings-row"><span><strong>启用 WebDAV 服务</strong><small>支持目录浏览、文件读取及 Range 请求</small></span><input v-model="form.webdavEnabled" type="checkbox" role="switch" class="switch" /></label>
        <div class="settings-row"><span><strong>服务地址</strong><small>使用管理员账号与密码；公网访问请配置 HTTPS</small></span><code>{{ form.publicURL }}/dav/</code></div>
        <div class="settings-row"><span><strong>目录映射</strong><small>使用固定的存储池 ID 作为根目录，重命名不影响路径</small></span><span>{{ state.storages.filter(s => s.enabled).length }} 个存储池</span></div>
        <div v-for="s in state.storages.filter(s => s.enabled)" :key="s.id" class="settings-row"><strong>{{ s.name }}</strong><code>/{{ s.id }}/</code></div>
      </section>
    </template>
    <template v-else>
      <section class="settings-section"><div class="settings-section-title"><Icon name="Server" /><div><h2>服务地址</h2><p>STRM 文件中的播放链接使用此地址。</p></div></div><label>外部访问地址<input v-model="form.publicURL" type="url" required placeholder="http://192.168.1.10:15151" /><small>填写媒体服务器可访问的地址；修改后需重新全量生成 STRM。</small></label></section>
      <section class="settings-section"><div class="settings-section-title"><Icon name="Box" /><div><h2>运行环境</h2></div></div><div class="settings-row"><strong>服务版本</strong><code>0.1.0</code></div><div class="settings-row"><strong>容器端口</strong><code>15151</code></div><div class="settings-row"><strong>STRM 根目录</strong><code>{{ state.strmRoot }}</code></div></section>
    </template>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p><div class="settings-actions"><button v-if="section === 'cache'" type="button" class="btn danger-outline" @click="clearConfirm = true"><Icon name="Trash2" />清空缓存</button><button class="btn primary" :disabled="busy"><Icon name="Save" />{{ busy ? '保存中…' : '保存设置' }}</button></div>
  </form>
  </div>
  <WebDAVUsers v-if="section === 'webdav'" />
  <Modal v-if="clearConfirm" title="清空缓存" @close="clearConfirm = false"><div class="modal-body">确认清空所有目录缓存及磁盘快照？存储中的文件不会受影响。</div><footer class="modal-footer"><button class="btn" @click="clearConfirm = false">取消</button><button class="btn danger" :disabled="busy" @click="clear">清空缓存</button></footer></Modal>
</template>
