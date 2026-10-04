<script setup>
import { ref } from 'vue'
import Icon from './Icon.vue'
defineProps({ username: String, password: String, currentPassword: Boolean })
defineEmits(['update:username', 'update:password'])
const show = ref(false)
</script>
<template>
  <div class="account-fields">
    <div class="credential-input"><Icon name="UserRound" :size="21" /><input :value="username" @input="$emit('update:username', $event.target.value)" aria-label="账号" placeholder="请输入账号" required maxlength="150" autocomplete="username" /></div>
    <div class="credential-input input-action"><Icon name="LockKeyhole" :size="21" /><input :value="password" @input="$emit('update:password', $event.target.value)" aria-label="密码" placeholder="请输入密码" :type="show ? 'text' : 'password'" required maxlength="72" :autocomplete="currentPassword ? 'current-password' : 'new-password'" /><button class="icon-btn" type="button" :aria-label="show ? '隐藏密码' : '显示密码'" :title="show ? '隐藏密码' : '显示密码'" :aria-pressed="show" @click="show = !show"><Icon :name="show ? 'Eye' : 'EyeOff'" :size="21" /></button></div>
  </div>
</template>
