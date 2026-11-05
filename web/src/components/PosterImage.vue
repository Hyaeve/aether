<script setup>
import {ref,watch} from 'vue'
import Icon from './Icon.vue'
const props=defineProps({src:String,title:String,kind:String}),failed=ref(false)
watch(()=>props.src,()=>failed.value=false)
</script>
<template><span class="poster-image"><img v-if="src&&!failed" :src="src" :alt="title||''" loading="lazy" decoding="async" referrerpolicy="no-referrer" @error="failed=true"/><span v-else class="poster-image-empty"><Icon :name="['tv','Series','Episode'].includes(kind)?'Tv':kind==='book'?'BookOpen':'Film'" :size="32"/><small>{{['tv','Series','Episode'].includes(kind)?'剧集':kind==='book'?'有声书':'电影'}}</small></span></span></template>
<style scoped>.poster-image{display:block;overflow:hidden;background:var(--bg);color:var(--muted)}.poster-image>img{display:block;width:100%;height:100%;object-fit:cover}.poster-image-empty{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;width:100%;height:100%}.poster-image-empty small{font-size:12px}</style>
