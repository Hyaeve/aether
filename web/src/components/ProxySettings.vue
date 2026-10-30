<script setup>
import { onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import { api, notify } from '../lib'
import { readPlugin, invalidatePlugin } from '../plugin-config'
import Icon from './Icon.vue'
import SecretInput from './SecretInput.vue'

const form = reactive({ enabled: false, address: '', username: '', password: '' })
const loaded = ref(false), busy = ref(false), error = ref('')
let alive = true
onBeforeUnmount(() => { alive = false })
async function load() {
  error.value = ''
  try {
    const value = await readPlugin('proxy', true)
    if (alive) { Object.assign(form, value); loaded.value = true }
  } catch (e) { if (alive) error.value = e.message }
}
async function request(test = false) {
  if (busy.value || !loaded.value) return
  busy.value = true; error.value = ''
  try {
    const value = { ...form }
    await api(test ? '/plugins/proxy/test' : '/plugins/proxy', test ? 'POST' : 'PUT', test ? { config: value } : value)
    if (!test) invalidatePlugin('proxy')
    if (alive) {
      if (!test) await load()
      notify(test ? '代理连接成功' : '代理配置已保存')
    }
  } catch (e) { if (alive) error.value = e.message }
  finally { if (alive) busy.value = false }
}
onMounted(load)
</script>
<template>
  <form class="proxy-settings account-panel" aria-label="代理配置" @submit.prevent="request()">
    <header class="settings-section-title"><Icon name="Network" /><h2>代理配置</h2></header>
    <p v-if="!loaded && !error" role="status">正在读取配置…</p>
    <fieldset :disabled="!loaded || busy">
      <label class="proxy-enabled"><span>启用代理</span><input v-model="form.enabled" type="checkbox" role="switch" class="switch" aria-label="启用代理" /></label>
      <label>代理地址<input v-model="form.address" placeholder="http://127.0.0.1:7890" autocomplete="off" /></label>
      <label>用户名（可选）<input v-model="form.username" autocomplete="off" /></label>
      <label>密码（可选）<SecretInput v-model="form.password" secret-path="/plugins/proxy/secret" secret-field="password" autocomplete="off" /></label>
    </fieldset>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    <footer class="proxy-actions"><button v-if="!loaded && error" type="button" class="btn" @click="load"><Icon name="RefreshCw" />重试</button><template v-else><button type="button" class="btn" :disabled="busy || !loaded" @click="request(true)"><Icon name="Activity" />测试连接</button><button class="btn primary" :disabled="busy || !loaded"><Icon name="Save" />保存配置</button></template></footer>
  </form>
</template>
<style scoped>
.proxy-settings{min-width:0;margin:0;padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}
.settings-section-title{gap:10px;margin-bottom:18px}.settings-section-title h2{font-size:16px;margin:0}
fieldset{display:grid;gap:16px;margin:0;padding:0;border:0;min-width:0}
label{min-width:0}input:not([type=checkbox]){width:100%;height:40px}
.proxy-enabled{display:flex;flex-direction:row;align-items:center;justify-content:space-between;gap:12px}
.proxy-actions{display:flex;gap:8px;justify-content:space-between;margin-top:18px;flex-wrap:wrap}
.error-message{margin-top:12px}
</style>
