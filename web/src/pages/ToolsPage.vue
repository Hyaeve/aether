<script setup>
import { ref } from 'vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import SecretInput from '../components/SecretInput.vue'
import QuarkTakeover from '../components/QuarkTakeover.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import { notify } from '../lib'
const busy = ref(false)
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
  { name: '夸克 STRM 接管', icon: 'ArrowLeftRight', detail: '接管夸克媒体链接与播放请求' },
  { name: '整理规则', icon: 'FolderTree', file: 'organize-rules.json', detail: '配置媒体命名、目录结构和整理规则' },
  { name: '二级分类', icon: 'Tags', file: 'categories.json', detail: '按媒体类型、地区和分类归档文件' },
  { name: '洗版策略', icon: 'RefreshCw', file: 'upgrade-policies.json', detail: '根据画质与版本偏好替换已有媒体' },
  { name: 'AI 辅助识别', icon: 'BrainCircuit', file: 'ai.json', detail: '辅助识别复杂文件名与媒体信息' },
  { name: '识别规则', icon: 'ListFilter', file: 'recognition-rules.json', detail: '最小视频、整理黑名单、自定义识别词、自定义匹配' },
  { name: 'TMDB 配置', icon: 'Film', detail: '配置影视元数据接口与语言偏好' },
  { name: '代理配置', icon: 'Network', detail: '管理外部服务请求使用的网络代理' },
  { name: '配置备份', icon: 'FileArchive', detail: '加密导入导出系统、存储池、以链与规则配置' }
]
</script>
<template>
  <section class="page-head"><h1>辅助工具</h1></section>
  <div class="plugin-grid">
    <button v-for="tool in tools" :key="tool.name" class="plugin-card" @click="selected = tool">
      <span v-if="tool.name === '夸克 STRM 接管'" class="plugin-symbol quark-takeover-symbol"><ProviderIcon type="quark" /><Icon name="ArrowLeftRight" :size="14" /></span><span v-else class="plugin-symbol"><Icon :name="tool.icon" :size="26" /></span><strong>{{ tool.name }}</strong><small class="plugin-description" :title="tool.detail">{{ tool.detail }}</small><span v-if="!['配置备份', '夸克 STRM 接管'].includes(tool.name)" class="status pending">待实现</span>
    </button>
  </div>
  <QuarkTakeover v-if="selected?.name === '夸克 STRM 接管'" @close="selected = null" />
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
.quark-takeover-symbol { position: relative; background: transparent; }
.quark-takeover-symbol > svg { position: absolute; bottom: -2px; right: -4px; background: var(--surface); color: var(--primary); border-radius: 4px; }
</style>
