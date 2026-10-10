export function rememberVisit(previous, visit) {
  previous = validVisits(previous)
  if (!validVisits([visit]).length) return previous
  const next = { storage: visit.storage, id: visit.id, history: visit.history.map(h => ({ id: h.id, name: h.name })) }
  const ancestors = item => item.history.map(h => h.id)
  const sameBranch = (a, b) => a.storage === b.storage && (a.id === b.id || ancestors(a).includes(b.id) || ancestors(b).includes(a.id))
  const descendants = previous.filter(item => item.storage === next.storage && ancestors(item).includes(next.id))
  // Revisiting a shared ancestor must not discard distinct sibling branches.
  if (descendants.length) return [...descendants, ...previous.filter(item => !sameBranch(item, next))].slice(0, 5)
  return [next, ...previous.filter(item => !sameBranch(item, next))].slice(0, 5)
}

export function validVisits(value) {
  return Array.isArray(value) ? value.filter(v => v && typeof v.storage === 'string' && typeof v.id === 'string' && Array.isArray(v.history) && v.history.length >= 2 && v.history.length <= 128 && v.history.every(h => h && typeof h.id === 'string' && typeof h.name === 'string')).slice(0, 5) : []
}
