<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { notify, state } from '../lib'
import Icon from './Icon.vue'
import NumberInput from './NumberInput.vue'
import RoundedSelect from './RoundedSelect.vue'
const props = defineProps({ mode: { type: String, default: 'transfer' } })
const emit = defineEmits(['saved'])
const form = reactive({}), loading = ref(false), saving = ref(false), loaded = ref(false), error = ref(''), keyword = ref('')
const backup = computed(() => props.mode === 'backup')
const endpoint = computed(() => `/api/${backup.value ? 'backup' : 'transfer'}/settings`)
const units = [{ value: 'KB/s', label: 'KB/s' }, { value: 'MB/s', label: 'MB/s' }]
const queueModes = [{ value: 'bounded', label: '自定义' }, { value: 'unlimited', label: '无限制' }]
const lowerModes = [{ value: 'auto', label: '默认一半' }, { value: 'custom', label: '自定义' }]
const queueMode = ref('bounded'), lowerMode = ref('auto')
let controller, generation = 0
function defaults() {
  return backup.value ? { version: 1, pauseBidirectional: false, queueUpper: 10000, queueLower: 0, maxConcurrentScan: 1 } : { version: 1, downloadSpeed: 0, downloadUnit: 'KB/s', uploadSpeed: 0, uploadUnit: 'KB/s', maxPreprocess: 5, maxUploads: 5, uploadDelay: 10, crossCloudTemp: true, ignoredExtensions: ['clfstmp', 'parts', 'tmp', 'downloading', 'aria2', 'temp', 'temp_video', '~#0'] }
}
async function request(method = 'GET') {
  controller?.abort(); controller = new AbortController()
  const token = ++generation, signal = controller.signal, url = endpoint.value
  error.value = ''; loading.value = method === 'GET'; saving.value = method === 'PUT'
  const payload = { ...form }
  if (backup.value) { if (queueMode.value === 'unlimited') payload.queueUpper = payload.queueLower = 0; else if (lowerMode.value === 'auto') payload.queueLower = 0 }
  try {
    const response = await fetch(url, { method, credentials: 'same-origin', signal, headers: method === 'PUT' ? { 'Content-Type': 'application/json' } : {}, body: method === 'PUT' ? JSON.stringify(payload) : undefined })
    const data = await response.json()
    if (token !== generation) return
    if (!response.ok) { if (response.status === 401) state.authenticated = false; throw new Error(data.error || '读取设置失败') }
    Object.assign(form, defaults(), data); loaded.value = true
    if (backup.value) { queueMode.value = form.queueUpper === 0 ? 'unlimited' : 'bounded'; lowerMode.value = form.queueLower === 0 ? 'auto' : 'custom' }
    if (method === 'PUT') { notify('设置已保存'); emit('saved', data) }
  } catch (e) { if (token === generation && e.name !== 'AbortError') error.value = e.message }
  finally { if (token === generation) loading.value = saving.value = false }
}
function addKeyword() {
  const value = keyword.value.trim().replace(/^\./, '').toLowerCase()
  if (value && !form.ignoredExtensions.includes(value)) form.ignoredExtensions.push(value)
  keyword.value = ''
}
watch(() => props.mode, () => { loaded.value = false; Object.keys(form).forEach(key => delete form[key]); Object.assign(form, defaults()); request() }, { immediate: true })
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>
<template>
  <form class="transfer-settings-form settings-form" :aria-busy="loading || saving" @submit.prevent="request('PUT')">
    <fieldset :disabled="loading || saving || !loaded">
      <template v-if="backup">
        <section class="settings-section">
          <div class="settings-section-title"><Icon name="SlidersHorizontal" /><h2>备份资源限制</h2></div>
          <label class="settings-row"><strong>暂停双向同步</strong><input v-model="form.pauseBidirectional" type="checkbox" role="switch" class="switch" /></label>
          <div class="settings-row"><strong>队列上限</strong><div class="resource-inputs"><RoundedSelect v-model="queueMode" label="队列上限模式" :options="queueModes" /><NumberInput v-if="queueMode === 'bounded'" v-model="form.queueUpper" aria-label="队列上限" :min="1" :max="100000" required /></div></div>
          <div class="settings-row"><strong>队列下限</strong><div class="resource-inputs"><RoundedSelect v-model="lowerMode" label="队列下限模式" :options="lowerModes" :disabled="queueMode === 'unlimited'" /><NumberInput v-if="lowerMode === 'custom' && queueMode !== 'unlimited'" v-model="form.queueLower" aria-label="队列下限" :min="1" :max="Math.max(1, Number(form.queueUpper) - 1)" required /></div></div>
          <div class="settings-row"><strong>最大并发扫描数</strong><NumberInput v-model="form.maxConcurrentScan" aria-label="最大并发扫描数" :min="1" :max="16" required /></div>
        </section>
      </template>
      <template v-else>
        <div class="transfer-settings-columns">
          <section class="settings-section"><div class="settings-section-title"><Icon name="Gauge" /><h2>速度限制</h2></div>
            <div class="settings-row"><strong>最大下载速度</strong><div class="resource-inputs"><NumberInput v-model="form.downloadSpeed" aria-label="最大下载速度" :min="0" :max="1000000" required /><RoundedSelect v-model="form.downloadUnit" label="下载速度单位" :options="units" /></div></div>
            <div class="settings-row"><strong>最大上传速度</strong><div class="resource-inputs"><NumberInput v-model="form.uploadSpeed" aria-label="最大上传速度" :min="0" :max="1000000" required /><RoundedSelect v-model="form.uploadUnit" label="上传速度单位" :options="units" /></div></div>
          </section>
          <section class="settings-section"><div class="settings-section-title"><Icon name="ListChecks" /><h2>任务限制</h2></div>
            <div class="settings-row"><strong>最大预处理任务数</strong><NumberInput v-model="form.maxPreprocess" aria-label="最大预处理任务数" :min="1" :max="64" required /></div>
            <div class="settings-row"><strong>最大上传任务数</strong><NumberInput v-model="form.maxUploads" aria-label="最大上传任务数" :min="1" :max="64" required /></div>
            <div class="settings-row"><strong>上传延迟</strong><NumberInput v-model="form.uploadDelay" aria-label="上传延迟" unit="s" :min="0" :max="86400" required /></div>
            <label class="settings-row"><strong>跨云复制使用本地临时文件</strong><input v-model="form.crossCloudTemp" type="checkbox" role="switch" class="switch" /></label>
          </section>
        </div>
        <section class="settings-section"><div class="settings-section-title"><Icon name="FileX2" /><h2>忽略的文件扩展名</h2></div>
          <div class="extension-chips"><button v-for="value in form.ignoredExtensions" :key="value" type="button" class="extension-chip" :aria-label="`移除 ${value}`" @click="form.ignoredExtensions = form.ignoredExtensions.filter(item => item !== value)"><span>{{ value }}</span><Icon name="X" :size="14" /></button></div>
          <input v-model="keyword" class="extension-entry" aria-label="添加忽略扩展名" placeholder="添加扩展名" @keydown.enter.prevent="addKeyword" />
        </section>
      </template>
    </fieldset>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <div class="settings-actions"><button v-if="!loaded && !loading" class="btn" type="button" @click="request()"><Icon name="RefreshCw" />重试</button><button class="btn primary" :disabled="loading || saving || !loaded"><Icon name="Save" />{{ saving ? '保存中' : '保存设置' }}</button></div>
  </form>
