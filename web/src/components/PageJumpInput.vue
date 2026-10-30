<script setup>
import { computed, watch } from 'vue'

const props = defineProps({ modelValue: [Number, String], pages: { type: Number, default: 1 } })
const emit = defineEmits(['update:modelValue'])
const maximum = computed(() => Number.isFinite(props.pages) ? Math.max(1, Math.trunc(props.pages)) : 1)
function normalize(value) {
  const number = Number(value)
  return Math.min(maximum.value, Math.max(1, Math.trunc(number) || 1))
}
function update(event, allowEmpty) {
  const value = allowEmpty && event.target.value === '' ? '' : normalize(event.target.value)
  event.target.value = String(value)
  emit('update:modelValue', value)
}
watch(maximum, () => {
  if (props.modelValue !== '') emit('update:modelValue', normalize(props.modelValue))
})
</script>
<template>
  <input :value="modelValue" type="number" min="1" :max="maximum" step="1" aria-label="跳转页码" @input="update($event, true)" @blur="update($event, false)" />
</template>
