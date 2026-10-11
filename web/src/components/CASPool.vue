<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, useId, watch } from 'vue'
import { state } from '../lib'
import Icon from './Icon.vue'
import NumberInput from './NumberInput.vue'
import ProviderIcon from './ProviderIcon.vue'
import RoundedSelect from './RoundedSelect.vue'

const emit = defineEmits(['close', 'saved'])
const id = useId(), closeButton = ref(null)
const loading = ref(false), saving = ref(false), loaded = ref(false), error = ref(''), saved = ref(false)
const groups = [{ type: 'mobile', name: '移动云盘' }, { type: 'tianyi', name: '天翼云盘' }]
const form = reactive(defaults())
const accounts = computed(() => Object.fromEntries(groups.map(({ type }) => [type, state.storages.filter(s =>
  s.enabled && s.type === type && (s.config?.mode === 'native' || (type === 'tianyi' && !s.config?.mode))
)])))
const busy = computed(() => loading.value || saving.value)
let active = true, requestId = 0, controller

function defaults() {
  return { mobile: { storageIds: [], retentionHours: 12 }, tianyi: { storageIds: [], retentionHours: 12 }, preferred: 'mobile', rotationMinutes: 10 }
}
function normalize(value) {
  const next = defaults()
  for (const { type } of groups) {
    next[type].storageIds = [...new Set((Array.isArray(value?.[type]?.storageIds) ? value[type].storageIds : []).filter(id => typeof id === 'string' && id))]
    next[type].retentionHours = value?.[type]?.retentionHours ?? 12
  }
  next.preferred = value?.preferred ?? 'mobile'
  next.rotationMinutes = value?.rotationMinutes ?? 10
  return next
}
function invalidate() { requestId++; controller?.abort() }
function dismiss() { invalidate(); emit('close') }
function changed() { saved.value = false; error.value = '' }
function unavailable(type) {
  return form[type].storageIds.filter(id => !accounts.value[type].some(s => s.id === id))
}
function accountName(id) { return state.storages.find(s => s.id === id)?.name || id }
function remove(type, id) { form[type].storageIds = form[type].storageIds.filter(value => value !== id); changed() }
function validInteger(value, max) { return Number.isInteger(value) && value >= 1 && value <= max }
function validationError() {
  if (!['mobile', 'tianyi'].includes(form.preferred)) return '请选择优先存储'
  if (!validInteger(form.rotationMinutes, 1440)) return '轮换时间须为 1 至 1440 分钟的整数'
  for (const { type, name } of groups) {
    if (!validInteger(form[type].retentionHours, 8760)) return `${name}保留时间须为 1 至 8760 小时的整数`
    if (form[type].storageIds.length > 32) return `${name}最多绑定 32 个存储`
    if (unavailable(type).length) return `请移除${name}中不可用的绑定`
  }
  return ''
}
async function request(method, body) {
  invalidate()
  const token = requestId, abort = new AbortController()
  controller = abort
  loading.value = method === 'GET'; saving.value = method === 'PUT'; error.value = ''; saved.value = false
  // Each window owns its request; closing it cannot mutate a later window or session.
  try {
    const response = await fetch('/api/cas/pool', {
      method, credentials: 'same-origin', signal: abort.signal,
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body)
    })
    let result
    try { result = await response.json() } catch { throw new Error('服务响应异常，请检查后端连接') }
    if (!active || token !== requestId) return
    if (!response.ok) throw new Error(result.error || '请求失败')
    Object.assign(form, normalize(result)); loaded.value = true
    if (method === 'PUT') { saved.value = true; emit('saved', normalize(result)) }
  } catch (e) {
    if (active && token === requestId && e.name !== 'AbortError') error.value = e.message
  } finally {
    if (active && token === requestId) { loading.value = false; saving.value = false; controller = undefined }
  }
}
function save() {
  if (busy.value || !loaded.value) return
  error.value = validationError()
  if (!error.value) request('PUT', normalize(form))
}
watch(() => state.authenticated, () => { loaded.value = false; dismiss() }, { flush: 'sync' })
onMounted(() => { closeButton.value?.focus({ preventScroll: true }); request('GET') })
onBeforeUnmount(() => { active = false; invalidate() })
</script>

