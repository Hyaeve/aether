<script setup>
import { ref, watch } from 'vue'
import Icon from './Icon.vue'
import { noticeTime } from '../library-notices'
import { archiveStatuses, archiveTriggers, archiveStatusClass } from '../archive-display'
const props = defineProps({ item: Object, disabled: Boolean })
defineEmits(['retry'])
const failedImage = ref(false)
watch(() => [props.item.id, props.item.poster], () => { failedImage.value = false })
</script>
<template>
  <article class="archive-row" role="listitem">
    <div class="archive-media">
      <span class="archive-poster"><img v-if="item.poster && !failedImage" :src="item.poster" :alt="item.title" loading="lazy" referrerpolicy="no-referrer" @error="failedImage=true" /><Icon v-else name="Image" :size="20" /></span>
      <div class="archive-name">
        <span class="archive-title"><strong :data-tooltip="item.title">{{ item.title || '未识别' }}</strong><small>{{ item.kind === 'tv' ? '剧集' : '电影' }}</small></span>
        <span class="archive-filename" tabindex="0" :data-tooltip="item.filename" data-tooltip-stationary data-tooltip-delay="300">{{ item.filename }}</span>
        <small class="archive-origin" :data-tooltip="`${item.taskName} / ${item.path}`">{{ item.taskName }}<template v-if="item.tmdb"> · TMDB {{ item.tmdb }}</template></small>
      </div>
    </div>
    <span class="archive-trigger"><Icon :name="item.trigger === 'manual' ? 'UserRound' : 'Clock3'" :size="15" />{{ archiveTriggers[item.trigger] || '未知来源' }}<small>{{ item.action === 'identify' ? '识别' : '识别刮削' }}</small></span>
    <time class="archive-time" :datetime="item.finishedAt || item.startedAt">{{ noticeTime(item.finishedAt || item.startedAt) }}</time>
    <span class="archive-status" :class="archiveStatusClass(item.status)" :data-tooltip="item.message" data-tooltip-always><Icon :name="item.status === 'success' ? 'CircleCheck' : item.status === 'running' ? 'LoaderCircle' : 'CircleAlert'" :size="15" />{{ archiveStatuses[item.status] || '未知状态' }}</span>
    <button class="icon-btn archive-retry" aria-label="重新识别刮削" data-tooltip="重新识别刮削" data-tooltip-always :disabled="disabled || item.status === 'running'" @click="$emit('retry',item)"><Icon name="ScanSearch" /></button>
  </article>
</template>
