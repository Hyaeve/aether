export function compactDate(value, empty = '--') {
  if (!value || String(value).startsWith('0001')) return empty
  const d = new Date(value)
  if (!Number.isFinite(d.getTime())) return empty
  const pad = n => String(n).padStart(2, '0')
  return `${pad(d.getFullYear() % 100)}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
