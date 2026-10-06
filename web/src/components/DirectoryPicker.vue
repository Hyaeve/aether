<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import VirtualList from './VirtualList.vue'
const props = defineProps({ storage: String, initial: String })
const emit = defineEmits(['select', 'close'])
const path = ref(props.initial || '/'), history = ref([]), items = ref([]), busy = ref(false), error = ref('')
const label = ref(path.value === '/' ? '根目录' : '已选目录')
async function load() {
  busy.value = true; error.value = ''
  try { items.value = (await api(`/files?storage=${encodeURIComponent(props.storage)}&path=${encodeURIComponent(path.value)}`)).filter(f => f.isDir) }
  catch (e) { error.value = e.message; items.value = [] }
  finally { busy.value = false }
}
function enter(item) { history.value.push({ id: path.value, name: label.value }); path.value = item.id; label.value = item.name; load() }
function back() { const parent = history.value.pop(); path.value = parent?.id || '/'; label.value = parent?.name || '根目录'; load() }
function root() { history.value = []; path.value = '/'; label.value = '根目录'; load() }
onMounted(load)
</script>
<template>
  <Modal title="选择存储目录" @close="emit('close')">
    <div class="modal-body"><div class="path-bar"><button class="icon-btn" :disabled="path === '/' || busy" aria-label="上级目录" @click="back"><Icon name="ArrowUp" /></button><button class="text-btn" :disabled="path === '/' || busy" @click="root">根目录</button><span>{{ label }}</span></div>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      <div v-if="busy" class="small-empty">正在读取目录…</div>
      <div v-else-if="!items.length && !error" class="small-empty">当前目录没有子文件夹</div>
      <div v-if="!busy && !error" style="height: min(360px, 50dvh)"><VirtualList :items="items"><template #default="{ item }"><button class="directory-row" @click="enter(item)"><Icon name="Folder" /><span>{{ item.name }}</span><Icon name="ChevronRight" /></button></template></VirtualList></div>
    </div>
    <footer class="modal-footer"><button class="btn" @click="emit('close')">取消</button><button class="btn primary" :disabled="busy || !!error" @click="emit('select', path)">选择当前目录</button></footer>
  </Modal>
</template>
