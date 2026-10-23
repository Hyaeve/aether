<script setup>
import { nextTick, onUnmounted, ref, watch } from 'vue'
import Modal from './Modal.vue'
import Icon from './Icon.vue'

defineProps({ items: { type: Array, default: () => [] }, busy: Boolean, market: Boolean })
const emit = defineEmits(['search', 'select', 'repositories'])
const open = ref(false), query = ref(''), input = ref(null)
let timer
watch(query, value => { clearTimeout(timer); timer = setTimeout(() => emit('search', value), 200) })
onUnmounted(() => clearTimeout(timer))
async function show() {
  query.value = ''; open.value = true; emit('search', '')
  await nextTick(); input.value?.focus()
}
function select(item) { open.value = false; emit('select', item) }
function move(event) {
  const nodes = [...event.currentTarget.querySelectorAll('input, .plugin-search-result')]
  const index = nodes.indexOf(document.activeElement)
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault(); nodes[(index + (event.key === 'ArrowDown' ? 1 : nodes.length - 1)) % nodes.length]?.focus()
  }
}
</script>
<template>
  <div class="plugin-float-tools">
    <button v-if="market" class="plugin-float-repository" aria-label="插件仓库" data-tooltip="插件仓库" @click="emit('repositories')"><Icon name="Warehouse" :size="21" /></button>
    <button class="plugin-float-search" :aria-label="market ? '搜索市场插件' : '搜索辅助工具'" :data-tooltip="market ? '搜索市场插件' : '搜索辅助工具'" @click="show"><Icon name="Search" :size="23" /></button>
  </div>
  <Modal v-if="open" standard :title="market ? '搜索市场插件' : '搜索辅助工具'" @close="open = false">
    <div class="modal-body plugin-search-body" @keydown="move">
      <div class="search-field"><Icon name="Search" /><input ref="input" v-model="query" aria-label="插件搜索关键词" autocomplete="off" /></div>
      <div class="plugin-search-results" :aria-busy="busy">
        <p v-if="busy" class="small-empty">正在搜索…</p>
        <p v-else-if="!items.length" class="small-empty">没有匹配的插件</p>
        <button v-for="item in items" :key="item.repository ? item.repository + ':' + item.id : item.name" class="plugin-search-result" @click="select(item)"><Icon :name="item.iconName || 'Puzzle'" :size="22" /><span><strong>{{ item.name }}</strong><small>{{ item.subtitle || item.source || item.detail }}</small></span><Icon name="ChevronRight" :size="16" /></button>
      </div>
    </div>
  </Modal>
</template>
<style scoped>
.plugin-float-tools{position:fixed;bottom:28px;right:28px;z-index:32;display:flex;flex-direction:column;align-items:center;gap:8px}.plugin-float-tools>button{width:46px;height:46px;border:1px solid var(--border);background:var(--surface);color:var(--text);box-shadow:var(--shadow);border-radius:50%;display:grid;place-items:center;transition:color .16s,transform .16s,opacity .16s}.plugin-float-tools>button:hover{color:var(--primary);transform:translateY(-2px)}.plugin-float-repository{opacity:0;pointer-events:none;transform:translateY(8px)}.plugin-float-tools:hover .plugin-float-repository,.plugin-float-tools:focus-within .plugin-float-repository{opacity:1;pointer-events:auto;transform:none}.plugin-search-body>.search-field{margin:0 0 12px}.plugin-search-body input{height:40px}.plugin-search-results{max-height:52vh;overflow:auto;min-height:100px}.plugin-search-result{width:100%;display:flex;align-items:center;text-align:left;gap:12px;padding:12px 8px;border-bottom:1px solid var(--border);background:transparent;border-radius:0}.plugin-search-result:hover,.plugin-search-result:focus-visible{background:var(--primary-soft)}.plugin-search-result>span{flex:1;min-width:0;display:grid;gap:4px}.plugin-search-result strong,.plugin-search-result small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.plugin-search-result small{color:var(--muted);font-size:12px}.plugin-search-result>svg{flex:none}
@media(hover:none){.plugin-float-repository{opacity:1;pointer-events:auto;transform:none}}@media(prefers-reduced-motion:reduce){.plugin-float-tools>button{transition:none}}
</style>
