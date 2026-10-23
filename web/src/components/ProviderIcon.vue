<script setup>
import { driverOf } from '../lib'
import Icon from './Icon.vue'
defineProps({ type: String, small: Boolean, compact: Boolean, unframed: Boolean })
const logos = {
  '115': '/providers/115.ico',
  mobile: '/providers/mobile.png',
  tianyi: '/providers/tianyi.png',
  quark: '/providers/quark.png',
  openlist: '/providers/openlist.svg'
}
</script>
<template>
  <span class="provider-icon" :data-provider="type" :class="{ small, compact }" :style="{ '--provider': driverOf(type).color }" aria-hidden="true">
    <img v-if="logos[type]" :src="unframed && ['mobile', 'quark'].includes(type) ? `/providers/${type}-mark.png` : logos[type]" alt="" class="provider-logo" draggable="false" />
    <Icon v-else-if="type === 'local'" name="CloudDownload" :size="small ? 20 : 27" />
    <Icon v-else-if="type === 'webdav'" name="WebDAV" :size="small ? 20 : 27" />
    <span v-else>{{ driverOf(type).icon }}</span>
  </span>
</template>
<style scoped>
/* Match visible artwork, not the differently padded source-image boxes. */
.provider-icon.compact { width:22px; height:22px; flex:none; padding:0; border:0; background:none; border-radius:0; }
.compact .provider-logo { width:20px; height:20px; }
.compact[data-provider=mobile] .provider-logo { width:22px; height:22px; }
.compact[data-provider="115"] .provider-logo { width:16px; height:16px; }
.compact[data-provider=openlist] .provider-logo { width:18px; height:18px; }
.compact > svg { width:18px; height:18px; }
.compact :deep(.dav-symbol) { width:18px !important; height:18px !important; }
.compact :deep(.dav-symbol > span) { font-size:5px; }
</style>
