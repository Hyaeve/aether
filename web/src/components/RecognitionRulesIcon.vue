<script setup>
defineProps({size:{type:Number,default:22}})
</script>
<template><svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 4H3v4m3 12H3v-4M10 4h3v4m-3 12h3v-4"/><path d="M5 12c1.7-3 4.3-3 6 0-1.7 3-4.3 3-6 0Z"/><circle cx="8" cy="12" r=".8" fill="currentColor" stroke="none"/><path d="M13 12h4m0-6v12m0-12h3m-3 6h3m-3 6h3"/><circle cx="21" cy="6" r="1"/><circle cx="21" cy="12" r="1"/><circle cx="21" cy="18" r="1"/></svg></template>
