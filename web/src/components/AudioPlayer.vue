<script setup>
import { nextTick, onBeforeUnmount, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ file: Object })
const emit = defineEmits(['close'])
const panel = ref(null), player = ref(null), failed = ref(false)
let previousFocus
function key(event) {
  event.stopPropagation()
  if (event.key === 'Escape') { event.preventDefault(); emit('close') }
}
watch(() => props.file.url, async () => { failed.value = false; await nextTick(); player.value?.play().catch(() => {}) })
onMounted(async () => { previousFocus = document.activeElement; await nextTick(); panel.value?.focus(); player.value?.play().catch(() => {}) })
onBeforeUnmount(() => { player.value?.pause(); player.value?.removeAttribute('src'); player.value?.load() })
onUnmounted(() => { if (previousFocus?.isConnected) previousFocus.focus() })
</script>
<template>
  <Teleport to="body">
  <div class="audio-player-shell">
    <section ref="panel" class="audio-player" role="dialog" :aria-label="file.name" tabindex="-1" @keydown="key">
      <header><span>音频播放</span><button class="icon-btn" aria-label="关闭音频播放" @click="emit('close')"><Icon name="X" /></button></header>
      <div class="audio-art" aria-hidden="true"><Icon name="FileAudio2" :size="70" /><div class="audio-wave"><i v-for="n in 15" :key="n" :style="{ height: `${12 + (n * 17 % 31)}px` }" /></div></div>
      <h2>{{ file.name }}</h2><span class="audio-format">{{ file.name.split('.').at(-1).toUpperCase() }}</span>
      <audio ref="player" :src="file.url" tabindex="0" controls preload="metadata" @error="failed = true" />
      <p v-if="failed" role="status">浏览器无法播放此格式，请下载后打开。</p>
      <a :href="`${file.url}${file.url.includes('?') ? '&' : '?'}download=1`" :download="file.name" target="_blank" rel="noopener noreferrer"><Icon name="Download" :size="17" />下载音频</a>
    </section>
  </div>
  </Teleport>
</template>
<style scoped>
.audio-player-shell { position:fixed; inset:48px 0 0; z-index:190; display:flex; justify-content:flex-end; pointer-events:none; }
.audio-player { display:flex; flex-direction:column; align-items:center; gap:18px; width:min(360px,100%); height:100%; padding:20px 24px; background:var(--surface); color:var(--text); border-left:1px solid var(--border); box-shadow:-12px 0 50px #0b173321; pointer-events:auto; overflow:auto; outline:none; animation:audio-slide .3s cubic-bezier(.2,.8,.2,1); }
.audio-player header { display:flex; align-items:center; justify-content:space-between; width:100%; font-size:14px; }.audio-player header .icon-btn { width:32px; height:32px; }
.audio-art { display:flex; flex-direction:column; justify-content:center; align-items:center; gap:30px; width:100%; aspect-ratio:1; max-height:300px; color:var(--primary); background:color-mix(in srgb,var(--primary-soft) 45%,var(--bg)); border-radius:8px; margin-top:12px; }
.audio-wave { height:44px; display:flex; align-items:center; gap:5px; }.audio-wave i { display:block; width:3px; border-radius:2px; background:currentColor; opacity:.42; }
.audio-player h2 { font-size:16px; font-weight:600; line-height:1.6; margin:0; width:100%; text-align:center; overflow-wrap:anywhere; }.audio-format { color:var(--muted); font-size:12px; }
.audio-player audio { width:100%; height:44px; flex:none; }.audio-player p { font-size:13px; color:var(--muted); text-align:center; }.audio-player a { display:flex; align-items:center; gap:7px; color:var(--primary); text-decoration:none; font-size:13px; }
[data-theme=dark] .audio-player audio { color-scheme:dark; }
@keyframes audio-slide { from { transform:translateX(100%); opacity:.6; } }
@media(prefers-reduced-motion:reduce) { .audio-player { animation:none; } }
</style>
