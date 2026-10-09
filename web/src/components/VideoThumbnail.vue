<script>
// Bound directory browsing to three simultaneous metadata/frame reads.
const queue = []
let active = 0
function drain() {
  while (active < 3 && queue.length) { const job = queue.shift(); if (!job.cancelled) { active++; job.start(() => { active--; drain() }) } }
}
</script>
<script setup>
import { onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ url: String })
const container = ref(null), video = ref(null), ready = ref(false)
let observer, job, release, timer, visible = false, generation = 0
function cleanup() {
  generation++; if (job) job.cancelled = true
  clearTimeout(timer); release?.(); release = null
  if (video.value) { video.value.pause(); video.value.removeAttribute('src'); video.value.load() }
}
function readFrame() {
  if (!visible || !props.url || ready.value || release || job && !job.cancelled) return
  const run = generation
  job = { cancelled: false, start(done) {
    release = done
    timer = setTimeout(() => { if (run === generation) cleanup() }, 12000)
    video.value.src = props.url; video.value.load()
  } }
  queue.push(job); drain()
}
function metadata() {
  if (!release) return
  const duration = video.value.duration
  if (Number.isFinite(duration) && duration > .1) video.value.currentTime = Math.min(1, duration / 10)
}
function frame() {
  if (!release || !video.value.videoWidth || video.value.seeking) return
  ready.value = true; video.value.pause(); clearTimeout(timer); release(); release = null
}
watch(() => props.url, () => { cleanup(); job = null; ready.value = false; readFrame() })
onMounted(() => {
  observer = new IntersectionObserver(entries => {
    visible = entries[0].isIntersecting
    if (visible) readFrame()
    else if (!ready.value) { cleanup(); job = null }
  }, { rootMargin: '100px' })
  observer.observe(container.value)
})
onUnmounted(() => { observer?.disconnect(); cleanup() })
</script>
<template>
  <span ref="container" class="video-thumbnail" aria-hidden="true">
    <video ref="video" :class="{ ready }" muted playsinline preload="metadata" @loadedmetadata="metadata" @seeked="frame" @loadeddata="frame" @error="cleanup" />
    <Icon v-if="!ready" name="FileVideo" :size="38" />
    <span v-if="ready" class="thumbnail-play"><Icon name="Play" :size="12" /></span>
  </span>
</template>
<style scoped>
.video-thumbnail { position:relative; display:grid; place-items:center; width:76px; height:64px; flex:none; color:var(--muted); }
.video-thumbnail video { position:absolute; inset:0; width:100%; height:100%; object-fit:cover; border-radius:6px; opacity:0; pointer-events:none; }
.video-thumbnail video.ready { opacity:1; }
.thumbnail-play { position:absolute; bottom:3px; right:3px; display:grid; place-items:center; width:20px; height:20px; border-radius:5px; color:white; background:#141823b3; pointer-events:none; }
</style>
