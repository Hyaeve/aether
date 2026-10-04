<script setup>
import { reactive, ref } from 'vue'
import { api, state, reload } from '../lib'
import Icon from '../components/Icon.vue'
import LoginUniverse from '../components/LoginUniverse.vue'
import AccountFields from '../components/AccountFields.vue'
const form = reactive({ username: '', password: '' })
const busy = ref(false), error = ref('')
async function submit() {
  busy.value = true; error.value = ''
  try { await api(state.initialized ? '/auth/login' : '/auth/setup', 'POST', form); state.initialized = true; state.authenticated = true; await reload() }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
</script>
<template>
  <main class="login-page">
    <LoginUniverse />
    <section class="login-form-side">
      <div class="login-status"><i />本地部署 · 独立掌控</div>
      <form class="login-form" @submit.prevent="submit">
        <span class="eyebrow">YOUR STORAGE, TOGETHER</span>
        <h1>{{ state.initialized ? '欢迎回到以太' : '建立你的以太空间' }}</h1>
        <p>{{ state.initialized ? '登录，连接你的存储世界。' : '创建管理员账号，开始连接存储。' }}</p>
        <AccountFields v-model:username="form.username" v-model:password="form.password" :current-password="state.initialized" />
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
        <button class="btn primary login-submit" :disabled="busy">{{ busy ? '正在连接…' : state.initialized ? '登录工作空间' : '创建管理员账号' }}<Icon name="ArrowRight" /></button>
        <div class="login-security"><Icon name="ShieldCheck" :size="16" />安全连接 · 凭据本地加密</div>
      </form>
    </section>
  </main>
</template>
