<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, useId } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ modelValue: String, label: String, icon: String, placeholder: { type: String, default: '' }, disabled: Boolean, allowEmpty: Boolean, upward: Boolean, hideCheck: Boolean, hoverOpen: Boolean, hoverDelay: { type: Number, default: 0 }, options: { type: Array, required: true } })
const emit = defineEmits(['update:modelValue'])
const root = ref(null), trigger = ref(null), opened = ref(false), active = ref(0)
const id = useId()
let hoverTimer
const selected = computed(() => props.options.find(o => o.value === props.modelValue))
function close() { clearTimeout(hoverTimer); opened.value = false }
function hoverIn() { if(props.hoverOpen){clearTimeout(hoverTimer);if(!opened.value)hoverTimer=setTimeout(() => show(false), props.hoverDelay)} }
function hoverOut() { if(props.hoverOpen){clearTimeout(hoverTimer);hoverTimer=setTimeout(close,240)} }
async function show(focus = true) {
  clearTimeout(hoverTimer)
  if (props.disabled || (!props.allowEmpty && !props.options.length)) return
  active.value = Math.max(0, props.options.findIndex(o => o.value === props.modelValue))
  opened.value = true
  await nextTick()
  if(focus)root.value?.querySelector('[role=listbox]')?.focus({preventScroll:true})
}
function choose(index) { emit('update:modelValue', props.options[index].value); close(); trigger.value?.focus({preventScroll:true}) }
function keydown(event) {
  if (event.key === 'Tab') { close(); return }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' ', 'Escape'].includes(event.key)) return
  event.preventDefault()
  if (event.key === 'Escape') { close(); trigger.value?.focus(); return }
  if (!props.options.length) return
  if (event.key === 'Enter' || event.key === ' ') { choose(active.value); return }
  active.value = event.key === 'Home' ? 0 : event.key === 'End' ? props.options.length - 1 : (active.value + (event.key === 'ArrowDown' ? 1 : -1) + props.options.length) % props.options.length
  nextTick(() => root.value?.querySelector(`#${CSS.escape(id)}-${active.value}`)?.scrollIntoView({ block: 'nearest' }))
}
function outside(event) { if (!root.value?.contains(event.target)) close() }
onMounted(() => document.addEventListener('pointerdown', outside))
onUnmounted(() => {clearTimeout(hoverTimer);document.removeEventListener('pointerdown', outside)})
</script>
<template>
  <div ref="root" class="rounded-select" :class="{ 'opens-up': upward }" @mouseenter="hoverIn" @mouseleave="hoverOut">
    <button ref="trigger" type="button" class="rounded-select-trigger" :disabled="disabled || (!allowEmpty && !options.length)" :aria-label="label" aria-haspopup="listbox" :aria-expanded="opened" :aria-controls="id" @click="hoverOpen || !opened ? show() : close()" @keydown.down.prevent="show" @keydown.up.prevent="show"><Icon v-if="icon" :name="icon" :size="18" /><template v-else><span class="select-label"><slot name="selected" :option="selected"><span v-if="selected?.marker" class="select-marker" :class="`marker-${selected.marker}`" aria-hidden="true" /><Icon v-if="selected?.icon" :name="selected.icon" :size="15" />{{ selected?.label || placeholder }}</slot></span><Icon name="ChevronDown" :size="16" :class="{ expanded: opened }" /></template></button>
    <Transition name="select-popup">
      <div v-if="opened" :id="id" role="listbox" :aria-label="`${label}选项`" :aria-activedescendant="`${id}-${active}`" tabindex="-1" class="rounded-select-popup" @keydown="keydown">
        <div v-if="!options.length" class="select-empty" role="status">暂无访问记录</div>
        <div v-for="(option, index) in options" :id="`${id}-${index}`" :key="option.value" role="option" :aria-label="option.label" :aria-selected="option.value === modelValue" class="rounded-select-option" :class="{ focused: index === active }" @pointermove="active = index" @click.stop.prevent="choose(index)"><span class="select-label"><slot name="option" :option="option"><span v-if="option.marker" class="select-marker" :class="`marker-${option.marker}`" aria-hidden="true" /><Icon v-if="option.icon" :name="option.icon" :size="15" />{{ option.label }}</slot></span><Icon v-if="!hideCheck && option.value === modelValue" name="Check" :size="16" /></div>
      </div>
    </Transition>
  </div>
</template>
<style scoped>
.select-label { display: flex; align-items: center; gap: 6px; min-width: 0; }
.select-label svg { flex-shrink: 0; }
.select-empty { padding:12px; color:var(--muted); font-size:13px; }
.select-marker { width:6px; height:6px; flex:none; align-self:center; background:var(--muted);border-radius:1.5px; }
.marker-pending { background:var(--primary); }.marker-ok { background:var(--green);transform:rotate(45deg);border-radius:2px; }.marker-error { background:var(--red);border-radius:50%; }
.marker-unmatched { background:var(--amber); }.marker-all { background:var(--muted);border-radius:50%; }
.opens-up .rounded-select-popup { top: auto; bottom: calc(100% + 6px); transform-origin: bottom; }
</style>
