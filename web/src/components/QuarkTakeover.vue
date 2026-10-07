<script setup>
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, notify } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import SecretInput from './SecretInput.vue'
defineEmits(['close'])
const pools = computed(() => state.storages.filter(s => s.type === 'quark' && s.enabled))
const storage = ref(''), busy = ref(false), loaded = ref(false), authorized = ref(false), consent = ref(false), image = ref(''), error = ref('')
const form = reactive({})
let timer, generation = 0
function stop() { clearTimeout(timer); image.value = ''; generation++ }
watch(storage, async id => {
  stop(); loaded.value = false; error.value = ''
  if (!id) return
  const run = generation
  try {
    const data = await api(`/quark-takeover/${id}`)
    if (run !== generation) return
    Object.assign(form, data.config); authorized.value = data.authorized; loaded.value = true
  } catch (e) { error.value = e.message }
})
async function save() {
  busy.value = true; error.value = ''
  try {
    if (form.broker && !consent.value) throw new Error('请确认信任此凭据换取服务')
    await api(`/quark-takeover/${storage.value}`, 'PUT', form)
    authorized.value ||= !!form.accessToken
    form.accessToken = ''; form.refreshToken = ''
    notify('夸克 STRM 接管已保存')
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function qr() {
  stop(); error.value = ''; busy.value = true
  const run = generation, id = storage.value
  try {
    const data = await api(`/quark-takeover/${id}/qr`, 'POST', { broker: form.broker, consent: consent.value })
    if (run !== generation) return
    image.value = data.image
    const deadline = Date.now() + 300000
    async function poll() {
      if (run !== generation) return
      if (Date.now() >= deadline) { error.value = '二维码已过期，请重新获取'; stop(); return }
      try {
        const result = await api(`/quark-takeover/${id}/poll`, 'POST', { session: data.session })
        if (run !== generation) return
        if (result.authorized) { authorized.value = true; stop(); notify('TV 凭据已绑定'); return }
        timer = setTimeout(poll, 2500)
      } catch (e) { if (run === generation) { error.value = e.message; stop() } }
    }
    timer = setTimeout(poll, 2500)
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
onUnmounted(stop)
</script>
<template>
  <Modal title="夸克 STRM 接管" compact wide @close="$emit('close')">
    <form @submit.prevent="save">
      <div class="modal-body">
        <div class="field"><label>绑定存储</label><RoundedSelect v-model="storage" label="绑定夸克存储" placeholder="选择夸克存储池" :options="pools.map(s => ({ value: s.id, label: s.name }))" /></div>
        <p v-if="!pools.length" class="muted">暂无已启用的夸克存储池</p>
        <div v-if="loaded" class="form-grid">
          <label class="toggle-line full"><span>启用接管</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" /></label>
          <div class="field"><label>接管模式</label><RoundedSelect v-model="form.mode" label="接管模式" :options="[{ value: 'adaptive', label: '自适应' }, { value: 'direct', label: 'TV 直连' }, { value: 'split', label: 'UA 分流' }]" /></div>
          <div class="field"><label>最高画质</label><RoundedSelect v-model="form.quality" label="最高画质" :options="['low','normal','high','super','2k','4k','dolby_vision'].map((v,i) => ({ value:v, label:['流畅','标清','高清','超清','2K','4K','杜比视界'][i] }))" /></div>
          <label class="toggle-line full"><span>允许杜比视界</span><input v-model="form.allowDolby" type="checkbox" role="switch" class="switch" /></label>
          <template v-if="form.mode === 'split'"><div class="field"><label>UA 规则</label><RoundedSelect v-model="form.uaListMode" label="UA 规则" :options="[{ value:'proxy_list',label:'匹配时走原通道' },{ value:'direct_list',label:'仅匹配时接管' }]" /></div><label>UA 关键词<textarea v-model="form.ua" rows="3" placeholder="一行一个关键词" /></label></template>
          <label class="full">TV Access Token<SecretInput v-model="form.accessToken" :placeholder="authorized ? '已绑定，留空保留' : '填写 TV Access Token'" autocomplete="off" /></label>
          <label class="full">TV Refresh Token<SecretInput v-model="form.refreshToken" autocomplete="off" /></label>
          <label class="full">TV 设备 ID<input v-model="form.device" autocomplete="off" /></label>
          <label class="full">HTTPS 凭据换取服务<input v-model="form.broker" type="url" placeholder="https://" /></label>
          <label class="full consent"><input v-model="consent" type="checkbox" />我信任该服务，同意向其发送授权码和刷新凭据。</label>
          <p class="full muted">请使用与所选存储相同的夸克账号。TV 凭据独立于网页 Cookie；手动凭据未配置换取服务时，过期后需重新填写。</p>
          <img v-if="image" class="quark-qr full" :src="image" alt="夸克 TV 授权二维码" />
        </div>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn" :disabled="busy || !loaded || !consent || !form.broker" @click="qr"><Icon name="ScanSearch" />扫码绑定</button><button class="btn primary" :disabled="busy || !loaded">保存设置</button></footer>
    </form>
  </Modal>
</template>
<style scoped>
.form-grid { margin-top: 18px; }
.consent { display: flex; flex-direction: row; align-items: center; gap: 8px; font-size: 13px; }
.consent input { width: 16px; height: 16px; }
.quark-qr { width: 210px; height: 210px; justify-self: center; }
:deep(.rounded-select-trigger), :deep(.rounded-select-popup) { background: var(--input); color: var(--text); border-color: var(--border); }
</style>
