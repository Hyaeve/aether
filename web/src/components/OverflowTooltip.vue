<script setup>
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
const text = ref(''), left = ref(0), top = ref(0), popup = ref(null), stationary = ref(false), playback = ref(false), rapidFile = ref(false)
let target, timer, value = '', delay = 350
let pointerX = 0, pointerY = 0
function move(event) {
  if (pointerX === event.clientX && pointerY === event.clientY) return
  pointerX = event.clientX; pointerY = event.clientY
  if (target && stationary.value) { clearTimeout(timer); text.value = ''; schedule() }
  else if (text.value) position()
}
function position() {
  const rect = popup.value?.getBoundingClientRect()
  left.value = Math.max(8, Math.min(pointerX + 14, innerWidth - (rect?.width || 320) - 8))
  top.value = Math.max(8, Math.min(pointerY + 18, innerHeight - (rect?.height || 180) - 8))
}
function hide() { clearTimeout(timer); text.value = ''; target = null; value = '' }
function schedule() {
  timer = setTimeout(async () => {
    if (!target?.isConnected) { hide(); return }
    text.value = value
    position(); await nextTick(); position()
  }, delay)
}
function show(event) {
  const el = event.target.closest('[data-tooltip], .file-view th, .file-view td, .file-name strong, .file-grid-name strong, .log-message, .log-module, .playback-scroller td, .playback-scroller th, .playback-copy')
  if (!el) return
  const content = el.dataset.tooltip || el.textContent
  const clipped = el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight
  if (!content || (!clipped && !el.hasAttribute('data-tooltip-always')) || target === el) return
  const fileName = el.matches('.file-browser .file-name strong, .file-browser .file-grid-name strong')
  hide(); target = el; value = content; playback.value = !!el.closest('.playback-scroller'); rapidFile.value = el.dataset.tooltipVariant === 'rapid-file'; stationary.value = fileName || playback.value || el.hasAttribute('data-tooltip-stationary')
  delay = Number(el.dataset.tooltipDelay) || (fileName ? 350 : stationary.value || el.hasAttribute('data-tooltip-always') ? 600 : 350)
  const r = el.getBoundingClientRect()
  pointerX = event.clientX ?? r.left; pointerY = event.clientY ?? r.bottom
  schedule()
}
function out(event) { if (target && !target.contains(event.relatedTarget)) hide() }
function key(event) { if (event.key === 'Escape') hide() }
onMounted(() => { document.addEventListener('mousemove', move); document.addEventListener('mouseover', show); document.addEventListener('mouseout', out); document.addEventListener('focusin', show); document.addEventListener('focusout', hide); document.addEventListener('keydown', key); window.addEventListener('scroll', hide, true) })
onUnmounted(() => { hide(); document.removeEventListener('mousemove', move); document.removeEventListener('mouseover', show); document.removeEventListener('mouseout', out); document.removeEventListener('focusin', show); document.removeEventListener('focusout', hide); document.removeEventListener('keydown', key); window.removeEventListener('scroll', hide, true) })
</script>
<template><Teleport to="body"><div v-if="text" ref="popup" role="tooltip" class="overflow-tooltip" :class="{'playback-tooltip':playback,'rapid-file-tooltip':rapidFile}" :style="{ left: `${left}px`, top: `${top}px` }">{{ text }}</div></Teleport></template>
