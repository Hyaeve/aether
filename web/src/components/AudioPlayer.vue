<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
import ThinScroll from './ThinScroll.vue'
import VirtualList from './VirtualList.vue'
import { audioSource, readAudioMetadata, readLRC } from '../audio-metadata'
const props = defineProps({ file: Object, queue: { type: Array, default: () => [] }, activation: Number })
const emit = defineEmits(['close', 'change'])
const panel = ref(null), player = ref(null), failed = ref(false), playing = ref(false)
const cover = ref(''), metadata = ref({}), lyrics = ref([]), loading = ref(false), position = ref(0), duration = ref(0), lyricsPanel = ref(null)
const expanded = ref(true), view = ref('cover'), mode = ref('order'), capsule = ref(null)
const more = ref(false), volumeOpen = ref(false), volume = ref(1)
const isDragging = ref(false)
const dock = ref({ side: 'right', y: Math.max(56, window.innerHeight * .65), x: null })
const modes = [{ value: 'order', label: '顺序播放', icon: 'ListOrdered' }, { value: 'shuffle', label: '随机播放', icon: 'Shuffle' }, { value: 'loop', label: '列表循环', icon: 'Repeat' }, { value: 'single', label: '单曲循环', icon: 'Repeat1' }]
const activeMode = computed(() => modes.find(item => item.value === mode.value))
const tracks = computed(() => props.queue.length ? props.queue : [props.file])
const trackIndex = computed(() => tracks.value.findIndex(file => file.id === props.file.id && file.storage === props.file.storage))
const canSkip = computed(() => tracks.value.length > 1)
const title = computed(() => metadata.value.title || props.file.name)
const bitrate = computed(() => Number.isFinite(metadata.value.bitrate) && metadata.value.bitrate > 0 ? Math.round(metadata.value.bitrate / 1000) : null)
const seekable = computed(() => Number.isFinite(duration.value) && duration.value > 0 && player.value?.readyState >= 1)
const dockStyle = computed(() => ({ top: `${dock.value.y}px`, left: dock.value.x !== null ? `${dock.value.x}px` : dock.value.side === 'left' ? '8px' : 'auto', right: dock.value.x !== null || dock.value.side === 'left' ? 'auto' : '8px' }))
let previousFocus, controller, artworkURL, generation = 0, deadline, drag, dragged = false
let shuffleBag = [], shuffleHistory = []
const activeLine = computed(() => {
  let index = -1
  for (let i = 0; i < lyrics.value.length; i++) if (lyrics.value[i].time !== null && lyrics.value[i].time <= position.value) index = i
  return index
})
const currentLyric = computed(() => lyrics.value[activeLine.value]?.text || lyrics.value[0]?.text || (loading.value ? '正在读取音乐资料…' : '暂无歌词'))
function time(value) {
  const seconds = Number.isFinite(value) ? Math.max(0, Math.floor(value)) : 0
  return `${Math.floor(seconds / 60).toString().padStart(2, '0')}:${(seconds % 60).toString().padStart(2, '0')}`
}
async function followLyric() {
  await nextTick()
  const area = lyricsPanel.value?.element, line = area?.querySelector(`[data-line="${activeLine.value}"]`)
  if (line) area.scrollTo({ top: line.offsetTop - area.offsetTop - area.clientHeight / 2 + line.clientHeight / 2, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' })
}
watch([activeLine, view, expanded, lyricsPanel], followLyric)
async function loadDetails(file) {
  const current = ++generation
  controller?.abort(); clearTimeout(deadline)
  if (artworkURL) URL.revokeObjectURL(artworkURL)
  artworkURL = ''; cover.value = ''; metadata.value = {}; lyrics.value = []; position.value = 0; duration.value = 0; failed.value = false
  controller = new AbortController()
  const signal = controller.signal
  deadline = setTimeout(() => controller?.abort(), 20000)
  const stem = file.name.replace(/\.[^.]+$/, '').toLowerCase(), companions = file.companions || []
  const lyric = companions.find(f => f.name.toLowerCase() === `${stem}.lrc`)
  const artwork = companions.find(f => /\.(?:jpe?g|png|webp)$/i.test(f.name) && f.name.replace(/\.[^.]+$/, '').toLowerCase() === stem) || companions.find(f => /^(?:cover|folder|front)\.(?:jpe?g|png|webp)$/i.test(f.name))
  if (artwork) cover.value = audioSource({ ...file, id: artwork.id })
  loading.value = true
  await Promise.allSettled([
    readAudioMetadata(file, signal).then(data => {
      if (signal.aborted || current !== generation) return
      metadata.value = data
      if (data.picture) { artworkURL = URL.createObjectURL(data.picture); cover.value = artworkURL }
      if (!lyrics.value.length) lyrics.value = data.lines
    }),
    lyric ? readLRC(audioSource({ ...file, id: lyric.id }), signal).then(lines => { if (!signal.aborted && current === generation && lines.length) lyrics.value = lines }) : Promise.resolve()
  ])
  if (current === generation) { loading.value = false; clearTimeout(deadline) }
}
function syncTime() { position.value = player.value?.currentTime || 0 }
function syncDuration() { const value = player.value?.duration; duration.value = Number.isFinite(value) ? value : 0 }
function seek(value) {
  if (!seekable.value) return
  player.value.currentTime = Math.min(duration.value, Math.max(0, Number(value)))
  syncTime()
}
async function play() {
  const audio = player.value
  if (!audio) return
  const current = generation
  try { await audio.play() } catch { if (current === generation) playing.value = !audio.paused }
}
function togglePlayback() { if (player.value?.paused) play(); else player.value?.pause() }
function setVolume(value) { volume.value = Math.max(0, Math.min(1, Number(value))); if (player.value) player.value.volume = volume.value }
function volumeWheel(event) { setVolume(volume.value + (event.deltaY < 0 ? .05 : -.05)) }
function cycleMode() {
  mode.value = modes[(modes.findIndex(item => item.value === mode.value) + 1) % modes.length].value
  shuffleBag = []; shuffleHistory = []
}
function selectTrack(index) {
  const file = tracks.value[index]
  if (!file) return
  if (index === trackIndex.value) { seek(0); play(); return }
  emit('change', file)
}
function skip(direction, automatic = false) {
  if (automatic && mode.value === 'single') { seek(0); play(); return }
  if (!canSkip.value) {
    if (!automatic || mode.value !== 'order') { seek(0); play() }
    return
  }
  const current = Math.max(0, trackIndex.value), count = tracks.value.length
  let next
  if (mode.value === 'shuffle') {
    if (direction < 0 && shuffleHistory.length) next = shuffleHistory.pop()
    else {
      shuffleBag = shuffleBag.filter(i => i !== current)
      if (!shuffleBag.length) {
        shuffleBag = Array.from({ length: count }, (_, i) => i).filter(i => i !== current)
        for (let i = shuffleBag.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [shuffleBag[i], shuffleBag[j]] = [shuffleBag[j], shuffleBag[i]] }
      }
      next = shuffleBag.pop()
      shuffleHistory.push(current); if (shuffleHistory.length > 200) shuffleHistory.shift()
    }
  } else {
    if (automatic && mode.value === 'order' && current === count - 1) return
    next = (current + direction + count) % count
  }
  selectTrack(next)
}
async function collapse(focus = true) { more.value = false; volumeOpen.value = false; expanded.value = false; if (focus) { await nextTick(); capsule.value?.focus() } }
async function reopen() { if (dragged) { dragged = false; return }; expanded.value = true; await nextTick(); panel.value?.focus() }
function outside(event) { if (expanded.value && !event.target.closest('.audio-player, .audio-capsule')) collapse(false) }
function key(event) {
  event.stopPropagation()
  if (event.key === 'Escape') { event.preventDefault(); emit('close') }
  if (event.key === ' ' && event.target === panel.value) { event.preventDefault(); togglePlayback() }
}
function constrainDock() { dock.value.y = Math.min(Math.max(56, dock.value.y), Math.max(56, window.innerHeight - 56)); dock.value.x = null }
function startDrag(event) {
  if (event.button !== 0 || event.target.closest('.capsule-toggle')) return
  const rect = capsule.value.getBoundingClientRect()
  drag = { x: event.clientX, y: event.clientY, left: rect.left, top: rect.top, width: rect.width }
  isDragging.value = true
  dragged = false
  event.currentTarget.setPointerCapture(event.pointerId)
}
function moveDrag(event) {
  if (!drag) return
  const dx = event.clientX - drag.x, dy = event.clientY - drag.y
  if (!dragged && Math.hypot(dx, dy) < 5) return
  dragged = true
  dock.value.x = Math.max(8, Math.min(window.innerWidth - drag.width - 8, drag.left + dx))
  dock.value.y = Math.max(56, Math.min(window.innerHeight - 56, drag.top + dy))
}
function endDrag() {
  if (dragged && drag) dock.value.side = (dock.value.x ?? drag.left) + drag.width / 2 < window.innerWidth / 2 ? 'left' : 'right'
  drag = null; dock.value.x = null; isDragging.value = false
}
function dockKey(event) {
  if (event.key === 'Escape') { event.preventDefault(); emit('close'); return }
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return
  event.preventDefault()
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') dock.value.side = event.key === 'ArrowLeft' ? 'left' : 'right'
  else dock.value.y += event.key === 'ArrowUp' ? -24 : 24
  constrainDock()
}
watch(() => props.file, async file => { loadDetails(file); await nextTick(); if (props.file !== file || !player.value) return; player.value.load(); play() }, { immediate: true })
watch(() => props.activation, () => { expanded.value = true; view.value = 'cover'; shuffleBag = []; shuffleHistory = [] })
onMounted(async () => {
  previousFocus = document.activeElement
  document.addEventListener('pointerdown', outside)
  window.addEventListener('resize', constrainDock)
  constrainDock(); await nextTick(); panel.value?.focus(); play()
})
onBeforeUnmount(() => {
  generation++; controller?.abort(); clearTimeout(deadline)
  document.removeEventListener('pointerdown', outside); window.removeEventListener('resize', constrainDock)
  if (artworkURL) URL.revokeObjectURL(artworkURL)
  player.value?.pause(); player.value?.removeAttribute('src'); player.value?.load()
})
onUnmounted(() => { if (previousFocus?.isConnected) previousFocus.focus() })
</script>
<template>
  <Teleport to="body">
    <div class="audio-player-shell">
      <Transition name="audio-slide" appear>
      <section v-show="expanded" ref="panel" class="audio-player" :class="{ 'has-artwork': cover }" role="dialog" :aria-label="file.name" tabindex="-1" @keydown="key">
        <img v-if="cover" class="audio-ambient" :src="cover" alt="" aria-hidden="true" />
        <div class="audio-shade" aria-hidden="true" />
        <header><button class="icon-btn" aria-label="收起音频播放" @click="collapse()"><Icon name="ChevronRight" /></button><span>音频播放</span><button class="icon-btn" aria-label="关闭音频播放" @click="emit('close')"><Icon name="X" /></button></header>
        <div class="audio-stage">
          <Transition name="audio-view" mode="out-in">
            <ThinScroll v-if="view === 'cover'" key="cover" class="audio-cover-view">
              <button class="audio-art" aria-label="切换到歌词页" @click="view = 'lyrics'"><img v-if="cover" :src="cover" :alt="`${metadata.album || title} 封面`" @error="cover = ''" /><template v-else><Icon name="FileAudio2" :size="70" /><div class="audio-wave" aria-hidden="true"><i v-for="n in 15" :key="n" :style="{ height: `${12 + (n * 17 % 31)}px` }" /></div></template></button>
              <div class="audio-cover-lyrics"><template v-if="lyrics.length"><button v-for="(line, i) in lyrics.slice(Math.max(0, activeLine), Math.max(0, activeLine) + 2)" :key="i" class="lyric-line" :aria-current="line === lyrics[activeLine] ? 'true' : undefined" @click="line.time !== null && seek(line.time)">{{ line.text }}</button></template><p v-else class="audio-lyrics-empty">{{ currentLyric }}</p></div>
            </ThinScroll>
            <div v-else-if="view === 'lyrics'" key="lyrics" class="audio-lyric-view"><button class="audio-view-back" aria-label="返回封面" @click="view = 'cover'"><Icon name="ChevronLeft" :size="17" />{{ metadata.artist || title }}</button><ThinScroll v-if="lyrics.length" ref="lyricsPanel" class="audio-lyrics" content-class="audio-lyrics-lines"><template v-for="(line, i) in lyrics" :key="i"><button v-if="line.time !== null" class="lyric-line" :class="{ current: activeLine === i }" :aria-current="activeLine === i ? 'true' : undefined" :data-line="i" @click="seek(line.time)">{{ line.text }}</button><p v-else class="lyric-line" :data-line="i">{{ line.text }}</p></template></ThinScroll><p v-else class="audio-lyrics-empty">{{ currentLyric }}</p></div>
            <div v-else key="queue" class="audio-queue-view"><button class="audio-view-back" @click="view = 'cover'"><Icon name="ChevronLeft" :size="17" />歌曲列表 · {{ tracks.length }}</button><VirtualList :items="tracks" :row-height="52" role="listbox" aria-label="歌曲列表"><template #default="{ item, index }"><button class="audio-track" role="option" :aria-selected="index === trackIndex" @click="selectTrack(index); view = 'cover'"><Icon :name="index === trackIndex && playing ? 'Music' : 'FileAudio2'" :size="19" /><span>{{ item.name }}</span><Icon v-if="index === trackIndex" name="Check" :size="16" /></button></template></VirtualList></div>
          </Transition>
        </div>
        <div class="audio-info"><div><h2>{{ title }}</h2><p v-if="metadata.artist || metadata.album" class="audio-artist">{{ [metadata.artist, metadata.album].filter(Boolean).join(' · ') }}</p></div><div class="audio-more"><button class="icon-btn" aria-label="音频操作" :aria-expanded="more" @click="more = !more; volumeOpen = false"><Icon name="Ellipsis" /></button><div v-if="more" class="audio-popover" role="menu"><a role="menuitem" :href="`${file.url}${file.url.includes('?') ? '&' : '?'}download=1`" :download="file.name"><Icon name="Download" :size="18" />下载</a><button role="menuitem" @click="view = 'queue'; more = false"><Icon name="List" :size="18" />歌曲列表</button></div></div></div>
        <div class="audio-timeline"><input type="range" aria-label="播放进度" :aria-valuetext="`${time(position)} / ${time(duration)}`" min="0" :max="duration || 1" step="0.1" :value="position" :disabled="!seekable" :style="{ '--played': `${duration ? position / duration * 100 : 0}%` }" @input="seek($event.target.value)" /><div class="audio-time-row"><time>{{ time(position) }}</time><span class="audio-format">{{ file.name.split('.').at(-1).toUpperCase() }}<template v-if="bitrate"> · {{ bitrate }} kbps</template></span><time>{{ time(duration) }}</time></div></div>
        <div class="audio-controls"><button class="icon-btn audio-mode" :aria-label="`播放模式：${activeMode.label}`" @click="cycleMode"><Icon :name="activeMode.icon" :size="22" /></button><button class="icon-btn" aria-label="上一首" @click="skip(-1)"><Icon name="SkipBack" :size="25" /></button><button class="icon-btn audio-play-toggle" :aria-label="playing ? '暂停' : '播放'" :disabled="failed" @click="togglePlayback"><Icon :name="playing ? 'Pause' : 'Play'" :size="32" /></button><button class="icon-btn" aria-label="下一首" @click="skip(1)"><Icon name="SkipForward" :size="25" /></button><div class="audio-volume" @wheel.stop.prevent="volumeWheel"><button class="icon-btn" aria-label="音量" :aria-expanded="volumeOpen" @click="volumeOpen = !volumeOpen; more = false"><Icon :name="volume ? 'Volume2' : 'VolumeX'" :size="23" /></button><div v-if="volumeOpen" class="audio-popover volume-popover"><input type="range" aria-label="音量大小" min="0" max="1" step="0.01" :value="volume" @input="setVolume($event.target.value)" /><small>{{ Math.round(volume * 100) }}%</small></div></div></div>
        <p v-if="failed" class="audio-error" role="status">浏览器无法播放此格式，请下载后打开。</p>
        <a v-if="failed" :href="`${file.url}${file.url.includes('?') ? '&' : '?'}download=1`" :download="file.name" target="_blank" rel="noopener noreferrer"><Icon name="Download" :size="17" />下载音频</a>
        <audio ref="player" :src="file.url" class="audio-native" tabindex="-1" aria-hidden="true" preload="metadata" @timeupdate="syncTime" @seeked="syncTime" @loadedmetadata="syncDuration" @durationchange="syncDuration" @play="playing = true" @pause="playing = false" @ended="skip(1, true)" @error="failed = true; playing = false" />
      </section>
      </Transition>
      <div v-if="!expanded" ref="capsule" class="audio-capsule" :class="{ dragging: isDragging, 'dock-left': dock.side === 'left', 'has-artwork': cover, playing }" :style="dockStyle" role="button" tabindex="0" :aria-label="`展开音乐播放器：${title}`" @pointerdown="startDrag" @pointermove="moveDrag" @pointerup="endDrag" @pointercancel="endDrag" @lostpointercapture="endDrag" @keydown="dockKey" @keydown.enter.prevent="reopen" @keydown.space.prevent="reopen" @click="reopen"><img v-if="cover" class="capsule-ambient" :src="cover" alt="" /><img v-if="cover" class="capsule-cover" :src="cover" alt="" /><Icon v-else name="Music" :size="24" /><span>{{ title }}{{ metadata.artist ? ` - ${metadata.artist}` : '' }}</span><button class="icon-btn capsule-toggle" :aria-label="playing ? '暂停' : '播放'" @click.stop="togglePlayback" @keydown.stop><Icon :name="playing ? 'Pause' : 'Play'" :size="19" /></button></div>
    </div>
  </Teleport>
</template>
<style scoped>
.audio-player-shell { position:fixed; inset:48px 0 0; z-index:190; display:flex; justify-content:flex-end; pointer-events:none; }
.audio-player { position:relative; isolation:isolate; display:flex; flex-direction:column; gap:12px; width:min(380px,100%); height:100%; padding:16px 24px 24px; background:var(--surface); color:var(--text); border-left:1px solid var(--border); box-shadow:-12px 0 50px #0b173321; pointer-events:auto; overflow:hidden; outline:none; }
.audio-player.has-artwork { --text:#f5f7f8; --muted:#c6ced0; --primary:#eef3f6; --primary-soft:#ffffff18; --border:#ffffff18; background:#182429; }
.audio-ambient { position:absolute; z-index:-2; inset:-36px; width:calc(100% + 72px); height:calc(100% + 72px); object-fit:cover; filter:blur(30px); opacity:.48; pointer-events:none; }
.audio-shade { position:absolute; z-index:-1; inset:0; background:linear-gradient(180deg,#0b192424,#11202535 35%,#111e27aa 80%); pointer-events:none; }.audio-player:not(.has-artwork) .audio-shade { display:none; }
.audio-player header { display:flex; align-items:center; justify-content:space-between; width:100%; font-size:13px; flex-shrink:0; }.audio-player header .icon-btn { width:32px; height:32px; }
.audio-stage { position:relative; flex:1; min-height:0; }.audio-cover-view, .audio-lyric-view, .audio-queue-view { height:100%; min-height:0; }.audio-lyric-view, .audio-queue-view { display:flex; flex-direction:column; gap:10px; }
.audio-art { display:flex; flex-direction:column; justify-content:center; align-items:center; gap:30px; width:100%; aspect-ratio:1; color:var(--primary); background:color-mix(in srgb,var(--primary-soft) 45%,var(--bg)); border:0; border-radius:8px; padding:0; overflow:hidden; cursor:pointer; }.audio-art img { width:100%; height:100%; object-fit:cover; }
.audio-wave { height:44px; display:flex; align-items:center; gap:5px; }.audio-wave i { display:block; width:3px; border-radius:2px; background:currentColor; opacity:.42; }
.audio-info { width:100%; flex-shrink:0; }.audio-player h2 { font-size:18px; font-weight:600; line-height:1.4; margin:0; width:100%; overflow-wrap:anywhere; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; }.audio-player .audio-artist { margin:5px 0 0; font-size:13px; color:var(--muted); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.audio-timeline { flex-shrink:0; width:100%; }.audio-timeline input { appearance:none; display:block; width:100%; height:24px; margin:0; padding:0; background:none; border:0; box-shadow:none; cursor:pointer; }.audio-timeline input::-webkit-slider-runnable-track { height:5px; border-radius:4px; background:linear-gradient(to right,var(--primary) var(--played),color-mix(in srgb,var(--muted) 28%,transparent) var(--played)); }.audio-timeline input::-webkit-slider-thumb { appearance:none; width:12px; height:12px; margin-top:-3.5px; border-radius:50%; background:var(--primary); border:0; }.audio-timeline input::-moz-range-track { height:5px; border-radius:4px; background:color-mix(in srgb,var(--muted) 28%,transparent); }.audio-timeline input::-moz-range-progress { height:5px; border-radius:4px; background:var(--primary); }.audio-timeline input::-moz-range-thumb { width:12px; height:12px; border:0; border-radius:50%; background:var(--primary); }.audio-timeline input:focus-visible { outline:2px solid var(--primary); outline-offset:2px; }.audio-timeline input:disabled { cursor:default; opacity:.4; }
.audio-time-row { display:flex; align-items:center; justify-content:space-between; gap:8px; color:var(--muted); font-size:12px; font-variant-numeric:tabular-nums; }.audio-format { text-align:center; }
.audio-controls { display:flex; justify-content:space-between; align-items:center; gap:8px; flex-shrink:0; }.audio-controls .icon-btn { width:44px; height:44px; color:var(--text); }.audio-controls .audio-play-toggle { width:56px; height:56px; }.audio-controls button:disabled { opacity:.3; }.audio-controls button:focus-visible, .audio-art:focus-visible, .audio-capsule:focus-visible, .audio-view-back:focus-visible { outline:2px solid var(--primary); outline-offset:2px; }
.audio-lyrics { width:100%; flex:1; min-height:0; }.audio-lyrics :deep(.thin-scroll-area) { position:relative; padding-block:60px; }.lyric-line { display:block; width:100%; margin:0; padding:12px 4px; border:0; border-radius:0; background:none; font-size:16px; font-weight:400; line-height:1.7; color:var(--muted); text-align:center; overflow-wrap:anywhere; transition:color .2s; }.audio-cover-lyrics { padding-top:14px; }.audio-cover-lyrics .lyric-line { font-size:14px; padding:8px 4px; }.lyric-line.current, .lyric-line[aria-current=true] { color:var(--text); font-weight:600; }.lyric-line:hover { color:var(--text); }.audio-lyrics-empty { font-size:13px; color:var(--muted); text-align:center; margin:auto; padding:24px 0; }
.audio-view-back { display:flex; align-items:center; gap:6px; flex-shrink:0; border:0; background:none; color:var(--muted); font-size:13px; text-align:left; padding:6px 0; }.audio-view-back:hover { color:var(--text); }.audio-track { display:flex; align-items:center; gap:12px; padding:8px 10px; border:0; border-radius:6px; background:none; color:var(--muted); text-align:left; font-size:14px; }.audio-track span { flex:1; }.audio-track svg { flex-shrink:0; }.audio-track:hover, .audio-track[aria-selected=true] { color:var(--text); background:var(--primary-soft); }
.audio-player .audio-error { margin:0; font-size:12px; color:var(--muted); }.audio-player a { display:flex; align-items:center; justify-content:center; gap:7px; color:var(--primary); text-decoration:none; font-size:13px; }.audio-native { position:absolute; width:1px; height:1px; opacity:0; pointer-events:none; }
.audio-capsule { position:fixed; display:flex; align-items:center; gap:9px; width:156px; height:48px; padding:6px 12px 6px 6px; border:1px solid color-mix(in srgb,var(--primary) 20%,var(--border)); border-radius:999px; background:var(--surface); color:var(--text); box-shadow:0 5px 24px #0b173325; pointer-events:auto; touch-action:none; user-select:none; cursor:grab; transition:box-shadow .2s; }.audio-capsule.dragging { cursor:grabbing; }.audio-capsule > img { flex-shrink:0; width:34px; height:34px; border-radius:50%; object-fit:cover; }.audio-capsule > svg { flex-shrink:0; }.audio-capsule > span { font-size:12px; flex:1; min-width:0; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }.audio-capsule:hover { box-shadow:0 5px 24px #0b173343; }
.audio-slide-enter-active, .audio-slide-leave-active { transition:transform .28s cubic-bezier(.2,.8,.2,1),opacity .28s; }.audio-slide-enter-from, .audio-slide-leave-to { transform:translateX(100%); opacity:.6; }.audio-view-enter-active, .audio-view-leave-active { transition:opacity .15s,transform .15s; }.audio-view-enter-from { opacity:0; transform:translateY(6px); }.audio-view-leave-to { opacity:0; transform:translateY(-6px); }
@media(max-height:700px) { .audio-player { gap:8px; padding:12px 20px; }.audio-art { max-width:220px; margin-inline:auto; }.audio-cover-lyrics .lyric-line { padding-block:5px; }.audio-controls .audio-play-toggle { height:48px; }.audio-player h2 { font-size:16px; } }
@media(prefers-reduced-motion:reduce) { .audio-slide-enter-active, .audio-slide-leave-active, .audio-view-enter-active, .audio-view-leave-active { transition:none; } }
.audio-player { border-radius:16px 0 0 16px; }
.audio-info { display:flex; align-items:center; gap:8px; }.audio-info > div:first-child { flex:1; min-width:0; }.audio-more,.audio-volume { position:relative; flex:none; }.audio-more > button { width:44px; height:36px; }
.audio-popover { position:absolute; right:0; bottom:100%; z-index:3; min-width:132px; padding:6px; background:var(--surface); color:var(--text); border:1px solid var(--border); border-radius:8px; box-shadow:0 8px 24px #0003; }.has-artwork .audio-popover { background:#253037ed; backdrop-filter:blur(18px); }.audio-popover > button,.audio-popover > a { display:flex; width:100%; gap:8px; justify-content:flex-start; align-items:center; padding:9px; background:none; border:0; color:inherit; font-size:13px; border-radius:5px; }.audio-popover > button:hover,.audio-popover > a:hover { background:var(--primary-soft); }
.volume-popover { display:flex; flex-direction:column; align-items:center; gap:8px; min-width:56px; padding:14px 10px; }.volume-popover input { writing-mode:vertical-lr; direction:rtl; width:22px; height:110px; padding:0; accent-color:var(--primary); }.volume-popover small { font-size:12px; }
.audio-cover-view :deep(.thin-scroll-area) { display:flex; flex-direction:column; align-items:center; overflow:hidden; }.audio-art { flex:1; min-height:0; max-height:calc(100% - 42px); aspect-ratio:auto; background:none; }.audio-art img { object-fit:contain; }.audio-cover-lyrics { flex:none; height:42px; padding-top:5px; overflow:hidden; width:100%; mask-image:linear-gradient(#000 65%,transparent); }.audio-cover-lyrics .lyric-line { padding:0; line-height:26px; }.audio-cover-lyrics .audio-lyrics-empty { margin:0; padding:5px; }
.audio-capsule { isolation:isolate; overflow:hidden; width:min(260px,calc(100vw - 16px)); }.audio-capsule .capsule-ambient { position:absolute; inset:-20px; z-index:-1; width:calc(100% + 40px); height:calc(100% + 40px); border-radius:0; filter:blur(20px) brightness(.65); object-fit:cover; }.audio-capsule.has-artwork { color:#fff; background:#30343dcc; backdrop-filter:blur(18px); }.audio-capsule .capsule-cover { animation:disc-spin 16s linear infinite; animation-play-state:paused; }.audio-capsule.playing .capsule-cover { animation-play-state:running; }.capsule-toggle { flex:none; width:30px; height:30px; color:inherit; }
@keyframes disc-spin { to { transform:rotate(360deg); } }
@media(max-height:700px) { .audio-art { max-width:100%; }.audio-cover-lyrics .lyric-line { padding:0; } }
@media(prefers-reduced-motion:reduce) { .audio-capsule .capsule-cover { animation:none; } }
</style>
