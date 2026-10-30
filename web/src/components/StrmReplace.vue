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
  <Modal v-if="preview" title="替换预览" compact @close="preview = null">
    <div class="modal-body replace-preview">
      <div class="replace-preview-summary"><p>{{ preview.count }} 个 STRM 文件将被替换</p><span v-if="preview.count > preview.samples.length">预览 {{ preview.samples.length }} 项</span></div>
      <div class="replace-preview-table" role="table" aria-label="替换预览条目" :aria-rowcount="preview.samples.length + 1">
        <div class="replace-preview-columns" role="row"><span role="columnheader">文件名称</span><span role="columnheader">原 STRM 内容</span><span role="columnheader">替换后 STRM 内容</span></div>
        <div class="replace-preview-list" role="rowgroup" tabindex="0" aria-label="替换预览列表">
          <div v-for="sample in preview.samples" :key="sample.name" class="replace-preview-row" role="row">
            <span role="cell" tabindex="0" class="replace-preview-text replace-preview-name" :data-tooltip="sample.name" data-tooltip-stationary data-tooltip-delay="300">{{ sample.name.split(/[/\\]/).at(-1) }}</span>
            <span role="cell" tabindex="0" class="replace-preview-text replace-preview-before" :data-tooltip="sample.before" data-tooltip-stationary data-tooltip-delay="300"><template v-for="(part,i) in parts(sample.before,sample.matches)" :key="i"><mark v-if="part.mark" class="match">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></span>
            <span role="cell" tabindex="0" class="replace-preview-text replace-preview-after" :data-tooltip="sample.after" data-tooltip-stationary data-tooltip-delay="300"><template v-for="(part,i) in parts(sample.after,sample.replacements)" :key="i"><mark v-if="part.mark" class="replacement">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></span>
          </div>
        </div>
      </div>
    </div>
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
:global(.modal:has(> .replace-preview)){width:min(960px,calc(100vw - 32px));height:min(660px,calc(var(--aether-visible-height,100dvh) - 32px));display:flex;flex-direction:column;overflow:hidden}
.replace-preview{display:flex;flex-direction:column;gap:16px;flex:1;min-height:0;overflow:hidden}
.replace-preview-summary{display:flex;align-items:center;justify-content:space-between;gap:12px;flex:none}
.replace-preview-summary>span{color:var(--muted);font-size:12px;white-space:nowrap}
.replace-preview-table{display:flex;flex-direction:column;flex:1;min-height:0;border:1px solid var(--border);border-radius:6px;overflow:hidden;background:var(--input)}
.replace-preview-columns,.replace-preview-row{display:grid;grid-template-columns:minmax(0,1.05fr) minmax(0,1.4fr) minmax(0,1.4fr);gap:12px;align-items:center;padding:0 12px;flex:none}
.replace-preview-columns{min-height:42px;background:var(--layer-frame);color:var(--muted);font-size:12px}
.replace-preview-list{flex:1;min-height:0;overflow-y:auto;overflow-x:hidden;scrollbar-width:none;overscroll-behavior:contain}
.replace-preview-list::-webkit-scrollbar{display:none}
.replace-preview-row{height:44px;border-top:1px solid var(--border);font-size:13px}
.replace-preview-row:hover{background:var(--primary-soft)}
.replace-preview-text{display:block;min-width:0;max-width:100%;line-height:24px;overflow:hidden;white-space:nowrap;text-overflow:ellipsis}
.replace-preview mark{color:inherit;border-radius:3px}.match{background:#efab6840}.replacement{background:#8981ed40}
@media(max-width:480px){.replace-preview-columns,.replace-preview-row{gap:8px;padding:0 8px}.replace-preview-summary{align-items:flex-start;gap:8px}.replace-preview-summary>p{min-width:0}}
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
