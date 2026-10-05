<script setup>
import { ref } from 'vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import { notify } from '../lib'
const busy = ref(false)
async function backup() {
  busy.value = true
  try {
    const response = await fetch('/api/config/backup', { method: 'POST', credentials: 'same-origin' })
    if (!response.ok) throw new Error('配置备份失败')
    const url = URL.createObjectURL(await response.blob()), anchor = document.createElement('a')
    anchor.href = url; anchor.download = `aether-config-${new Date().toISOString().slice(0, 10)}.zip`; anchor.click()
    setTimeout(() => URL.revokeObjectURL(url), 30000); notify('配置备份已导出'); selected.value = null
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
const selected = ref(null)
const tools = [
  { name: '配置备份', icon: 'ArchiveRestore', detail: '备份系统配置、存储凭据与整理识别规则' },
  { name: '115 STRM 增强', icon: 'Sparkles', detail: '增强 115 媒体链接生成与播放解析' },
  { name: '115 分享 STRM', icon: 'Share2', detail: '从 115 分享目录生成媒体播放链接' },
  { name: '夸克 STRM 接管', icon: 'ArrowLeftRight', detail: '接管夸克媒体链接与播放请求' },
  { name: '整理规则', icon: 'FolderTree', file: 'organize-rules.json', detail: '配置媒体命名、目录结构和整理规则' },
  { name: '二级分类', icon: 'Tags', file: 'categories.json', detail: '按媒体类型、地区和分类归档文件' },
  { name: '洗版策略', icon: 'RefreshCw', file: 'upgrade-policies.json', detail: '根据画质与版本偏好替换已有媒体' },
  { name: 'AI 辅助识别', icon: 'BrainCircuit', file: 'ai.json', detail: '辅助识别复杂文件名与媒体信息' },
  { name: '识别规则', icon: 'ListFilter', file: 'recognition-rules.json', detail: '最小视频、整理黑名单、自定义识别词、自定义匹配' },
  { name: 'TMDB 配置', icon: 'Film', detail: '配置影视元数据接口与语言偏好' },
  { name: '代理配置', icon: 'Network', detail: '管理外部服务请求使用的网络代理' }
]
</script>
<template>
  <section class="page-head"><h1>辅助工具</h1></section>
  <div class="plugin-grid">
    <button v-for="tool in tools" :key="tool.name" class="plugin-card" @click="selected = tool">
      <span class="plugin-symbol"><Icon :name="tool.icon" :size="26" /></span><strong>{{ tool.name }}</strong><small class="plugin-description" :title="tool.detail">{{ tool.detail }}</small><span v-if="tool.name !== '配置备份'" class="status pending">待实现</span>
    </button>
  </div>
  <Modal v-if="selected" :title="selected.name" @close="selected = null">
    <template v-if="selected.name === '配置备份'"><div class="modal-body"><p>备份包含账号、网盘凭据和解密密钥，请妥善保管，勿公开分享。</p></div><footer class="modal-footer"><button class="btn primary" :disabled="busy" @click="backup"><Icon name="Download" />导出配置备份</button></footer></template>
    <div v-else class="modal-body"><p>该插件尚未实现，当前不能启用或执行。</p><p v-if="selected.detail">{{ selected.detail }}</p><code v-if="selected.file">/config/organize/{{ selected.file }}</code></div>
  </Modal>
</template>
