<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, notify } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import ProviderIcon from './ProviderIcon.vue'

const emit = defineEmits(['close', 'changed'])
const bindings = ref([])
const broker = ref('')
const storage = ref('')
const adding = ref(false)
const editing = ref('')
const busy = ref(false)
const consent = ref(false)
const image = ref('')
const error = ref('')
const form = reactive({})
const original = ref('')
const removing = ref(null)
const selectedBinding = computed(() => bindings.value.find(b => b.id === editing.value))
const changed = computed(() => JSON.stringify(form) !== original.value)
const clientKeywords = computed({ get: () => (form.ua || '').split(/[\r\n;；]+/).filter(Boolean).join(';'), set: value => { form.ua = value.split(/[\r\n;；]+/).map(v => v.trim()).filter(Boolean).join('\n') } })
let timer
let generation = 0
const remaining = ref(0)
let countdown

const pools = computed(() => state.storages.filter(s => s.type === 'quark' && s.enabled && !bindings.value.some(b => b.id === s.id)))

function stop() {
  clearTimeout(timer)
  clearInterval(countdown)
  remaining.value = 0
  busy.value = false
  image.value = ''
  generation++
}

async function load() {
  try {
    const data = await api('/quark-takeover')
    bindings.value = data.bindings || []
    broker.value = data.broker || ''
    emit('changed', data)
    if (!bindings.value.some(b => b.id === editing.value)) {
      editing.value = ''
      if (bindings.value.length) await edit(bindings.value[0])
    }
  } catch (e) {
    error.value = e.message
  }
}

function add() {
  stop()
  error.value = ''
  storage.value = ''
  consent.value = false
  adding.value = true
}

