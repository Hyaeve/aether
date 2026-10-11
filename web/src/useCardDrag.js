import { onMounted, onUnmounted, onUpdated, ref } from 'vue'
import './card-interactions.css'

const controls = 'button, a, input, select, textarea, summary, [role="button"], [role="switch"], [contenteditable], .rounded-select, .task-row-menu, .storage-menu-control'
export const cardControl = event => !!event.target.closest(controls)

export function useCardDrag({ selector, disabled = () => false, onDrop, onStart = () => {} }) {
  const armed = ref(''), dragging = ref(''), dropTarget = ref('')
  let pointer, controlPointer, timer, ghost, blockedClick, clickTimer

  function removePreview() {
    ghost?.remove(); ghost = null
    document.documentElement.classList.remove('card-drag-active')
  }
  function cancel() {
    clearTimeout(timer)
    const current = pointer
    pointer = null
    if (current?.card.hasPointerCapture?.(current.id)) current.card.releasePointerCapture(current.id)
    armed.value = ''; dragging.value = ''; dropTarget.value = ''
    removePreview()
  }
  function suppress(card) {
    blockedClick = card
    clearTimeout(clickTimer)
    clickTimer = setTimeout(() => { blockedClick = null }, 700)
  }
  function preview(card) {
    const box = card.getBoundingClientRect()
    ghost = card.cloneNode(true)
    // Preserve ancestor-dependent and scoped styles when moving the copy to body.
    const originals = [card, ...card.querySelectorAll('*')]
    const copies = [ghost, ...ghost.querySelectorAll('*')]
    originals.forEach((node, index) => {
      const computed = getComputedStyle(node)
      for (const property of computed) copies[index].style.setProperty(property, computed.getPropertyValue(property))
      copies[index].removeAttribute('id')
    })
    ghost.querySelectorAll('.task-menu, .storage-menu').forEach(menu => menu.remove())
    ghost.classList.add('card-drag-ghost')
    ghost.setAttribute('aria-hidden', 'true'); ghost.inert = true
    Object.assign(ghost.style, { position: 'fixed', left: `${box.left}px`, top: `${box.top}px`, width: `${box.width}px`, height: `${box.height}px`, margin: '0', opacity: '1', pointerEvents: 'none', zIndex: '10000', transition: 'none', transform: 'none', boxShadow: 'var(--shadow)' })
    document.body.append(ghost)
  }
  function hold(event, item) {
    if (event.button !== 0 || event.isPrimary === false || disabled()) return
    blockedClick = null; clearTimeout(clickTimer)
    if (cardControl(event)) {
      controlPointer = { id: event.pointerId, control: event.target.closest(controls), card: event.currentTarget }
      return
    }
    controlPointer = null
    cancel(); blockedClick = null; clearTimeout(clickTimer)
    pointer = { id: event.pointerId, item: item.id, card: event.currentTarget, x: event.clientX, y: event.clientY, dx: 0, dy: 0 }
    timer = setTimeout(() => {
      if (!pointer || !pointer.card.isConnected || disabled()) { cancel(); return }
      onStart(); preview(pointer.card)
      armed.value = pointer.item; dragging.value = pointer.item
      suppress(pointer.card)
      document.documentElement.classList.add('card-drag-active')
      try { pointer.card.setPointerCapture?.(pointer.id) } catch {}
    }, 160)
  }
  function move(event) {
    if (!pointer || event.pointerId !== pointer.id) return
    pointer.dx = event.clientX - pointer.x; pointer.dy = event.clientY - pointer.y
    if (!dragging.value) {
      if (Math.hypot(pointer.dx, pointer.dy) > 8) { suppress(pointer.card); cancel() }
      return
    }
    event.preventDefault()
    ghost.style.transform = `translate3d(${pointer.dx}px, ${pointer.dy}px, 0)`
    const card = document.elementFromPoint(event.clientX, event.clientY)?.closest(selector)
    dropTarget.value = card?.parentElement === pointer.card.parentElement ? card.dataset.id || '' : ''
  }
  function up(event) {
    if (controlPointer?.id === event.pointerId) {
      const released = document.elementFromPoint(event.clientX, event.clientY)
      if (!controlPointer.control.contains(released)) suppress(controlPointer.card)
      controlPointer = null
    }
    if (!pointer || event.pointerId !== pointer.id) return
    const id = pointer.item, target = dropTarget.value, active = !!dragging.value
    if (active) suppress(pointer.card)
    cancel()
    if (active && target && id !== target) onDrop(id, target)
  }
  function cancelled() { controlPointer = null; if (pointer) suppress(pointer.card); cancel() }
  function escape(event) { if (event.key === 'Escape') cancelled() }
  function visibility() { if (document.hidden) cancelled() }
  function touchMove(event) { if (dragging.value) event.preventDefault() }
  function captureClick(event) {
    if (!blockedClick?.contains(event.target)) return
    event.preventDefault(); event.stopImmediatePropagation()
    blockedClick = null; clearTimeout(clickTimer)
  }
  function contextMenu(event) { if (dragging.value) { event.preventDefault(); event.stopImmediatePropagation() } }
  function cardClick(event, item, open) { if (!cardControl(event)) open(item) }
  onUpdated(() => { if (pointer && !pointer.card.isConnected) cancelled() })
  onMounted(() => {
    document.addEventListener('pointermove', move, { passive: false })
    document.addEventListener('pointerup', up)
    document.addEventListener('pointercancel', cancelled)
    document.addEventListener('lostpointercapture', cancelled)
    document.addEventListener('keydown', escape)
    document.addEventListener('visibilitychange', visibility)
    document.addEventListener('touchmove', touchMove, { passive: false })
    document.addEventListener('click', captureClick, true)
    document.addEventListener('contextmenu', contextMenu, true)
    window.addEventListener('blur', cancelled)
  })
  onUnmounted(() => {
    cancel(); clearTimeout(clickTimer)
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', up)
    document.removeEventListener('pointercancel', cancelled)
    document.removeEventListener('lostpointercapture', cancelled)
    document.removeEventListener('keydown', escape)
    document.removeEventListener('visibilitychange', visibility)
    document.removeEventListener('touchmove', touchMove)
    document.removeEventListener('click', captureClick, true)
    document.removeEventListener('contextmenu', contextMenu, true)
    window.removeEventListener('blur', cancelled)
  })
  return { armed, dragging, dropTarget, hold, cardClick, cancel }
}
