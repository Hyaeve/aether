export const tabScroll = {
  mounted(el) {
    const wheel = event => {
      if (el.scrollWidth <= el.clientWidth + 1) return
      event.preventDefault()
      event.stopPropagation()
      el.scrollLeft += Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY
    }
    const reveal = () => {
      const active = el.querySelector('a.active')
      if (!active) return
      const a = active.getBoundingClientRect(), b = el.getBoundingClientRect()
      if (a.left < b.left) el.scrollLeft -= b.left - a.left
      else if (a.right > b.right) el.scrollLeft += a.right - b.right
    }
    const observer = new MutationObserver(reveal)
    observer.observe(el, { subtree: true, attributes: true, attributeFilter: ['class'] })
    el.addEventListener('wheel', wheel, { passive: false })
    el._tabCleanup = () => { el.removeEventListener('wheel', wheel); observer.disconnect() }
    requestAnimationFrame(reveal)
  },
  unmounted(el) { el._tabCleanup?.() }
}
