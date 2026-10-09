<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
const props = defineProps({ entries: Array, disabled: Boolean })
const emit = defineEmits(['jump'])
const root = ref(null), measure = ref(null), hidden = ref(0), menu = ref(false)
const displayed = computed(() => props.entries.map((entry, index) => ({ ...entry, index })).slice(hidden.value))
function fit() {
  if (!root.value || !measure.value) return
  const widths = [...measure.value.children].map(el => el.getBoundingClientRect().width + 16)
  const available = root.value.clientWidth - 60
  let total = widths.reduce((sum, n) => sum + n, 0), count = 0
  while (count < widths.length - 1 && total + (count ? 32 : 0) > available) total -= widths[count++]
  hidden.value = count
}
watch(() => props.entries, async () => { menu.value = false; await nextTick(); fit() }, { deep: true })
let observer
onMounted(() => { observer = new ResizeObserver(fit); observer.observe(root.value); fit() })
onUnmounted(() => observer?.disconnect())
</script>
<template>
  <nav ref="root" class="file-breadcrumbs compact-path" aria-label="文件路径">
    <button :disabled="disabled" :aria-current="!entries.length ? 'location' : undefined" @click="emit('jump', -1)">根目录</button>
    <div v-if="hidden" class="path-overflow"><button :disabled="disabled" aria-label="展开省略路径" :aria-expanded="menu" @click="menu = !menu"><Icon name="Ellipsis" :size="16" /></button><div v-if="menu" class="path-overflow-menu"><button v-for="(entry, index) in entries.slice(0,hidden)" :key="index" @click="emit('jump', index); menu = false">{{ entry.name }}</button></div></div>
    <template v-for="entry in displayed" :key="entry.index"><Icon name="ChevronRight" :size="12" /><button :disabled="disabled" :aria-current="entry.index === entries.length - 1 ? 'location' : undefined" :data-tooltip="entry.name" @click="emit('jump', entry.index)">{{ entry.name }}</button></template>
    <div ref="measure" class="path-measure" aria-hidden="true"><span v-for="(entry,index) in entries" :key="index">{{ entry.name }}</span></div>
  </nav>
</template>
<style scoped>
.compact-path { position: relative; width: 100%; flex-wrap: nowrap; gap: 1px; }
.compact-path > button { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; padding: 4px 2px; min-width: 0; font: inherit; flex-shrink: 1; }
.compact-path > button:first-child, .compact-path > svg { flex-shrink: 0; }
.path-measure { position: absolute; top: 0; left: 0; visibility: hidden; pointer-events: none; display: flex; width: max-content; max-width: 0; overflow: hidden; white-space: nowrap; }
.path-measure span { flex-shrink: 0; font: inherit; padding: 4px 2px; }
.path-overflow { flex-shrink: 0; }
.path-overflow-menu { position: absolute; top: 100%; left: 42px; z-index: 15; display: flex; flex-direction: column; padding: 6px; max-width: min(300px,100%); max-height: 280px; overflow: auto; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); box-shadow: var(--shadow); }
.path-overflow-menu button { text-align: left; }
</style>
