<script setup>
import { driverOf } from '../lib'
import Icon from './Icon.vue'
defineProps({ type: String, small: Boolean })
</script>
<template>
  <span class="provider-icon" :class="{ small }" :style="{ '--provider': driverOf(type).color }" aria-hidden="true">
    <Icon v-if="type === 'local'" name="HardDrive" :size="small ? 20 : 27" />
    <Icon v-else-if="type === 'webdav'" name="Network" :size="small ? 20 : 27" />
    <span v-else>{{ driverOf(type).icon }}</span>
  </span>
</template>
