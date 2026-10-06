<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import VirtualList from './VirtualList.vue'
const props = defineProps({ initial: { type: String, default: '' } })
const emit = defineEmits(['select', 'close'])
const directory = ref({ path: '', items: [] }), busy = ref(false), error = ref('')
let generation = 0
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
  busy.value = true; error.value = ''
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
      <nav class="directory-crumbs" aria-label="容器目录路径"><template v-for="(crumb, i) in crumbs" :key="crumb.path"><Icon v-if="i" name="ChevronRight" :size="14" /><button type="button" :disabled="busy" :aria-current="i === crumbs.length - 1 ? 'location' : undefined" @click="load(crumb.path)">{{ crumb.label }}</button></template><button v-if="!crumbs.length && error" class="text-btn" @click="load('')">根目录</button></nav>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      <div class="container-directory-list" :aria-busy="busy">
        <p v-if="busy" class="small-empty">正在读取目录…</p>
        <template v-else-if="!error"><VirtualList v-if="directory.items.length" :items="directory.items"><template #default="{ item }"><button type="button" class="directory-row" @click="load(item.path)"><Icon name="Folder" /><span>{{ item.name }}</span><Icon name="ChevronRight" /></button></template></VirtualList><p v-else class="small-empty">此目录没有子目录</p></template>
      </div>
    </div>
    <footer class="modal-footer"><button class="btn" @click="emit('close')">取消</button><button class="btn primary" :disabled="busy || !!error || !directory.path" @click="emit('select', directory.path)">选择当前目录</button></footer>
  </Modal>
</template>
