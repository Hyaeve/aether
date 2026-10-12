<script setup>
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
defineProps({ to: String, label: String, hint: String })
const router = useRouter()
</script>
<template><button type="button" class="icon-btn settings-entry" :aria-label="label" :data-tooltip="hint" data-tooltip-always @click="router.push(to)"><Icon name="ChevronLeft" :size="32" /></button></template>
<style scoped>
.settings-entry { width:44px; height:76px; color:var(--primary); border-radius:16px; align-self:center; }
.settings-entry :deep(svg) { width:25px; height:46px; stroke-width:2.5; stroke-linecap:round; stroke-linejoin:round; }
</style>
