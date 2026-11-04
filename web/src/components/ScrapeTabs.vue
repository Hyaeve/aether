<script setup>
import Icon from './Icon.vue'
defineProps({section:String})
</script>
<template><nav v-tab-scroll class="content-tabs" aria-label="刮削平台栏目"><RouterLink v-for="tab in [['strm','STRM刮削','Tv'],['library','链库刮削','VirtualLibrary'],['directory','目录刮削','FolderSearch']]" :key="tab[0]" :to="'/scrape/'+tab[0]" :class="{active:section===tab[0]}"><Icon :name="tab[2]"/>{{tab[1]}}</RouterLink></nav></template>
