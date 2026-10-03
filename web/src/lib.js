import { reactive } from 'vue'

export const state = reactive({
  loaded: false, initialized: false, authenticated: false, storages: [], tasks: [],
  settings: {}, logs: [], cache: {}, traffic: {}, username: '', strmRoot: '', uptime: 0
})
export const notices = reactive([])
export function notify(message, error = false) {
  const id = Date.now() + Math.random()
  notices.push({ id, message, error })
  setTimeout(() => { const i = notices.findIndex(n => n.id === id); if (i >= 0) notices.splice(i, 1) }, 5000)
}
export async function api(url, method = 'GET', body) {
  const response = await fetch('/api' + url, {
    method, credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body)
  })
  let data
  try { data = await response.json() } catch { throw new Error('服务响应异常，请检查后端连接') }
  if (!response.ok) {
    if (response.status === 401 && !url.startsWith('/auth/')) state.authenticated = false
    throw new Error(data.error || '请求失败')
  }
  return data
}
export async function reload() {
  if (!state.authenticated) return
  Object.assign(state, await api('/state'))
}
export const drivers = [
  { id: '115', name: '115 网盘', subtitle: 'Open API', icon: '115', color: '#2389dc', kind: '云端存储', auth: '访问令牌', root: '0', tags: ['官方 API', '直连接入'] },
  { id: 'mobile', name: '移动云盘', subtitle: 'China Mobile', icon: 'M', color: '#269cc0', kind: '云端存储', auth: '原生 / OpenList 网关', root: '/', tags: ['原生个人云', 'CAS'] },
  { id: 'tianyi', name: '天翼云盘', subtitle: 'China Telecom', icon: '天', color: '#db9234', kind: '云端存储', auth: '原生账号登录', root: '-11', tags: ['原生个人云', 'CAS'] },
  { id: 'quark', name: '夸克网盘', subtitle: 'Quark', icon: 'Q', color: '#277eaf', kind: '云端存储', auth: 'Cookie', root: '0', tags: ['Cookie', '本机代理'] },
  { id: 'openlist', name: 'OpenList', subtitle: 'Storage gateway', icon: 'O', color: '#5479cb', kind: '协议与本地', auth: 'API Token', root: '/', tags: ['聚合存储', 'API'] },
  { id: 'webdav', name: 'WebDAV', subtitle: 'Web Distributed', icon: 'dav', color: '#5c8c78', kind: '协议与本地', auth: '账号密码', root: '/', tags: ['标准协议'] },
  { id: 'local', name: '本机存储', subtitle: 'Local filesystem', icon: 'local', color: '#7b8491', kind: '协议与本地', auth: '本地目录', root: '/mnt', tags: ['本地磁盘', '容器目录'] }
]
export const driverOf = type => drivers.find(d => d.id === type) || drivers[0]
export function bytes(value = 0) {
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let n = value / 1024, i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(1)} ${units[i]}`
}
export function date(value) {
  return !value || value.startsWith('0001') ? '尚未执行' : new Date(value).toLocaleString('zh-CN', { hour12: false })
}
