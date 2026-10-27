<script setup>
import { computed, onUnmounted, ref } from 'vue'
import { api, notify } from '../lib'
import Icon from './Icon.vue'
import SecretInput from './SecretInput.vue'

const props = defineProps({ kind: String, initial: Object })
const emit = defineEmits(['saved'])
const isQQ = computed(() => props.kind === 'qqBot')
const title = computed(() => isQQ.value ? 'QQ Bot' : '飞书通知')
const fields = computed(() => isQQ.value ? [
  { key: 'appId', label: 'AppID' },
  { key: 'secret', label: 'AppSecret', secret: true },
  { key: 'groupId', label: '群 OpenID' }
] : [
  { key: 'webhook', label: 'Webhook 地址', secret: true },
  { key: 'secret', label: '签名密钥', secret: true }
])
const secretFields = computed(() => isQQ.value ? ['secret'] : ['webhook', 'secret'])
const flag = field => field === 'webhook' ? 'hasWebhook' : 'hasSecret'
function masked(value) {
  const next = { enabled: false, appId: '', groupId: '', webhook: '', secret: '', ...value }
  for (const field of secretFields.value) next[field] = next[flag(field)] ? '********' : ''
  return next
}
const form = ref(masked(props.initial)), busy = ref(false), testing = ref(false)
const endpoint = computed(() => `/notification-service/${props.kind}`)
let alive = true
onUnmounted(() => { alive = false })
async function save() {
  if (busy.value || testing.value) return
  busy.value = true
  const value = { enabled: form.value.enabled, clearSecrets: [] }
  for (const field of fields.value) {
    const text = form.value[field.key]
    value[field.key] = text === '********' ? '' : text
    if (field.secret && text === '' && form.value[flag(field.key)]) value.clearSecrets.push(field.key)
  }
  try {
    const result = await api(endpoint.value, 'PUT', { [props.kind]: value })
    if (alive) {
      form.value = masked(result[props.kind])
      emit('saved', result[props.kind])
      notify(`${title.value}设置已保存`)
    }
  } catch (e) { if (alive) notify(e.message, true) }
  finally { if (alive) busy.value = false }
}
async function test() {
  if (busy.value || testing.value) return
  testing.value = true
  try {
    await api(`${endpoint.value}/test`, 'POST', {})
    if (alive) notify(`${title.value}测试消息已发送`)
  } catch (e) { if (alive) notify(e.message, true) }
  finally { if (alive) testing.value = false }
}
</script>
<template>
  <form class="notification-form channel-form" :aria-label="title" novalidate @submit.prevent="save">
    <header><h2><Icon :name="isQQ ? 'Bot' : 'Feather'" />{{ title }}</h2><input v-model="form.enabled" type="checkbox" role="switch" class="switch" :aria-label="`启用${title}`" :disabled="busy || testing" /></header>
    <div class="channel-fields">
      <label v-for="field in fields" :key="field.key">{{ field.label }}
        <SecretInput v-if="field.secret" v-model="form[field.key]" :secret-path="`${endpoint}/secret`" :secret-field="field.key" :aria-label="field.label" autocomplete="new-password" :disabled="busy || testing" />
        <input v-else v-model.trim="form[field.key]" :aria-label="field.label" :inputmode="field.key === 'appId' ? 'numeric' : undefined" autocomplete="off" :disabled="busy || testing" />
      </label>
    </div>
    <footer><button type="button" class="btn" :disabled="busy || testing || (isQQ ? !form.hasSecret : !form.hasWebhook)" @click="test"><Icon :name="testing ? 'LoaderCircle' : 'Send'" :class="{ spin: testing }" />测试通知</button><button class="btn primary" :disabled="busy || testing"><Icon :name="busy ? 'LoaderCircle' : 'Save'" :class="{ spin: busy }" />保存设置</button></footer>
  </form>
</template>
<style scoped>
.channel-form{display:flex;flex-direction:column;gap:18px;min-width:0;width:100%;padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}
header,h2{display:flex;align-items:center;gap:10px}header{justify-content:space-between}h2{margin:0;font-size:15px}.switch{flex:none}
.channel-fields{display:grid;gap:18px;min-width:0}.channel-fields label{display:grid;gap:8px;min-width:0}.channel-fields :deep(.secret-input),.channel-fields :deep(input){width:100%;min-width:0;height:var(--control-height);min-height:var(--control-height)}
footer{display:flex;gap:10px;margin-top:auto;padding-top:2px}footer .btn{flex:1;height:var(--toolbar-height);min-height:var(--toolbar-height);padding-block:0}
</style>
