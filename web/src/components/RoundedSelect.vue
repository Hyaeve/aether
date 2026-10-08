<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, useId } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ modelValue: String, label: String, placeholder: { type: String, default: '' }, disabled: Boolean, upward: Boolean, options: { type: Array, required: true } })
const emit = defineEmits(['update:modelValue'])
const root = ref(null), trigger = ref(null), opened = ref(false), active = ref(0)
const id = useId()
const selected = computed(() => props.options.find(o => o.value === props.modelValue))
function close() { opened.value = false }
async function show() {
  if (props.disabled || !props.options.length) return
  active.value = Math.max(0, props.options.findIndex(o => o.value === props.modelValue))
  opened.value = true
  await nextTick()
  root.value?.querySelector('[role=listbox]')?.focus()
}
function choose(index) { emit('update:modelValue', props.options[index].value); close(); trigger.value?.focus() }
function keydown(event) {
  if (event.key === 'Tab') { close(); return }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' ', 'Escape'].includes(event.key)) return
  event.preventDefault()
  if (event.key === 'Escape') { close(); trigger.value?.focus(); return }
  if (event.key === 'Enter' || event.key === ' ') { choose(active.value); return }
  active.value = event.key === 'Home' ? 0 : event.key === 'End' ? props.options.length - 1 : (active.value + (event.key === 'ArrowDown' ? 1 : -1) + props.options.length) % props.options.length
  nextTick(() => root.value?.querySelector(`#${CSS.escape(id)}-${active.value}`)?.scrollIntoView({ block: 'nearest' }))
}
function outside(event) { if (!root.value?.contains(event.target)) close() }
onMounted(() => document.addEventListener('pointerdown', outside))
onUnmounted(() => document.removeEventListener('pointerdown', outside))
</script>
<template>
  <div ref="root" class="rounded-select" :class="{ 'opens-up': upward }">
    <button ref="trigger" type="button" class="rounded-select-trigger" :disabled="disabled || !options.length" :aria-label="label" aria-haspopup="listbox" :aria-expanded="opened" :aria-controls="id" @click="opened ? close() : show()" @keydown.down.prevent="show" @keydown.up.prevent="show"><span class="select-label"><Icon v-if="selected?.icon" :name="selected.icon" :size="15" />{{ selected?.label || placeholder }}</span><Icon name="ChevronDown" :size="16" :class="{ expanded: opened }" /></button>
    <Transition name="select-popup">
      <div v-if="opened" :id="id" role="listbox" :aria-label="`${label}选项`" :aria-activedescendant="`${id}-${active}`" tabindex="-1" class="rounded-select-popup" @keydown="keydown">
        <div v-for="(option, index) in options" :id="`${id}-${index}`" :key="option.value" role="option" :aria-selected="option.value === modelValue" class="rounded-select-option" :class="{ focused: index === active }" @pointermove="active = index" @click="choose(index)"><span class="select-label"><Icon v-if="option.icon" :name="option.icon" :size="15" />{{ option.label }}</span><Icon v-if="option.value === modelValue" name="Check" :size="16" /></div>
      </div>
    </Transition>
  </div>
</template>
<style scoped>
.select-label { display: flex; align-items: center; gap: 6px; min-width: 0; }
.select-label svg { flex-shrink: 0; }
.opens-up .rounded-select-popup { top: auto; bottom: calc(100% + 6px); transform-origin: bottom; }
</style>
