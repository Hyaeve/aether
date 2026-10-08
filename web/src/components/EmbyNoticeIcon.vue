<script setup>
import { Bell } from 'lucide-vue-next'
</script>
<template><span class="emby-notice-icon" aria-hidden="true"><img src="/media/emby.png" alt="" /><Bell :size="13" :stroke-width="1.7" /></span></template>
<style scoped>
.emby-notice-icon { position: relative; display: inline-flex; width: 30px; height: 30px; flex-shrink: 0; }
img { width: 100%; height: 100%; object-fit: contain; }
.emby-notice-icon > svg { position: absolute; right: -4px; bottom: -3px; padding: 2px; box-sizing: content-box; border-radius: 50%; background: var(--surface); color: #429b49; box-shadow: 0 0 0 1px var(--border); }
</style>
