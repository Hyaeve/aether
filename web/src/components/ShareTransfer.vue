<script setup>
import { computed, ref, watch } from 'vue'
import { state } from '../lib'
import { transferShares } from '../share-jobs'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
import RoundedSelect from './RoundedSelect.vue'
const props = defineProps({ storage: Object, parent: String, trail: Array, initialLinks: String, provider: String })
const emit = defineEmits(['close'])
const pools = computed(() => state.storages.filter(s => s.enabled && (!props.provider || s.type === props.provider) && (['115', 'quark'].includes(s.type) || s.type === 'mobile' && s.config?.mode === 'native')))
const selected = ref(props.storage?.id || pools.value[0]?.id || '')
const storage = computed(() => props.storage || pools.value.find(s => s.id === selected.value))
const supported = computed(() => storage.value?.enabled && (['115', 'quark'].includes(storage.value.type) || storage.value.type === 'mobile' && storage.value.config?.mode === 'native'))
const key = computed(() => `aether-share-directory:${state.username}:${storage.value?.id}`)
const target = ref('/'), targetLabel = ref('根目录'), targetHistory = ref([])
let restoring = false
watch(key, () => {
  restoring = true
  let saved = {}
  try { saved = JSON.parse(localStorage.getItem(key.value) || '{}') || {} } catch {}
  target.value = typeof saved.path === 'string' ? saved.path : props.parent || '/'
  targetLabel.value = typeof saved.label === 'string' ? saved.label : props.trail?.at(-1)?.name || '根目录'
  targetHistory.value = (Array.isArray(saved.history) ? saved.history : typeof saved.path === 'string' ? [] : props.trail || []).map(c => ({ id: c.id, name: c.name }))
  restoring = false
}, { immediate: true })
const pickerTrail = computed(() => targetHistory.value.map((c, i) => ({ id: c.id, name: i ? targetHistory.value[i - 1].name : '根目录' })))
const links = ref(props.initialLinks || ''), picker = ref(false)
const lines = computed(() => [...new Set(links.value.split(/\r?\n/).map(s => s.trim()).filter(Boolean))])
const placeholder = computed(() => `${{ '115': '115 网盘', quark: '夸克网盘', mobile: '移动云盘' }[storage.value?.type] || storage.value?.name || ''}分享链接，一行一条`)
watch([target, targetLabel, targetHistory], () => { if (!restoring && storage.value) { try { localStorage.setItem(key.value, JSON.stringify({ path: target.value, label: targetLabel.value, history: targetHistory.value })) } catch {} } }, { deep: true, flush: 'sync' })
function chooseTarget(value) {
  target.value = value.source; targetLabel.value = value.sourceLabel
  targetHistory.value = (value.sourceTrail || []).map((c, i, trail) => ({ id: c.id, name: trail[i + 1]?.name || value.sourceLabel }))
  picker.value = false
}
function submit() {
  if (!supported.value || !lines.value.length || lines.value.length > 50) return
  const history = targetHistory.value.map(c => ({ ...c }))
  const root = storage.value.config?.root || (['115', 'quark'].includes(storage.value.type) ? '0' : '/')
  if (!['/', root].includes(target.value) && !history.length) history.push({ id: '/', name: targetLabel.value })
  transferShares(storage.value, target.value, lines.value, history)
  emit('close')
}
</script>
<template>
  <Modal title="分享转存" compact @close="emit('close')">
    <form @submit.prevent="submit">
      <div class="modal-body share-transfer">
        <div v-if="!props.storage" class="field"><label>存储池</label><RoundedSelect v-model="selected" label="转存存储池" :options="pools.map(s => ({value:s.id,label:s.name}))" /></div>
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
