export function changedNameParts(before, after) {
  const a = Array.from(before), b = Array.from(after)
  // Names are bounded by the rename API; cap fallback input to avoid a large matrix.
  if (a.length > 512 || b.length > 512) return [{ text: after, changed: before !== after }]
  const rows = Array.from({ length: a.length + 1 }, () => new Uint16Array(b.length + 1))
  for (let i = a.length - 1; i >= 0; i--) for (let j = b.length - 1; j >= 0; j--) rows[i][j] = a[i] === b[j] ? rows[i + 1][j + 1] + 1 : Math.max(rows[i + 1][j], rows[i][j + 1])
  const parts = []
  function add(text, changed) { const last = parts.at(-1); if (last?.changed === changed) last.text += text; else parts.push({ text, changed }) }
  let i = 0, j = 0
  while (j < b.length) {
    if (i < a.length && a[i] === b[j]) { add(b[j++], false); i++ }
    else if (i < a.length && rows[i + 1][j] > rows[i][j + 1]) i++
    else add(b[j++], true)
  }
  return parts
}