<template>
  <section class="cas-pool" aria-label="CAS 存储绑定池" @keydown.esc="!$event.defaultPrevented && dismiss()">
    <header class="cas-pool-header">
      <button ref="closeButton" type="button" class="icon-btn cas-pool-close" title="关闭存储绑定池" aria-label="关闭存储绑定池" @click="dismiss"><Icon name="ChevronRight" :size="28" /></button>
      <h3>存储绑定池</h3>
    </header>
    <form @submit.prevent="save" @input="changed" @change="changed" :aria-busy="busy">
      <p v-if="loading" class="cas-pool-feedback" role="status"><Icon name="LoaderCircle" />加载中...</p>
      <fieldset class="cas-pool-fields" :disabled="busy || !loaded">
        <div class="cas-pool-groups">
          <fieldset v-for="group in groups" :key="group.type" class="cas-pool-group">
            <legend><ProviderIcon :type="group.type" small /><span>{{ group.name }}</span><small>{{ form[group.type].storageIds.length }}</small></legend>
            <label v-for="storage in accounts[group.type]" :key="storage.id" class="cas-pool-account">
              <input v-model="form[group.type].storageIds" type="checkbox" :value="storage.id" :aria-label="`绑定 ${storage.name}`" />
              <ProviderIcon :type="storage.type" small /><span>{{ storage.name }}</span>
            </label>
            <p v-if="!accounts[group.type].length && loaded" class="cas-pool-empty">暂无已启用的原生账号</p>
            <div v-for="storageId in unavailable(group.type)" :key="storageId" class="cas-pool-unavailable">
              <span>{{ accountName(storageId) }} · 不可用</span><button type="button" class="icon-btn" :aria-label="`移除不可用绑定 ${accountName(storageId)}`" title="移除绑定" @click="remove(group.type, storageId)"><Icon name="X" /></button>
            </div>
          </fieldset>
        </div>
        <details class="cas-pool-advanced">
          <summary><Icon name="Settings2" :size="16" />高级设置<Icon name="ChevronDown" :size="16" /></summary>
          <div class="cas-pool-options">
            <div class="field"><label :id="`${id}-preferred`">优先存储</label><RoundedSelect v-model="form.preferred" label="优先存储" :disabled="busy || !loaded" :options="[{ value: 'mobile', label: '移动云盘（默认）' }, { value: 'tianyi', label: '天翼云盘' }]" @update:model-value="changed" /></div>
            <div class="field"><label :for="`${id}-rotation`">轮换时间</label><NumberInput :id="`${id}-rotation`" v-model="form.rotationMinutes" aria-label="轮换时间" unit="分钟" :min="1" :max="1440" :step="1" required :disabled="busy || !loaded" /></div>
            <div v-for="group in groups" :key="group.type" class="field"><label :for="`${id}-${group.type}-retention`">{{ group.name }}保留时间</label><NumberInput :id="`${id}-${group.type}-retention`" v-model="form[group.type].retentionHours" :aria-label="`${group.name}保留时间`" unit="小时" :min="1" :max="8760" :step="1" required :disabled="busy || !loaded" /></div>
          </div>
        </details>
      </fieldset>
      <p v-if="error" class="cas-pool-feedback cas-pool-error" role="alert"><Icon name="CircleAlert" /><span>{{ error }}</span></p>
      <p v-if="saved" class="cas-pool-feedback cas-pool-saved" role="status"><Icon name="CircleCheck" />存储绑定池已保存</p>
      <footer class="cas-pool-actions">
        <button v-if="!loaded && !loading" type="button" class="btn" @click="request('GET')"><Icon name="RefreshCw" />重新加载</button>
        <button type="button" class="btn" @click="dismiss">关闭</button>
        <button class="btn primary" :disabled="busy || !loaded"><Icon :name="saving ? 'LoaderCircle' : 'Save'" />{{ saving ? '保存中...' : '保存绑定池' }}</button>
      </footer>
    </form>
  </section>
