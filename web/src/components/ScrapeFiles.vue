<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, notify } from '../lib'
import { useVirtualList } from '../virtual-list'
import { documentIcon } from '../file-icon'
import Icon from './Icon.vue'
import ImageViewer from './ImageViewer.vue'
import TextFileEditor from './TextFileEditor.vue'
import ScrollRail from './ScrollRail.vue'
const props = defineProps({ task: String, query: String, works: Array, running: Boolean, excluded: {type:Array, default:()=>[]} })
const emit = defineEmits(['reset', 'identify'])
const dir = ref(''), files = ref([]), loading = ref(false), error = ref(''), viewport = ref(null), editor = ref(null), image = ref(null)
let run = 0, editRun = 0, alive = true
const endpoint = path => `/strm-scrape/files?taskId=${encodeURIComponent(props.task)}&path=${encodeURIComponent(path || '.')}`
const filtered = computed(() => files.value.filter(f => f.name.toLowerCase().includes((props.query || '').toLowerCase()) && !props.excluded.some(p => p === '.' || f.id === p || f.id.startsWith(p + '/'))))
const { shown, top, bottom, reset } = useVirtualList(filtered, viewport, { rowHeight: 56 })
const crumbs = computed(() => dir.value.split('/').filter(Boolean).map((name, i, all) => ({ name, path: all.slice(0, i + 1).join('/') })))
const isImage = f => /\.(png|jpe?g|webp|gif|avif)$/i.test(f.name)
const images = computed(() => files.value.filter(isImage))
function work(f) { return props.works?.find(w => (w.directories || [w.path.split('/').slice(0, -1).join('/')]).some(p => p === f.id || p.startsWith(f.id + '/') && /^(?:season\s*\d+|s\d+|第.+季)$/i.test(p.slice(f.id.length + 1)))) }
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
function closeEditor() { editor.value = null; editRun++ }
watch(() => props.task, () => load(), { immediate: true })
watch(() => props.query, reset)
onUnmounted(() => { alive = false; run++; editRun++ })
defineExpose({ refresh: () => load(dir.value) })
</script>
<template>
  <div class="scrape-file-path path-bar"><button class="text-btn" @click="load()"><Icon name="Folder" />根目录</button><template v-for="crumb in crumbs" :key="crumb.path"><Icon name="ChevronRight" :size="14" /><button class="text-btn" @click="load(crumb.path)">{{ crumb.name }}</button></template></div>
  <div class="scrape-file-shell"><div ref="viewport" class="scrape-files">
    <div v-if="top" :style="{height:`${top}px`}" />
    <article v-for="f in shown" :key="f.id" class="scrape-file-row">
      <button class="file-name" @click="open(f)"><Icon :name="f.isDir ? 'Folder' : documentIcon(f.name) || 'Image'" :size="24" :class="{'folder-color':f.isDir}" /><strong :data-tooltip="f.name">{{ f.name }}</strong></button>
      <div v-if="f.isDir && work(f)" class="scrape-folder-actions"><button class="text-btn" :disabled="running" @click="emit('reset',work(f))"><Icon name="RotateCcw" />重置</button><button class="text-btn" :disabled="running" @click="emit('identify',work(f))"><Icon name="ScanSearch" />识别</button></div>
    </article>
    <div v-if="bottom" :style="{height:`${bottom}px`}" />
    <p v-if="loading || error || !filtered.length" class="small-empty" :role="error ? 'alert' : 'status'">{{ loading ? '正在读取目录…' : error || '暂无文件' }}</p>
  </div>
  <ScrollRail :element="viewport" /></div>
  <ImageViewer v-if="image" :images="images" :initial="image" @close="image = null" />
  <TextFileEditor v-if="editor" :file="editor.file" :endpoint="endpoint(editor.file.id)" :content="editor.content" :revision="editor.revision" :disabled="running" @close="closeEditor" />
</template>
<style scoped>
.scrape-file-shell{position:relative;flex:1;min-height:0}.scrape-files{height:100%;overflow:auto;scrollbar-width:none}.scrape-files::-webkit-scrollbar{display:none}
.scrape-file-path { display:flex; align-items:center; gap:5px; padding:10px 14px; overflow:auto; white-space:nowrap; scrollbar-width:none; }
.scrape-file-path .text-btn { flex:none; font-size:14px; }
.scrape-file-path .text-btn { color:var(--muted); }.scrape-file-path .text-btn:hover { color:var(--primary); }.scrape-folder-actions .text-btn:hover { color:var(--primary); background:var(--primary-soft); border-radius:6px; }
.scrape-file-row { display:flex; align-items:center; gap:12px; height:56px; padding:8px 14px; border-bottom:1px solid color-mix(in srgb,var(--border) 45%,transparent); }
.scrape-file-row:hover { background:var(--primary-soft); }
.scrape-file-row .file-name { flex:1; min-width:0; }
.scrape-file-row strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.scrape-file-row small { color:var(--muted); }
.scrape-folder-actions { display:flex; gap:14px; opacity:0; }
.scrape-file-row:hover .scrape-folder-actions,.scrape-file-row:focus-within .scrape-folder-actions { opacity:1; }
@media(hover:none) { .scrape-folder-actions { opacity:1; } }
@media(max-width:600px) { .scrape-file-row small { display:none; }.scrape-folder-actions { gap:7px; } }
</style>
