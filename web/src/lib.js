import { reactive, watch } from 'vue'
import {prepareConfigRemoval,cancelCardRemovals} from './config-card-removal.js'
import {clearMembershipCache,mergeMemberships} from './storage-membership-cache.mjs'

export const state = reactive({
  loaded: false, initialized: false, authenticated: false, storages: [], tasks: [], scrapeLibraries: [],
  settings: {}, logs: [], links: [], fileFavorites: [], fileVisits: [], libraryNotices: [], shareNotices: [], cache: {}, traffic: {}, username: '', strmRoot: '', uptime: 0
})
let linksRequest, linksGeneration = 0
watch(() => state.authenticated, authenticated => {
	if(!authenticated){cancelCardRemovals();clearMembershipCache()}
  linksGeneration++
  linksRequest = null
  state.links = []
  state.shareNotices = []
  state.fileFavorites = []
  state.fileVisits = []
  state.scrapeLibraries = []
}, { flush: 'sync' })
// Memory-only, redacted cards survive route changes, never logout or another session.
export function loadLinks() {
  if (linksRequest) return linksRequest
  const generation = linksGeneration
  const request = api('/links').then(links => {
    if (generation === linksGeneration && state.authenticated) state.links = links
  }).finally(() => { if (linksRequest === request) linksRequest = null })
  linksRequest = request
  return request
}
export const notices = reactive([])
const noticeTimers = new Map()
export function notify(message, error = false) {
  const existing = notices.find(n => n.message === message && n.error === error)
  if (existing) {
    clearTimeout(noticeTimers.get(existing.id))
    noticeTimers.set(existing.id, setTimeout(() => {
      const i = notices.findIndex(n => n.id === existing.id)
      if (i >= 0) notices.splice(i, 1)
      noticeTimers.delete(existing.id)
    }, 5000))
    return existing
  }
  const id = Date.now() + Math.random()
  notices.push({ id, message, error })
  noticeTimers.set(id, setTimeout(() => { const i = notices.findIndex(n => n.id === id); if (i >= 0) notices.splice(i, 1); noticeTimers.delete(id) }, 5000))
  return notices.at(-1)
}
export async function api(url, method = 'GET', body) {
	const removed=prepareConfigRemoval(url,method,body)
  const response = await fetch('/api' + url, {
    method, credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body)
  })
  let data
  try { data = await response.json() } catch { throw new Error('服务响应异常，请检查后端连接') }
  if (!response.ok) {
    if (response.status === 401 && !url.startsWith('/auth/')) state.authenticated = false
    const error = new Error(data.error || '请求失败')
    error.status = response.status
    error.field = data.field
    throw error
  }
  removed()
  return data
}
export async function reload() {
  if (!state.authenticated) return
  const generation=linksGeneration
  const data = await api('/state')
  if(!state.authenticated||generation!==linksGeneration)return
  if(Array.isArray(data.storages))mergeMemberships(data.username,data.storages)
  Object.assign(state, { shareNotices: [], scrapeLibraries: [] }, data)
}
export const drivers = [
  { id: '115', name: '115 网盘', subtitle: '115driver', icon: '115', color: '#2389dc', kind: '云端存储', auth: 'CK', root: '0', tags: ['Cookie', '直连接入'] },
  { id: 'mobile', name: '移动云盘', subtitle: 'China Mobile', icon: 'M', color: '#269cc0', kind: '云端存储', auth: '原生个人云', root: '/', tags: ['原生个人云', 'CAS'] },
  { id: 'tianyi', name: '天翼云盘', subtitle: 'China Telecom', icon: '天', color: '#db9234', kind: '云端存储', auth: '原生账号登录', root: '-11', tags: ['原生个人云', 'CAS'] },
  { id: 'quark', name: '夸克网盘', subtitle: 'Quark', icon: 'Q', color: '#277eaf', kind: '云端存储', auth: 'Cookie', root: '0', tags: ['Cookie', '本机代理'] },
  { id: 'openlist', name: 'OpenList', subtitle: 'Storage gateway', icon: 'O', color: '#5479cb', kind: '协议与本地', auth: 'API Token', root: '/', tags: ['聚合存储', 'API'] },
  { id: 'webdav', name: 'WebDAV', subtitle: 'Web Distributed', icon: 'dav', color: '#5c8c78', kind: '协议与本地', auth: '账号密码', root: '/dav', tags: ['标准协议'] },
  { id: 'local', name: '本机存储', subtitle: 'Local filesystem', icon: 'local', color: '#7b8491', kind: '协议与本地', auth: '本地目录', root: '', tags: ['本地磁盘', '容器目录'] }
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
