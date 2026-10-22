<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
import VolumeSlider from './VolumeSlider.vue'
import { bytes } from '../lib'
const props = defineProps({ file: Object, queue: { type: Array, default: () => [] } })
const emit = defineEmits(['close', 'change'])
const panel = ref(null), player = ref(null), failed = ref(false), tools = ref(true)
const fullscreen = ref(false), fullscreenBusy = ref(false), playing = ref(false)
const position = ref(0), duration = ref(0), volume = ref(1), muted = ref(false)
const subtitlesOpen = ref(false), subtitlesLoading = ref(false), subtitleError = ref(''), subtitleTracks = ref([]), subtitleSelection = ref('off'), subtitleURL = ref('')
const volumeOpen = ref(false), audioOpen = ref(false), infoOpen = ref(false), volumeNotice = ref(''), ratio = ref(16 / 9), viewport = ref({ width: innerWidth, height: innerHeight })
const audioTracks = ref([]), mediaInfo = ref(null), infoLoading = ref(false), infoError = ref(''), buffered = ref(0)
const videoDetails = computed(() => mediaInfo.value?.streams?.find(stream => stream.codec_type === 'video'))
const audioDetails = computed(() => mediaInfo.value?.streams?.filter(stream => stream.codec_type === 'audio') || [])
const bitrate = value => Number(value) > 0 ? `${(Number(value) / 1000000).toFixed(2)} Mbps` : '未知'
const frameRate = value => { const [n,d=1] = String(value || '').split('/').map(Number); return n > 0 && d > 0 ? `${(n/d).toFixed(2)} fps` : '未知' }
let infoController, infoProbed = false, audioCloseTimer
const dimensions = computed(() => {
  if (fullscreen.value) return { width: `${viewport.value.width}px`, height: `${viewport.value.height}px`, '--viewer-height': `${viewport.value.height}px` }
  const width = Math.max(1, Math.min(1080, viewport.value.width - 32, (viewport.value.height - 48) * ratio.value))
  return { width: `${width}px`, height: `${width / ratio.value}px`, '--viewer-height': `${width / ratio.value}px` }
})
const index = computed(() => props.queue.findIndex(file => file.id === props.file.id && file.storage === props.file.storage))
const hasPrevious = computed(() => index.value > 0), hasNext = computed(() => index.value >= 0 && index.value + 1 < props.queue.length)
const seekable = computed(() => duration.value > 0 && Number.isFinite(duration.value))
const volumeIcon = computed(() => muted.value || volume.value === 0 ? 'VolumeX' : volume.value < .5 ? 'Volume1' : 'Volume2')
const time = value => { const seconds = Math.floor(Math.max(0, value || 0)); return seconds >= 3600 ? `${Math.floor(seconds / 3600)}:${String(Math.floor(seconds / 60) % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}` : `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` }
let previousFocus, previousOverflow, hideTimer, volumeNoticeTimer, nativeFullscreen = false, active = true, autoPlay = false, generation = 0, subtitleController, subtitleCloseTimer, subtitleProbed = false, subtitleStart = 0, selectedEmbedded = null, subtitleRequest = 0, exitedFullscreenAt = 0, leavingFullscreen = false
function resize() { viewport.value = { width: innerWidth, height: innerHeight } }
function metadata() {
  if (player.value.videoWidth && player.value.videoHeight) ratio.value = player.value.videoWidth / player.value.videoHeight
  syncDuration(); syncVolume(); refreshNativeTracks(); refreshAudioTracks()
  if (autoPlay) { autoPlay = false; play() }
}
function syncDuration() { duration.value = Number.isFinite(player.value?.duration) ? player.value.duration : 0 }
function syncTime() {
  position.value = player.value?.currentTime || 0
  buffered.value = 0
  for (let i=0; i<(player.value?.buffered.length || 0); i++) { if(player.value.buffered.start(i)<=position.value && player.value.buffered.end(i)>=position.value) buffered.value=player.value.buffered.end(i)-position.value }
  if (selectedEmbedded && !subtitlesLoading.value && Math.floor(position.value / 300) * 300 !== subtitleStart) selectSubtitle(selectedEmbedded)
}
function syncVolume() { volume.value = player.value?.volume ?? 1; muted.value = !!player.value?.muted }
function showTools() {
  tools.value = true; clearTimeout(hideTimer)
    hideTimer = setTimeout(() => { if (!subtitlesOpen.value && !volumeOpen.value && !audioOpen.value && !infoOpen.value && !panel.value?.querySelector(':focus-visible')) tools.value = false }, 2500)
}
function fullscreenChanged() {
  if (nativeFullscreen && document.fullscreenElement !== panel.value && !leavingFullscreen) exitedFullscreenAt = performance.now()
  nativeFullscreen = document.fullscreenElement === panel.value
  fullscreen.value = nativeFullscreen
  if (active) { resize(); panel.value?.focus(); showTools() }
}
async function leaveFullscreen() {
  leavingFullscreen = true
  if (document.fullscreenElement === panel.value) {
    try { await document.exitFullscreen() } catch { leavingFullscreen = false; return }
  }
  nativeFullscreen = false; fullscreen.value = false
  leavingFullscreen = false
}
async function toggleFullscreen() {
  if (fullscreenBusy.value) return
  fullscreenBusy.value = true
  try {
    if (fullscreen.value) await leaveFullscreen()
    else if (document.fullscreenEnabled && panel.value?.requestFullscreen) {
      const target = panel.value
      try {
        await target.requestFullscreen()
        if (!active && document.fullscreenElement === target) await document.exitFullscreen()
      } catch { if (active) fullscreen.value = true }
    } else fullscreen.value = true
  } finally {
    fullscreenBusy.value = false
    if (active) { resize(); panel.value?.focus(); showTools() }
  }
}
async function play() { const current = generation; try { await player.value?.play() } catch { if (current === generation) playing.value = !player.value?.paused } }
function togglePlayback() { if (player.value?.paused) play(); else player.value?.pause(); showTools() }
function seek(value) { if (seekable.value) { player.value.currentTime = Math.max(0, Math.min(duration.value, Number(value))); syncTime() } }
function setVolume(value) { if (player.value) { player.value.volume = Math.max(0, Math.min(1, Number(value))); player.value.muted = false; syncVolume(); showTools() } }
function nudgeVolume(value) { setVolume(value); volumeNotice.value = `${Math.round(volume.value * 100)}`; clearTimeout(volumeNoticeTimer); volumeNoticeTimer = setTimeout(() => { volumeNotice.value = '' }, 1000) }
function seekRelative(delta) { if (seekable.value) seek(position.value + delta) }
function changeEpisode(direction) {
  const file = props.queue[index.value + direction]
  if (!file) return
  player.value?.pause(); autoPlay = true; emit('change', file)
}
function refreshNativeTracks() {
  const tracks = [...(player.value?.textTracks || [])].filter(track => ['subtitles', 'captions'].includes(track.kind) && !track.label.startsWith('Aether:'))
  subtitleTracks.value = [...tracks.map((track, i) => ({ value: `native:${i}`, label: track.label || track.language || `字幕 ${i + 1}`, supported: true, track })), ...subtitleTracks.value.filter(track => !track.track)]
}
function refreshAudioTracks() { audioTracks.value = [...(player.value?.audioTracks || [])].map((track, index) => ({ index, label: track.label || track.language || `音轨 ${index + 1}`, enabled:track.enabled })) }
function selectAudio(index) {
  if(!player.value?.audioTracks?.[index])return
  for(let i=0;i<player.value.audioTracks.length;i++)player.value.audioTracks[i].enabled=i===index
  refreshAudioTracks(); audioOpen.value=false
}
async function probeInfo() {
  if(infoProbed || infoLoading.value)return
  infoProbed=true;infoLoading.value=true;infoError.value=''
  const current=generation;infoController=new AbortController()
  try {
    const response=await fetch('/api/files/video-info?'+new URLSearchParams({storage:props.file.storage,parent:props.file.parent,id:props.file.id}),{credentials:'same-origin',signal:infoController.signal})
    const data=await response.json();if(!response.ok)throw new Error(data.error || '视频信息读取失败')
    if(active&&current===generation)mediaInfo.value=data
  }catch(error){if(active&&current===generation&&error.name!=='AbortError')infoError.value=error.message}
  finally{if(current===generation)infoLoading.value=false}
}
function openInfo(){infoOpen.value=true;showTools();probeInfo()}
function openAudioMenu(){clearTimeout(audioCloseTimer);audioOpen.value=true;subtitlesOpen.value=false;refreshAudioTracks();showTools();if(!audioTracks.value.length)probeInfo()}
function closeAudioMenu(){audioCloseTimer=setTimeout(()=>{audioOpen.value=false},180)}
function holdAudioMenu(){clearTimeout(audioCloseTimer)}
const subtitleEndpoint = () => '/api/files/video-subtitles?' + new URLSearchParams({ storage: props.file.storage, parent: props.file.parent, id: props.file.id })
async function openSubtitles() {
  clearTimeout(subtitleCloseTimer); subtitlesOpen.value = true; audioOpen.value=false; showTools(); refreshNativeTracks()
  if (subtitleProbed || subtitlesLoading.value || subtitleTracks.value.some(track => track.track)) return
  subtitleProbed = true; subtitlesLoading.value = true; subtitleError.value = ''
  const current = generation
  const request = ++subtitleRequest
  subtitleController = new AbortController()
  try {
    const response = await fetch(subtitleEndpoint(), { credentials: 'same-origin', signal: subtitleController.signal })
    const data = await response.json()
    if (!response.ok) throw new Error(data.error || '字幕探测失败')
    if (active && current === generation && request === subtitleRequest) subtitleTracks.value = [...subtitleTracks.value.filter(track => track.track), ...(data.tracks || []).map(track => ({ ...track, value: `embedded:${track.index}`, label: track.title || track.language || `字幕 ${track.index + 1}` }))]
  } catch (error) { if (active && current === generation && request === subtitleRequest && error.name !== 'AbortError') subtitleError.value = error.message }
  finally { if (current === generation && request === subtitleRequest) subtitlesLoading.value = false }
}
function closeSubtitles(event) {
  clearTimeout(subtitleCloseTimer)
  // Loading disables the focused option in some engines; hover still owns the menu.
  if (event?.type === 'focusout' && (event.currentTarget.contains(event.relatedTarget) || event.currentTarget.matches(':hover'))) return
  subtitleCloseTimer = setTimeout(() => { subtitlesOpen.value = false; showTools() }, 180)
}
function holdSubtitles() { clearTimeout(subtitleCloseTimer) }
function clearSubtitleURL() { if (subtitleURL.value) URL.revokeObjectURL(subtitleURL.value); subtitleURL.value = '' }
async function selectSubtitle(option) {
  if (subtitlesLoading.value && option.value !== 'off') return
  const request = ++subtitleRequest
  subtitleController?.abort(); clearSubtitleURL()
  for (const track of player.value?.textTracks || []) track.mode = 'disabled'
  subtitleSelection.value = option.value; subtitleError.value = ''
  selectedEmbedded = null
  if (option.value === 'off') { subtitlesLoading.value = false; return }
  if (option.track) { option.track.mode = 'showing'; return }
  const current = generation
  selectedEmbedded = option; subtitleStart = Math.floor(position.value / 300) * 300
  subtitlesLoading.value = true; subtitleController = new AbortController()
  try {
    const response = await fetch(subtitleEndpoint() + `&track=${option.index}&start=${subtitleStart}`, { credentials: 'same-origin', signal: subtitleController.signal })
    if (!response.ok) { const error = await response.json().catch(() => ({})); throw new Error(error.error || '字幕加载失败') }
    const text = await response.text()
    if (active && current === generation && request === subtitleRequest) subtitleURL.value = URL.createObjectURL(new Blob([text], { type: 'text/vtt' }))
  } catch (error) { if (active && current === generation && request === subtitleRequest && error.name !== 'AbortError') { subtitleError.value = error.message; subtitleSelection.value = 'off'; selectedEmbedded = null } }
  finally { if (current === generation && request === subtitleRequest) subtitlesLoading.value = false }
}
function subtitleLoaded(event) {
  if (!subtitleSelection.value.startsWith('embedded:')) return
  for (const cue of event.target.track.cues || []) { cue.startTime += subtitleStart; cue.endTime += subtitleStart }
  event.target.track.mode = 'showing'
}
function close() { emit('close') }
function keepOpen(event) { if (event.target === event.currentTarget) { event.preventDefault(); panel.value?.focus(); showTools() } }
function key(event) {
  event.stopPropagation()
  if (event.key === 'Escape') {
    event.preventDefault()
    if (exitedFullscreenAt && performance.now() - exitedFullscreenAt < 300) { exitedFullscreenAt = 0; return }
    if (fullscreen.value || nativeFullscreen || document.fullscreenElement === panel.value || fullscreenBusy.value) leaveFullscreen(); else close()
    return
  }
  if (event.code === 'Space' || event.key === ' ') { event.preventDefault(); if (!event.repeat) togglePlayback() }
  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') { event.preventDefault(); nudgeVolume(volume.value + (event.key === 'ArrowUp' ? .05 : -.05)) }
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); seekRelative(event.key === 'ArrowRight' ? 10 : -10) }
  if (event.key === 'Tab') {
    showTools()
    const nodes = [...panel.value.querySelectorAll('video,button:not(:disabled),input:not(:disabled),a[href]')].filter(node => node.getClientRects().length)
    if (event.shiftKey && (document.activeElement === nodes[0] || document.activeElement === panel.value)) { event.preventDefault(); nodes.at(-1)?.focus() }
    else if (!event.shiftKey && document.activeElement === nodes.at(-1)) { event.preventDefault(); nodes[0]?.focus() }
  }
}
watch(() => [props.file.storage, props.file.id, props.file.url], async () => {
  infoController?.abort();mediaInfo.value=null;infoError.value='';infoProbed=false;infoLoading.value=false;infoOpen.value=false;audioOpen.value=false;audioTracks.value=[];buffered.value=0
  generation++; subtitleController?.abort(); clearSubtitleURL(); subtitleTracks.value = []; subtitleSelection.value = 'off'; subtitleError.value = ''; subtitleProbed = false; subtitlesLoading.value = false; subtitlesOpen.value = false
  selectedEmbedded = null; subtitleStart = 0
  position.value = 0; duration.value = 0; failed.value = false; playing.value = false; ratio.value = 16 / 9
  await nextTick(); player.value?.load(); panel.value?.focus(); showTools()
})
onMounted(async () => {
  previousFocus = document.activeElement; previousOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden'
  window.addEventListener('resize', resize); document.addEventListener('fullscreenchange', fullscreenChanged)
  player.value?.textTracks?.addEventListener('addtrack', refreshNativeTracks)
  player.value?.audioTracks?.addEventListener('addtrack', refreshAudioTracks)
  player.value?.audioTracks?.addEventListener('change', refreshAudioTracks)
  await nextTick(); panel.value?.focus(); showTools()
})
onBeforeUnmount(() => {
  active = false; generation++; subtitleController?.abort(); clearSubtitleURL(); player.value?.pause()
  infoController?.abort();clearTimeout(audioCloseTimer);clearTimeout(volumeNoticeTimer)
  player.value?.audioTracks?.removeEventListener('addtrack', refreshAudioTracks)
  player.value?.audioTracks?.removeEventListener('change', refreshAudioTracks)
  player.value?.textTracks?.removeEventListener('addtrack', refreshNativeTracks)
  document.removeEventListener('fullscreenchange', fullscreenChanged)
  if (document.fullscreenElement === panel.value) document.exitFullscreen().catch(() => {})
})
onUnmounted(() => {
  clearTimeout(hideTimer); clearTimeout(subtitleCloseTimer); window.removeEventListener('resize', resize)
  document.body.style.overflow = previousOverflow; previousFocus?.focus()
})
</script>
<template>
  <Teleport to="body">
    <div class="video-backdrop" @pointerdown="keepOpen">
      <section ref="panel" class="video-viewer" :class="{ fullscreen }" :style="dimensions" role="dialog" aria-modal="true" :aria-label="file.name" tabindex="-1" @keydown="key" @pointermove="showTools" @focusin="showTools">
        <video ref="player" :src="file.url" tabindex="0" playsinline preload="metadata" @loadedmetadata="metadata" @durationchange="syncDuration" @timeupdate="syncTime" @volumechange="syncVolume" @play="playing = true" @pause="playing = false" @ended="playing = false" @error="failed = true">
          <track v-if="subtitleURL" :key="subtitleURL" :src="subtitleURL" kind="subtitles" label="Aether:字幕" @load="subtitleLoaded" />
        </video>
        <header class="video-overlay" :class="{ hidden: !tools && !failed }"><span>{{ file.name }} <em>网速 待定</em></span><button v-if="fullscreen" aria-label="视频信息" @click="openInfo"><Icon name="CircleAlert" :size="21" /></button><button v-else aria-label="关闭视频预览" @click="close"><Icon name="X" :size="21" /></button></header>
        <div class="video-controls" :class="{ hidden: !tools && !failed }">
          <input class="video-progress" type="range" min="0" :max="duration || 1" step="0.1" :value="position" :disabled="!seekable" aria-label="视频进度" :style="{ '--progress': `${duration ? position / duration * 100 : 0}%` }" @input="seek($event.target.value)" />
          <div class="video-control-row">
            <button aria-label="上一集" :disabled="!hasPrevious" @click="changeEpisode(-1)"><Icon name="SkipBack" :size="20" /></button>
            <button :aria-label="playing ? '暂停视频' : '播放视频'" @click="togglePlayback"><Icon :name="playing ? 'Pause' : 'Play'" :size="21" /></button>
            <button aria-label="下一集" :disabled="!hasNext" @click="changeEpisode(1)"><Icon name="SkipForward" :size="20" /></button>
            <span class="video-time">{{ time(position) }} / {{ time(duration) }}</span><span class="video-controls-spacer" />
            <div class="video-subtitles" @pointerenter="openSubtitles" @pointerleave="closeSubtitles" @focusin="openSubtitles" @focusout="closeSubtitles">
              <button aria-label="字幕" aria-haspopup="menu" :aria-expanded="subtitlesOpen" @click="openSubtitles"><Icon name="Captions" :size="22" /></button>
              <div v-if="subtitlesOpen" class="video-subtitle-menu" role="menu" aria-label="字幕选择" @pointerenter="holdSubtitles" @pointerleave="closeSubtitles">
                <button role="menuitemradio" :aria-checked="subtitleSelection === 'off'" @click="selectSubtitle({ value: 'off' })">关闭字幕</button>
                <button v-for="option in subtitleTracks" :key="option.value" role="menuitemradio" :aria-checked="subtitleSelection === option.value" :disabled="!option.supported || subtitlesLoading" @click="selectSubtitle(option)">{{ option.label }}<small v-if="!option.supported">图片字幕暂不支持</small></button>
                <span v-if="subtitlesLoading" role="status"><Icon name="LoaderCircle" class="spin" :size="15" />加载字幕…</span>
                <span v-else-if="subtitleError" role="status">{{ subtitleError }}</span><span v-else-if="!subtitleTracks.length" role="status">暂无内置字幕</span>
              </div>
            </div>
            <div class="video-audio-tracks" @pointerenter="openAudioMenu" @pointerleave="closeAudioMenu" @focusin="openAudioMenu" @focusout="closeAudioMenu">
              <button aria-label="音轨" aria-haspopup="menu" :aria-expanded="audioOpen" @click="audioOpen ? audioOpen = false : openAudioMenu()"><Icon name="Music2" :size="22" /></button>
              <div v-if="audioOpen" class="video-audio-menu" role="menu" aria-label="音轨选择" @pointerenter="holdAudioMenu" @pointerleave="closeAudioMenu"><button v-for="track in audioTracks" :key="track.index" role="menuitemradio" :aria-checked="track.enabled" @click="selectAudio(track.index)">{{track.label}}</button><template v-if="!audioTracks.length"><button v-for="track in audioDetails" :key="track.index" role="menuitem" disabled>{{track.tags?.title || track.tags?.language || track.codec_name || `音轨 ${track.index}`}}</button><span>{{infoLoading ? '读取音轨…' : '当前浏览器不支持切换音轨'}}</span></template></div>
            </div>
            <div class="video-volume" @pointerenter="volumeOpen = true" @pointerleave="volumeOpen = false" @focusin="volumeOpen = true" @focusout="volumeOpen = false" @wheel.prevent="setVolume(volume + ($event.deltaY < 0 ? .05 : -.05))">
              <button :aria-label="muted || volume === 0 ? '取消静音' : '静音'" @click="player.muted = !player.muted; syncVolume()"><Icon :name="volumeIcon" :size="22" /></button>
              <div v-if="volumeOpen" class="video-volume-popup"><VolumeSlider :model-value="muted ? 0 : volume" label="视频音量" @update:model-value="setVolume" /></div>
            </div>
            <button class="video-fullscreen" :aria-label="fullscreen ? '退出全屏' : '全屏'" :aria-pressed="fullscreen" :disabled="fullscreenBusy" @click="toggleFullscreen"><Icon :name="fullscreen ? 'Minimize' : 'Maximize'" :size="20" /></button>
          </div>
          <span v-if="volumeNotice" class="video-volume-notice" role="status">音量 {{ volumeNotice }}</span>
        </div>
        <div v-if="infoOpen" class="video-info-panel" role="region" aria-label="视频信息"><button class="video-info-close" aria-label="关闭视频信息" @click="infoOpen = false"><Icon name="X" /></button><strong>{{file.name}}</strong><dl><dt>播放</dt><dd>{{playing ? '播放中' : '已暂停'}} · 缓冲 {{buffered.toFixed(1)}} s</dd><dt>容器</dt><dd>{{mediaInfo?.format?.format_name || '未知'}} · {{bytes(mediaInfo?.size ?? file.size)}}</dd><dt>尺寸</dt><dd>{{player?.videoWidth || videoDetails?.width || '未知'}} × {{player?.videoHeight || videoDetails?.height || '未知'}}</dd><dt>视频</dt><dd>{{videoDetails?.codec_name || '未知'}} · {{videoDetails?.profile || '未知'}} · {{frameRate(videoDetails?.avg_frame_rate)}}</dd><dt>动态范围</dt><dd>{{videoDetails?.color_transfer === 'smpte2084' ? 'HDR PQ' : videoDetails?.color_transfer === 'arib-std-b67' ? 'HDR HLG' : videoDetails?.color_transfer || '未知'}}</dd><dt>码率</dt><dd>{{bitrate(mediaInfo?.format?.bit_rate)}}</dd><template v-for="track in audioDetails" :key="track.index"><dt>音轨 {{track.index}}</dt><dd>{{track.codec_name}} · {{track.channel_layout || `${track.channels || '未知'} 声道`}} · {{track.sample_rate || '未知'}} Hz · {{bitrate(track.bit_rate)}}</dd></template><dt>时长</dt><dd>{{time(duration)}}</dd><dt>实时网速</dt><dd>待定</dd></dl><span v-if="infoLoading" role="status">读取媒体信息…</span><span v-else-if="infoError" role="status">{{infoError}}</span></div>
        <div v-if="failed" class="video-error" role="status"><Icon name="FileVideo" :size="30" /><p>浏览器无法播放此格式，请下载后打开。</p><a :href="`${file.url}${file.url.includes('?') ? '&' : '?'}download=1`" :download="file.name" target="_blank" rel="noopener noreferrer"><Icon name="Download" :size="18" />下载视频</a></div>
      </section>
    </div>
  </Teleport>
