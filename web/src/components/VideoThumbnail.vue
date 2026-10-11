<script setup>
import MediaThumbnail from './MediaThumbnail.vue'
defineProps({ url: String, storage: String, id: String, parent: String })
</script>
<template>
  <MediaThumbnail :url="url" :storage="storage" :id="id" :parent="parent" type="video" />
</template>
