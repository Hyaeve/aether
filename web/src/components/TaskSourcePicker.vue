<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, date, driverOf } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import VirtualList from './VirtualList.vue'
const props = defineProps({ storages: Array, storage: String, initial: String, initialLabel: String, initialTrail: Array, allowAll: Boolean })
const emit = defineEmits(['select', 'close'])
const selected = ref(props.storage || '')
const dir = ref(props.initial || '/'), trail = ref((props.initialTrail || []).map(c => ({ ...c }))), items = ref([]), busy = ref(false), error = ref(''), query = ref('')
const label = ref(props.initialLabel || (dir.value === '/' ? '根目录' : '已选目录'))
let generation = 0
const visible = computed(() => items.value.filter(f => f.name.toLowerCase().includes(query.value.toLowerCase())))
const wholeStorage = computed(() => props.allowAll && !!selected.value && trail.value.length === 0 && (dir.value === '/' || dir.value === props.storages.find(s => s.id === selected.value)?.config?.root))
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
function root() { dir.value = '/'; label.value = '根目录'; trail.value = []; query.value = '' }
function choose(id) { if (selected.value === id) return; selected.value = id; root() }
function enter(item) { trail.value.push({ id: dir.value, name: label.value }); dir.value = item.id; label.value = item.name; query.value = '' }
function back() { const parent = trail.value.pop(); dir.value = parent?.id || '/'; label.value = parent?.name || '根目录'; query.value = '' }
watch([selected, dir], load, { immediate: true })
onUnmounted(() => generation++)
</script>
<template>
  <Modal title="选择存储目录" compact wide @close="emit('close')">
    <div class="task-source-picker">
      <aside class="source-accounts"><h3>选择存储</h3><button v-if="allowAll" type="button" :class="{ active: !selected }" @click="choose('')"><Icon name="Layers" /><span>所有存储池</span></button><button v-for="s in storages" :key="s.id" type="button" :class="{ active: selected === s.id }" :aria-pressed="selected === s.id" @click="choose(s.id)"><ProviderIcon :type="s.type" small /><span><strong>{{ s.name }}</strong><small>{{ driverOf(s.type).name }}</small></span></button><p v-if="!storages.length" class="small-empty">暂无可用存储池</p></aside>
      <section class="source-directories">
        <div class="source-toolbar"><button type="button" class="icon-btn bordered" aria-label="上级目录" :disabled="dir === '/' || busy" @click="back"><Icon name="ArrowUp" /></button><button type="button" class="text-btn" :disabled="dir === '/' || busy" @click="root">根目录</button><span class="source-path">{{ label }}</span><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="筛选当前目录文件夹" placeholder="筛选当前目录文件夹" /></div></div>
        <div class="source-list" :aria-busy="busy">
          <div class="source-columns"><span>名称</span><span>修改时间</span></div>
          <p v-if="error" class="error-message" role="alert">{{ error }}</p>
          <p v-else-if="busy" class="small-empty">正在读取目录…</p>
          <p v-else-if="!visible.length" class="small-empty">当前目录没有匹配的文件夹</p>
          <VirtualList v-if="!busy && !error && visible.length" :items="visible"><template #default="{ item }"><button type="button" class="source-directory" :aria-label="item.name" @click="enter(item)"><Icon name="Folder" /><span>{{ item.name }}</span><time>{{ item.modified && !item.modified.startsWith('0001') ? date(item.modified) : '-' }}</time></button></template></VirtualList>
        </div>
        <footer class="source-footer"><button type="button" class="btn" :disabled="busy" @click="load"><Icon name="RefreshCw" />刷新</button><button type="button" class="btn primary" :disabled="busy || !!error || (!selected && !allowAll)" @click="emit('select', { storageId: selected, source: dir, sourceLabel: selected ? (wholeStorage ? '根目录' : label) : '所有存储池', sourceTrail: trail })">{{ !selected && allowAll ? '选择所有存储池' : wholeStorage ? '选择整个存储池' : '选择当前目录' }}</button></footer>
      </section>
    </div>
  </Modal>
</template>
