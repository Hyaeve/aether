import { watch } from 'vue'
import { api, state } from './lib.js'

export function createCloudSessionCache(fetchPage) {
  const entries = new Map()
  return {
    clear() { entries.clear() },
    peek(key) { return entries.get(key)?.snapshot || null },
    async load(key, kind, storage, page = 1, refresh = false) {
      let entry = entries.get(key)
      if (!entry || refresh) {
        entry = { snapshot: null, pending: new Map() }
        entries.delete(key)
        entries.set(key, entry)
        if (entries.size > 32) entries.delete(entries.keys().next().value)
      }
      if (!refresh && page === 1 && entry.snapshot) return entry.snapshot
      if (entry.pending.has(page)) return entry.pending.get(page)
      const promise = fetchPage(kind, storage, page).then(value => {
        if (entries.get(key) !== entry) return null
        const incoming = (value.items || []).map(item => storage === 'cas' ? { ...item, url: '' } : item)
        const previous = page === 1 ? [] : entry.snapshot?.items || []
        const known = new Set(previous.map(item => item.id))
        const added = incoming.filter(item => !known.has(item.id))
        if (page > 1 && incoming.length && !added.length) throw new Error('云盘分页内容重复，请刷新核对')
        entry.snapshot = {
          ...value, items: [...previous, ...added],
          nextPage: Number.isInteger(value.nextPage) && value.nextPage > page ? value.nextPage : 0
        }
        return entry.snapshot
      }).finally(() => entry.pending.delete(page))
      entry.pending.set(page, promise)
      return promise
    }
  }
}

export const cloudListings = createCloudSessionCache((kind, storage, page) =>
  api(`/cloud/manage?kind=${kind}&storage=${encodeURIComponent(storage)}&page=${page}`))
const selections = new Map()
export const cloudListingKey = (kind, storage) => JSON.stringify([
  state.username, kind, storage,
  state.storages.map(({ id, type, name, enabled, config, viewRevision }) => ({ id, type, name, enabled, config, viewRevision }))
])
export const cloudSelectedPool = kind => selections.get(kind)
export function rememberCloudPool(kind, storage) { selections.set(kind, storage) }
watch(() => [state.authenticated, state.username], () => {
  cloudListings.clear()
  selections.clear()
}, { flush: 'sync' })
