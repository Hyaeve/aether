<script setup>
import { computed, ref, watch } from 'vue'
import { state } from '../lib'
import { transferShares } from '../share-jobs'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
const props = defineProps({ storage: Object, parent: String, trail: Array })
const emit = defineEmits(['close'])
const storage = props.storage
const supported = computed(() => storage?.enabled && (['115', 'quark'].includes(storage.type) || storage.type === 'mobile' && storage.config?.mode === 'native'))
const key = `aether-share-directory:${state.username}:${storage?.id}`
let saved = {}
try { saved = JSON.parse(localStorage.getItem(key) || '{}') || {} } catch {}
const target = ref(typeof saved.path === 'string' ? saved.path : props.parent || '/')
const targetLabel = ref(typeof saved.label === 'string' ? saved.label : props.trail?.at(-1)?.name || '根目录')
const targetHistory = ref((Array.isArray(saved.history) ? saved.history : typeof saved.path === 'string' ? [] : props.trail || []).map(c => ({ id: c.id, name: c.name })))
const pickerTrail = computed(() => targetHistory.value.map((c, i) => ({ id: c.id, name: i ? targetHistory.value[i - 1].name : '根目录' })))
const links = ref(''), picker = ref(false)
const lines = computed(() => [...new Set(links.value.split(/\r?\n/).map(s => s.trim()).filter(Boolean))])
const placeholder = computed(() => `${{ '115': '115 网盘', quark: '夸克网盘', mobile: '移动云盘' }[storage?.type] || storage?.name || ''}分享链接，一行一条`)
watch([target, targetLabel, targetHistory], () => { try { localStorage.setItem(key, JSON.stringify({ path: target.value, label: targetLabel.value, history: targetHistory.value })) } catch {} }, { deep: true })
function chooseTarget(value) {
  target.value = value.source; targetLabel.value = value.sourceLabel
  targetHistory.value = (value.sourceTrail || []).map((c, i, trail) => ({ id: c.id, name: trail[i + 1]?.name || value.sourceLabel }))
  picker.value = false
}
function submit() {
  if (!supported.value || !lines.value.length || lines.value.length > 50) return
  const history = targetHistory.value.map(c => ({ ...c }))
  const root = storage.config?.root || (['115', 'quark'].includes(storage.type) ? '0' : '/')
  if (!['/', root].includes(target.value) && !history.length) history.push({ id: '/', name: targetLabel.value })
  transferShares(storage, target.value, lines.value, history)
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
  <TaskSourcePicker v-if="picker" :storages="[storage]" :storage="storage.id" :initial="target" :initial-label="targetLabel" :initial-trail="pickerTrail" @close="picker = false" @select="chooseTarget" />
</template>
<style scoped>
.share-transfer { display: grid; gap: 16px; }
.share-transfer textarea { resize: vertical; min-height: 130px; }
</style>
