<script setup>
import Icon from './Icon.vue'
const tabs = [
  ['/files', '文件管理', 'FolderOpen'],
  ['/files/webdav', 'WebDAV', 'WebDAV'],
  ['/files/mounts', '本地挂载', 'CloudDownload']
]
</script>
<template>
  <nav class="content-tabs file-tabs" aria-label="文件服务栏目">
    <RouterLink v-for="[path, label, icon] in tabs" :key="path" :to="path" :class="{ active: $route.path === path }" :aria-current="$route.path === path ? 'page' : undefined"><Icon :name="icon" :size="19" />{{ label }}</RouterLink>
  </nav>
</template>
