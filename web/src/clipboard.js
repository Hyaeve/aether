export async function copyText(text) {
  if (!text) return
  try {
    if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(text); return }
  } catch {}
  const previous = document.activeElement
  const input = document.createElement('textarea')
  input.value = text
  input.setAttribute('readonly', '')
  Object.assign(input.style, { position: 'fixed', left: '-9999px', top: '0', opacity: '0' })
  document.body.appendChild(input)
  try {
    input.focus(); input.select()
    if (!document.execCommand('copy')) throw new Error('复制失败')
  } finally { input.remove(); previous?.focus({ preventScroll: true }) }
}
