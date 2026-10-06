<script setup>
import { computed, ref, watch } from 'vue'
import { useVirtualList } from '../virtual-list'
const props = defineProps({ items: { type: Array, required: true }, rowHeight: { type: Number, default: 52 } })
const viewport = ref(null)
const { shown, start, top, bottom, reset } = useVirtualList(computed(() => props.items), viewport, { rowHeight: computed(() => props.rowHeight) })
watch(() => props.items, reset)
</script>
<template>
  <div ref="viewport" class="virtual-directory-list">
    <div :style="{ height: `${top}px` }" aria-hidden="true" />
    <div v-for="(item, index) in shown" :key="item.id || item.path" class="virtual-directory-row" :style="{ height: `${rowHeight}px` }"><slot :item="item" :index="start + index" /></div>
    <div :style="{ height: `${bottom}px` }" aria-hidden="true" />
  </div>
</template>
<style scoped>
.virtual-directory-list { min-height: 0; height: 100%; overflow: auto; scrollbar-width: none; overflow-anchor: none; overscroll-behavior: contain; }
.virtual-directory-list::-webkit-scrollbar { display: none; }
.virtual-directory-row { box-sizing: border-box; overflow: hidden; }
.virtual-directory-row :deep(button) { height: 100%; min-height: 0; width: 100%; }
.virtual-directory-row :deep(button > span) { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
</style>
