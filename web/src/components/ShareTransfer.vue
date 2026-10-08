<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, state, notify } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
const props = defineProps({ storage: Object, parent: String, trail: Array })
const emit = defineEmits(['close', 'changed'])
const supported = s => s.enabled && (['115', 'quark'].includes(s.type) || s.type === 'mobile' && s.config?.mode === 'native')
const storages = computed(() => state.storages.filter(supported))
const storageId = ref(props.storage && supported(props.storage) ? props.storage.id : storages.value[0]?.id || '')
const storage = computed(() => storages.value.find(s => s.id === storageId.value))
const target = ref(storageId.value === props.storage?.id ? props.parent || '/' : '/')
const targetLabel = ref(storageId.value === props.storage?.id ? props.trail?.map(c => c.name).join(' / ') || '根目录' : '根目录')
const link = ref(''), password = ref(''), preview = ref(''), items = ref([]), selected = ref([])
const busy = ref(false), picker = ref(false), error = ref(''), result = ref(null)
let timer, alive = true, generation = 0
function invalidate() { generation++; clearTimeout(timer); preview.value = ''; items.value = []; selected.value = []; result.value = null; error.value = '' }
onUnmounted(() => { alive = false; clearTimeout(timer) })
watch([link, password], invalidate)
watch(storageId, () => { invalidate(); target.value = '/'; targetLabel.value = '根目录' })
async function parse() {
  busy.value = true; error.value = ''; result.value = null; preview.value = ''; items.value = []; selected.value = []
  try {
    const data = await api('/files/share/preview', 'POST', { storageId: storageId.value, url: link.value, password: password.value })
    preview.value = data.preview; items.value = data.items
    selected.value = data.items.slice(0, 100).map(item => item.id)
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function save() {
  busy.value = true; error.value = ''
  try {
    result.value = await api('/files/share/save', 'POST', { storageId: storageId.value, preview: preview.value, parent: target.value, ids: selected.value })
    notify(result.value.message); emit('changed')
    if (storage.value.type === 'quark' && result.value.taskId) timer = setTimeout(() => poll(generation, 0), 2000)
  } catch (e) { error.value = e.message; preview.value = '' } finally { busy.value = false }
}
async function poll(run, attempt) {
  try {
    const data = await api('/files/share/status', 'POST', {storageId:storageId.value, preview:preview.value})
    if (!alive || run !== generation) return
    result.value = data
    if (data.status === 'completed') { emit('changed'); notify(data.message) }
    else if (data.status === 'failed') notify(data.message, true)
    else if (attempt < 59) timer = setTimeout(() => poll(run, attempt + 1), 2000)
    else error.value = '网盘仍在处理，可稍后刷新目标目录；请勿重复提交。'
  } catch (e) { if (alive && run === generation) error.value = e.message }
}
</script>
<template>
  <Modal title="分享转存" compact wide @close="!busy && emit('close')">
    <form @submit.prevent="preview ? save() : parse()">
      <div class="modal-body share-transfer">
        <div class="field"><label>目标存储</label><RoundedSelect v-model="storageId" label="转存目标存储" :disabled="busy" :options="storages.map(s => ({value:s.id,label:s.name}))" /></div>
        <label>分享链接<textarea v-model="link" rows="3" :disabled="busy" required placeholder="115、移动云盘或夸克分享链接" /></label>
        <div class="form-grid">
          <label>提取码<input v-model="password" :disabled="busy" maxlength="32" autocomplete="off" /></label>
          <div class="field"><label>目标目录</label><button type="button" class="source-trigger" :disabled="busy || !storage" aria-label="选择转存目录" @click="picker = true"><span>{{ targetLabel }}</span><Icon name="FolderOpen" /></button></div>
        </div>
        <template v-if="items.length && !result">
          <div class="share-selection"><label><input type="checkbox" :disabled="busy || items.length > 100" :checked="selected.length === items.length" @change="selected = $event.target.checked ? items.map(i => i.id) : []" />全选</label><small>已选 {{ selected.length }} / {{ items.length }} 项</small></div>
          <div class="share-items"><label v-for="item in items" :key="item.id"><input v-model="selected" type="checkbox" :value="item.id" :disabled="busy || selected.length >= 100 && !selected.includes(item.id)" /><Icon :name="item.isDir ? 'Folder' : 'FileVideo'" /><span>{{ item.name }}</span></label></div>
        </template>
        <p v-else-if="preview" class="small-empty">分享目录为空</p>
        <div v-if="result" class="share-result" role="status"><p>{{ result.message }}</p><small v-if="result.taskId">任务编号：{{ result.taskId }}</small><button type="button" class="btn" @click="emit('changed'); notify('已请求刷新目标目录')"><Icon name="RefreshCw" />刷新目录</button></div>
        <p v-if="!storages.length" class="error-message">没有可用的 115、夸克或原生移动个人云存储。</p>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="emit('close')">关闭</button><button v-if="!result" class="btn primary" :disabled="busy || !storage || !link.trim() || !!preview && !selected.length"><Icon :name="busy ? 'LoaderCircle' : preview ? 'FolderInput' : 'ScanSearch'" :class="{spin:busy}" />{{ busy ? '处理中…' : preview ? '确认转存' : '解析分享' }}</button></footer>
    </form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="[storage]" :storage="storageId" :initial="target" :initial-label="targetLabel" @close="picker = false" @select="target = $event.source; targetLabel = $event.sourceLabel; picker = false" />
</template>
<style scoped>
.share-transfer { display: grid; gap: 16px; }
.share-selection { display: flex; justify-content: space-between; align-items: center; }
.share-selection label, .share-items label { display: flex; flex-direction: row; align-items: center; gap: 10px; margin: 0; }
.share-selection input, .share-items input { width: 16px; height: 16px; flex-shrink: 0; }
.share-selection small, .share-result small { color: var(--muted); }
.share-items { max-height: 260px; overflow: auto; border-block: 1px solid var(--border); }
.share-items label { padding: 10px 0; border-bottom: 1px solid var(--border); }
.share-items svg { width: 18px; flex-shrink: 0; color: var(--muted); }
.share-items span, .share-result { min-width: 0; overflow-wrap: anywhere; }
.share-result { display: grid; gap: 10px; }
.share-result p { margin: 0; }
.share-result button { justify-self: start; }
</style>
