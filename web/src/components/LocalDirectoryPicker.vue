<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import VirtualList from './VirtualList.vue'
import PathBreadcrumbs from './PathBreadcrumbs.vue'
const props = defineProps({ initial: { type: String, default: '' } })
const emit = defineEmits(['select', 'close'])
const directory = ref({ path: '', items: [] }), busy = ref(false), error = ref('')
let generation = 0
const creating = ref(false), createOpen = ref(false), folderName = ref(''), createError = ref(''), nameInput = ref(null)
async function showCreate() {
  createOpen.value = !createOpen.value; createError.value = ''; folderName.value = ''
  if (createOpen.value) { await nextTick(); nameInput.value?.focus() }
}
async function createFolder() {
  if (creating.value || busy.value || !directory.value.path || !folderName.value.trim()) return
  const run = generation, path = directory.value.path
  creating.value = true; createError.value = ''
  try {
    await api('/local-directories', 'POST', { path, name: folderName.value.trim() })
    if (run !== generation) return
    createOpen.value = false; folderName.value = ''; await load(path)
  } catch (e) { if (run === generation) createError.value = e.message }
  finally { creating.value = false }
}
const crumbs = computed(() => {
  const normalized = directory.value.path.replaceAll('\\', '/')
  if (!normalized) return []
  const drive = normalized.match(/^[A-Za-z]:\//)?.[0]
  const root = drive || '/'
  const parts = normalized.slice(root.length).split('/').filter(Boolean)
  return [{ label: drive || '根目录', path: root }, ...parts.map((name, i) => ({ label: name, path: root + parts.slice(0, i + 1).join('/') }))]
})
async function load(path) {
  const run = ++generation
  busy.value = true; error.value = ''; createOpen.value = false; createError.value = ''
  try {
    const result = await api(`/local-directories?path=${encodeURIComponent(path)}`)
    if (run === generation) directory.value = result
  } catch (e) { if (run === generation) error.value = e.message }
  finally { if (run === generation) busy.value = false }
}
onMounted(() => load(props.initial))
onUnmounted(() => generation++)
</script>
<template>
  <Modal title="选择容器目录" compact wide @close="emit('close')">
    <div class="modal-body local-directory-picker">
      <div class="picker-toolbar"><PathBreadcrumbs class="directory-crumbs" aria-label="容器目录路径" :entries="crumbs.slice(1).map(c => ({ name: c.label }))" :disabled="busy || creating" @jump="load(crumbs[$event + 1]?.path || '')" /><button type="button" class="icon-btn" aria-label="新建文件夹" :aria-expanded="createOpen" :disabled="!directory.path || busy || creating || !!error" @click="showCreate"><Icon name="FolderPlus" :size="23" /></button></div>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      <div class="container-directory-list" :aria-busy="busy">
        <p v-if="busy" class="small-empty">正在读取目录…</p>
        <template v-else-if="!error"><VirtualList v-if="directory.items.length" :items="directory.items"><template #default="{ item }"><button type="button" class="directory-row" :disabled="creating" @click="load(item.path)"><Icon name="Folder" /><span>{{ item.name }}</span><Icon name="ChevronRight" /></button></template></VirtualList><p v-else class="small-empty">此目录没有子目录</p></template>
      </div>
    </div>
    <footer class="modal-footer"><button class="btn" @click="emit('close')">取消</button><button class="btn primary" :disabled="busy || creating || !!error || !directory.path" @click="emit('select', directory.path)">选择当前目录</button></footer>
  </Modal>
  <Modal v-if="createOpen" title="新建文件夹" compact @close="!creating && (createOpen = false)"><form @submit.prevent="createFolder"><div class="modal-body"><label>文件夹名称<input ref="nameInput" v-model="folderName" maxlength="255" required :disabled="creating" /></label><p v-if="createError" class="error-message" role="alert">{{ createError }}</p></div><footer class="modal-footer"><button type="submit" class="btn primary" :disabled="creating || busy || !folderName.trim()">确认创建</button><button type="button" class="btn" :disabled="creating" @click="createOpen = false">取消</button></footer></form></Modal>
</template>
<style scoped>
.picker-toolbar { display: flex; align-items: center; gap: 8px; }
.picker-toolbar .directory-crumbs { flex: 1; min-width: 0; }
.picker-toolbar > .icon-btn { flex: 0 0 36px; }
.picker-create-form { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 8px 0; }
.picker-create-form input { flex: 1; min-width: 100px; width: 0; }
.picker-create-form .error-message { flex-basis: 100%; margin: 0; }
</style>
