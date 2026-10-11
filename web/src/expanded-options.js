import { nextTick } from 'vue'

export async function revealOptions(element) {
  await nextTick()
  if (!element?.isConnected) return
  const panel = element.closest('.modal')
  if (!panel) return
  const content = element.matches('.disclosure') ? element.nextElementSibling : element
  if (!content) return
  let bottom = content.getBoundingClientRect().bottom
  const behavior = matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth'
  for (let node = element.parentElement; node; node = node.parentElement) {
    const room = node.scrollHeight - node.clientHeight - node.scrollTop
    if (room > 0 && /auto|scroll/.test(getComputedStyle(node).overflowY)) {
      const edge = Math.min(node.getBoundingClientRect().bottom, panel.getBoundingClientRect().bottom)
      const distance = Math.min(room, Math.max(0, bottom - edge + 24))
      node.scrollBy({ top: distance, behavior })
      bottom -= distance
    }
    if (node === panel) break
  }
}
