// Session locations are separate from the browser's long-lived preferences.
const jumpPageParents = {
  '/tasks/cache/settings': '/tasks/cache',
  '/tasks/cas/pool': '/tasks/cas',
  '/tasks/ed2k/pool': '/tasks/ed2k',
  '/tools/115-rapid': '/tools',
  '/virtual/quark/settings': '/virtual/quark',
  '/transfer/backup/settings': '/transfer/backup'
}

export function readSession(user, module) {
  try { return JSON.parse(sessionStorage.getItem(`aether-session:${user}:${module}`) || 'null') } catch { return null }
}
export function writeSession(user, module, value) {
  try { sessionStorage.setItem(`aether-session:${user}:${module}`, JSON.stringify(value)) } catch {}
}
export function modulePath(path) {
  return '/' + path.split('/').filter(Boolean)[0]
}
export function rememberRoute(user, path) {
  const saved = jumpPageParents[path] || path
  writeSession(user, `route:${modulePath(saved)}`, saved)
}
export function rememberedRoute(user, path) {
  const saved = readSession(user, `route:${path}`)
  return typeof saved === 'string' && modulePath(saved) === path ? jumpPageParents[saved] || saved : path
}
