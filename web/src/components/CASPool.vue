<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, useId, watch } from 'vue'
import { state } from '../lib'
import Icon from './Icon.vue'
import NumberInput from './NumberInput.vue'
import ProviderIcon from './ProviderIcon.vue'
import RoundedSelect from './RoundedSelect.vue'
import ScrollRail from './ScrollRail.vue'

const emit = defineEmits(['close', 'saved', 'busy', 'ready'])
const id = useId(), formElement = ref(null), lists = reactive({ mobile: null, tianyi: null })
const loading = ref(false), saving = ref(false), loaded = ref(false), error = ref(''), saved = ref(false)
const groups = [{ type: 'mobile', name: '移动云盘' }, { type: 'tianyi', name: '天翼云盘' }]
const form = reactive(defaults())
const accounts = computed(() => Object.fromEntries(groups.map(({ type }) => [type, state.storages.filter(s =>
  s.enabled && s.type === type && (s.config?.mode === 'native' || (type === 'tianyi' && !s.config?.mode))
)])))
const busy = computed(() => loading.value || saving.value)
const rotationHours = computed({ get: () => form.rotationMinutes === '' ? '' : form.rotationMinutes / 60, set: value => { form.rotationMinutes = value === '' ? '' : value * 60; changed() } })
let active = true, requestId = 0, controller
function defaults() { return { mobile: { storageIds: [], retentionHours: 12 }, tianyi: { storageIds: [], retentionHours: 12 }, preferred: 'mobile', rotationMinutes: 720 } }
function normalize(value) {
  const next = defaults()
  for (const { type } of groups) {
    next[type].storageIds = [...new Set((Array.isArray(value?.[type]?.storageIds) ? value[type].storageIds : []).filter(id => typeof id === 'string' && id))]
    next[type].retentionHours = value?.[type]?.retentionHours ?? 12
  }
  next.preferred = value?.preferred ?? 'mobile'
  next.rotationMinutes = value?.rotationMinutes ?? 720
  return next
}
function invalidate() { requestId++; controller?.abort() }
function changed() { saved.value = false; error.value = '' }
function unavailable(type) { return form[type].storageIds.filter(id => !accounts.value[type].some(s => s.id === id)) }
function accountName(id) { return state.storages.find(s => s.id === id)?.name || id }
function remove(type, id) { form[type].storageIds = form[type].storageIds.filter(value => value !== id); changed() }
function toggle(type, storageId) {
  if (busy.value || !loaded.value) return
  if(accounts.value[type][0]?.id === storageId) return
  if (form[type].storageIds.includes(storageId)) remove(type, storageId)
  else { form[type].storageIds.push(storageId); changed() }
}
function validInteger(value, max) { return Number.isInteger(value) && value >= 1 && value <= max }
function validationError() {
  if (!['mobile', 'tianyi'].includes(form.preferred)) return '请选择优先秒传'
  if (typeof form.rotationMinutes !== 'number' || !Number.isFinite(form.rotationMinutes) || form.rotationMinutes < 1 || form.rotationMinutes > 1440 || Math.abs(form.rotationMinutes - Math.round(form.rotationMinutes)) > 1e-8) return '轮换时间须为 1/60 至 24 h，精确到分钟'
  for (const { type, name } of groups) {
    if (!validInteger(form[type].retentionHours, 8760)) return `${name}保留时间须为 1 至 8760 h 的整数`
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
  try {
    const response = await fetch('/api/cas/pool', { method, credentials: 'same-origin', signal: abort.signal,
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })
    let result
    try { result = await response.json() } catch { throw new Error('服务响应异常，请检查后端连接') }
    if (!active || token !== requestId) return
    if (!response.ok) throw new Error(result.error || '请求失败')
    Object.assign(form, normalize(result))
    for (const { type } of groups) { const first=accounts.value[type][0]?.id; if(first)form[type].storageIds=[first,...form[type].storageIds.filter(id=>id!==first)] }
    loaded.value = true
    if (method === 'PUT') { saved.value = true; emit('saved', normalize(result)) }
  } catch (e) { if (active && token === requestId && e.name !== 'AbortError') error.value = e.message }
  finally { if (active && token === requestId) { loading.value = false; saving.value = false; controller = undefined } }
}
function save() {
  if (busy.value || !loaded.value) return
  if (!formElement.value?.reportValidity()) return
  error.value = validationError()
  if (!error.value) request('PUT', { ...normalize(form), rotationMinutes: Math.round(form.rotationMinutes) })
}
watch(busy, value => emit('busy', value), { immediate: true })
watch(loaded, value => emit('ready', value), { immediate: true })
watch(accounts, () => { for(const {type} of groups) { const first=accounts.value[type][0]?.id; if(first && !form[type].storageIds.includes(first))form[type].storageIds.unshift(first) } })
watch(() => state.authenticated, () => { loaded.value = false; invalidate(); emit('close') }, { flush: 'sync' })
onMounted(() => request('GET'))
onBeforeUnmount(() => { active = false; invalidate() })
defineExpose({ save })
</script>
<template>
  <section class="cas-pool" aria-label="CAS 存储绑定池" :aria-busy="busy">
    <form ref="formElement" @submit.prevent="save" @input="changed" @change="changed">
      <p v-if="loading" class="cas-pool-feedback" role="status"><Icon name="LoaderCircle" />加载中...</p>
      <fieldset class="cas-pool-fields" :disabled="busy || !loaded">
        <div class="cas-pool-groups">
          <section v-for="group in groups" :key="group.type" class="cas-pool-group" :aria-label="group.name">
            <header><ProviderIcon :type="group.type" small /><h3>{{ group.name }}</h3></header>
            <div class="cas-pool-list-frame">
              <div :ref="el => lists[group.type] = el" class="cas-pool-list" :class="{'two-columns':accounts[group.type].length>3}" :aria-label="`${group.name}存储列表`" tabindex="0">
                <article v-for="storage in accounts[group.type]" :key="storage.id" class="cas-pool-account" :class="{ 'binding-disabled': !form[group.type].storageIds.includes(storage.id) }">
                  <button type="button" class="cas-binding-toggle" :aria-label="`绑定 ${storage.name}`" :aria-pressed="form[group.type].storageIds.includes(storage.id)" :disabled="busy || !loaded || accounts[group.type][0]?.id===storage.id" @click="toggle(group.type, storage.id)"><ProviderIcon :type="storage.type" /></button>
                  <div class="cas-account-label"><strong>{{storage.name}}</strong><small>{{storage.usage?.username || storage.config?.username || storage.config?.phone || storage.config?.account || '—'}}</small></div>
                </article>
                <div v-for="slot in Math.max(0, (accounts[group.type].length>3?6:3) - accounts[group.type].length)" :key="`blank-${slot}`" class="cas-pool-account cas-pool-blank" aria-hidden="true" />
              </div>
              <ScrollRail :element="lists[group.type]" :thickness="3" />
            </div>
            <div v-for="storageId in unavailable(group.type)" :key="storageId" class="cas-pool-unavailable">
              <span>{{ accountName(storageId) }} · 不可用</span><button type="button" class="icon-btn" :aria-label="`移除不可用绑定 ${accountName(storageId)}`" @click="remove(group.type, storageId)"><Icon name="X" /></button>
            </div>
          </section>
        </div>
        <section class="cas-pool-advanced" aria-label="高级设置">
          <h3><Icon name="Settings2" :size="18" />高级设置</h3>
          <div class="cas-pool-options">
            <div class="field"><label>优先秒传</label><RoundedSelect v-model="form.preferred" label="优先秒传" :disabled="busy || !loaded" :options="[{ value: 'mobile', label: '移动云盘' }, { value: 'tianyi', label: '天翼云盘' }]" @update:model-value="changed" /></div>
            <div class="field"><label :for="`${id}-rotation`">轮换时间</label><NumberInput :id="`${id}-rotation`" v-model="rotationHours" aria-label="轮换时间" unit="h" :min="1 / 60" :max="24" :step="1 / 60" required :disabled="busy || !loaded" /></div>
            <div v-for="group in groups" :key="group.type" class="field"><label :for="`${id}-${group.type}-retention`">{{ group.name }}保留时间</label><NumberInput :id="`${id}-${group.type}-retention`" v-model="form[group.type].retentionHours" :aria-label="`${group.name}保留时间`" unit="h" :min="1" :max="8760" :step="1" required :disabled="busy || !loaded" /></div>
          </div>
        </section>
      </fieldset>
      <p v-if="error" class="cas-pool-feedback cas-pool-error" role="alert"><Icon name="CircleAlert" /><span>{{ error }}</span></p>
      <p v-if="saved" class="cas-pool-feedback cas-pool-saved" role="status"><Icon name="CircleCheck" />存储绑定池已保存</p>
      <button v-if="!loaded && !loading" type="button" class="btn" @click="request('GET')"><Icon name="RefreshCw" />重新加载</button>
    </form>
  </section>
</template>
<style scoped>
.cas-pool { min-width:0; color:var(--text); }
.cas-pool-fields { margin:0; padding:0; border:0; min-width:0; }
.cas-pool-groups { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }
.cas-pool-group { min-width:0; }
.cas-pool-group header { display:flex; align-items:center; gap:8px; padding:12px 0; }
.cas-pool-group h3,.cas-pool-advanced h3 { font-size:14px; font-weight:600; }
.cas-pool-list-frame { position:relative; border:1px solid var(--border); border-radius:8px; padding:8px; background:var(--surface); }
.cas-pool-list { height:204px; display:grid; grid-template-columns:minmax(0,1fr); grid-auto-rows:64px; gap:6px; overflow-y:auto; overscroll-behavior:contain; scrollbar-width:none; }.cas-pool-list.two-columns { grid-template-columns:repeat(2,minmax(0,1fr)); }
.cas-pool-list::-webkit-scrollbar { display:none; }
.cas-pool-account { height:64px; min-width:0; display:flex; align-items:center; gap:8px; padding:8px; border:1px solid var(--border); border-radius:8px; background:var(--input); transition:opacity .15s,border-color .15s; }.cas-account-label { display:grid;gap:3px;min-width:0; }.cas-account-label strong,.cas-account-label small { overflow:hidden;text-overflow:ellipsis;white-space:nowrap; }.cas-account-label strong {font-size:14px;}.cas-account-label small {font-size:12px;color:var(--muted);}
.cas-pool-account:last-child { margin-bottom:0; }
.cas-pool-account:hover:not(.cas-pool-blank) { border-color:color-mix(in srgb,var(--primary) 60%,var(--border)); }
.cas-pool-account span { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:14px; }
.cas-pool-account.binding-disabled { opacity:.48; }
.cas-pool-blank { background:transparent; border-style:dashed; opacity:.3; }
.cas-binding-toggle { width:44px; height:44px; flex:none; border:0; padding:0; background:transparent; cursor:pointer; border-radius:8px; }
.cas-binding-toggle:focus-visible { outline:2px solid var(--primary); outline-offset:2px; }
.cas-binding-toggle:disabled { cursor:default; }
.cas-pool :deep(.provider-icon) { width:40px; height:40px; background:transparent; border:0; border-radius:0; padding:0; box-shadow:none; }
.cas-pool header :deep(.provider-icon) { width:24px; height:24px; }
.cas-pool :deep(.provider-logo) { width:100%; height:100%; object-fit:contain; }
.cas-pool-unavailable { display:flex; justify-content:space-between; align-items:center; gap:8px; font-size:13px; color:var(--red); padding-top:6px; overflow-wrap:anywhere; }
.cas-pool-advanced { margin-top:20px; border-top:1px solid var(--border); padding-top:16px; }
.cas-pool-advanced h3 { display:flex; align-items:center; gap:8px; margin-bottom:16px; }
.cas-pool-options { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }
.cas-pool-options .field { min-width:0; }
.cas-pool-options :deep(.rounded-select-trigger) { height:40px; }
.cas-pool-feedback { display:flex; align-items:center; gap:8px; margin:12px 0; font-size:13px; overflow-wrap:anywhere; }
.cas-pool-error { color:var(--red); }.cas-pool-saved { color:var(--green); }
@media(max-width:600px) { .cas-pool-groups,.cas-pool-options { grid-template-columns:minmax(0,1fr); } }
@media(prefers-reduced-motion:reduce) { .cas-pool-account { transition:none; } }
</style>