</template>

<style scoped>
.cas-pool { min-width: 0; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); color: var(--text); }
.cas-pool-header { display: flex; align-items: center; gap: 8px; padding: 8px 12px; border-bottom: 1px solid var(--border); }
.cas-pool-header h3 { font-size: 15px; overflow-wrap: anywhere; }
.cas-pool-close { width: 44px; height: 44px; color: var(--primary); }
.cas-pool-close :deep(svg) { transform: scaleX(.7); }
.cas-pool form { padding: 16px; }
.cas-pool-fields, .cas-pool-group { margin: 0; padding: 0; border: 0; min-width: 0; }
.cas-pool-groups { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 20px; }
.cas-pool-group legend { display: flex; align-items: center; gap: 8px; width: 100%; margin-bottom: 8px; padding: 0; font-size: 14px; font-weight: 600; }
.cas-pool-group legend small { margin-left: auto; font-weight: 400; color: color-mix(in srgb,var(--text) 72%,var(--surface)); }
.cas-pool-account { flex-direction: row; align-items: center; gap: 8px; min-height: 44px; padding: 6px 4px; border-bottom: 1px solid var(--border); cursor: pointer; }
.cas-pool-account:hover { background: var(--bg); }
.cas-pool-account input { width: 16px; height: 16px; margin: 0; flex: none; accent-color: var(--primary); }
.cas-pool-account span, .cas-pool-unavailable span { min-width: 0; overflow-wrap: anywhere; }
.cas-pool :deep(.provider-icon) { width: 24px; height: 24px; flex: none; background: transparent; border: 0; border-radius: 0; padding: 0; box-shadow: none; }
.cas-pool :deep(.provider-logo) { width: 100%; height: 100%; object-fit: contain; }
.cas-pool-empty { padding: 12px 4px; font-size: 13px; color: color-mix(in srgb,var(--text) 72%,var(--surface)); }
.cas-pool-unavailable { display: flex; justify-content: space-between; align-items: center; gap: 8px; font-size: 13px; color: var(--red); }
.cas-pool-advanced { margin-top: 16px; border-top: 1px solid var(--border); }
.cas-pool-advanced summary { display: flex; align-items: center; gap: 8px; min-height: 44px; cursor: pointer; font-size: 13px; list-style: none; }
.cas-pool-advanced summary::-webkit-details-marker { display: none; }
.cas-pool-advanced summary > :last-child { margin-left: auto; transition: transform .15s; }
.cas-pool-advanced[open] summary > :last-child { transform: rotate(180deg); }
.cas-pool-advanced summary:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
.cas-pool-options { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 16px; padding: 4px 0 12px; }
.cas-pool-options .field { min-width: 0; }
.cas-pool-options :deep(.rounded-select-trigger) { height: 40px; }
.cas-pool-feedback { display: flex; align-items: center; gap: 8px; margin: 12px 0; font-size: 13px; overflow-wrap: anywhere; }
.cas-pool-error { color: var(--red); }.cas-pool-saved { color: var(--green); }
.cas-pool-actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; padding-top: 16px; border-top: 1px solid var(--border); margin-top: 12px; }
.cas-pool-actions .btn { min-height: 44px; }
@media(max-width:600px) { .cas-pool-groups, .cas-pool-options { grid-template-columns: minmax(0,1fr); }.cas-pool form { padding: 12px; } }
@media(prefers-reduced-motion:reduce) { .cas-pool-advanced summary > :last-child { transition: none; } }
</style>
