<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
const text = ref(''), left = ref(0), top = ref(0)
let target, timer
function hide() { clearTimeout(timer); text.value = ''; target = null }
function show(event) {
  const el = event.target.closest('[data-tooltip], .file-view th, .file-view td, .file-name strong, .file-grid-name strong, .log-message, .log-module, .playback-scroller td, .playback-scroller th, .playback-copy')
  if (!el) return
  const value = el.dataset.tooltip || el.textContent
  const clipped = el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight
  if (!value || (!el.dataset.tooltip && !clipped)) return
  hide(); target = el
  timer = setTimeout(() => {
    const r = el.getBoundingClientRect()
    text.value = value
    left.value = Math.max(8, Math.min(r.left, innerWidth - Math.min(460, innerWidth - 16) - 8))
    top.value = Math.max(8, Math.min(r.bottom + 8, innerHeight - 190))
  }, 350)
}
function out(event) { if (target && !target.contains(event.relatedTarget)) hide() }
function key(event) { if (event.key === 'Escape') hide() }
onMounted(() => { document.addEventListener('mouseover', show); document.addEventListener('mouseout', out); document.addEventListener('focusin', show); document.addEventListener('focusout', hide); document.addEventListener('keydown', key); window.addEventListener('scroll', hide, true) })
onUnmounted(() => { hide(); document.removeEventListener('mouseover', show); document.removeEventListener('mouseout', out); document.removeEventListener('focusin', show); document.removeEventListener('focusout', hide); document.removeEventListener('keydown', key); window.removeEventListener('scroll', hide, true) })
</script>
<template><Teleport to="body"><div v-if="text" role="tooltip" class="overflow-tooltip" :style="{ left: `${left}px`, top: `${top}px` }">{{ text }}</div></Teleport></template>
