<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, notify } from '../lib'
import Modal from './Modal.vue'
import LocalDirectoryPicker from './LocalDirectoryPicker.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'

const emit = defineEmits(['close', 'started', 'progress'])
const form = reactive({ directory: '', find: '', replace: '', mode: 'literal' })
const preview = ref(null)
function parts(text, ranges) {
  const chars = Array.from(text), result = []; let offset = 0
  for (const [start, end] of ranges || []) { result.push({text:chars.slice(offset,start).join('')}); result.push({text:chars.slice(start,end).join(''),mark:true}); offset=end }
  result.push({text:chars.slice(offset).join('')}); return result
}
const picker = ref(false), confirmation = ref(null), busy = ref(false), loaded = ref(false), error = ref(''), pollError = ref(''), task = ref(null)
const running = computed(() => task.value?.status === 'running')
const labels = { running: '正在替换', completed: '已完成', failed: '执行失败' }
let timer, disposed = false, generation = 0

function accept(result) {
  task.value = result
  emit('progress', result)
}
async function poll() {
  const run = ++generation
  try {
    const result = await api('/strm-replace')
    if (disposed || run !== generation) return
    accept(result.tasks[0] || null)
    loaded.value = true
    pollError.value = ''
  } catch (e) {
    if (!disposed && run === generation) pollError.value = e.message
  } finally {
    if (!disposed && run === generation) timer = setTimeout(poll, 1500)
  }
}
async function review(showPreview = false) {
  if (!loaded.value || busy.value || running.value || !form.directory || !form.find) return
  busy.value = true; error.value = ''
  const input = { ...form }
  try {
    const result = await api('/strm-replace/preview', 'POST', input)
    if (disposed) return
    if (showPreview) preview.value = result
    else if (result.count) confirmation.value = { ...input, count: result.count }
    else error.value = '没有需要替换的 STRM 文件'
  } catch (e) { if (!disposed) error.value = e.message }
  finally { if (!disposed) busy.value = false }
}
async function execute() {
  if (!confirmation.value || busy.value) return
  const { count, ...input } = confirmation.value
  const payload = { ...input, confirmed: true }
  busy.value = true
  error.value = ''
  clearTimeout(timer)
  generation++
  try {
    const result = await api('/strm-replace', 'POST', payload)
    if (disposed) return
    accept(result)
    confirmation.value = null
    emit('started', result)
    notify('STRM 内容替换已开始')
  } catch (e) {
    if (!disposed) { error.value = e.message; confirmation.value = null }
  } finally {
    if (!disposed) { busy.value = false; timer = setTimeout(poll, 1500) }
  }
}
onMounted(poll)
onUnmounted(() => { disposed = true; generation++; clearTimeout(timer) })
</script>

