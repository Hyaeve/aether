<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
const props = defineProps({ parents: Array, deepest: String })
const root = ref(null), measure = ref(null), parent = ref(''), omitted = ref(false)
const full = computed(() => [...(props.parents || []), props.deepest].join('/'))
let observer
function fit() {
  if (!root.value || !measure.value) return
  const available = root.value.clientWidth
  const width = text => { measure.value.setAttribute('data-measure', text); return measure.value.getBoundingClientRect().width }
  const parents = props.parents || []
  let count = parents.length
  while (count > 0 && width(`/${count < parents.length ? '../' : ''}${parents.slice(-count).join('/')}/${props.deepest}`) > available) count--
  omitted.value = count < parents.length
  parent.value = count ? parents.slice(-count).join('/') : ''
}
onMounted(() => { observer = new ResizeObserver(fit); observer.observe(root.value); fit() })
watch(full, fit, { flush: 'post' })
onUnmounted(() => observer?.disconnect())
</script>
<template><span ref="root" class="visit-path" :aria-label="full"><span v-if="omitted">/../</span><span v-else>/</span><span v-if="parent" class="visit-parent">{{ parent }}/</span><span class="visit-deepest">{{ deepest }}</span><span ref="measure" class="visit-measure" aria-hidden="true" /></span></template>
<style scoped>
.visit-path { display:flex; align-items:center; flex:1; min-width:0; gap:0; color:var(--muted); white-space:nowrap; }
.visit-parent { flex:none; }.visit-deepest { min-width:0; overflow:hidden; text-overflow:ellipsis; }.visit-measure { position:absolute; visibility:hidden; white-space:pre; pointer-events:none; }.visit-measure::before { content:attr(data-measure); }
</style>
