<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ file: Object })
const emit = defineEmits(['close'])
const panel = ref(null), player = ref(null), failed = ref(false), tools = ref(true)
const ratio = ref(16 / 9), viewport = ref({ width: innerWidth, height: innerHeight })
const dimensions = computed(() => {
  const width = Math.max(1, Math.min(1080, viewport.value.width - 32, (viewport.value.height - 48) * ratio.value))
  return { width: `${width}px`, height: `${width / ratio.value}px` }
})
let previousFocus, previousOverflow, hideTimer
function resize() { viewport.value = { width: innerWidth, height: innerHeight } }
function metadata() { if (player.value.videoWidth && player.value.videoHeight) ratio.value = player.value.videoWidth / player.value.videoHeight }
function showTools() {
  tools.value = true; clearTimeout(hideTimer)
  hideTimer = setTimeout(() => { if (!panel.value?.querySelector('button:focus')) tools.value = false }, 2500)
}
function key(event) {
  event.stopPropagation()
  if (event.key === 'Escape') { event.preventDefault(); emit('close') }
  if (event.key === 'Tab') {
    showTools()
    const nodes = [...panel.value.querySelectorAll('video,button,a[href]')]
    if (event.shiftKey && (document.activeElement === nodes[0] || document.activeElement === panel.value)) { event.preventDefault(); nodes.at(-1)?.focus() }
    else if (!event.shiftKey && document.activeElement === nodes.at(-1)) { event.preventDefault(); nodes[0]?.focus() }
  }
}
onMounted(async () => {
  previousFocus = document.activeElement; previousOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden'
  window.addEventListener('resize', resize)
  await nextTick(); panel.value?.focus(); showTools()
})
onUnmounted(() => {
  player.value?.pause(); clearTimeout(hideTimer); window.removeEventListener('resize', resize)
  document.body.style.overflow = previousOverflow; previousFocus?.focus()
})
</script>
<template>
  <Teleport to="body">
    <div class="video-backdrop" @pointerdown.self="emit('close')">
      <section ref="panel" class="video-viewer" :style="dimensions" role="dialog" aria-modal="true" :aria-label="file.name" tabindex="-1" @keydown="key" @pointermove="showTools" @focusin="showTools">
        <video ref="player" :src="file.url" tabindex="0" controls playsinline preload="metadata" @loadedmetadata="metadata" @error="failed = true" />
        <header class="video-overlay" :class="{ hidden: !tools && !failed }"><span>{{ file.name }}</span><button aria-label="关闭视频预览" @click="emit('close')"><Icon name="X" :size="21" /></button></header>
        <div v-if="failed" class="video-error" role="status"><Icon name="FileVideo" :size="30" /><p>浏览器无法播放此格式，请下载后打开。</p><a :href="`${file.url}${file.url.includes('?') ? '&' : '?'}download=1`" :download="file.name" target="_blank" rel="noopener noreferrer"><Icon name="Download" :size="18" />下载视频</a></div>
      </section>
    </div>
  </Teleport>
</template>
<style scoped>
.video-backdrop { position:fixed; inset:0; z-index:200; display:grid; place-items:center; background:#090d18ba; backdrop-filter:blur(10px); overscroll-behavior:contain; }
.video-viewer { position:relative; overflow:hidden; border-radius:10px; background:#000; box-shadow:0 20px 80px #0008; outline:none; animation:video-enter .2s ease; }
.video-viewer > video { display:block; width:100%; height:100%; object-fit:contain; }
.video-overlay { position:absolute; top:0; left:0; right:0; display:flex; align-items:center; gap:12px; padding:10px 12px; color:#f1f4ff; background:linear-gradient(#080b16ae,transparent); pointer-events:none; transition:opacity .2s ease; }
.video-overlay.hidden { opacity:0; }.video-overlay span { min-width:0; flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:14px; text-shadow:0 1px 4px #000; }
.video-overlay button { display:grid; place-items:center; width:34px; height:34px; padding:0; border:0; border-radius:7px; background:#17203288; color:inherit; pointer-events:auto; cursor:pointer; }
.video-overlay button:hover { background:#34415fba; }.video-overlay button:focus-visible { outline:2px solid #b7c4fb; outline-offset:-2px; }
.video-error { position:absolute; inset:52px 8px 42px; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:12px; color:#dce2f2; text-align:center; font-size:14px; }
.video-error p { margin:0; }.video-error a { display:flex; align-items:center; gap:7px; color:#b6c7ff; text-decoration:none; }
@keyframes video-enter { from { opacity:0; transform:scale(.98); } }
@media(prefers-reduced-motion:reduce) { .video-viewer { animation:none; }.video-overlay { transition:none; } }
</style>
