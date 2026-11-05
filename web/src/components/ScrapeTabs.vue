<script setup>
import Icon from './Icon.vue'
defineProps({section:String})
</script>
<template><nav v-tab-scroll class="content-tabs" aria-label="刮削平台栏目"><RouterLink v-for="tab in [['strm','STRM刮削','STRMScrape'],['library','链库刮削','LibraryScrape'],['directory','目录刮削','FolderSearch'],['settings','刮削设置','Settings2']]" :key="tab[0]" :to="'/scrape/'+tab[0]" :class="{active:section===tab[0]}"><Icon :name="tab[2]"/>{{tab[1]}}</RouterLink></nav></template>
