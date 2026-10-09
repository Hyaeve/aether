<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ images: { type: Array, required: true }, initial: String })
const emit = defineEmits(['close'])
const index = ref(Math.max(0, props.images.findIndex(f => f.id === props.initial)))
const current = computed(() => props.images[index.value])
const panel = ref(null), stage = ref(null), picture = ref(null)
const loaded = ref(false), failed = ref(false), scale = ref(1), rotation = ref(0), x = ref(0), y = ref(0)
const original = ref(false), dragging = ref(false), dockVisible = ref(true)
const smooth = ref(false)
const natural = ref({ width: 0, height: 0 }), bounds = ref({ width: 0, height: 0 })
const fitScale = computed(() => {
  const swapped = rotation.value % 180 !== 0
  const width = swapped ? natural.value.height : natural.value.width
  const height = swapped ? natural.value.width : natural.value.height
  return width && height ? Math.min(1, bounds.value.width / width, bounds.value.height / height) : 1
})
const actualScale = computed(() => (original.value ? 1 : fitScale.value) * scale.value)
const imageStyle = computed(() => ({
  width: `${natural.value.width}px`, height: `${natural.value.height}px`,
  transform: `translate(-50%, -50%) translate(${x.value}px, ${y.value}px) rotate(${rotation.value}deg) scale(${actualScale.value})`
}))
const percent = computed(() => Math.round(actualScale.value * 100))
let previousFocus, previousOverflow, hideTimer, observer, gesture
const pointers = new Map()
function reset() {
  scale.value = 1; rotation.value = 0; x.value = 0; y.value = 0; original.value = false
  loaded.value = false; failed.value = false; dragging.value = false
  smooth.value = false
  natural.value = { width: 0, height: 0 }; pointers.clear(); gesture = null
}
function scheduleHide() {
  clearTimeout(hideTimer)
  hideTimer = setTimeout(() => { if (!panel.value?.querySelector('.viewer-dock:focus-within,.viewer-dock:hover')) dockVisible.value = false }, 3000)
}
function showTools() { dockVisible.value = true; scheduleHide() }
function navigate(step) {
  if (props.images.length < 2) return
  panel.value?.focus({ preventScroll: true })
  index.value = (index.value + step + props.images.length) % props.images.length
  showTools()
}
watch(() => current.value?.id, reset, { flush: 'sync' })
function imageLoaded(event) {
  if (event.target !== picture.value) return
  bounds.value = { width: stage.value.clientWidth, height: stage.value.clientHeight }
  natural.value = { width: event.target.naturalWidth, height: event.target.naturalHeight }
  loaded.value = true; failed.value = false
}
function zoom(factor) {
  if (!loaded.value) return
  smooth.value = true
  scale.value = Math.max(.25, Math.min(8 / Math.max(.001, original.value ? 1 : fitScale.value), scale.value * factor))
  showTools()
}
function toggleOriginal() { if (!loaded.value) return; smooth.value = true; original.value = !original.value; scale.value = 1; x.value = 0; y.value = 0; showTools() }
function rotate() { smooth.value = true; rotation.value = (rotation.value + 90) % 360; x.value = 0; y.value = 0; showTools() }
function wheel(event) { zoom(event.deltaY < 0 ? 1.15 : 1 / 1.15) }
function pointDistance() { const [a, b] = [...pointers.values()]; return a && b ? Math.hypot(a.x - b.x, a.y - b.y) : 0 }
function beginGesture() {
  const point = [...pointers.values()][0]
  gesture = point && { x: point.x, y: point.y, startX: x.value, startY: y.value, scale: scale.value, distance: pointDistance() }
}
function pointerDown(event) {
  if (!loaded.value || event.button !== 0) return
  stage.value.setPointerCapture(event.pointerId)
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  dragging.value = true; beginGesture()
}
function pointerMove(event) {
  if (!pointers.has(event.pointerId) || !gesture) return
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  if (pointers.size > 1 && gesture.distance > 0) {
    scale.value = Math.max(.25, Math.min(8 / Math.max(.001, original.value ? 1 : fitScale.value), gesture.scale * pointDistance() / gesture.distance))
  } else { x.value = gesture.startX + event.clientX - gesture.x; y.value = gesture.startY + event.clientY - gesture.y }
}
function pointerUp(event) { pointers.delete(event.pointerId); dragging.value = pointers.size > 0; beginGesture() }
function download() {
  const link = document.createElement('a'), url = new URL(current.value.url, location.origin)
  url.searchParams.set('download', '1')
  link.href = url.toString(); link.download = current.value.name; link.target = '_blank'; link.rel = 'noopener noreferrer'
  link.click(); showTools()
}
function key(event) {
  event.stopPropagation()
  if (event.key === 'Escape') { event.preventDefault(); emit('close'); return }
  if (event.key === 'ArrowLeft') { event.preventDefault(); navigate(-1) }
  if (event.key === 'ArrowRight') { event.preventDefault(); navigate(1) }
  if (event.key === '+' || event.key === '=') { event.preventDefault(); zoom(1.2) }
  if (event.key === '-') { event.preventDefault(); zoom(1 / 1.2) }
  if (event.key === 'Tab') {
    clearTimeout(hideTimer); dockVisible.value = true
    const nodes = [...panel.value.querySelectorAll('button:not([disabled])')]
    if (event.shiftKey && (document.activeElement === nodes[0] || document.activeElement === panel.value)) { event.preventDefault(); nodes.at(-1)?.focus() }
    else if (!event.shiftKey && document.activeElement === nodes.at(-1)) { event.preventDefault(); nodes[0]?.focus() }
  }
}
onMounted(async () => {
  previousFocus = document.activeElement; previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  await nextTick(); panel.value?.focus()
  observer = new ResizeObserver(() => { if (stage.value) bounds.value = { width: stage.value.clientWidth, height: stage.value.clientHeight } })
  observer.observe(stage.value); scheduleHide()
})
onUnmounted(() => { clearTimeout(hideTimer); observer?.disconnect(); pointers.clear(); document.body.style.overflow = previousOverflow; previousFocus?.focus() })
</script>
<template>
  <Teleport to="body">
    <section ref="panel" class="image-viewer" tabindex="-1" role="dialog" aria-modal="true" :aria-label="current?.name || '图片预览'" @keydown="key" @pointermove="showTools" @wheel.prevent="wheel">
      <header class="viewer-heading"><span :data-tooltip="current?.name">{{ current?.name }}</span><button class="viewer-close" aria-label="关闭图片预览" @click="emit('close')"><Icon name="X" :size="23" /></button></header>
      <div ref="stage" class="viewer-stage" :class="{ dragging }" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerUp" @lostpointercapture="pointerUp" @dblclick.prevent="toggleOriginal">
        <img v-if="current" :key="current.id" ref="picture" :src="current.url" :alt="current.name" :style="imageStyle" :class="{ loaded, smooth }" draggable="false" @load="imageLoaded" @error="failed = true" />
        <div v-if="!loaded || failed" class="viewer-status" role="status"><Icon :name="failed ? 'CircleAlert' : 'LoaderCircle'" :class="{ spin: !failed }" :size="28" /><p>{{ failed ? '图片加载失败' : '正在读取图片…' }}</p><button v-if="failed" @click="download">下载图片</button></div>
      </div>
      <button v-if="images.length > 1" class="viewer-edge previous" aria-label="上一张图片" @click="navigate(-1)"><Icon name="ChevronLeft" :size="34" /></button>
      <button v-if="images.length > 1" class="viewer-edge next" aria-label="下一张图片" @click="navigate(1)"><Icon name="ChevronRight" :size="34" /></button>
      <div class="viewer-dock-zone" @pointerenter="showTools" @pointerleave="scheduleHide">
        <div class="viewer-dock" :class="{ hidden: !dockVisible }" @focusin="showTools" @wheel.stop.prevent>
          <span class="viewer-counter">{{ index + 1 }} / {{ images.length }}</span><i />
          <button aria-label="缩小图片" data-tooltip="缩小" :disabled="!loaded || failed" @click="zoom(1 / 1.2)"><Icon name="ZoomOut" :size="24" /></button>
          <span class="viewer-scale">{{ loaded ? `${percent}%` : '—' }}</span>
          <button aria-label="放大图片" data-tooltip="放大" :disabled="!loaded || failed" @click="zoom(1.2)"><Icon name="ZoomIn" :size="24" /></button><i />
          <button :aria-label="original ? '适应窗口' : '原始尺寸'" :data-tooltip="original ? '适应窗口' : '原始尺寸'" :aria-pressed="original" :disabled="!loaded || failed" @click="toggleOriginal"><Icon :name="original ? 'Minimize' : 'Maximize'" :size="23" /></button>
          <button aria-label="旋转图片" data-tooltip="旋转" :disabled="!loaded || failed" @click="rotate"><Icon name="RotateCw" :size="23" /></button>
          <button aria-label="下载图片" data-tooltip="下载" @click="download"><Icon name="Download" :size="23" /></button>
        </div>
      </div>
    </section>
  </Teleport>
