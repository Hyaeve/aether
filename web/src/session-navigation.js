// Session locations are separate from the browser's long-lived preferences.
export function readSession(user, module) {
  try { return JSON.parse(sessionStorage.getItem(`aether-session:${user}:${module}`) || 'null') } catch { return null }
}
export function writeSession(user, module, value) {
  try { sessionStorage.setItem(`aether-session:${user}:${module}`, JSON.stringify(value)) } catch {}
}
export function modulePath(path) {
  return '/' + path.split('/').filter(Boolean)[0]
}
export function rememberedRoute(user, path) {
  const saved = readSession(user, `route:${path}`)
  return typeof saved === 'string' && modulePath(saved) === path ? saved : path
}
