<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
const props = defineProps({ task: String, excluded: Array })
const emit = defineEmits(['close', 'save'])
const nodes = ref([]), excluded = ref([...(props.excluded || [])]), loading = ref(true), error = ref('')
let alive = true
const isChecked = path => !excluded.value.some(d => d === '.' || d === path || path.startsWith(d + '/'))
const mixed = path => isChecked(path) && excluded.value.some(d => d.startsWith(path + '/'))
function toggle(path, checked) {
  if (!path) { excluded.value = checked ? [] : ['.']; return }
  if (checked && excluded.value.includes('.')) excluded.value = nodes.value.map(n=>n.path)
  // Re-including a descendant of an excluded folder keeps its siblings excluded.
  const ancestors = []
  function find(list) { for (const n of list) { if (n.path === path || path.startsWith(n.path + '/')) { ancestors.push(n); if (n.children) find(n.children) } } }
  find(nodes.value)
  if (checked) for (const n of ancestors.slice(0,-1)) if (excluded.value.includes(n.path)) {
    excluded.value = excluded.value.filter(d => d !== n.path)
    excluded.value.push(...(n.children || []).filter(c => c.path !== path && !path.startsWith(c.path + '/')).map(c => c.path))
  }
  excluded.value = excluded.value.filter(d => d !== path && !d.startsWith(path + '/'))
  if (!checked) excluded.value.push(path)
}
async function children(path) {
  const data = await api(`/strm-scrape/directories?taskId=${encodeURIComponent(props.task)}&path=${encodeURIComponent(path)}`)
  if (!Array.isArray(data.directories)) throw new Error('目录数据无效')
  return data.directories.map(path => ({ path, open: false, children: null, loading: false }))
}
async function expand(node) {
  if (node.loading) return
  if (node.children) { node.open = !node.open; return }
  node.loading = true; error.value = ''
  try { const result = await children(node.path); if (alive) { node.children = result; node.open = true } }
  catch(e) { if (alive) error.value = e.message }
  finally { node.loading = false }
}
const rows = computed(() => {
  const result = []
  function walk(list, depth) { for (const node of list) { result.push({node,depth}); if(node.open && node.children) walk(node.children,depth+1) } }
  walk(nodes.value,0); return result
})
onMounted(async () => { try { const result = await children(''); if(alive) nodes.value = result } catch(e) { if(alive) error.value = e.message } finally { loading.value = false } })
onUnmounted(() => { alive = false })
</script>
<template>
  <Modal title="刮削范围" compact @close="emit('close')">
    <div class="modal-body scope-tree">
      <label class="scope-all"><input type="checkbox" :checked="!excluded.length" :indeterminate="!!excluded.length && nodes.some(n=>isChecked(n.path))" :disabled="loading" @change="toggle('', $event.target.checked)" />全部目录</label>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p><p v-if="loading" class="muted">读取目录中…</p>
      <div v-for="{node,depth} in rows" :key="node.path" class="scope-node" :style="{paddingLeft: `${depth * 20}px`}">
        <button class="icon-btn" :aria-label="`${node.open ? '折叠' : '展开'} ${node.path}`" :aria-expanded="node.open" @click="expand(node)"><Icon :name="node.loading ? 'LoaderCircle' : 'ChevronRight'" :class="{spin:node.loading, expanded:node.open}" :size="17" /></button>
        <label><input type="checkbox" :aria-label="node.path" :checked="isChecked(node.path)" :indeterminate="mixed(node.path)" @change="toggle(node.path,$event.target.checked)" /><Icon name="Folder" :size="17" /><span>{{ node.path.split('/').at(-1) }}</span></label>
      </div>
    </div>
    <footer class="modal-footer"><button class="btn primary" :disabled="loading || !!error" @click="emit('save', [...excluded])">确认范围</button><button class="btn" @click="emit('close')">取消</button></footer>
  </Modal>
</template>
<style scoped>
.scope-tree { height:360px; overflow:auto; }.scope-tree label { display:flex; flex-direction:row; align-items:center; gap:8px; margin:0; }.scope-all { margin-bottom:12px !important; }.scope-node { display:flex; align-items:center; min-height:36px; }.scope-node > .icon-btn { width:28px; height:28px; flex:none; }.scope-node label { min-width:0; flex:1; }.scope-node span { overflow-wrap:anywhere; }.scope-node .expanded { transform:rotate(90deg); }.scope-node svg { transition:transform .18s; }
</style>
