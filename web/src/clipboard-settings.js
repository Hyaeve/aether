import { computed, ref } from 'vue'
import { state } from './lib'

const revision = ref(0)
const fallback = new Map()
const key = () => `aether-clipboard-enabled:${state.username}`
export const clipboardStatus = ref('unknown')
export const clipboardEnabled = computed({
  get() {
    revision.value
    if (fallback.has(key())) return fallback.get(key())
    try { return localStorage.getItem(key()) === 'true' && localStorage.getItem(`${key()}:consent`) === '1' } catch { return false }
  },
  set(value) {
    fallback.set(key(), !!value)
    try { localStorage.setItem(key(), String(!!value)); if (value) localStorage.setItem(`${key()}:consent`, '1') } catch { /* Keep the switch usable without browser storage. */ }
    revision.value++
  }
})

export function clipboardCapability() {
  if (!window.isSecureContext) return 'insecure'
  return navigator.clipboard?.readText ? 'available' : 'unavailable'
}

export function authorizeClipboard() {
  window.dispatchEvent(new Event('aether:listen-shares'))
}
