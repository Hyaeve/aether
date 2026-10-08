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
  const sync = () => {
    if (!viewport.value) return
    if (options.window) {
      scroll.value = Math.max(0, -viewport.value.getBoundingClientRect().top)
      height.value = window.innerHeight
    } else scroll.value = viewport.value.scrollTop
  }
  watch(viewport, (el, previous) => {
    previous?.removeEventListener('scroll', sync)
    window.removeEventListener('scroll', sync)
    window.removeEventListener('resize', sync)
    observer?.disconnect()
    if (!el) return
    ;(options.window ? window : el).addEventListener('scroll', sync, { passive: true })
    if (options.window) window.addEventListener('resize', sync, { passive: true })
    observer = new ResizeObserver(() => {
      height.value = options.window ? window.innerHeight : el.clientHeight
      width.value = el.clientWidth
      sync()
    })
    observer.observe(el)
    sync()
  }, { flush: 'post' })
  function reset() {
    scroll.value = 0
    if (!viewport.value) return
    if (options.window) {
      if (viewport.value.getBoundingClientRect().top < 0) viewport.value.scrollIntoView({ block: 'start' })
    } else viewport.value.scrollTop = 0
  }
  function reveal(index) {
    if (!viewport.value || index < 0) return
    const offset = Math.floor(index / columns.value) * rowHeight.value
    if (options.window) window.scrollTo(0, window.scrollY + viewport.value.getBoundingClientRect().top + offset)
    else viewport.value.scrollTop = offset
    sync()
  }
  watch(() => items.value.length, () => {
    if (scroll.value > rows.value * rowHeight.value) reset()
  })
  onUnmounted(() => { observer?.disconnect(); viewport.value?.removeEventListener('scroll', sync); window.removeEventListener('scroll', sync); window.removeEventListener('resize', sync) })
  return { shown, start, top, bottom, columns, reset, reveal }
}
