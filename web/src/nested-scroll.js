const motions = new WeakMap()

// Treat the page header and its virtual list as one continuous wheel surface.
export function listWheel(event) {
  if (event.ctrlKey || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return
  const list = event.currentTarget, page = list.closest('.page-scroll .thin-scroll-area')
  if (!page) return
  const delta = event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? page.clientHeight : 1)
  const boundary = list.closest('.file-browser,.transfer-table,.log-list-shell,.playback-list-shell') || list
  const header = Math.round(Math.min(page.scrollHeight - page.clientHeight, Math.max(0, page.scrollTop + boundary.getBoundingClientRect().top - page.getBoundingClientRect().top - 8)))
  const limit = header + Math.max(0, list.scrollHeight - list.clientHeight)
  if (!limit || !delta) return
  event.preventDefault()
  let motion = motions.get(list)
  const actual = page.scrollTop + list.scrollTop
  if (!motion || Math.abs(actual - motion.last) > 2) {
    if (motion) cancelAnimationFrame(motion.frame)
    motion = { target: actual, position: actual, last: actual, frame: 0, time: 0 }
    motions.set(list, motion)
  }
  motion.target = Math.min(limit, Math.max(0, motion.target + delta))
  const apply = value => {
    motion.position = value
    page.scrollTop = Math.min(header, value)
    list.scrollTop = Math.max(0, value - header)
    motion.last = page.scrollTop + list.scrollTop
  }
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
    cancelAnimationFrame(motion.frame); motion.frame = 0; apply(motion.target); return
  }
  if (motion.frame) return
  motion.time = performance.now()
  const tick = now => {
    if (!list.isConnected || !page.isConnected) { motions.delete(list); return }
    const elapsed = Math.min(40, now - motion.time)
    motion.time = now
    const distance = motion.target - motion.position
    if (Math.abs(distance) < .5) { apply(motion.target); motion.frame = 0; return }
    apply(motion.position + distance * (1 - Math.exp(-elapsed / 45)))
    motion.frame = requestAnimationFrame(tick)
  }
  motion.frame = requestAnimationFrame(tick)
}
