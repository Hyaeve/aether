<script setup>
import { computed, ref, watch } from 'vue'
import { state } from '../lib'
import { transferShares } from '../share-jobs'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
const props = defineProps({ storage: Object, parent: String, trail: Array })
const emit = defineEmits(['close', 'changed'])
const storage = props.storage
const supported = computed(() => storage?.enabled && (['115', 'quark'].includes(storage.type) || storage.type === 'mobile' && storage.config?.mode === 'native'))
const key = `aether-share-directory:${state.username}:${storage?.id}`
let saved = {}
try { saved = JSON.parse(localStorage.getItem(key) || '{}') || {} } catch {}
const target = ref(typeof saved.path === 'string' ? saved.path : props.parent || '/')
const targetLabel = ref(typeof saved.label === 'string' ? saved.label : props.trail?.map(c => c.name).join(' / ') || '根目录')
const links = ref(''), picker = ref(false)
const lines = computed(() => [...new Set(links.value.split(/\r?\n/).map(s => s.trim()).filter(Boolean))])
const placeholder = computed(() => `${{ '115': '115 网盘', quark: '夸克网盘', mobile: '移动云盘' }[storage?.type] || storage?.name || ''}分享链接，一行一条`)
watch([target, targetLabel], () => { try { localStorage.setItem(key, JSON.stringify({ path: target.value, label: targetLabel.value })) } catch {} })
function submit() {
  if (!supported.value || !lines.value.length || lines.value.length > 50) return
  transferShares(storage, target.value, lines.value, () => emit('changed'))
  emit('close')
}
</script>
<template>
  <Modal title="分享转存" compact @close="emit('close')">
    <form @submit.prevent="submit">
      <div class="modal-body share-transfer">
        <label>分享链接<textarea v-model="links" rows="5" required :placeholder="placeholder" maxlength="200000" /></label>
        <div class="field"><label>转存目录</label><button type="button" class="source-trigger" :disabled="!supported" aria-label="选择转存目录" @click="picker = true"><span>{{ targetLabel }}</span><Icon name="FolderOpen" /></button></div>
        <p v-if="!supported" class="error-message">当前存储不支持分享转存。</p>
        <p v-if="lines.length > 50" class="error-message">一次最多提交 50 条分享链接。</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn cancel" @click="emit('close')">取消</button><button class="btn primary" :disabled="!supported || !lines.length || lines.length > 50"><Icon name="FolderInput" />开始转存</button></footer>
    </form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="[storage]" :storage="storage.id" :initial="target" :initial-label="targetLabel" @close="picker = false" @select="target = $event.source; targetLabel = $event.sourceLabel; picker = false" />
</template>
<style scoped>
.share-transfer { display: grid; gap: 16px; }
.share-transfer textarea { resize: vertical; min-height: 130px; }
</style>
