<script setup>
import { computed, ref } from 'vue'
import { api, notify } from '../lib'
import Icon from './Icon.vue'
import Modal from './Modal.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
import ProviderIcon from './ProviderIcon.vue'
const props = defineProps({ storage: Object, parent: String, trail: Array })
const emit = defineEmits(['close'])
const mode = ref('url'), urls = ref(''), torrentFiles = ref([]), target = ref(props.parent || '/'), busy = ref(false), error = ref('')
const picker = ref(false), fileInput = ref(null), targetLabel = ref(props.trail?.map(c => c.name).join(' / ') || '根目录'), results = ref([])
const supported = computed(() => !!props.storage?.id)
const native115 = computed(() => props.storage?.type === '115')
const supportsCAS = computed(() => ['mobile', 'tianyi'].includes(props.storage?.type))
function chooseFiles(event) { torrentFiles.value = [...event.target.files] }
async function submit() {
  const list = urls.value.split(/\r?\n/).map(v => v.trim()).filter(Boolean)
  if (!supported.value) { error.value = '当前存储池暂不支持离线下载'; return }
  if (mode.value === 'url' && !list.length) { error.value = '请至少输入一个下载链接'; return }
  if (mode.value !== 'url' && !torrentFiles.value.length) { error.value = mode.value === 'cas' ? '请选择 CAS 文件' : '请选择种子文件'; return }
  busy.value = true; error.value = ''
  try {
    if (mode.value === 'url') {
      results.value = await api('/files/offline', 'POST', { storageId: props.storage.id, parent: target.value, urls: list })
    } else {
      const body = new FormData()
      body.set('storageId', props.storage.id); body.set('parent', target.value)
      body.set('mode', mode.value)
      for (const file of torrentFiles.value) body.append(mode.value === 'cas' ? 'cas' : 'torrents', file)
      const response = await fetch('/api/files/offline', { method: 'POST', credentials: 'same-origin', body })
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || '种子提交失败')
      results.value = data
    }
    notify(`成功提交 ${results.value.filter(r => r.success).length} / ${results.value.length} 个任务`)
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
</script>
<template>
  <Modal :title="native115 ? '115 云下载' : mode === 'cas' ? 'CAS 秒传' : '内置离线下载'" compact wide @close="!busy && emit('close')">
    <form @submit.prevent="submit"><div class="modal-body offline-form">
      <div class="selected-driver"><ProviderIcon v-if="native115 || mode === 'cas'" :type="storage.type" small /><Icon v-else name="Download" /><h3>{{ native115 ? '115 云端任务' : mode === 'cas' ? '云端秒传' : '本机下载队列' }}</h3></div>
      <div class="segmented"><button type="button" :disabled="busy" :class="{ active: mode === 'url' }" @click="mode = 'url'">链接下载</button><button type="button" :disabled="busy" :class="{ active: mode === 'torrent' }" @click="mode = 'torrent'; torrentFiles = []">BT 下载</button><button v-if="supportsCAS" type="button" :disabled="busy" :class="{ active: mode === 'cas' }" @click="mode = 'cas'; torrentFiles = []">CAS 秒传</button></div>
      <label v-if="mode === 'url'">下载链接<textarea v-model="urls" rows="7" placeholder="一行一个链接" :disabled="busy" /></label>
      <div v-else><input :key="mode" ref="fileInput" type="file" multiple :accept="mode === 'cas' ? '.cas' : '.torrent'" hidden @change="chooseFiles" /><button class="offline-file-picker" type="button" :disabled="busy" @click="fileInput.click()"><Icon name="FolderOpen" />{{ torrentFiles.length ? `已选择 ${torrentFiles.length} 个文件` : mode === 'cas' ? '选择 CAS 文件' : '选择种子文件' }}</button></div>
      <div class="field"><label>{{ native115 ? '云下载目录' : '上传到' }}</label><button type="button" class="source-trigger" :disabled="busy" @click="picker = true"><span>{{ storage.name }} / {{ targetLabel }}</span><Icon name="FolderOpen" /></button></div>
      <div v-if="results.length" class="offline-results"><p v-for="(item, i) in results" :key="i" :class="{ 'error-message': !item.success }">{{ item.name }}：{{ item.success ? '已提交' : item.message || '提交失败' }}</p></div>
      <p v-if="error" class="error-message">{{ error }}</p>
    </div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="emit('close')">关闭</button><button class="btn primary" :disabled="busy || !supported">{{ busy ? '提交中…' : native115 ? '提交云下载' : mode === 'cas' ? '开始秒传' : '加入下载队列' }}</button></footer></form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="[storage]" :storage="storage.id" :initial="target" :initial-label="targetLabel" @close="picker = false" @select="target = $event.source; targetLabel = $event.sourceLabel; picker = false" />
</template>
<style scoped>
.offline-form .segmented button { font-size:15px; line-height:1.4; min-height:36px; }
</style>