</template>
<style scoped>
.video-backdrop { position:fixed; inset:0; z-index:200; display:grid; place-items:center; background:#090d18ba; backdrop-filter:blur(10px); overscroll-behavior:contain; }
.video-viewer { position:relative; overflow:hidden; border-radius:10px; background:#000; box-shadow:0 20px 80px #0008; outline:none; animation:video-enter .2s ease; }
.video-viewer.fullscreen { border-radius:0; box-shadow:none; }
.video-viewer > video { display:block; width:100%; height:100%; object-fit:contain; }
.video-overlay { position:absolute; top:0; left:0; right:0; display:flex; align-items:center; gap:12px; padding:10px 12px; color:#f1f4ff; background:linear-gradient(#080b16ae,transparent); pointer-events:none; transition:opacity .2s ease; }
.video-overlay span { min-width:0; flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:14px; text-shadow:0 1px 4px #000; }.video-overlay em{font-style:normal;font-size:11px;color:#d3d9e8;margin-left:8px}
.video-viewer button { display:grid; place-items:center; flex:none; width:34px; height:34px; padding:0; border:0; border-radius:7px; background:transparent; color:inherit; pointer-events:auto; cursor:pointer; }
.video-viewer button:hover { background:#0006; }.video-viewer button:focus-visible { outline:2px solid #b7c4fb; outline-offset:-2px; }.video-viewer button:disabled { opacity:.3; cursor:default; background:transparent; }
.video-overlay button { background:#17203288; }
.video-controls { position:absolute; inset:auto 0 0; padding:14px 12px 6px; color:#f1f4ff; background:linear-gradient(transparent,#080b16d9); transition:opacity .2s ease; }
.video-overlay.hidden,.video-controls.hidden { opacity:0; pointer-events:none; }.video-controls.hidden button,.video-overlay.hidden button { pointer-events:none; }
.video-progress { display:block; width:100%; height:14px; margin:0 0 3px; padding:0; border:0; box-shadow:none; appearance:none; cursor:pointer; background:transparent; }
.video-progress::-webkit-slider-runnable-track { height:3px; border-radius:2px; background:linear-gradient(to right,#b9c5ff var(--progress),#ffffff50 var(--progress)); }.video-progress::-moz-range-track { height:3px; border-radius:2px; background:#ffffff50; }.video-progress::-moz-range-progress { background:#b9c5ff; height:3px; }
.video-progress::-webkit-slider-thumb { appearance:none; width:10px; height:10px; margin-top:-3.5px; border-radius:50%; background:#f1f4ff; }.video-progress::-moz-range-thumb { width:10px; height:10px; border:0; border-radius:50%; background:#f1f4ff; }
.video-control-row { display:flex; align-items:center; gap:10px; min-width:0; }.video-controls-spacer { flex:1; }.video-time { flex:none; font-size:12px; font-variant-numeric:tabular-nums; margin-left:8px; white-space:nowrap; }
.video-subtitles,.video-volume,.video-audio-tracks { position:relative; flex:none; }.video-subtitle-menu,.video-volume-popup,.video-audio-menu { position:absolute; bottom:100%; right:-34px; margin-bottom:8px; border:1px solid #ffffff20; border-radius:8px; background:#303b44; backdrop-filter:none; box-shadow:0 8px 30px #0006; }
.video-subtitle-menu::after,.video-volume-popup::after,.video-audio-menu::after { content:''; position:absolute; top:100%; height:10px; left:0; right:0; }.video-subtitle-menu { width:240px; max-width:calc(100vw - 40px); height:188px; max-height:calc(var(--viewer-height) - 72px); overflow-y:auto; scrollbar-width:none; padding:5px; font-size:13px; }.video-subtitle-menu::-webkit-scrollbar { display:none; }
.video-subtitle-menu button { display:flex; flex-direction:column; align-items:flex-start; justify-content:center; width:100%; height:44px; min-height:44px; padding:7px 9px; text-align:left; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.video-subtitle-menu button[aria-checked=true] { color:#d0d8ff; background:#8392d932; }.video-subtitle-menu small { font-size:11px; }.video-subtitle-menu > span { display:flex; gap:7px; align-items:center; padding:8px 9px; color:#b9c0d2; overflow-wrap:anywhere; }
.video-audio-menu{width:220px;max-height:188px;overflow:auto;scrollbar-width:none;min-height:44px;padding:5px;font-size:13px}.video-audio-menu button,.video-audio-menu span{display:block;width:100%;padding:9px;text-align:left;border:0;background:none;color:#f1f4ff;border-radius:6px}.video-audio-menu button:hover{background:#ffffff14}.video-audio-menu span{color:#b9c0d2}
.video-volume-popup { right:0; width:44px; height:144px; max-height:calc(var(--viewer-height) - 72px); padding:10px 8px; border-radius:22px; }
.video-volume-notice{position:absolute;right:12px;bottom:62px;padding:5px 9px;border-radius:6px;background:#121923d9;color:#f1f4ff;font-size:12px;pointer-events:none}.video-info-panel{position:absolute;top:48px;right:12px;display:grid;gap:8px;width:min(280px,calc(100% - 24px));padding:14px 38px 14px 14px;border:1px solid #ffffff20;border-radius:8px;background:#111821e8;box-shadow:0 10px 30px #0008;color:#edf2ff;font-size:12px}.video-info-panel strong{font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.video-info-close{position:absolute;top:7px;right:7px!important;width:28px!important;height:28px!important}
.video-error { position:absolute; inset:52px 8px 74px; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:12px; color:#dce2f2; text-align:center; font-size:14px; pointer-events:none; }.video-error a { pointer-events:auto; }.video-error p { margin:0; }.video-error a { display:flex; align-items:center; gap:7px; color:#b6c7ff; text-decoration:none; }
.video-info-panel{width:min(430px,calc(100% - 24px));max-height:calc(100% - 110px);overflow:auto;scrollbar-width:none;overscroll-behavior:contain;background:#14191fe8}
.video-info-panel dl{display:grid;grid-template-columns:64px minmax(0,1fr);gap:8px;margin:0}.video-info-panel dt{color:#b7c0cf}.video-info-panel dd{margin:0;overflow-wrap:anywhere}
.video-audio-menu button{height:auto;min-height:38px}.video-audio-menu button[aria-checked=true]{color:#d0d8ff;background:#8392d932}.video-audio-menu button:disabled{opacity:.7}
@keyframes video-enter { from { opacity:0; transform:scale(.98); } }
@media(max-width:480px) { .video-controls { padding-left:8px; padding-right:8px; }.video-control-row { gap:4px; }.video-time { margin-left:3px; font-size:11px; }.video-viewer button { width:30px; height:32px; }.video-overlay { padding:6px 8px; }.video-error { inset:42px 8px 60px; gap:5px; font-size:12px; } }
@media(prefers-reduced-motion:reduce) { .video-viewer { animation:none; }.video-overlay,.video-controls { transition:none; } }
</style>
