<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, notify } from '../lib'
import Modal from './Modal.vue'
import LocalDirectoryPicker from './LocalDirectoryPicker.vue'
import Icon from './Icon.vue'

const emit = defineEmits(['close', 'started', 'progress'])
const form = reactive({ directory: '', find: '', replace: '' })
const picker = ref(false), confirmation = ref(null), busy = ref(false), loaded = ref(false), error = ref(''), task = ref(null)
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
    error.value = ''
  } catch (e) {
    if (!disposed && run === generation) error.value = e.message
  } finally {
    if (!disposed && run === generation) timer = setTimeout(poll, 1500)
  }
}
function review() {
  if (!loaded.value || busy.value || running.value || !form.directory || !form.find) return
  confirmation.value = { ...form }
}
async function execute() {
  if (!confirmation.value || busy.value) return
  const payload = { ...confirmation.value, confirmed: true }
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
  <Modal title="STRM 内容替换" compact wide @close="emit('close')">
    <form @submit.prevent="review">
      <div class="modal-body strm-replace">
        <label>容器目录
          <span class="strm-replace-directory">
            <input v-model="form.directory" required :disabled="busy || running" aria-label="容器目录" />
            <button type="button" class="icon-btn" title="选择容器目录" aria-label="选择容器目录" :disabled="busy || running" @click="picker = true"><Icon name="Folder" /></button>
          </span>
        </label>
        <label>匹配字段<input v-model="form.find" required maxlength="16384" :disabled="busy || running" /></label>
        <label>替换字段<input v-model="form.replace" maxlength="16384" :disabled="busy || running" /></label>
        <section v-if="task" class="strm-replace-status" role="status" aria-live="polite" aria-atomic="true" :aria-busy="running">
          <strong>{{ labels[task.status] || task.status }}</strong>
          <progress v-if="running" aria-label="STRM 替换进度" />
          <dl><div><dt>已扫描</dt><dd>{{ task.scanned }}</dd></div><div><dt>已处理</dt><dd>{{ task.processed }}</dd></div><div><dt>已修改</dt><dd>{{ task.changed }}</dd></div><div><dt>已跳过</dt><dd>{{ task.skipped }}</dd></div></dl>
          <p v-if="task.error" class="error-message">{{ task.error }}</p>
        </section>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn" @click="emit('close')">关闭</button><button class="btn primary" :disabled="!loaded || busy || running || !form.directory || !form.find"><Icon name="Play" />执行替换</button></footer>
    </form>
  </Modal>
  <LocalDirectoryPicker v-if="picker" :initial="form.directory" @close="picker = false" @select="form.directory = $event; picker = false" />
  <Modal v-if="confirmation" title="确认替换 STRM 内容" compact confirmation @close="!busy && (confirmation = null)">
    <div class="modal-body strm-replace-confirm">
      <p>将递归修改以下目录中的 .strm 文件内容，文件名不变。此操作无法自动撤销，失败前已完成的修改仍会保留。</p>
      <code>{{ confirmation.directory }}</code>
      <dl><dt>查找内容</dt><dd>{{ confirmation.find }}</dd><dt>替换为</dt><dd>{{ confirmation.replace || '（空内容）' }}</dd></dl>
    </div>
    <footer class="modal-footer"><button class="btn" :disabled="busy" @click="confirmation = null">取消</button><button class="btn primary" :disabled="busy" @click="execute"><Icon name="Check" />确认替换</button></footer>
  </Modal>
</template>

<style scoped>
.strm-replace { display: grid; gap: 16px; }
.strm-replace label { display: grid; gap: 8px; min-width: 0; }
.strm-replace-directory { display: grid; grid-template-columns: minmax(0, 1fr) 44px; gap: 8px; }
.strm-replace-directory .icon-btn { width: 44px; height: 44px; }
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