</template>
<style scoped>
fieldset { border: 0; padding: 0; margin: 0; min-width: 0; display: grid; gap: 16px; }
.transfer-settings-columns { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.settings-section { border: 1px solid var(--border); border-radius: 8px; background: var(--surface); padding: 18px; }
.settings-section-title h2 { font-size: 16px; margin: 0; }
.settings-section-title { gap: 10px; margin-bottom: 12px; color: var(--text); }
.settings-row { gap: 12px; min-height: 58px; }
.settings-row strong { font-size: 14px; }
.resource-inputs { display: flex; gap: 8px; justify-content: flex-end; min-width: 0; }
:deep(.number-control) { width: 132px; max-width: 100%; }
:deep(.number-control input), :deep(.number-unit) { font-size: 14px; }
.resource-inputs :deep(.rounded-select) { width: 110px; flex-shrink: 0; }
.extension-entry { width: min(100%, 320px); background: var(--input); }
.extension-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
.extension-chip { position: relative; border:1px solid var(--border); border-radius:6px; background:var(--bg); color:var(--text); padding:5px 9px; font-size:13px; cursor:pointer; }
.extension-chip svg { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); opacity: 0; }
.extension-chip:hover span, .extension-chip:focus-visible span { opacity: .15; }
.extension-chip:hover svg, .extension-chip:focus-visible svg { opacity: 1; }
@media (max-width: 1100px) { .transfer-settings-columns { grid-template-columns: minmax(0,1fr); } }
@media (max-width: 520px) { .settings-section { padding: 14px; }.settings-row { flex-wrap: wrap; }.resource-inputs { margin-left: auto; } :deep(.number-control) { width: 112px; } }
</style>
