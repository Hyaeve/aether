<script setup>
import { reactive, ref } from 'vue'
import { api, state, reload, drivers } from '../lib'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
const form = reactive({ username: '', password: '' })
const busy = ref(false), error = ref(''), show = ref(false)
async function submit() {
  busy.value = true; error.value = ''
  try { await api(state.initialized ? '/auth/login' : '/auth/setup', 'POST', form); state.initialized = true; state.authenticated = true; await reload() }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
</script>
<template>
  <main class="login-page">
    <section class="login-universe"><a class="login-brand"><img src="/aether.svg" alt="" /><span>Aether<small>以太 · 存储工作空间</small></span></a>
      <div class="orbital-system"><div class="orbit orbit-one" /><div class="orbit orbit-two" /><div class="orbit orbit-three" /><div class="orbital-center"><img src="/aether.svg" alt="Aether" /></div><div v-for="(d, i) in drivers" :key="d.id" class="satellite" :class="`satellite-${i}`"><ProviderIcon :type="d.id" /><span>{{ d.name }}</span></div></div>
      <div class="universe-caption"><span>EVERY SPACE, CONNECTED</span><h2>万千存储，同一片以太。</h2><p>让数据流动，让空间相连。</p></div><footer>AETHER WORKSPACE <span>01 / CONNECT YOUR SPACE</span></footer>
    </section>
    <section class="login-form-side"><div class="login-status"><i />本地部署 · 独立掌控</div><form class="login-form" @submit.prevent="submit"><span class="eyebrow">YOUR STORAGE, TOGETHER</span><h1>{{ state.initialized ? '欢迎回到以太' : '建立你的以太空间' }}</h1><p>{{ state.initialized ? '登录，连接你的存储世界。' : '创建管理员账户，开始连接存储。' }}</p><label>账户名<input v-model="form.username" required autocomplete="username" placeholder="请输入账户名" /></label><label>密码<div class="input-action"><input v-model="form.password" :type="show ? 'text' : 'password'" required :minlength="state.initialized ? 1 : 12" maxlength="72" :autocomplete="state.initialized ? 'current-password' : 'new-password'" :placeholder="state.initialized ? '请输入密码' : '至少 12 个字符'" /><button type="button" class="icon-btn" :aria-label="show ? '隐藏密码' : '显示密码'" @click="show = !show"><Icon :name="show ? 'EyeOff' : 'Eye'" /></button></div></label><p v-if="error" class="error-message" role="alert">{{ error }}</p><button class="btn primary login-submit" :disabled="busy">{{ busy ? '正在连接…' : state.initialized ? '登录工作空间' : '创建管理员账户' }}<Icon name="ArrowRight" /></button><div class="login-security"><Icon name="ShieldCheck" :size="16" />安全连接 · 凭据本地加密</div></form><footer>Aether 以太 <span>v0.1.0</span></footer></section>
  </main>
</template>
