<script setup>
import {ref,watch} from 'vue'
import Icon from './Icon.vue'
const props=defineProps({poster:String,title:String})
const failed=ref(false)
watch(()=>props.poster,()=>failed.value=false)
</script>
<template><span class="archive-poster"><img v-if="poster&&!failed" :src="poster" :alt="title||'媒体海报'" loading="lazy" referrerpolicy="no-referrer" @error="failed=true"/><Icon v-else name="Image" :size="20"/></span></template>
