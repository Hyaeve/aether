<script setup>
import { driverOf } from '../lib'
import Icon from './Icon.vue'
defineProps({ type: String, small: Boolean })
const logos = {
  '115': '/providers/115.ico',
  mobile: '/providers/mobile.png',
  tianyi: '/providers/tianyi.png',
  quark: '/providers/quark.png',
  openlist: '/providers/openlist.svg'
}
</script>
<template>
  <span class="provider-icon" :class="{ small }" :style="{ '--provider': driverOf(type).color }" aria-hidden="true">
    <img v-if="logos[type]" :src="logos[type]" alt="" class="provider-logo" draggable="false" />
    <Icon v-else-if="type === 'local'" name="FolderSync" :size="small ? 20 : 27" />
    <Icon v-else-if="type === 'webdav'" name="CloudDownload" :size="small ? 20 : 27" />
    <span v-else>{{ driverOf(type).icon }}</span>
  </span>
</template>
