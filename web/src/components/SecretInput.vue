<script setup>
import { ref } from 'vue'
import Icon from './Icon.vue'
defineOptions({ inheritAttrs: false })
defineProps({ modelValue: String })
const emit = defineEmits(['update:modelValue'])
const visible = ref(false)
</script>
<template><span class="secret-input"><input v-bind="$attrs" :value="modelValue" :type="visible ? 'text' : 'password'" @input="emit('update:modelValue', $event.target.value)" /><button type="button" class="icon-btn" :aria-label="visible ? '隐藏内容' : '显示内容'" @click.prevent="visible = !visible"><Icon :name="visible ? 'Eye' : 'EyeOff'" :size="17" /></button></span></template>
