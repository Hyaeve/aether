<script setup>
import { ref } from 'vue'
const props = defineProps({ modelValue: { type: String, default: '' }, label: { type: String, default: '回收站密码' }, disabled: Boolean })
const emit = defineEmits(['update:modelValue'])
const inputs = ref([])
function input(event, index) {
  const digits = event.target.value.replace(/\D/g, '')
  const next = props.modelValue.padEnd(6, ' ').split('')
  event.target.value=digits.slice(0,1)
  if (!digits) next[index]=' '
  else next.splice(index, digits.length, ...digits.slice(0, 6 - index))
  emit('update:modelValue', next.join('').slice(0, 6).trimEnd())
  if (digits) inputs.value[Math.min(5, index + digits.length)]?.focus()
}
function key(event, index) {
  if (event.key === 'ArrowLeft') inputs.value[Math.max(0, index - 1)]?.focus()
  if (event.key === 'ArrowRight') inputs.value[Math.min(5, index + 1)]?.focus()
  if (event.key === 'Backspace' && !props.modelValue[index]?.trim() && index) inputs.value[index - 1]?.focus()
}
function paste(event, index) {
  event.preventDefault()
  const digits = event.clipboardData.getData('text').replace(/\D/g, '').slice(0, 6 - index)
  if (!digits) return
  const next=props.modelValue.padEnd(6,' ').split('');next.splice(index,digits.length,...digits)
  emit('update:modelValue', next.join('').slice(0,6).trimEnd())
  inputs.value[Math.min(5, index + digits.length)]?.focus()
}
</script>
<template>
  <div class="pin-input" role="group" :aria-label="label"><input v-for="index in 6" :key="index" :ref="el => inputs[index - 1] = el" type="text" inputmode="numeric" autocomplete="off" :aria-label="`${label}第${index}位`" :disabled="disabled" :value="modelValue[index - 1]?.trim() || ''" @focus="$event.target.select()" @input="input($event, index - 1)" @keydown="key($event, index - 1)" @paste="paste($event, index - 1)" /></div>
</template>
<style scoped>
.pin-input{display:flex;gap:8px;width:100%;max-width:310px}.pin-input input{flex:1;min-width:0;width:0;height:40px;padding:0;text-align:center;font-size:18px;font-variant-numeric:tabular-nums}
</style>
