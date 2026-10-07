<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { api, notify } from '../lib'
import { copyText } from '../clipboard'
import Modal from './Modal.vue'
import SecretInput from './SecretInput.vue'
import Icon from './Icon.vue'
const props = defineProps({ kind: String, title: String })
const emit = defineEmits(['close', 'changed'])
const form = reactive({ enabled: false, apiURL: '', imageURL: '', apiKey: '', language: 'zh-CN', model: '', address: '', token: '' })
const loaded = ref(false), busy = ref(false), error = ref(''), text = ref(''), result = ref(null)
const webhook = computed(() => `${location.origin}/api/emby/webhook?token=${encodeURIComponent(form.token)}`)
const formatted = computed(() => typeof result.value === 'string' ? result.value : JSON.stringify(result.value, null, 2))
async function load() {
  try { Object.assign(form, await api(`/plugins/${props.kind}`)); loaded.value = true }
  catch (e) { error.value = e.message }
}
async function save() {
  busy.value = true; error.value = ''
  try {
    await api(`/plugins/${props.kind}`, 'PUT', form)
    await load(); emit('changed', { kind: props.kind, enabled: form.enabled }); notify('配置已保存')
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function action(name) {
  busy.value = true; error.value = ''; result.value = null
  try {
    const response = await api(`/plugins/${props.kind}/${name}`, 'POST', { config: form, text: text.value })
    if (name === 'test') notify(props.kind === 'proxy' ? '代理连接成功' : '连接成功')
    else result.value = response.result
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function copy() {
  try { await copyText(webhook.value); notify('Webhook 地址已复制') } catch { notify('复制失败', true) }
}
onMounted(load)
</script>
<template>
  <Modal :title="title" compact wide @close="$emit('close')">
    <form @submit.prevent="save">
      <div class="modal-body plugin-settings">
        <label class="toggle-line"><span>启用{{ title }}</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" :disabled="!loaded" /></label>
        <template v-if="kind === 'tmdb'">
          <label>API 域名<input v-model="form.apiURL" type="url" required /></label>
          <label>图片域名<input v-model="form.imageURL" type="url" required /></label>
          <label>API 密钥<SecretInput v-model="form.apiKey" :secret-path="`/plugins/${kind}/secret`" secret-field="apiKey" autocomplete="off" /></label>
          <div class="field"><label>语言</label><div class="language-options"><button v-for="option in [{ value: 'zh-CN', label: '中文' }, { value: 'en-US', label: '英文' }]" :key="option.value" type="button" :aria-pressed="form.language === option.value" @click="form.language = option.value">{{ option.label }}</button></div></div>
        </template>
        <template v-else-if="kind === 'ai'">
          <label>API 地址<input v-model="form.apiURL" type="url" required /></label>
          <label>模型名称<input v-model="form.model" required /></label>
          <label>API Key<SecretInput v-model="form.apiKey" :secret-path="`/plugins/${kind}/secret`" secret-field="apiKey" autocomplete="off" /></label>
        </template>
        <template v-else-if="kind === 'proxy'">
          <label>代理地址<SecretInput v-model="form.address" :secret-path="`/plugins/${kind}/secret`" secret-field="address" placeholder="http://127.0.0.1:7890" autocomplete="off" /></label>
          <p class="muted">用于 TMDB 和 AI 请求。支持 HTTP、HTTPS、SOCKS5。</p>
        </template>
        <template v-else-if="kind === 'emby'">
          <label>通知令牌<SecretInput v-model="form.token" autocomplete="off" placeholder="保存时自动生成" /></label>
          <div v-if="form.token" class="webhook-address"><code>{{ webhook }}</code><button type="button" class="icon-btn" aria-label="复制 Webhook 地址" @click="copy"><Icon name="Copy" /></button></div>
          <ol class="webhook-help"><li>在 Emby 通知设置中添加 Webhook，填写以上地址。</li><li>请求方式选择 POST，内容类型选择 application/json。</li><li>勾选媒体入库事件（library.new），保存后添加媒体验证。</li><li>Emby 必须能访问此主机与端口；跨容器时请将地址中的主机改为可访问的 Aether 地址。</li></ol>
        </template>
        <div v-if="['tmdb', 'ai'].includes(kind)" class="plugin-query">
          <label>{{ kind === 'ai' ? '待识别文件名' : '搜索名称' }}<input v-model="text" :maxlength="kind === 'ai' ? 4096 : 500" /></label>
          <button type="button" class="btn" :disabled="busy || !form.enabled || !text.trim()" @click="action(kind === 'ai' ? 'recognize' : 'search')"><Icon :name="kind === 'ai' ? 'Sparkles' : 'Search'" />{{ kind === 'ai' ? '识别' : '搜索' }}</button>
          <small>使用已保存配置{{ kind === 'ai' ? '发送文件名进行识别，不发送文件内容。' : '查询。' }}</small>
        </div>
        <pre v-if="result !== null" class="plugin-result">{{ formatted }}</pre>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button v-if="kind !== 'emby'" type="button" class="btn" :disabled="busy || !loaded" @click="action('test')"><Icon name="Activity" />测试连接</button><button class="btn primary" :disabled="busy || !loaded"><Icon name="Save" />保存配置</button></footer>
    </form>
  </Modal>
</template>
<style scoped>
.plugin-settings { display: grid; gap: 16px; }
.language-options { display: inline-flex; background: var(--bg); border-radius: 8px; padding: 3px; gap: 4px; }
.language-options button { border: 0; padding: 7px 20px; border-radius: 6px; color: var(--muted); background: transparent; }
.language-options button[aria-pressed=true] { color: var(--primary); background: var(--surface); }
.plugin-query { display: grid; grid-template-columns: minmax(0,1fr) auto; gap: 8px; align-items: end; border-top: 1px solid var(--border); padding-top: 16px; }
.plugin-query small { grid-column: 1 / -1; color: var(--muted); }
.plugin-result { margin: 0; max-height: 230px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; background: var(--bg); padding: 12px; border-radius: 8px; font-size: 13px; }
.webhook-address { display: flex; align-items: center; gap: 8px; padding: 12px; border-radius: 8px; background: var(--bg); }
.webhook-address code { flex: 1; min-width: 0; overflow-wrap: anywhere; font-size: 13px; }
.webhook-help { padding-left: 20px; color: var(--muted); font-size: 13px; line-height: 1.9; margin: 0; }
</style>
