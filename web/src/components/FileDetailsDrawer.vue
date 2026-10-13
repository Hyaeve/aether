<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import Icon from './Icon.vue'
defineProps({ title: String, count: Number })
const emit = defineEmits(['close'])
const panel = ref(null), opened = ref(false)
let previous
function close() { opened.value = false }
function key(event) { if (event.key === 'Escape') { event.stopPropagation(); close() } }
function outside(event) { if (panel.value && !panel.value.contains(event.target)) close() }
onMounted(() => { previous = document.activeElement; opened.value = true; document.addEventListener('keydown', key); document.addEventListener('pointerdown', outside) })
onUnmounted(() => { document.removeEventListener('keydown', key); document.removeEventListener('pointerdown', outside); if(previous?.isConnected)previous.focus({preventScroll:true}) })
</script>
<template>
  <Teleport to=".file-browser" defer><div class="details-boundary"><Transition appear name="details-slide" @after-enter="panel?.focus({preventScroll:true})" @after-leave="emit('close')"><aside v-if="opened" ref="panel" class="file-details-drawer" role="dialog" :aria-label="title" tabindex="-1"><header><strong>{{ title }}</strong><span class="details-count">{{ count }} 个项目</span><button class="icon-btn" aria-label="关闭" @click="close"><Icon name="X" /></button></header><div class="details-content"><slot /></div></aside></Transition></div></Teleport>
</template>
<style scoped>
.file-details-drawer { position:absolute; inset:0 0 0 auto; z-index:12; width:34%; min-width:300px; max-width:100%; border:1px solid var(--border); border-radius:8px; background:var(--surface); box-shadow:-10px 0 28px #0000000c; display:flex; flex-direction:column; outline:none; }
.details-boundary { position:absolute; inset:0; overflow:hidden; border-radius:8px; pointer-events:none; z-index:12; }.file-details-drawer { pointer-events:auto; }
header { display:flex; align-items:center; gap:10px; padding:12px 18px; border-bottom:1px solid var(--border); font-size:14px; }.details-count { color:var(--muted); font-size:12px; }header button { margin-left:auto; }
.details-content { flex:1; min-height:0; overflow:auto; scrollbar-width:none; }.details-content::-webkit-scrollbar { display:none; }
.details-content :deep(.file-details) { max-height:none; overflow:visible; padding:18px; font-size:14px; }
.details-content :deep(dl) { padding:8px 0; gap:10px; grid-template-columns:72px minmax(0,1fr); }
.details-content :deep(dd),.details-content :deep(dt),.details-content :deep(button) { font-size:14px; line-height:1.6; font-variant-numeric:normal; overflow-wrap:anywhere; }
.details-content :deep(dl) { align-items:start; }.details-content :deep(.text-btn) { padding:0; text-align:left; align-items:start; }.details-content :deep(.text-btn svg) { margin-top:4px; }
.details-slide-enter-active,.details-slide-leave-active { transition:transform .44s cubic-bezier(.25,.65,.3,1),opacity .36s ease; }.details-slide-enter-from,.details-slide-leave-to { transform:translateX(100%);opacity:.5; }
@media(max-width:600px) { .file-details-drawer { width:100%; min-width:0; } }
@media(prefers-reduced-motion:reduce) { .details-slide-enter-active,.details-slide-leave-active { transition:none; } }
</style>
