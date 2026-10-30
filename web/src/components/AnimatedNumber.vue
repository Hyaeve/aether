<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { numberDuration, numberEasing, numberFrameInterval } from '../number-motion'

const props = defineProps({ value: { type: Number, default: 0 }, duration: { type: Number, default: 1500 } })
const displayed = ref(0)
let frame = 0, timer = 0, deadline = 0
function stop() {
  if (frame) cancelAnimationFrame(frame)
  clearTimeout(timer)
  frame = timer = 0
}
function animate(next) {
  stop()
  const target = Number.isFinite(next) ? Math.max(0, Math.round(next)) : 0
  const from = displayed.value
  const started = performance.now()
  const duration = numberDuration(props.duration)
  if (from === target || !duration || matchMedia('(prefers-reduced-motion: reduce)').matches) {
    displayed.value = target; deadline = 0; return
  }
  // Refreshing a statistic during its count-up must not extend the deadline.
  deadline = deadline > started ? Math.min(deadline, started + duration) : started + duration
  const remaining = deadline - started
  let last = started
  const finish = () => { stop(); displayed.value = target; deadline = 0 }
  const tick = now => {
    const progress = Math.min(1, (now - started) / remaining)
    if (progress === 1) { finish(); return }
    if (now - last >= numberFrameInterval(progress, target - from)) {
      displayed.value = Math.round(from + (target - from) * numberEasing(progress))
      last = now
    }
    frame = requestAnimationFrame(tick)
  }
  timer = setTimeout(finish, remaining)
  frame = requestAnimationFrame(tick)
}
watch(() => [props.value, props.duration], ([value]) => animate(value), { immediate: true })
onBeforeUnmount(stop)
</script>
<template><span class="animated-number" :aria-label="String(displayed)">{{ displayed }}</span></template>
