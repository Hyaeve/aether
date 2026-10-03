<script setup>
import { ref } from 'vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
const selected = ref(null)
const tools = [
  { name: '115 STRM 增强', icon: 'FileVideo' },
  { name: '115 分享 STRM', icon: 'Link' },
  { name: '夸克 STRM 接管', icon: 'Cloud' },
  { name: '整理规则', icon: 'FolderTree', file: 'organize-rules.json' },
  { name: '二级分类', icon: 'Layers3', file: 'categories.json' },
  { name: '洗版策略', icon: 'RefreshCw', file: 'upgrade-policies.json' },
  { name: 'AI 辅助识别', icon: 'ScanSearch', file: 'ai.json' },
  { name: '识别规则', icon: 'ListTodo', file: 'recognition-rules.json', detail: '最小视频、整理黑名单、自定义识别词、自定义匹配' },
  { name: 'TMDB 配置', icon: 'FileVideo' },
  { name: '代理配置', icon: 'Network' }
]
</script>
<template>
  <section class="page-head"><h1>辅助工具</h1></section>
  <div class="plugin-grid">
    <button v-for="tool in tools" :key="tool.name" class="plugin-card" @click="selected = tool">
      <Icon :name="tool.icon" :size="28" /><strong>{{ tool.name }}</strong><span class="status pending">待实现</span>
    </button>
  </div>
  <Modal v-if="selected" :title="selected.name" @close="selected = null">
    <div class="modal-body"><p>该插件尚未实现，当前不能启用或执行。</p><p v-if="selected.detail">{{ selected.detail }}</p><code v-if="selected.file">/config/organize/{{ selected.file }}</code></div>
  </Modal>
</template>
