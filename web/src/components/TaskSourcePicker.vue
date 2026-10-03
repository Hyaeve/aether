<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, date, driverOf } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
const props = defineProps({ storages: Array, storage: String, initial: String })
const emit = defineEmits(['select', 'close'])
const selected = ref(props.storage || props.storages[0]?.id || '')
const dir = ref(props.initial || '/'), trail = ref([]), items = ref([]), busy = ref(false), error = ref(''), query = ref('')
let generation = 0
const visible = computed(() => items.value.filter(f => f.name.toLowerCase().includes(query.value.toLowerCase())))
async function load() {
  const run = ++generation
  error.value = ''; busy.value = true; items.value = []
  if (!selected.value) { busy.value = false; return }
  try {
    const result = await api(`/files?storage=${encodeURIComponent(selected.value)}&path=${encodeURIComponent(dir.value)}`)
    if (run === generation) items.value = result.filter(f => f.isDir)
  } catch (e) { if (run === generation) error.value = e.message }
  finally { if (run === generation) busy.value = false }
}
function choose(id) { if (selected.value === id) return; selected.value = id; dir.value = '/'; trail.value = []; query.value = '' }
function enter(item) { trail.value.push(dir.value); dir.value = item.id; query.value = '' }
watch([selected, dir], load, { immediate: true })
onUnmounted(() => generation++)
</script>
<template>
  <Modal title="选择存储目录" wide @close="emit('close')">
    <div class="task-source-picker">
      <aside class="source-accounts"><h3>选择账号</h3><button v-for="s in storages" :key="s.id" type="button" :class="{ active: selected === s.id }" :aria-pressed="selected === s.id" @click="choose(s.id)"><ProviderIcon :type="s.type" small /><span><strong>{{ s.name }}</strong><small>{{ driverOf(s.type).name }}</small></span></button><p v-if="!storages.length" class="small-empty">暂无可用存储池</p></aside>
      <section class="source-directories">
        <div class="source-toolbar"><button type="button" class="icon-btn bordered" title="上级目录" aria-label="上级目录" :disabled="!trail.length || busy" @click="dir = trail.pop()"><Icon name="ArrowUp" /></button><span class="source-path">{{ dir === '/' ? '根目录' : dir }}</span><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="筛选当前目录文件夹" placeholder="筛选当前目录文件夹" /></div></div>
        <div class="source-list" :aria-busy="busy">
          <div class="source-columns"><span>名称</span><span>修改时间</span></div>
          <p v-if="error" class="error-message" role="alert">{{ error }}</p>
          <p v-else-if="busy" class="small-empty">正在读取目录…</p>
          <p v-else-if="!visible.length" class="small-empty">当前目录没有匹配的文件夹</p>
          <button v-for="item in visible" :key="item.id" type="button" class="source-directory" :aria-label="item.name" @click="enter(item)"><Icon name="Folder" /><span>{{ item.name }}</span><time>{{ item.modified && !item.modified.startsWith('0001') ? date(item.modified) : '-' }}</time></button>
        </div>
        <footer class="source-footer"><button type="button" class="btn" :disabled="busy" @click="load"><Icon name="RefreshCw" />刷新</button><button type="button" class="btn primary" :disabled="busy || !!error || !selected" @click="emit('select', { storageId: selected, source: dir })">选择当前目录</button></footer>
      </section>
    </div>
  </Modal>
</template>
