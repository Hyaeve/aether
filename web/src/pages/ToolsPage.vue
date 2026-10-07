<script setup>
import { onMounted, ref } from 'vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import SecretInput from '../components/SecretInput.vue'
import QuarkTakeover from '../components/QuarkTakeover.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import PluginSettings from '../components/PluginSettings.vue'
import { api, notify } from '../lib'
const busy = ref(false)
const takeover = ref({ enabled: false, bindings: [] })
const pluginStates = ref({})
async function togglePlugin(tool) {
  busy.value = true
  try {
    const current = await api(`/plugins/${tool.kind}`)
    await api(`/plugins/${tool.kind}`, 'PUT', { ...current, enabled: !current.enabled })
    pluginStates.value[tool.kind] = !current.enabled
    notify(`${current.enabled ? '已停用' : '已启用'} ${tool.name}`)
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
onMounted(() => Promise.all(['emby', 'tmdb', 'ai', 'proxy'].map(async kind => {
  try { pluginStates.value[kind] = (await api(`/plugins/${kind}`)).enabled } catch (e) { notify(e.message, true) }
})))
async function loadTakeover() {
  try { takeover.value = await api('/quark-takeover') } catch (e) { notify(e.message, true) }
}
async function toggleTakeover() {
  if (!takeover.value.bindings.length) { selected.value = tools[2]; return }
  busy.value = true
  try {
    await api('/quark-takeover', 'PUT', { enabled: !takeover.value.enabled })
    await loadTakeover()
    notify(takeover.value.enabled ? '已启用夸克 STRM 接管' : '已停用夸克 STRM 接管')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
onMounted(loadTakeover)
const flow = ref(''), password = ref(''), backupFile = ref(null), importStep = ref(1)
function start(mode) { flow.value = mode; password.value = ''; backupFile.value = null; importStep.value = 1 }
async function restore() {
  busy.value = true
  try {
    const data = new FormData(); data.append('file', backupFile.value); data.append('password', password.value)
    const res = await fetch('/api/config/import', { method: 'POST', credentials: 'same-origin', body: data })
    const result = await res.json()
    if (!res.ok) throw new Error(result.error || '导入失败')
    notify(result.message); flow.value = ''; selected.value = null
  } catch (e) { notify(e.message, true) } finally { busy.value = false; password.value = '' }
}
async function backup() {
  busy.value = true
  try {
    const response = await fetch('/api/config/backup', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: password.value }) })
    if (!response.ok) throw new Error((await response.json()).error || '配置备份失败')
    const url = URL.createObjectURL(await response.blob()), anchor = document.createElement('a')
    anchor.href = url; anchor.download = `aether-config-${new Date().toISOString().slice(0, 10)}.aether`; anchor.click()
    setTimeout(() => URL.revokeObjectURL(url), 30000); notify('配置备份已导出'); selected.value = null; flow.value = ''
  } catch (e) { notify(e.message, true) } finally { busy.value = false; password.value = '' }
}
const selected = ref(null)
const tools = [
  { name: '115 STRM 增强', icon: 'Sparkles', detail: '增强 115 媒体链接生成与播放解析' },
  { name: '115 分享 STRM', icon: 'Share2', detail: '从 115 分享目录生成媒体播放链接' },
  { name: '夸克 STRM 接管', icon: 'ArrowLeftRight', subtitle: '夸克网盘 · TV 版 302 直链', detail: '让夸克 STRM 改走 TV 版 302 直链；转码画质和字幕受影响且部分第三方播放器不兼容。' },
  { name: '整理规则', icon: 'FolderTree', file: 'organize-rules.json', detail: '配置媒体命名、目录结构和整理规则' },
  { name: '二级分类', icon: 'Tags', file: 'categories.json', detail: '按媒体类型、地区和分类归档文件' },
  { name: '洗版策略', icon: 'RefreshCw', file: 'upgrade-policies.json', detail: '根据画质与版本偏好替换已有媒体' },
  { name: 'AI 辅助识别', kind: 'ai', icon: 'BrainCircuit', file: 'ai.json', detail: '通过 OpenAI 兼容模型识别媒体名称与季集信息' },
  { name: '识别规则', icon: 'ListFilter', file: 'recognition-rules.json', detail: '最小视频、整理黑名单、自定义识别词、自定义匹配' },
  { name: 'TMDB 配置', kind: 'tmdb', icon: 'Film', detail: '配置影视元数据接口、图片域名与语言偏好' },
  { name: '代理配置', kind: 'proxy', icon: 'Network', detail: '管理 TMDB 与 AI 请求使用的网络代理' },
  { name: 'Emby 入库通知', kind: 'emby', icon: 'Bell', detail: '接收媒体入库事件并显示在通知列表中' },
  { name: '配置备份', icon: 'FileArchive', detail: '加密导入导出系统、存储池、以链与规则配置' }
]
</script>
<template>
  <section class="page-head"><h1>辅助工具</h1></section>
  <div class="plugin-grid">
    <article v-for="tool in tools" :key="tool.name" class="plugin-card" role="button" tabindex="0" :aria-label="tool.name" @click="selected = tool" @keydown.enter.self="selected = tool" @keydown.space.prevent.self="selected = tool">
      <button v-if="tool.name === '夸克 STRM 接管'" class="plugin-symbol quark-takeover-symbol" :aria-label="`${takeover.enabled ? '停用' : '启用'}夸克 STRM 接管`" :aria-pressed="takeover.enabled && takeover.bindings.length > 0" :disabled="busy" @click.stop="toggleTakeover"><ProviderIcon type="quark" /><Icon name="ArrowLeftRight" :size="14" /></button><button v-else-if="tool.kind" class="plugin-symbol plugin-toggle" :aria-label="`${pluginStates[tool.kind] ? '停用' : '启用'}${tool.name}`" :aria-pressed="!!pluginStates[tool.kind]" :disabled="busy" @click.stop="togglePlugin(tool)"><Icon :name="tool.icon" :size="26" /></button><span v-else class="plugin-symbol"><Icon :name="tool.icon" :size="26" /></span><strong>{{ tool.name }}</strong><small v-if="tool.subtitle" class="plugin-subtitle">{{ tool.subtitle }}</small><small class="plugin-description" :data-tooltip="tool.detail">{{ tool.detail }}</small><span v-if="!tool.kind && !['配置备份', '夸克 STRM 接管'].includes(tool.name)" class="status pending">待实现</span>
    </article>
  </div>
  <QuarkTakeover v-if="selected?.name === '夸克 STRM 接管'" @close="selected = null" @changed="takeover = $event" />
  <PluginSettings v-else-if="selected?.kind" :key="selected.kind" :kind="selected.kind" :title="selected.name" @close="selected = null" @changed="pluginStates[$event.kind] = $event.enabled" />
  <Modal v-else-if="selected" :title="selected.name" @close="selected = null">
    <template v-if="selected.name === '配置备份'"><div class="modal-body"><p>备份包含账号、存储池、以链和规则配置，使用你设置的密码加密。导入后需重启容器，并使用备份中的账号登录。</p></div><footer class="modal-footer backup-actions"><button class="btn" @click="start('import')"><Icon name="ArchiveRestore" />导入配置备份</button><button class="btn primary" @click="start('export')"><Icon name="Download" />导出配置备份</button></footer></template>
    <div v-else class="modal-body"><p>该插件尚未实现，当前不能启用或执行。</p><p v-if="selected.detail">{{ selected.detail }}</p><code v-if="selected.file">/config/organize/{{ selected.file }}</code></div>
  </Modal>
  <Modal v-if="flow" :title="flow === 'export' ? '加密导出' : '导入配置'" compact @close="!busy && (flow = '')">
    <form @submit.prevent="flow === 'export' ? backup() : importStep === 1 ? (importStep = 2) : restore()">
      <div class="modal-body"><label v-if="flow === 'import' && importStep === 1">选择备份文件<input type="file" accept=".aether" required @change="backupFile = $event.target.files[0]" /></label><label v-else>备份密码<SecretInput v-model="password" required autocomplete="off" /></label><p v-if="flow === 'import'" class="muted">现有配置将由备份中的配置覆盖，重启后生效。</p></div>
      <footer class="modal-footer"><button v-if="flow === 'import' && importStep === 2" type="button" class="btn" :disabled="busy" @click="importStep = 1">上一步</button><button class="btn primary" :disabled="busy">{{ flow === 'export' ? '确认导出' : importStep === 1 ? '下一步' : '确认导入' }}</button></footer>
    </form>
  </Modal>
</template>
<style scoped>
.quark-takeover-symbol { position: relative; background: transparent; padding: 0; border: 0; }
.plugin-toggle { border: 0; padding: 0; }
.plugin-toggle[aria-pressed=false] { opacity: .5; }
.quark-takeover-symbol :deep(.provider-icon), .quark-takeover-symbol :deep(.provider-logo) { width: 100%; height: 100%; padding: 0; border-radius: 8px; }
.quark-takeover-symbol[aria-pressed=false] { opacity: .55; }
.plugin-subtitle { color: var(--muted); font-size: 12px; margin-top: 4px; }
.plugin-card:has(.quark-takeover-symbol) { display: grid; grid-template-columns: 44px minmax(0,1fr); gap: 5px 14px; cursor: pointer; }
.quark-takeover-symbol { grid-row: 1 / 3; }
.plugin-card:has(.quark-takeover-symbol) .plugin-subtitle { grid-column: 2; margin: 0; }
.plugin-card:has(.quark-takeover-symbol) .plugin-description { grid-column: 1 / -1; margin-top: 10px; }
.plugin-card { height: 156px; align-content: center; overflow: hidden; }
.plugin-card:has(.quark-takeover-symbol) .plugin-description { white-space: nowrap; display: block; }
.quark-takeover-symbol > svg { position: absolute; bottom: -2px; right: -4px; background: var(--surface); color: var(--primary); border-radius: 4px; }
</style>
