<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, notify } from '../lib'
import Icon from './Icon.vue'
defineOptions({ inheritAttrs: false })
const props = defineProps({ modelValue: String, secretPath: String, secretField: String })
const emit = defineEmits(['update:modelValue'])
const visible = ref(false)
const loading = ref(false)
const touched = ref(false)
const savedLength = ref(8)
const mask = computed(() => '*'.repeat(Math.min(4096, Math.max(1, props.modelValue === '********' ? savedLength.value : [...(props.modelValue || '')].length))))
onMounted(async () => {
  if (props.modelValue !== '********' || !props.secretPath) return
  try {
    const result = await api(props.secretPath, 'POST', { field: props.secretField, metadataOnly: true })
    if (alive) savedLength.value = result.length
  } catch { /* Keep a placeholder if metadata cannot be read; revealing still reports errors. */ }
})
function input(event) { touched.value = true; emit('update:modelValue', event.target.value) }
let alive = true
onUnmounted(() => { alive = false })
async function toggle() {
  if (visible.value) { visible.value = false; return }
  if (props.modelValue === '********' && props.secretPath) {
    loading.value = true
    const previous = props.modelValue
    try {
      const result = await api(props.secretPath, 'POST', { field: props.secretField })
      if (!alive || props.modelValue !== previous) return
      savedLength.value = [...result.value].length
      emit('update:modelValue', result.value)
    } catch (e) { if (alive) notify(e.message, true); return }
    finally { loading.value = false }
  }
  if (alive) visible.value = true
}
</script>
<template><span class="secret-input"><input v-bind="$attrs" :value="!visible && modelValue && secretPath && !touched ? mask : modelValue" :type="visible ? 'text' : 'password'" @focus="!visible && secretPath && !touched && $event.target.select()" @input="input" /><button type="button" class="icon-btn" :disabled="loading" :aria-label="visible ? '隐藏内容' : '显示内容'" @click.prevent="toggle"><Icon :name="loading ? 'LoaderCircle' : visible ? 'Eye' : 'EyeOff'" :size="17" :class="{ spin: loading }" /></button></span></template>