<template>
  <Modal v-if="!preview" title="STRM 内容替换" compact wide @close="emit('close')">
    <form @submit.prevent="review(false)">
      <div class="modal-body strm-replace">
        <div class="replace-first-row"><label>替换目录
          <span class="strm-replace-directory">
            <input v-model="form.directory" required :disabled="busy || running" aria-label="替换目录" />
            <button type="button" class="icon-btn" aria-label="选择替换目录" :disabled="busy || running" @click="picker = true"><Icon name="FolderOpen" /></button>
          </span>
        </label><label>替换模式<RoundedSelect v-model="form.mode" class="replace-mode" hide-check label="替换模式" :disabled="busy || running" :options="[{value:'literal',label:'常规替换'},{value:'regex',label:'正则替换'}]" /></label></div>
        <label>匹配字段<input v-model="form.find" required maxlength="16384" :disabled="busy || running" /></label>
        <label>替换字段<input v-model="form.replace" maxlength="16384" :disabled="busy || running" /></label>
        <section v-if="task" class="strm-replace-status" role="status" aria-live="polite" aria-atomic="true" :aria-busy="running">
          <strong>{{ labels[task.status] || task.status }}</strong>
          <progress v-if="running" aria-label="STRM 替换进度" :value="task.total>0?task.processed||0:0" :max="task.total||1" :aria-valuetext="`已扫描 ${task.scanned||0}，已处理 ${task.processed||0}`" />
          <dl><div><dt>已扫描</dt><dd>{{ task.scanned }}</dd></div><div><dt>已处理</dt><dd>{{ task.processed }}</dd></div><div><dt>已修改</dt><dd>{{ task.changed }}</dd></div><div><dt>已跳过</dt><dd>{{ task.skipped }}</dd></div></dl>
          <p v-if="task.error" class="error-message">{{ task.error }}</p>
        </section>
        <p v-if="error || pollError" class="error-message" role="alert">{{ error || pollError }}</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn" @click="emit('close')">关闭</button><button type="button" class="btn preview-btn" :disabled="!loaded || busy || running || !form.directory || !form.find" @click="review(true)"><Icon name="Eye" />预览</button><button class="btn primary" :disabled="!loaded || busy || running || !form.directory || !form.find"><Icon name="Play" />执行替换</button></footer>
    </form>
  </Modal>
  <Modal v-if="preview" title="替换预览" standard @close="preview = null">
    <div class="modal-body replace-preview"><p>{{ preview.count }} 个 STRM 文件将被替换</p><section v-for="sample in preview.samples" :key="sample.name"><strong>{{ sample.name }}</strong><div class="replace-comparison"><div><h4>原 STRM 内容</h4><pre><template v-for="(part,i) in parts(sample.before,sample.matches)" :key="i"><mark v-if="part.mark" class="match">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></pre></div><div><h4>替换后 STRM 内容</h4><pre><template v-for="(part,i) in parts(sample.after,sample.replacements)" :key="i"><mark v-if="part.mark" class="replacement">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></pre></div></div></section></div>
    <footer class="modal-footer"><button class="btn" @click="preview = null"><Icon name="ArrowLeft" />返回</button></footer>
  </Modal>
  <LocalDirectoryPicker v-if="picker" :initial="form.directory" @close="picker = false" @select="form.directory = $event; picker = false" />
  <Modal v-if="confirmation" title="确认替换 STRM 内容" compact confirmation @close="!busy && (confirmation = null)">
    <div class="modal-body strm-replace-confirm">
      <p>将替换 {{ confirmation.count }} 个 STRM 文件，文件名不变。此操作无法自动撤销，失败前已完成的修改仍会保留。</p>
      <code>{{ confirmation.directory }}</code>
      <dl><dt>查找内容</dt><dd>{{ confirmation.find }}</dd><dt>替换为</dt><dd>{{ confirmation.replace || '（空内容）' }}</dd></dl>
    </div>
    <footer class="modal-footer"><button class="btn" :disabled="busy" @click="confirmation = null">取消</button><button class="btn primary" :disabled="busy" @click="execute"><Icon name="Check" />确认替换</button></footer>
  </Modal>
</template>

<style scoped>
.strm-replace { display: grid; gap: 16px; }
.preview-btn { background:color-mix(in srgb,#e9d78a 12%,var(--surface));border-color:color-mix(in srgb,#e9d78a 28%,var(--border)); }
.replace-mode :deep(.rounded-select-option[aria-selected=true]) { background:var(--primary-soft); }
.replace-mode :deep(.rounded-select-option.focused:not([aria-selected=true])) { background:color-mix(in srgb,var(--primary-soft) 45%,var(--surface)); }
.replace-first-row { display:grid;grid-template-columns:2fr 1fr;gap:14px;min-width:0; }
.replace-comparison { display:grid;grid-template-columns:1fr 1fr;gap:12px; }.replace-comparison > div { min-width:0; }
.replace-preview section { margin-top:18px; }.replace-preview strong { display:block;overflow-wrap:anywhere;font-size:13px; }
.replace-comparison h4 { font-size:13px;color:var(--muted); }.replace-comparison pre { white-space:pre-wrap;overflow-wrap:anywhere;background:var(--input);border:1px solid var(--border);padding:12px;border-radius:8px;max-height:180px;overflow:auto; }
.replace-comparison mark { color:inherit;border-radius:3px; }.match { background:#efab6840; }.replacement { background:#8981ed40; }
.strm-replace label { display: grid; gap: 8px; min-width: 0; }
.strm-replace-directory { position:relative;display:block; }
.strm-replace-directory input { padding-right:44px; }
.strm-replace-directory .icon-btn { position:absolute;right:4px;top:50%;transform:translateY(-50%);width:36px;height:36px; }
.strm-replace input, .strm-replace textarea { width: 100%; min-width: 0; box-sizing: border-box; }
.strm-replace textarea { resize: vertical; }
.strm-replace-status { border-top: 1px solid var(--border); padding-top: 16px; }
.strm-replace-status progress { display: block; width: 100%; height: 6px; margin-top: 12px; }
.strm-replace-status dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
.strm-replace-status dt { font-size: 12px; color: var(--muted); }
.strm-replace-status dd { margin: 6px 0 0; font-variant-numeric: tabular-nums; }
.strm-replace-confirm code, .strm-replace-confirm dd, .error-message { white-space: pre-wrap; overflow-wrap: anywhere; }
.strm-replace-confirm dd { margin: 6px 0 16px; max-height: 120px; overflow: auto; }
.strm-replace-confirm dt { color: var(--muted); }
@media (max-width: 420px) { .strm-replace-status dl { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
