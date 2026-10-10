<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, bytes, notify } from '../lib'
import { useVirtualList } from '../virtual-list'
import { documentIcon } from '../file-icon'
import Icon from './Icon.vue'
import Modal from './Modal.vue'
import ImageViewer from './ImageViewer.vue'
const props = defineProps({ task: String, query: String, works: Array, running: Boolean, excluded: {type:Array, default:()=>[]} })
const emit = defineEmits(['reset', 'identify'])
const dir = ref(''), files = ref([]), loading = ref(false), error = ref(''), viewport = ref(null), editor = ref(null), image = ref(null), saving = ref(false)
let run = 0, editRun = 0, alive = true
const endpoint = path => `/strm-scrape/files?taskId=${encodeURIComponent(props.task)}&path=${encodeURIComponent(path || '.')}`
const filtered = computed(() => files.value.filter(f => f.name.toLowerCase().includes((props.query || '').toLowerCase()) && !props.excluded.some(p => p === '.' || f.id === p || f.id.startsWith(p + '/'))))
const { shown, top, bottom, reset } = useVirtualList(filtered, viewport, { rowHeight: 56, window: true })
const crumbs = computed(() => dir.value.split('/').filter(Boolean).map((name, i, all) => ({ name, path: all.slice(0, i + 1).join('/') })))
const isImage = f => /\.(png|jpe?g|webp|gif|avif)$/i.test(f.name)
const images = computed(() => files.value.filter(isImage))
function work(f) { return props.works?.find(w => (w.directories || [w.path.split('/').slice(0, -1).join('/')]).includes(f.id)) }
async function load(path = '') {
  const request = ++run
  editRun++; editor.value = null; image.value = null
  loading.value = true; error.value = ''; dir.value = path; files.value = []; reset()
  if (!props.task) { loading.value = false; return }
  try { const result = await api(endpoint(path)); if (alive && run === request) files.value = result }
  catch (e) { if (alive && run === request) error.value = e.message }
  finally { if (alive && run === request) loading.value = false }
}
async function open(f) {
  if (f.isDir) { load(f.id); return }
  if (isImage(f)) { image.value = f.id; return }
  const request = ++editRun
  try { const data = await api(endpoint(f.id)); if (alive && request === editRun) editor.value = { ...data, file: f } }
  catch (e) { if (alive && request === editRun) notify(e.message, true) }
}
async function save() {
  const current = editor.value
  saving.value = true
  try { await api(endpoint(current.file.id), 'PUT', { content: current.content, revision: current.revision }); editor.value = null; notify('文件已保存') }
  catch (e) { notify(e.message, true) } finally { saving.value = false }
}
function closeEditor() { if (!saving.value) { editor.value = null; editRun++ } }
watch(() => props.task, () => load(), { immediate: true })
watch(() => props.query, reset)
onUnmounted(() => { alive = false; run++; editRun++ })
defineExpose({ refresh: () => load(dir.value) })
</script>
<template>
  <div class="scrape-file-path path-bar"><button class="text-btn" @click="load()"><Icon name="Folder" />根目录</button><template v-for="crumb in crumbs" :key="crumb.path"><Icon name="ChevronRight" :size="14" /><button class="text-btn" @click="load(crumb.path)">{{ crumb.name }}</button></template></div>
  <div ref="viewport" class="scrape-files">
    <div v-if="top" :style="{height:`${top}px`}" />
    <article v-for="f in shown" :key="f.id" class="scrape-file-row">
      <button class="file-name" @click="open(f)"><Icon :name="f.isDir ? 'Folder' : documentIcon(f.name) || 'Image'" :size="24" :class="{'folder-color':f.isDir}" /><strong :data-tooltip="f.name">{{ f.name }}</strong></button>
      <small>{{ f.isDir ? '文件夹' : bytes(f.size) }}</small>
      <div v-if="f.isDir && work(f)" class="scrape-folder-actions"><button class="text-btn" :disabled="running" @click="emit('reset',work(f))"><Icon name="RotateCcw" />重置</button><button class="text-btn" :disabled="running" @click="emit('identify',work(f))"><Icon name="ScanSearch" />识别</button></div>
    </article>
    <div v-if="bottom" :style="{height:`${bottom}px`}" />
    <p v-if="loading || error || !filtered.length" class="small-empty" :role="error ? 'alert' : 'status'">{{ loading ? '正在读取目录…' : error || '暂无文件' }}</p>
  </div>
  <ImageViewer v-if="image" :images="images" :initial="image" @close="image = null" />
  <Modal v-if="editor" :title="editor.file.name" wide @close="closeEditor"><form @submit.prevent="save"><div class="modal-body"><textarea v-model="editor.content" class="scrape-text-editor" aria-label="文件内容" spellcheck="false" :disabled="saving" /></div><footer class="modal-footer"><button class="btn primary" :disabled="saving || running"><Icon name="Save" />保存</button><button type="button" class="btn" :disabled="saving" @click="closeEditor">取消</button></footer></form></Modal>
</template>
<style scoped>
.scrape-file-path { display:flex; align-items:center; gap:5px; padding:10px 14px; overflow:auto; white-space:nowrap; scrollbar-width:none; }
.scrape-file-path .text-btn { flex:none; font-size:14px; }
.scrape-file-row { display:flex; align-items:center; gap:12px; height:56px; padding:8px 14px; border-bottom:1px solid color-mix(in srgb,var(--border) 45%,transparent); }
.scrape-file-row:hover { background:var(--primary-soft); }
.scrape-file-row .file-name { flex:1; min-width:0; }
.scrape-file-row strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.scrape-file-row small { color:var(--muted); }
.scrape-folder-actions { display:flex; gap:14px; opacity:0; }
.scrape-file-row:hover .scrape-folder-actions,.scrape-file-row:focus-within .scrape-folder-actions { opacity:1; }
.scrape-text-editor { width:100%; height: min(55vh,480px); resize:vertical; font-family:Consolas,monospace; font-size:14px; line-height:1.6; }
@media(hover:none) { .scrape-folder-actions { opacity:1; } }
@media(max-width:600px) { .scrape-file-row small { display:none; }.scrape-folder-actions { gap:7px; } }
</style>
