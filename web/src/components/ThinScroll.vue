<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
const props = defineProps({ contentClass: String, thickness: { type: Number, default: 1 } })
const area = ref(null), thumb = ref(null), visible = ref(false)
defineExpose({ element: area })
let observer, mutations, frame = 0, drag
function update() {
  cancelAnimationFrame(frame)
  frame = requestAnimationFrame(() => {
    const el = area.value
    if (!el) return
    const h = el.clientHeight, total = el.scrollHeight
    visible.value = total > h + 1
    const size = Math.min(h, Math.max(24, h * h / total))
    const top = total > h ? el.scrollTop / (total - h) * (h - size) : 0
    const scale = (window.devicePixelRatio || 1) * (window.visualViewport?.scale || 1)
    thumb.value?.style.setProperty('--hairline', `${props.thickness / scale}px`)
    if (thumb.value) Object.assign(thumb.value.style, { height: `${size}px`, transform: `translateY(${top}px)` })
  })
}
function start(event) {
  const el = area.value
  drag = { y: event.clientY, top: el.scrollTop }
  event.currentTarget.setPointerCapture(event.pointerId)
}
function move(event) {
  if (!drag) return
  const el = area.value, travel = el.clientHeight - thumb.value.clientHeight
  if (travel > 0) el.scrollTop = drag.top + (event.clientY - drag.y) * (el.scrollHeight - el.clientHeight) / travel
}
onMounted(async () => {
  await nextTick()
  observer = new ResizeObserver(update)
  observer.observe(area.value)
  const observeChildren = () => { for (const child of area.value.children) observer.observe(child); update() }
  mutations = new MutationObserver(observeChildren)
  mutations.observe(area.value, { childList: true, subtree: true, characterData: true })
  observeChildren()
  window.addEventListener('resize', update)
  window.visualViewport?.addEventListener('resize', update)
})
onUnmounted(() => {
  observer?.disconnect(); mutations?.disconnect(); cancelAnimationFrame(frame)
  window.removeEventListener('resize', update)
  window.visualViewport?.removeEventListener('resize', update)
})
</script>
<template>
  <div class="thin-scroll">
    <div ref="area" class="thin-scroll-area" :class="props.contentClass" tabindex="0" @scroll.passive="update"><slot /></div>
    <div v-show="visible" class="thin-scroll-rail" aria-hidden="true"><div ref="thumb" class="thin-scroll-thumb" @pointerdown.prevent="start" @pointermove="move" @pointerup="drag = null" @pointercancel="drag = null" @lostpointercapture="drag = null" /></div>
  </div>
</template>
<style scoped>
.thin-scroll { position: relative; min-height: 0; }
.thin-scroll-area { height: 100%; overflow: auto; scrollbar-width: none !important; }
.thin-scroll-area::-webkit-scrollbar { display: none !important; }
.thin-scroll-rail { position: absolute; inset: 0 0 0 auto; width: 8px; pointer-events: none; z-index: 2; }
.thin-scroll-thumb { width: 8px; position: absolute; right: 0; top: 0; pointer-events: auto; touch-action: none; }
.thin-scroll-thumb::after { content: ''; position: absolute; right: 1px; inset-block: 0; width: var(--hairline, 1px); border-radius: 4px; background: color-mix(in srgb,var(--primary) 30%,transparent); }
.thin-scroll-thumb:hover::after { background: color-mix(in srgb,var(--primary) 60%,transparent); }
</style>
