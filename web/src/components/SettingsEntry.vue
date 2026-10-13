<script setup>
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
defineProps({ to: String, label: String, hint: String })
const router = useRouter()
</script>
<template><button type="button" class="icon-btn settings-entry" :aria-label="label" :data-tooltip="hint" data-tooltip-always data-tooltip-stationary @click="router.push(to)"><Icon name="ChevronLeft" :size="32" preserveAspectRatio="none" /></button></template>
<style scoped>
.settings-entry { width:44px; height:76px; color:color-mix(in srgb,var(--primary) 45%,var(--muted)); align-self:center; }
.settings-entry:hover,.settings-entry:focus-visible { background:transparent; color:var(--primary); }
.settings-entry :deep(svg) { width:25px; height:64px; stroke-width:2.5; stroke-linecap:round; stroke-linejoin:round; transition:color .18s,transform .2s; }
.settings-entry:hover :deep(svg),.settings-entry:focus-visible :deep(svg) { transform:scale(1.06); }
.settings-entry :deep(svg path) { vector-effect:non-scaling-stroke; }
@media(prefers-reduced-motion:reduce){.settings-entry :deep(svg){transition:none}.settings-entry:hover :deep(svg){transform:none}}
</style>
