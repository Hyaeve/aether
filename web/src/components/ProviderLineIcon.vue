<script setup>
defineProps({ type: String })
const paths = {
  '115': ['M5 16a7 7 0 1 1 13-8', 'M7 6h9l-1 5h-4a4 4 0 1 0 4 4', 'M16 17h5', 'M19 14v6'],
  mobile: ['M5 17 10 7a4 4 0 0 1 7 4l-4 8', 'M3 15 7 7', 'M17 18l4-8', 'M8 20l4-8'],
  tianyi: ['M7 18H6a4 4 0 0 1-1-8 6 6 0 0 1 11-3 5 5 0 0 1 2 11h-1', 'M7 13c2-3 8-3 10 0', 'M7 16c2 4 8 4 10 0'],
  quark: ['M16 5a8 8 0 1 0 3 12', 'M16 5c-5 1-8 5-7 9s6 5 10 3', 'M14 14l7 7']
}
</script>
<template><svg class="provider-line-icon" :data-provider="type" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path v-for="(path,index) in paths[type] || paths['115']" :key="index" :d="path" /></svg></template>
<style scoped>.provider-line-icon{width:19px;height:19px;flex:none;vertical-align:middle}</style>
