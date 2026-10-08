import { watch } from 'vue'
import { api, state } from './lib'
const cached = new Map(), pending = new Map()
let generation = 0
watch(() => state.authenticated, () => { generation++; cached.clear(); pending.clear() }, { flush: 'sync' })
export function readPlugin(kind, fresh = false) {
  if (!fresh && cached.has(kind)) return Promise.resolve({ ...cached.get(kind) })
  if (pending.has(kind)) return pending.get(kind)
  const run = generation
  const request = api(`/plugins/${kind}`).then(value => { if (run === generation) cached.set(kind, value); return { ...value } }).finally(() => { if (pending.get(kind) === request) pending.delete(kind) })
  pending.set(kind, request)
  return request
}
export function invalidatePlugin(kind) { cached.delete(kind) }