async function edit(binding) {
  busy.value = true
  error.value = ''
  try {
    Object.assign(form, await api(`/quark-takeover/${binding.id}`).then(v => v.config))
    original.value = JSON.stringify(form)
    editing.value = binding.id
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function remove(binding) {
  busy.value = true
  try {
    await api(`/quark-takeover/${binding.id}`, 'DELETE')
    removing.value = null
    await load()
    notify('已解除绑定')
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function save() {
  busy.value = true
  try {
    await api(`/quark-takeover/${editing.value}`, 'PUT', form)
    original.value = JSON.stringify(form)
    await load()
    notify('接管设置已保存')
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function toggleBinding(binding) {
  busy.value = true
  try {
    const { config } = await api(`/quark-takeover/${binding.id}`)
    await api(`/quark-takeover/${binding.id}`, 'PUT', { ...config, enabled: !config.enabled })
    await load()
  } catch (e) { error.value = e.message } finally { busy.value = false }
}

async function qr() {
  stop()
  busy.value = true
  error.value = ''
  const run = generation
  const id = storage.value
  try {
    const data = await api(`/quark-takeover/${id}/qr`, 'POST', { broker: broker.value, consent: consent.value })
    if (run !== generation) return
    image.value = data.image
    const deadline = Date.now() + 300000
    remaining.value = 300
    countdown = setInterval(() => { remaining.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000)) }, 1000)
    async function poll() {
      if (run !== generation) return
      if (Date.now() >= deadline) {
        error.value = '二维码已过期，请重新获取'
        stop()
        return
      }
      try {
        const result = await api(`/quark-takeover/${id}/poll`, 'POST', { session: data.session })
        if (run !== generation) return
        if (result.authorized) {
          adding.value = false
          stop()
          await load()
          notify('夸克存储已绑定')
          return
        }
        timer = setTimeout(poll, 2500)
      } catch (e) {
        if (run !== generation) return
        error.value = e.message
        stop()
      }
    }
    timer = setTimeout(poll, 2500)
  } catch (e) {
    if (run === generation) error.value = e.message
  } finally {
    if (run === generation) busy.value = false
  }
}

watch([storage, consent], () => { stop(); if (storage.value && consent.value) qr() })
onMounted(load)
onUnmounted(stop)
</script>

<template>
  <Modal v-if="!adding" title="夸克 STRM 接管" compact wide @close="$emit('close')">
    <div class="takeover-workspace"><aside class="binding-workspace"><h3>账号绑定</h3><div class="binding-list">
      <div v-for="binding in bindings" :key="binding.id" class="binding-row" :class="{active: editing === binding.id}">
        <button class="binding-toggle" :aria-label="`${binding.enabled ? '停用' : '启用'}绑定 ${binding.name}`" :aria-pressed="binding.enabled" :disabled="busy || !binding.valid" @click="toggleBinding(binding)"><ProviderIcon type="quark" /></button>
        <button class="binding-select" :aria-label="`设置绑定 ${binding.name}`" :aria-pressed="editing === binding.id" :disabled="busy" @click="edit(binding)"><strong>{{ binding.name }}</strong><small>TV · {{ binding.nickname || '已绑定' }} · {{ binding.valid ? '已授权' : '凭据已变更' }}</small></button>
        <span class="binding-status" :class="{on:binding.enabled && binding.valid}" />
      </div>
      <p v-if="!bindings.length" class="muted">暂无绑定账号</p>
      </div><button class="btn binding-add" :disabled="!pools.length || busy" @click="add"><Icon name="Plus" />添加绑定</button>
    </aside><form class="takeover-settings" @submit.prevent="save"><header class="takeover-account"><div><h3>{{ selectedBinding?.name || '播放设置' }}</h3><p v-if="selectedBinding">TV 账号：{{ selectedBinding.nickname || '已绑定' }} · {{ selectedBinding.valid ? '已授权' : '凭据已变更' }}</p></div><button v-if="selectedBinding" type="button" class="binding-remove" :aria-label="`解除绑定 ${selectedBinding.name}`" :disabled="busy" @click="removing = selectedBinding">解绑</button></header><p v-if="!editing" class="muted takeover-empty">选择或添加绑定账号</p><fieldset v-else :disabled="busy">
      <div class="field"><label>清晰度偏好</label><RoundedSelect v-model="form.quality" label="最高画质" :options="['low','normal','high','super','2k','4k','dolby_vision'].map((v,i) => ({ value:v, label:['流畅','标清','高清','超清','2K','4K','杜比视界'][i] }))" /></div>
      <label class="takeover-dolby"><span><strong>杜比视界</strong><small>SVIP 限额</small></span><input v-model="form.allowDolby" aria-label="允许杜比视界" type="checkbox" role="switch" class="switch" /></label>
      <div class="field"><label>接管模式</label><RoundedSelect v-model="form.mode" label="接管模式" :options="[{ value: 'adaptive', label: '智能变轨' }, { value: 'direct', label: '强制直连' }, { value: 'split', label: '策略分流' }]" /></div>
      <div v-if="form.mode === 'split'" class="field"><label for="takeover-clients">客户端分流</label><div class="takeover-clients"><div role="group" aria-label="UA 规则"><button v-for="option in [{value:'direct_list',label:'直连名单'},{value:'proxy_list',label:'代理名单'}]" :key="option.value" type="button" :class="{active:form.uaListMode === option.value}" :aria-pressed="form.uaListMode === option.value" @click="form.uaListMode = option.value">{{ option.label }}</button></div><input id="takeover-clients" v-model="clientKeywords" aria-label="UA 关键词" /></div></div>
    </fieldset><footer class="takeover-footer"><button class="btn primary" :disabled="busy || !editing || !changed">保存设置</button></footer><p v-if="error" class="error-message" role="alert">{{ error }}</p></form></div>
  </Modal>

  <Modal v-if="removing" title="解绑夸克 TV" compact confirmation @close="!busy && (removing = null)"><div class="modal-body">确认解除「{{ removing.name }}」的 TV 绑定？</div><footer class="modal-footer"><button class="btn danger" :disabled="busy" @click="remove(removing)">确认解绑</button><button class="btn" :disabled="busy" @click="removing = null">取消</button></footer></Modal>

  <Modal v-if="adding" title="选择绑定的存储" compact @close="stop(); adding = false">
    <div class="modal-body">
      <RoundedSelect v-model="storage" label="绑定夸克存储" placeholder="选择夸克存储池" :options="pools.map(s => ({ value: s.id, label: s.name }))" />
      <label class="consent"><input v-model="consent" type="checkbox" />同意通过第三方 extscreen 服务换取 TV 凭据（授权码与刷新凭据）。</label>
      <div class="qr-area"><img v-if="image" :src="image" alt="夸克 TV 授权二维码" /><div v-else class="qr-placeholder"><Icon :name="busy ? 'LoaderCircle' : error ? 'CircleAlert' : 'ScanLine'" :class="{ spin: busy }" :size="28" /><p class="muted">{{ busy ? '正在获取二维码…' : error ? '二维码暂不可用' : '选择存储并确认授权后扫码绑定' }}</p></div></div>
      <p class="qr-hint">请使用与所选存储相同的夸克账号扫码，并在手机端确认登录。</p>
      <p v-if="image" class="qr-countdown" role="status">等待扫码 · {{ Math.floor(remaining / 60) }}:{{ String(remaining % 60).padStart(2, '0') }}</p>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    </div>
    <footer class="modal-footer"><button class="btn primary" :disabled="busy || !storage || !consent" @click="qr">获取二维码</button></footer>
  </Modal>

</template>

<style scoped>
.takeover-workspace { display:grid; grid-template-columns:230px minmax(0,1fr); min-height:440px; font-size:13px; }
.binding-workspace { display:flex; flex-direction:column; gap:10px; min-width:0; padding:16px 12px; background:var(--input); border-right:1px solid var(--border); }
.binding-list { flex:1; min-height:0; max-height:360px; overflow-y:auto; scrollbar-width:thin; }
.takeover-workspace h3 { font-size:14px; margin:0; }.binding-workspace > h3 { font-size:12px; color:var(--muted); padding:0 8px; }
.takeover-settings { display:flex; flex-direction:column; gap:20px; padding:22px; min-width:0; }
.takeover-account { display:flex; align-items:flex-start; gap:12px; }.takeover-account > div { flex:1; min-width:0; overflow-wrap:anywhere; }.takeover-account p { color:var(--muted); font-size:12px; margin:6px 0 0; }
.binding-remove { border:0; background:transparent; color:var(--danger); padding:0; font-size:13px; }
.takeover-settings fieldset { display:grid; gap:18px; border:0; margin:0; padding:0; min-width:0; }.takeover-settings label { font-size:13px; }
.takeover-footer { margin-top:auto; display:flex; justify-content:flex-end; }.takeover-empty { flex:1; display:grid; place-items:center; }
.binding-select { min-width:0; flex:1; text-align:left; border:0; padding:0; background:transparent; color:var(--text); font-size:13px; }.binding-select strong { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.binding-row { display:flex; gap:8px; align-items:center; padding:10px 8px; border-radius:7px; transition:background .18s; }.binding-row:hover { background:color-mix(in srgb,var(--primary) 5%,transparent); }.binding-row.active { background:color-mix(in srgb,var(--primary) 11%,transparent); }
.binding-status { width:6px; height:6px; flex-shrink:0; background:var(--muted); border-radius:50%; }.binding-status.on { background:#43aa86; }
.takeover-dolby { display:flex; flex-direction:row; align-items:center; justify-content:space-between; gap:12px; margin:0; }.takeover-dolby small { color:var(--muted); font-size:11px; margin-left:8px; }
.takeover-clients { border:1px solid var(--border); border-radius:8px; overflow:hidden; background:var(--input); }.takeover-clients > div { display:flex; gap:4px; padding:4px; border-bottom:1px solid var(--border); }.takeover-clients button { flex:1; border:0; border-radius:5px; background:transparent; color:var(--muted); font-size:12px; padding:6px; }.takeover-clients button.active { color:var(--primary); background:var(--surface); }.takeover-clients input { border:0; background:transparent; border-radius:0; }
@media(max-width:600px) { .takeover-workspace { grid-template-columns:1fr; min-height:0; }.binding-workspace { border-right:0; border-bottom:1px solid var(--border); }.binding-list { max-height:140px; }.takeover-settings { padding:18px; } }
.binding-toggle { width: 36px; height: 36px; flex-shrink: 0; border: 0; background: transparent; padding: 0; }
.binding-toggle[aria-pressed=false] { opacity: .45; }
.binding-toggle :deep(.provider-icon) { width:36px; height:36px; padding:0; background:transparent; border:0; }
.binding-row > div { flex: 1; min-width: 0; }
.binding-row small { display: block; color: var(--muted); margin-top: 5px; overflow-wrap: anywhere; }
.binding-add { width:100%; margin-top:auto; border-style:dashed; background:transparent; }
.consent { display: flex; flex-direction: row; align-items: flex-start; gap: 8px; margin-top: 18px; font-size: 13px; }
.consent input { width: 16px; height: 16px; flex-shrink: 0; }
.qr-area { min-height: 230px; display: grid; place-items: center; }
.qr-area img { width: 210px; height: 210px; }
.qr-area img { background: white; padding: 8px; border-radius: 8px; }
.qr-placeholder { display: grid; justify-items: center; gap: 12px; color: var(--muted); text-align: center; }
.qr-hint, .qr-countdown { text-align: center; font-size: 13px; color: var(--muted); line-height: 1.6; }
.qr-countdown { margin-top: 8px; color: var(--primary); }
:deep(.rounded-select-trigger), :deep(.rounded-select-popup) { background: var(--input); color: var(--text); border-color: var(--border); }
</style>
