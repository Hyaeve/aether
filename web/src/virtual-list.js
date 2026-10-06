import { computed, onUnmounted, ref, unref, watch } from 'vue'

export function useVirtualList(items, viewport, options = {}) {
  const scroll = ref(0), height = ref(500), width = ref(800)
  const rowHeight = computed(() => unref(options.rowHeight) || 52)
  const columns = computed(() => options.grid?.value ? Math.max(1, Math.floor(width.value / 166)) : 1)
  const header = computed(() => unref(options.header) || 0)
  const rows = computed(() => Math.ceil(items.value.length / columns.value))
  const first = computed(() => Math.min(Math.max(0, rows.value - 1), Math.max(0, Math.floor((scroll.value - header.value) / rowHeight.value) - 5)))
  const last = computed(() => Math.min(rows.value, Math.ceil((scroll.value + height.value) / rowHeight.value) + 6))
  const start = computed(() => first.value * columns.value)
  const end = computed(() => last.value * columns.value)
  const shown = computed(() => items.value.slice(start.value, end.value))
  const top = computed(() => first.value * rowHeight.value)
  const bottom = computed(() => Math.max(0, rows.value - last.value) * rowHeight.value)
  let observer
  const sync = () => { if (viewport.value) scroll.value = viewport.value.scrollTop }
  watch(viewport, (el, previous) => {
    previous?.removeEventListener('scroll', sync)
    observer?.disconnect()
    if (!el) return
    el.addEventListener('scroll', sync, { passive: true })
    observer = new ResizeObserver(() => {
      height.value = el.clientHeight
      width.value = el.clientWidth
    })
    observer.observe(el)
    sync()
  }, { flush: 'post' })
  function reset() { scroll.value = 0; if (viewport.value) viewport.value.scrollTop = 0 }
  function reveal(index) {
    if (!viewport.value || index < 0) return
    viewport.value.scrollTop = Math.floor(index / columns.value) * rowHeight.value
    sync()
  }
  watch(() => items.value.length, () => {
    if (scroll.value > rows.value * rowHeight.value) reset()
  })
  onUnmounted(() => { observer?.disconnect(); viewport.value?.removeEventListener('scroll', sync) })
  return { shown, start, top, bottom, columns, reset, reveal }
}