</template>
<style scoped>
.image-viewer { position:fixed; inset:0; z-index:200; display:flex; flex-direction:column; color:#eef1fb; background:rgba(15,17,29,.78); backdrop-filter:blur(14px) saturate(.8); overflow:hidden; overscroll-behavior:contain; outline:none; animation:viewer-in .22s ease; }
.viewer-heading { flex:none; display:flex; align-items:center; gap:20px; padding:12px 20px; height:64px; }
.viewer-heading > span { min-width:0; max-width:calc(100% - 64px); overflow:hidden; white-space:nowrap; text-overflow:ellipsis; color:#d9deee; font-size:14px; }
.image-viewer button { display:grid; place-items:center; padding:0; color:inherit; background:transparent; border:0; cursor:pointer; }
.image-viewer button:focus-visible { outline:2px solid #afbcef; outline-offset:2px; }
.viewer-close { margin-left:auto; width:40px; height:40px; flex:none; border-radius:8px; }
.viewer-close:hover { background:#ffffff12; }
.viewer-stage { flex:1; min-height:0; min-width:0; position:relative; margin:0 72px 112px; display:grid; place-items:center; touch-action:none; cursor:grab; }
.viewer-stage.dragging { cursor:grabbing; }
.viewer-stage img { position:absolute; top:50%; left:50%; display:block; max-width:none; max-height:none; user-select:none; pointer-events:none; opacity:0; transform-origin:center; transition:opacity .18s ease; }
.viewer-stage img.smooth { transition:transform .2s ease,opacity .18s ease; }
.viewer-stage img.loaded { opacity:1; }
.viewer-stage.dragging img { transition:none; }
.viewer-status { position:absolute; inset:0; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:12px; color:#d9deee; font-size:14px; }
.viewer-status p { margin:0; }.viewer-status button { padding:8px 12px; border-radius:8px; background:#ffffff14; }
.viewer-edge { position:absolute; top:64px; bottom:100px; width:64px; opacity:0; transition:opacity .18s ease; filter:drop-shadow(0 2px 6px #0009); }
.viewer-edge.previous { left:0; }.viewer-edge.next { right:0; }
.viewer-edge:hover,.viewer-edge:focus-visible { opacity:1; }
.viewer-dock-zone { position:absolute; bottom:0; left:0; right:0; height:104px; display:flex; justify-content:center; align-items:flex-end; padding-bottom:max(20px,env(safe-area-inset-bottom)); pointer-events:none; }
.viewer-dock { display:flex; align-items:center; gap:4px; padding:7px 12px; min-height:56px; border:1px solid #ffffff15; border-radius:28px; background:#191c2ae8; box-shadow:0 14px 40px #0005; backdrop-filter:blur(18px); pointer-events:auto; transition:opacity .25s ease,transform .3s ease; }
.viewer-dock.hidden { opacity:0; transform:translateY(18px); pointer-events:none; }
.viewer-dock button { width:40px; height:40px; border-radius:8px; flex:none; }
.viewer-dock button:hover,.viewer-dock button[aria-pressed=true] { background:#ffffff15; }
.viewer-dock button:disabled { opacity:.35; cursor:default; }
.viewer-dock > i { width:1px; height:24px; background:#ffffff24; margin:0 5px; }
.viewer-counter,.viewer-scale { min-width:54px; text-align:center; font-size:14px; font-variant-numeric:tabular-nums; white-space:nowrap; }
@keyframes viewer-in { from { opacity:0; } }
@media(max-width:600px) { .viewer-heading { padding:8px 12px; height:56px; }.viewer-stage { margin:0 12px 94px; }.viewer-dock { padding:6px 7px; gap:0; }.viewer-dock button { width:36px; height:38px; }.viewer-counter,.viewer-scale { min-width:43px; font-size:13px; }.viewer-dock > i { margin:0 3px; }.viewer-edge { width:38px; top:56px; opacity:.65; }.viewer-dock-zone { height:88px; } }
@media(prefers-reduced-motion:reduce) { .image-viewer,.viewer-stage img,.viewer-dock,.viewer-edge { animation:none; transition:none; } }
</style>
