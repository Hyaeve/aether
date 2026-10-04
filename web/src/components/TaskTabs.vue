<script setup>
const tabs = [
  ['/tasks/strm', 'STRM 任务'], ['/tasks/cas', 'CAS 任务'], ['/tasks/cache', '缓存任务'],
  ['/tasks/organize', '目录整理'], ['/tasks/scrape', 'STRM 刮削']
]
</script>
<template>
  <nav class="content-tabs" aria-label="任务栏目">
    <RouterLink v-for="[path, label] in tabs" :key="path" :to="path" :class="{ active: ($route.path === '/tasks' ? '/tasks/strm' : $route.path).startsWith(path) }">{{ label }}</RouterLink>
  </nav>
</template>
