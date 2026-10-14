import { clipboardShareBatches } from './share-links.js'
import {withoutOwnShares} from './own-shares.js'

export function clipboardShares(text) {
  const batches = clipboardShareBatches(text)
  return batches.length ? { ...batches[0], batches } : null
}

export function watchShareClipboard({ ready, open, blocked = () => {}, status = () => {}, clipboard = navigator.clipboard, permissions = navigator.permissions, interval = 1500 }) {
  let stopped = false, reading = false, resumePending = false, prompted = false, generation = 0, readable = false, previous = '', warned = false
  const editing = 'input, textarea, [contenteditable]:not([contenteditable="false"])'
  const available = (resuming = false) => !stopped && ready() && document.visibilityState === 'visible' && document.hasFocus() && !document.querySelector('.modal, .video-viewer, .image-viewer') && (resuming || !document.activeElement?.closest?.(editing))
  function accept(text, explicit = false, resuming = false) {
    if (!available(resuming)) return
    const share = clipboardShares(withoutOwnShares(text))
    if (!share) { previous = ''; return }
    const signature = JSON.stringify(share.batches)
    if ((explicit || signature !== previous) && open(share) !== false) {
      previous = signature
    }
  }
  function unavailable(reason) { if (!warned) { warned = true; blocked(reason) } }
  async function read(gesture = false, explicit = false, resuming = false) {
    if (!clipboard?.readText) { status(window.isSecureContext ? 'unavailable' : 'insecure'); if (explicit) unavailable(window.isSecureContext ? 'unavailable' : 'insecure'); return }
    if (!available(resuming)) return
    if (reading) { resumePending ||= resuming; return }
    reading = true
    const run = generation
    try {
      let granted = readable
      try {
        const permission = (await permissions?.query({ name: 'clipboard-read' }))?.state
        granted = permission === 'granted' || readable
        if (run === generation) status(granted ? 'granted' : permission === 'denied' ? 'denied' : 'unknown')
      } catch {}
      // Idle polling never prompts; focus resume and explicit activation may retry.
      if (!granted && (!gesture || prompted && !explicit && !resuming)) return
      if (!granted) prompted = true
      if (run !== generation || !available(resuming)) return
      const text = await clipboard.readText()
      readable = true
      if (run === generation) status('granted')
      if (run === generation) accept(text, explicit, resuming)
    } catch { readable = false; if (run === generation) { status(window.isSecureContext ? 'denied' : 'insecure'); if (explicit) unavailable(window.isSecureContext ? 'permission' : 'insecure') } }
    finally {
      reading = false
      if (resumePending) { resumePending = false; read(true, false, true) }
    }
  }
  const paste = event => {
    if (!event.isTrusted || event.target?.closest?.(editing)) return
    accept(event.clipboardData?.getData('text/plain'), true)
  }
  const gesture = event => { if (event.isTrusted) read(true) }
  const focus = () => read(true, false, true)
  const visibility = () => { if (document.visibilityState === 'visible') read(true, false, true) }
  const activate = () => read(true, true, true)
  document.addEventListener('paste', paste)
  document.addEventListener('pointerup', gesture)
  window.addEventListener('focus', focus)
  document.addEventListener('visibilitychange', visibility)
  window.addEventListener('aether:listen-shares', activate)
  const timer = setInterval(() => read(), interval)
  read()
  return () => { stopped = true; generation++; clearInterval(timer); document.removeEventListener('paste', paste); document.removeEventListener('pointerup', gesture); window.removeEventListener('focus', focus); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('aether:listen-shares', activate) }
}
