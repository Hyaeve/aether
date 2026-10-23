<script setup>
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { api, date, driverOf, notify } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import VirtualList from './VirtualList.vue'
import PathBreadcrumbs from './PathBreadcrumbs.vue'
const props = defineProps({ storages: Array, storage: String, initial: String, initialLabel: String, initialTrail: Array, allowAll: Boolean, includeFullPath:Boolean, clickablePath:Boolean })
const emit = defineEmits(['select', 'close'])
const selected = ref(props.storage || '')
const dir = ref(props.initial || '/'), trail = ref((props.initialTrail || []).map(c => ({ ...c }))), items = ref([]), busy = ref(false), error = ref(''), query = ref('')
const initialStorage = props.storages.find(s => s.id === selected.value)
const pathBased = initialStorage && (['local','openlist','webdav'].includes(initialStorage.type) || initialStorage.type === 'mobile' && initialStorage.config?.mode !== 'native')
const initialPath = props.initialLabel?.startsWith('/') ? props.initialLabel : pathBased && dir.value.startsWith('/') ? dir.value : ''
const atRoot = dir.value === '/' || dir.value === initialStorage?.config?.root || (['115','quark'].includes(initialStorage?.type) && dir.value === '0') || initialStorage?.type === 'tianyi' && dir.value === '-11'
const label = ref(atRoot ? '根目录' : (initialPath ? initialPath.split('/').filter(Boolean).at(-1) : props.initialLabel) || '已选目录')
let restorePending = !!initialPath && !atRoot && !trail.value.length
let restore115Pending = initialStorage?.type === '115' && !atRoot && !trail.value.length && !initialPath
let incompletePending = props.includeFullPath && initialStorage?.type !== '115' && !atRoot && !trail.value.length && !initialPath
let generation = 0
const creating = ref(false), createOpen = ref(false), folderName = ref(''), createError = ref(''), nameInput = ref(null)
async function showCreate() {
  createOpen.value = !createOpen.value; createError.value = ''; folderName.value = ''
  if (createOpen.value) { await nextTick(); nameInput.value?.focus() }
}
async function createFolder() {
  if (creating.value || busy.value || !selected.value || !folderName.value.trim()) return
  const run = generation, storageId = selected.value, source = dir.value
  creating.value = true; createError.value = ''
  try {
    await api('/files/action', 'POST', { storageId, source, action: 'mkdir', ids: [], name: folderName.value.trim() })
    if (run !== generation) return
    createOpen.value = false; folderName.value = ''; query.value = ''; await load()
  } catch (e) { if (run === generation) createError.value = e.message }
  finally { creating.value = false }
}
const visible = computed(() => items.value.filter(f => f.name.toLowerCase().includes(query.value.toLowerCase())))
const wholeStorage = computed(() => props.allowAll && !!selected.value && trail.value.length === 0 && (dir.value === '/' || dir.value === props.storages.find(s => s.id === selected.value)?.config?.root))
async function load() {
  const run = ++generation
  const restore = restorePending; restorePending = false
  const restore115 = restore115Pending; restore115Pending = false
  const incomplete = incompletePending; incompletePending = false
  error.value = ''; busy.value = true; items.value = []
  if (!selected.value) { busy.value = false; return }
  try {
    if (incomplete) throw new Error('旧目录记录不包含完整路径，请从根目录重新选择')
    if (restore115) {
      const result = await api(`/files/directory-trail?storage=${encodeURIComponent(selected.value)}&path=${encodeURIComponent(dir.value)}`)
      if (run !== generation) return
      if (!Array.isArray(result.trail) || !result.path?.startsWith('/') || !result.label) throw new Error('完整路径不可用，请从根目录重新选择')
      trail.value = result.trail; label.value = result.label
    }
    if (restore) {
      const parts = initialPath.split('/').filter(Boolean)
      if (parts.length > 128 || parts.some(p => p === '.' || p === '..')) throw new Error('已保存路径无效，请从根目录重新选择')
      let parent = '/', name = '根目录'
      const restored = []
      for (const part of parts) {
        const entries = await api(`/files?storage=${encodeURIComponent(selected.value)}&path=${encodeURIComponent(parent)}`)
        if (run !== generation) return
        const matches = entries.filter(f => f.isDir && f.name === part)
        if (matches.length !== 1) throw new Error('已保存路径已变化，请从根目录重新选择')
        restored.push({id:parent,name}); parent = matches[0].id; name = matches[0].name
      }
      if (parent !== dir.value) throw new Error('已保存目录身份已变化，请从根目录重新选择')
      trail.value = restored; label.value = name
    }
    const result = await api(`/files?storage=${encodeURIComponent(selected.value)}&path=${encodeURIComponent(dir.value)}`)
    if (run === generation) items.value = result.filter(f => f.isDir)
  } catch (e) { if (run === generation) { error.value = e.message; if (restore || restore115 || incomplete) notify(e.message,true) } }
  finally { if (run === generation) busy.value = false }
}
function root() { restorePending = false; restore115Pending = false; incompletePending = false; dir.value = '/'; label.value = '根目录'; trail.value = []; query.value = '' }
function choose(id) { if (creating.value || selected.value === id) return; selected.value = id; root() }
function enter(item) { if (creating.value) return; trail.value.push({ id: dir.value, name: label.value }); dir.value = item.id; label.value = item.name; query.value = '' }
const crumbs = computed(() => {
  const entries = [...trail.value, {id:dir.value,name:label.value}]
  const pool = props.storages.find(s => s.id === selected.value)
  const first = entries[0]?.id
  if (first === pool?.config?.root || (['115','quark'].includes(pool?.type) && first === '0') || pool?.type === 'tianyi' && first === '-11') entries[0] = {id:'/',name:'根目录'}
  if (entries[0]?.id !== '/') entries.unshift({id:'/',name:'根目录'})
  return entries.filter((c,i) => !i || c.id !== entries[i-1].id)
})
const fullPath = computed(()=>'/'+crumbs.value.filter(c=>c.id!=='/').map(c=>c.name.replace(/^\/+/, '')).join('/'))
function jump(index) {
  const entry=crumbs.value[index]
  if (!entry || creating.value) return
  trail.value=crumbs.value.slice(0,index); dir.value=entry.id; label.value=entry.name; query.value=''
}
watch([selected, dir], () => { createOpen.value = false; createError.value = ''; load() }, { immediate: true })
onUnmounted(() => generation++)
</script>
<template>
  <Modal title="选择存储目录" compact wide @close="emit('close')">
    <div class="task-source-picker">
      <aside class="source-accounts"><h3>选择存储</h3><button v-if="allowAll" type="button" :class="{ active: !selected }" @click="choose('')"><Icon name="Layers" /><span>所有存储池</span></button><button v-for="s in storages" :key="s.id" type="button" :class="{ active: selected === s.id }" :aria-pressed="selected === s.id" @click="choose(s.id)"><ProviderIcon :type="s.type" small /><span><strong>{{ s.name }}</strong><small>{{ driverOf(s.type).name }}</small></span></button><p v-if="!storages.length" class="small-empty">暂无可用存储池</p></aside>
      <section class="source-directories">
        <div class="source-toolbar"><PathBreadcrumbs class="directory-crumbs" aria-label="存储目录路径" :entries="crumbs.slice(1)" :disabled="busy || creating" @jump="jump($event + 1)" /><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="筛选当前目录文件夹" placeholder="筛选当前目录文件夹" /></div><button type="button" class="icon-btn" aria-label="新建文件夹" :disabled="!selected || busy || creating || !!error" @click="showCreate"><Icon name="FolderPlus" :size="23" /></button></div>
        <div class="source-list" :aria-busy="busy">
          <div class="source-columns"><span>名称</span><span>修改时间</span></div>
          <p v-if="error" class="error-message" role="alert">{{ error }}</p>
          <p v-else-if="busy" class="small-empty">正在读取目录…</p>
          <p v-else-if="!visible.length" class="small-empty">当前目录没有匹配的文件夹</p>
          <VirtualList v-if="!busy && !error && visible.length" :items="visible"><template #default="{ item }"><button type="button" class="source-directory" :aria-label="item.name" @click="enter(item)"><Icon name="Folder" /><span>{{ item.name }}</span><time>{{ item.modified && !item.modified.startsWith('0001') ? date(item.modified) : '-' }}</time></button></template></VirtualList>
        </div>
        <footer class="source-footer"><button type="button" class="btn" :disabled="busy || creating" @click="load"><Icon name="RefreshCw" />刷新</button><button type="button" class="btn primary" :disabled="busy || creating || !!error || (!selected && !allowAll)" @click="emit('select', { storageId: selected, source: dir, sourceLabel: selected ? (wholeStorage ? '根目录' : label) : '所有存储池', sourceTrail: trail, ...(includeFullPath ? {sourcePath: fullPath} : {}) })">{{ !selected && allowAll ? '选择所有存储池' : wholeStorage ? '选择整个存储池' : '选择当前目录' }}</button></footer>
      </section>
    </div>
  </Modal>
  <Modal v-if="createOpen" title="新建文件夹" compact @close="!creating && (createOpen = false)"><form @submit.prevent="createFolder"><div class="modal-body"><label>文件夹名称<input ref="nameInput" v-model="folderName" maxlength="255" required :disabled="creating" /></label><p v-if="createError" class="error-message" role="alert">{{ createError }}</p></div><footer class="modal-footer"><button type="submit" class="btn primary" :disabled="creating || busy || !folderName.trim()">确认创建</button><button type="button" class="btn" :disabled="creating" @click="createOpen = false">取消</button></footer></form></Modal>
</template>
<style scoped>
.picker-create-form { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 8px 0; }
.picker-create-form input { flex: 1; min-width: 100px; width: 0; }
.picker-create-form .error-message { flex-basis: 100%; margin: 0; }
.source-toolbar { display:grid; grid-template-columns:minmax(0,1fr) 36px; margin-bottom:10px; }
.source-toolbar .directory-crumbs { grid-column:1/-1; min-width:0; margin:0; }
.source-toolbar .search-field { width:100%; margin:0; }
.source-toolbar .search-field input { margin:0; }
.source-toolbar > .icon-btn { flex: 0 0 36px; }
.source-columns { padding:8px 14px; font-size:12px; color:var(--text); }
.source-directory time { font-size:14px; }
@media (max-width: 700px) {
  .source-toolbar .search-field { order:0; }
}
</style>
