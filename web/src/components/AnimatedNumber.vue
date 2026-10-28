<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { numberEasing } from '../number-motion'

const props = defineProps({ value: { type: Number, default: 0 }, duration: { type: Number, default: 2200 } })
const displayed = ref(0)
let frame = 0
function stop() { if (frame) cancelAnimationFrame(frame); frame = 0 }
function animate(next) {
  stop()
  const target = Number.isFinite(next) ? Math.max(0, Math.round(next)) : 0
  const from = displayed.value
  if (from === target) return
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) { displayed.value=target; return }
  const started = performance.now()
  const tick = now => {
    const progress = Math.min(1, (now - started) / Math.max(1, props.duration))
    const eased = numberEasing(progress)
    displayed.value = Math.round(from + (target - from) * eased)
    if (progress < 1) frame = requestAnimationFrame(tick)
    else frame = 0
  }
  frame = requestAnimationFrame(tick)
}
watch(() => props.value, animate, { immediate: true })
onBeforeUnmount(stop)
</script>
<template><span class="animated-number" :aria-label="String(displayed)">{{ displayed }}</span></template>
