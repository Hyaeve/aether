<script setup>
import { useId } from 'vue'
import refreshImage from '../assets/icons/refresh.png'

defineProps({ size: { type: Number, default: 18 } })
const maskId = `refresh-${useId()}`
</script>

<template>
  <svg class="refresh-icon" :width="size" :height="size" viewBox="0 0 200 200" fill="currentColor" stroke="none" aria-hidden="true">
    <defs>
      <mask :id="maskId" mask-type="alpha" maskUnits="userSpaceOnUse" x="0" y="0" width="200" height="200">
        <image :href="refreshImage" width="200" height="200" />
      </mask>
    </defs>
    <rect width="200" height="200" :mask="`url(#${maskId})`" />
  </svg>
</template>
