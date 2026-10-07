<script setup>
import Icon from './Icon.vue'
const tabs = [
  ['/tasks/strm', 'STRM 任务', 'FileVideo'], ['/tasks/cas', 'CAS 任务', 'Box'], ['/tasks/ed2k', 'ED2K 任务', 'Link'], ['/tasks/cache', '缓存任务', 'Database'],
  ['/tasks/organize', '目录整理', 'FolderTree'], ['/tasks/scrape', 'STRM 刮削', 'ScanSearch']
]
</script>
<template>
  <nav class="content-tabs" aria-label="任务栏目">
    <RouterLink v-for="[path, label, icon] in tabs" :key="path" :to="path" :class="{ active: ($route.path === '/tasks' ? '/tasks/strm' : $route.path).startsWith(path) }"><Icon :name="icon" :size="17" />{{ label }}</RouterLink>
  </nav>
</template>
