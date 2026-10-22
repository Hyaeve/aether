<script>
const queue = []
let active = 0
function drain() {
  while (active < 3 && queue.length) {
    const job = queue.shift()
    if (job.cancelled) continue
    active++
    let finished = false
    job.start(() => { if (!finished) { finished = true; active--; drain() } })
  }
}
</script>
<script setup>
import { onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ url: String, storage: String, id: String, parent: String, type: { type: String, default: 'image' } })
const container = ref(null), video = ref(null), ready = ref(false), thumbnail = ref('')
let observer, visible = false, generation = 0, job, release, timer, controller, sourceImage, objectURL
let frameCallback = 0, paintCallback = 0, frameVideo
function endpoint() {
  if (!props.storage || !props.id) return ''
  const query = new URLSearchParams({ storage: props.storage, id: props.id })
  if (props.parent) query.set('parent', props.parent)
  return `/api/files/thumbnail?${query}`
}
function source() {
  const api = endpoint()
  if (api) return api.replace('/thumbnail?', '/thumbnail-source?')
  try {
    const url = new URL(props.url, location.origin)
    return url.origin === location.origin && /^\/(stream\/|api\/)/.test(url.pathname) ? url.href : ''
  } catch { return '' }
}
function cancelFrame() {
  if (frameCallback) frameVideo?.cancelVideoFrameCallback(frameCallback)
  if (paintCallback) cancelAnimationFrame(paintCallback)
  frameCallback = paintCallback = 0; frameVideo = null
}
function finish() { clearTimeout(timer); cancelFrame(); release?.(); release = null }
function cleanup() {
  generation++
  if (job) job.cancelled = true
  controller?.abort(); controller = null
  if (sourceImage) { sourceImage.onload = sourceImage.onerror = null; sourceImage.src = ''; sourceImage = null }
  if (video.value) { video.value.pause(); video.value.removeAttribute('src'); video.value.load() }
  if (objectURL) URL.revokeObjectURL(objectURL)
  objectURL = null; thumbnail.value = ''; ready.value = false
  finish()
}
function display(blob, run) {
  if (run !== generation) return
  if (objectURL) URL.revokeObjectURL(objectURL)
  objectURL = URL.createObjectURL(blob); thumbnail.value = objectURL; ready.value = true
}
async function capture(media, run, api, revision) {
  if (run !== generation || !release) return
  const width = media.videoWidth || media.naturalWidth, height = media.videoHeight || media.naturalHeight
  if (!width || !height) return
  try {
    const scale = Math.min(1, 320 / Math.max(width, height))
    const canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(width * scale)); canvas.height = Math.max(1, Math.round(height * scale))
    canvas.getContext('2d').drawImage(media, 0, 0, canvas.width, canvas.height)
    const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', .8))
    if (run !== generation || !blob || blob.size > 512 * 1024) return
    display(blob, run)
    if (video.value) { video.value.pause(); video.value.removeAttribute('src'); video.value.load() }
    if (api && revision) {
      const response = await fetch(api, { method: 'POST', credentials: 'same-origin', signal: controller.signal,
        headers: { 'Content-Type': 'image/jpeg', 'X-Thumbnail-Revision': revision }, body: blob })
      if (run !== generation) return
      if (response.ok) display(await response.blob(), run)
      else if ([401, 403, 409].includes(response.status)) { cleanup(); return }
    }
  } catch { /* Unsupported codecs or tainted frames retain the file icon. */ }
  finally { if (run === generation) finish() }
}
let frameRun = 0, frameAPI = '', frameRevision = '', capturing = false
function metadata() {
  if (!release || !video.value) return
  const duration = video.value.duration
  if (Number.isFinite(duration) && duration > .1) video.value.currentTime = Math.min(1, duration / 10)
}
function frame() {
  if (!release || capturing || !video.value?.videoWidth || video.value.seeking) return
  capturing = true
  const media = video.value, run = frameRun
  frameVideo = media
  const present = () => {
    frameCallback = paintCallback = 0; frameVideo = null
    if (run !== generation || !release || video.value !== media) return
    if (media.seeking || media.readyState < 2) { capturing = false; return }
    media.pause()
    capture(media, run, frameAPI, frameRevision)
  }
  // loadeddata/seeked can precede presentation, especially with an async decoder.
  if (media.requestVideoFrameCallback) frameCallback = media.requestVideoFrameCallback(present)
  const playing = media.play()
  if (!media.requestVideoFrameCallback) {
    const afterPlayback = () => {
      if (run !== generation || !release) return
      paintCallback = requestAnimationFrame(() => { paintCallback = requestAnimationFrame(present) })
    }
    playing?.then(afterPlayback).catch(() => { if (run === generation) finish() })
  } else playing?.catch(() => { /* A paused seek can still deliver its frame callback. */ })
}
function load() {
  if (!visible || ready.value || release || job && !job.cancelled) return
  const run = generation
  job = { cancelled: false, async start(done) {
    release = done; controller = new AbortController()
    timer = setTimeout(() => { if (run === generation) cleanup() }, 20000)
    const api = endpoint()
    let revision = ''
    try {
      if (api) {
        const response = await fetch(api, { credentials: 'same-origin', signal: controller.signal })
        if (run !== generation) return
        if (response.ok) {
          const blob = await response.blob()
          if (run !== generation) return
          display(blob, run); finish(); return
        }
        if (response.status !== 404) { finish(); return }
        revision = response.headers.get('X-Thumbnail-Revision') || ''
        if (!revision) { finish(); return }
      }
      if (run !== generation) return
      const url = source()
      if (!url) { finish(); return }
      if (props.type === 'video') {
        frameRun = run; frameAPI = api; frameRevision = revision; capturing = false
        video.value.src = url; video.value.load()
      } else {
        sourceImage = new Image()
        sourceImage.onload = () => capture(sourceImage, run, api, revision)
        sourceImage.onerror = () => { if (run === generation) finish() }
        sourceImage.src = url
      }
    } catch { if (run === generation) finish() }
  } }
  queue.push(job); drain()
}
watch(() => [props.url, props.storage, props.id, props.parent, props.type], () => { cleanup(); job = null; load() }, { flush: 'post' })
onMounted(() => {
  observer = new IntersectionObserver(entries => {
    visible = entries[0].isIntersecting
    if (visible) load()
    else if (!ready.value) { cleanup(); job = null }
  }, { rootMargin: '100px' })
  observer.observe(container.value)
})
onUnmounted(() => { observer?.disconnect(); cleanup() })
</script>
<template>
  <span ref="container" class="media-thumbnail" :class="{ 'video-thumbnail': type === 'video', 'file-thumbnail': type === 'image' }" aria-hidden="true">
    <img v-if="thumbnail" :src="thumbnail" alt="" />
    <video v-if="type === 'video'" ref="video" muted playsinline preload="metadata" @loadedmetadata="metadata" @seeked="frame" @loadeddata="frame" @error="finish" />
    <Icon v-if="!ready" :name="type === 'video' ? 'FileVideo' : 'FileImage'" :size="38" />
    <span v-if="ready && type === 'video'" class="thumbnail-play"><Icon name="Play" :size="12" /></span>
  </span>
</template>
<style scoped>
.media-thumbnail { position:relative; display:grid; place-items:center; width:76px; height:64px; flex:none; color:var(--muted); }
.media-thumbnail img { position:absolute; inset:0; width:100%; height:100%; object-fit:cover; border-radius:6px; pointer-events:none; }
.media-thumbnail video { position:absolute; width:1px; height:1px; opacity:0; pointer-events:none; }
.thumbnail-play { position:absolute; bottom:3px; right:3px; display:grid; place-items:center; width:20px; height:20px; border-radius:5px; color:white; background:#141823b3; pointer-events:none; }
</style>
