<script setup>
import Icon from './Icon.vue'
import ArchivePoster from './ArchivePoster.vue'
import {noticeTime} from '../library-notices'
import {archiveStatuses,archiveStatusClass} from '../archive-display'
import {bytes as formatBytes} from '../lib'
defineProps({item:Object})
const methods={move:'移动',copy:'复制',hardlink:'硬链接',symlink:'软链接'}
</script>
<template><article class="archive-organize-row" role="listitem"><div class="archive-media"><ArchivePoster :poster="item.poster" :title="item.title"/><div class="archive-name"><strong :data-tooltip="item.title">{{item.title||item.filename}}</strong><small class="muted">{{item.kind==='tv'?'剧集':'电影'}}<template v-if="item.year"> / {{item.year}}</template><template v-if="item.tmdb"> · TMDB {{item.tmdb}}</template></small><small class="archive-filename" :data-tooltip="item.filename" data-tooltip-stationary data-tooltip-delay="300">{{item.filename}}</small></div></div><div class="archive-path-flow"><span :data-tooltip="item.sourcePath" data-tooltip-stationary data-tooltip-delay="300"><Icon name="Circle" :size="12"/><small>{{item.sourceStorage}}</small>{{item.sourcePath}}</span><span :data-tooltip="item.targetPath" data-tooltip-stationary data-tooltip-delay="300"><Icon name="CircleCheck" :size="12"/><small>{{item.targetStorage}}</small>{{item.targetPath}}</span></div><div class="archive-organize-result"><span class="archive-status" :class="archiveStatusClass(item.status)" :data-tooltip="item.message">{{archiveStatuses[item.status]||item.status}}</span><small>{{methods[item.method]||item.method}} · {{formatBytes(item.size||0)}}</small><time :datetime="item.finishedAt">{{noticeTime(item.finishedAt||item.startedAt)}}</time></div></article></template>
