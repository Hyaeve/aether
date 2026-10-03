<script setup>
import { ref, useAttrs } from 'vue'
import Icon from './Icon.vue'
defineOptions({ inheritAttrs: false })
defineProps({ modelValue: [Number, String], unit: { default: '' } })
const emit = defineEmits(['update:modelValue'])
const attrs = useAttrs(), input = ref()
function step(direction) {
  if (attrs.disabled) return
  if (direction > 0) input.value.stepUp()
  else input.value.stepDown()
  emit('update:modelValue', input.value.value === '' ? '' : Number(input.value.value))
}
</script>
<template>
  <span class="number-control">
    <input ref="input" v-bind="$attrs" :value="modelValue" type="number" @input="emit('update:modelValue', $event.target.value === '' ? '' : Number($event.target.value))" />
    <span class="number-accessory">
      <span class="number-unit">{{ unit }}</span>
      <span class="number-steppers">
        <button type="button" :disabled="!!$attrs.disabled" :aria-label="`增加${$attrs['aria-label'] || ''}`" title="增加" @click.prevent="step(1)"><Icon name="ChevronUp" :size="14" /></button>
        <button type="button" :disabled="!!$attrs.disabled" :aria-label="`减少${$attrs['aria-label'] || ''}`" title="减少" @click.prevent="step(-1)"><Icon name="ChevronDown" :size="14" /></button>
      </span>
    </span>
  </span>
</template>
