<script setup>
import Icon from './Icon.vue'
import PosterImage from './PosterImage.vue'
defineProps({media:Object});defineEmits(['open','search','subscribe'])
</script>
<template><article class="recommendation-poster"><button class="recommendation-open" :aria-label="`${media.title} 详情`" @click="$emit('open',media)"><PosterImage :src="media.poster" :title="media.title" :kind="media.kind"/><span class="poster-kind">{{media.kind==='tv'?'剧集':'电影'}}</span><span class="poster-score">{{Number(media.score||0).toFixed(1)}}</span><span class="poster-caption"><small>{{media.year||'年份待定'}}</small><strong>{{media.title}}</strong><span class="poster-overview">{{media.overview||'暂无简介'}}</span></span></button><div class="poster-hover-actions"><button class="icon-btn" :aria-label="`搜索资源 ${media.title}`" @click="$emit('search',media)"><Icon name="Search"/></button><button class="icon-btn" :aria-label="`添加订阅 ${media.title}`" @click="$emit('subscribe',media)"><Icon name="Heart"/></button></div></article></template>
