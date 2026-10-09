<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, notify, date, state, loadLinks } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import SecretInput from '../components/SecretInput.vue'
import { copyText } from '../clipboard'
const types = [{ id: 'audiobookshelf', name: 'AudioBookShelf', icon: 'abs' }, { id: 'emby', name: 'Emby', icon: 'emby' }, { id: 'fnos', name: '飞牛影视', icon: 'fnmovie' }]
const modes = [{ value: 'always', label: '始终跳转', icon: 'ExternalLink' }, { value: 'public', label: '公网跳转', icon: 'Globe' }, { value: 'private', label: '内网跳转', icon: 'House' }, { value: 'never', label: '始终中继', icon: 'CornerDownLeft' }]
const route = useRoute()
const tab = computed(() => route.path.endsWith('/cache') ? 'cache' : 'manage')
const links = computed(() => state.links), modal = ref(false), step = ref(1), busy = ref(false), error = ref(''), events = shallowRef([]), query = ref(''), deleting = ref(null)
const form = reactive({})
const menu = ref(null)
const outcome = ref('all')
const outcomes = [{ value: 'all', label: '全部类型' }, { value: 'redirect', label: '302' }, { value: 'proxy', label: '中继' }, { value: 'transcode', label: '适配' }, { value: 'passthrough', label: '透传' }, { value: 'local', label: '本地' }, { value: 'error', label: '失败' }, { value: 'unauthorized', label: '未授权' }]
const armed = ref(''), dragging = ref(''), dropTarget = ref(''), sorting = ref(false)
let holdTimer, releaseTimer, suppressClick = false, pointer = null
const interactive = event => event.target.closest('button, a, input, .rounded-select')
function hold(event, link) {
  if (event.button !== 0 || sorting.value || interactive(event)) return
  clearTimeout(holdTimer); clearTimeout(releaseTimer)
  const card = event.currentTarget
  pointer = { id: event.pointerId, x: event.clientX, y: event.clientY, link: link.id }
  holdTimer = setTimeout(() => { armed.value = link.id; suppressClick = true; card?.setPointerCapture?.(event.pointerId) }, 160)
}
function pointerMove(event) {
  if (!pointer || event.pointerId !== pointer.id) return
  if (!armed.value) {
    if (Math.hypot(event.clientX - pointer.x, event.clientY - pointer.y) > 8) { clearTimeout(holdTimer); pointer = null }
    return
  }
  if (Math.hypot(event.clientX - pointer.x, event.clientY - pointer.y) < 5) return
  event.preventDefault()
  dragging.value = pointer.link
  dropTarget.value = document.elementFromPoint(event.clientX, event.clientY)?.closest('.link-card')?.dataset.id || ''
}
function pointerUp(event) {
  if (!pointer || event.pointerId !== pointer.id) return
  const id = pointer.link, target = dropTarget.value
  pointer = null
  if (dragging.value && target) moveLink(id, target)
  else dragEnd()
}
function pointerCancel() { pointer = null; dragEnd() }
function release() {
  clearTimeout(holdTimer)
  releaseTimer = setTimeout(() => { armed.value = ''; suppressClick = false }, 100)
}
function cardClick(event, link) { if (!suppressClick && !interactive(event)) open(link) }
function dragEnd() { dragging.value = ''; dropTarget.value = ''; release() }
async function moveLink(id, target) {
  dragEnd()
  if (!id || id === target || sorting.value) return
  sorting.value = true
  try { await api('/links/reorder', 'POST', { id, target }); await load() }
  catch (e) { notify(e.message, true) } finally { sorting.value = false }
}
function cardKey(event, link) {
  if (event.target !== event.currentTarget) return
  if (event.key === 'Enter') { open(link); return }
  if (!event.altKey || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return
  event.preventDefault()
  const index = links.value.findIndex(l => l.id === link.id)
  const target = links.value[index + (['ArrowLeft', 'ArrowUp'].includes(event.key) ? -1 : 1)]
  if (target) moveLink(link.id, target.id)
}
function context(event, link) { menu.value = { link, x: Math.min(event.clientX, window.innerWidth - 160), y: Math.min(event.clientY, window.innerHeight - 190) } }
function closeMenu() { menu.value = null }
function escapeMenu(e) { if (e.key === 'Escape') closeMenu() }
async function testLink(link) {
  closeMenu()
  try { await api(`/links/${link.id}/test`, 'POST'); notify('以链连接成功') } catch (e) { notify(e.message, true) }
}
const displayed = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q && outcome.value === 'all') return events.value
  return events.value.filter(e => (outcome.value === 'all' || e.outcome === outcome.value) && (!q || `${linkName(e.upstream)} ${e.mediaPath} ${e.path} ${e.target} ${e.client} ${e.userAgent} ${e.effectiveUserAgent} ${e.error}`.toLowerCase().includes(q)))
})
const scrollTop = ref(0), playbackScroller = ref(null)
const start = computed(() => Math.max(0, Math.floor(scrollTop.value / 52) - 5))
const shown = computed(() => displayed.value.slice(start.value, start.value + 40))
watch(() => displayed.value.length, () => {
  if (start.value >= displayed.value.length) {
    scrollTop.value = 0
    if (playbackScroller.value) playbackScroller.value.scrollTop = 0
  }
})
watch([query, outcome], () => { scrollTop.value = 0; if (playbackScroller.value) playbackScroller.value.scrollTop = 0 })
const outcomeLabel = e => ({ redirect: '302', proxy: '中继', transcode: '适配', passthrough: '透传', local: '本地', error: '失败', unauthorized: '拒绝' })[e.outcome] || e.outcome
const targetText = e => e.target || e.mediaPath || e.error || e.path || '—'
const uaText = e => e.effectiveUserAgent && e.effectiveUserAgent !== e.userAgent ? `${e.userAgent || '空'} → ${e.effectiveUserAgent}` : e.userAgent || '空'
const cacheText = e => e.cacheSource === 'restored' ? '恢复命中' : e.cacheSource === 'hit' || e.cacheHit ? '缓存命中' : '首次获取'
const cacheClass = e => e.cacheSource === 'restored' ? 'restored' : e.cacheSource === 'hit' || e.cacheHit ? 'hit' : 'fresh'
const ttl = e => {
  if (!(e.cacheTtlSeconds > 0)) return '不缓存'
  const minutes = Math.ceil(e.cacheTtlSeconds / 60)
  return minutes > 60 ? `${Math.floor(minutes / 60)} h${minutes % 60 ? ` ${minutes % 60} min` : ''}` : `${minutes} min`
}
async function copyValue(value) { if (!value) return; try { await copyText(value); notify('已复制') } catch { notify('复制失败，请检查浏览器权限', true) } }
function clock(value) {
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : `${String(d.getMonth() + 1).padStart(2, '0')}/${String(d.getDate()).padStart(2, '0')} ${d.toLocaleTimeString('zh-CN', { hour12: false })}`
}
const typeOf = type => types.find(t => t.id === type) || types[0]
const linkName = id => links.value.find(l => l.id === id)?.name || id
const endpoint = link => { const u = new URL(location.href); u.port = String(link.port); u.pathname = '/'; u.search = ''; u.hash = ''; u.protocol = 'http:'; return u.href }
async function load() {
  try { await loadLinks() }
  catch (e) { notify(e.message, true) }
}
let playbackLoading = false
async function loadPlayback() {
  if (playbackLoading) return
  playbackLoading = true
  try { events.value = (await api('/link-playback')).recentEvents || [] }
  catch (e) { notify(e.message, true) }
  finally { playbackLoading = false }
}
watch(tab, value => { if (value === 'cache') loadPlayback() }, { immediate: true })
function open(link) { closeMenu(); Object.assign(form, { id: '', name: '', type: '', address: '', port: '', apiKey: '', username: '', password: '', mode: 'always', enabled: true, blockedUA: '', mediaRoot: '', upstreamRoot: '' }, link || {}); error.value = ''; step.value = link ? 2 : 1; modal.value = true }
async function save() {
  busy.value = true; error.value = ''
  try { await api(form.id ? `/links/${form.id}` : '/links', form.id ? 'PUT' : 'POST', form); await load(); modal.value = false; notify('以链已保存') }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
async function update(link, patch) {
  busy.value = true
  try { await api(`/links/${link.id}`, 'PUT', { ...link, ...patch }); await load(); notify('以链已更新') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
async function remove() {
  busy.value = true
  try { await api(`/links/${deleting.value.id}`, 'DELETE'); deleting.value = null; await load(); notify('以链已删除') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
let timer
onMounted(() => { document.addEventListener('pointermove', pointerMove, { passive: false }); document.addEventListener('pointerup', pointerUp); document.addEventListener('pointercancel', pointerCancel); document.addEventListener('click', closeMenu); document.addEventListener('keydown', escapeMenu); load(); timer = setInterval(() => { if (tab.value === 'cache') loadPlayback() }, 5000) })
onUnmounted(() => { document.removeEventListener('pointermove', pointerMove); document.removeEventListener('pointerup', pointerUp); document.removeEventListener('pointercancel', pointerCancel); clearInterval(timer); clearTimeout(holdTimer); clearTimeout(releaseTimer); document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', escapeMenu) })
</script>
<template>
  <div class="link-heading">
    <nav v-tab-scroll class="content-tabs" aria-label="以太链接栏目"><RouterLink to="/links/manage" :class="{ active: tab === 'manage' }"><Icon name="Waypoints" :size="17" />以链管理</RouterLink><RouterLink to="/links/cache" :class="{ active: tab === 'cache' }"><Icon name="ListVideo" :size="17" />直链缓存</RouterLink></nav>
    <div v-if="tab === 'cache'" class="playback-toolbar"><button class="icon-btn" aria-label="刷新播放流水" @click="loadPlayback"><Icon name="RefreshCw" /></button><RoundedSelect v-model="outcome" label="筛选播放类型" :options="outcomes" /><div class="toolbar-right"><div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索播放流水" placeholder="搜索播放流水…" /></div></div></div>
  </div>
  <div v-if="tab === 'manage'" class="link-grid">
    <article v-for="link in links" :key="link.id" class="link-card" :data-id="link.id" tabindex="0" :aria-label="link.name" :class="{ 'drag-armed': armed === link.id, dragging: dragging === link.id, 'drop-target': dropTarget === link.id }" :draggable="false" @pointerdown="hold($event, link)" @click="cardClick($event, link)" @keydown="cardKey($event, link)" @dragstart.prevent @contextmenu.prevent.stop="context($event, link)">
      <div class="link-identity"><button class="link-toggle" :aria-label="`${link.enabled ? '停用' : '启用'}以链 ${link.name}`" :aria-pressed="link.enabled" :disabled="busy" @click.stop="update(link, { enabled: !link.enabled })"><img :src="`/media/${typeOf(link.type).icon}.png`" :alt="typeOf(link.type).name" /></button><strong>{{ link.name }}</strong></div>
      <div class="link-card-controls"><RoundedSelect :model-value="link.mode" :label="`${link.name}跳转模式`" :options="modes" :disabled="busy" @update:model-value="update(link, { mode: $event })" /><div class="link-port-actions"><a :href="endpoint(link)" target="_blank" rel="noopener noreferrer"><Icon name="ArrowUpRight" :size="15" />{{ link.port }}</a><button class="icon-btn" :aria-label="`以链操作 ${link.name}`" :aria-expanded="menu?.link.id === link.id" @click.stop="context($event, link)"><Icon name="Ellipsis" /></button></div></div>
    </article>
    <button class="add-storage-tile link-add" @click="open()"><Icon name="Plus" :size="28" /><strong>添加以太链接</strong></button>
  </div>
  <section v-else class="link-playback">
    <div ref="playbackScroller" class="table-wrap playback-scroller" @scroll="scrollTop = $event.target.scrollTop"><table><colgroup><col style="width:132px" /><col style="width:112px" /><col style="width:10%" /><col style="width:62px" /><col /><col style="width:12%" /><col style="width:96px" /><col style="width:96px" /><col style="width:72px" /></colgroup><thead><tr><th>时间</th><th>上游</th><th>UA</th><th>模式</th><th>链接</th><th>请求 IP</th><th>缓存状态</th><th>缓存期</th><th>耗时</th></tr></thead><tbody>
      <tr v-if="start" class="playback-spacer" :style="{ height: `${start * 52}px` }" aria-hidden="true"><td colspan="9" /></tr>
      <tr v-for="(event, i) in shown" :key="start + i" class="playback-event">
        <td :data-tooltip="new Date(event.time).toLocaleString('zh-CN', { hour12: false })" data-tooltip-always tabindex="0">{{ clock(event.time) }}</td><td><span class="playback-pill playback-upstream" :data-tooltip="linkName(event.upstream)">{{ linkName(event.upstream) }}</span></td>
        <td><button class="playback-copy playback-pill" :disabled="!event.userAgent" :data-tooltip="uaText(event)" @click="copyValue(event.userAgent)">{{ uaText(event) }}</button></td>
        <td><span class="playback-result" :class="event.outcome">{{ outcomeLabel(event) }}</span></td>
        <td><button class="playback-copy playback-pill" :disabled="!event.target" :data-tooltip="targetText(event)" @click="copyValue(event.target)">{{ targetText(event) }}</button></td>
        <td><button class="playback-copy playback-pill" :disabled="!event.client" :data-tooltip="event.client" @click="copyValue(event.client)">{{ event.client || '未知' }}</button></td>
        <td><span class="playback-pill cache-state" :class="cacheClass(event)" :data-tooltip="cacheText(event)">{{ cacheText(event) }}</span></td>
        <td>{{ ttl(event) }}</td><td>{{ Math.round((event.durationMs || 0) / 1e6) }} ms</td>
      </tr>
      <tr v-if="displayed.length > start + shown.length" class="playback-spacer" :style="{ height: `${(displayed.length - start - shown.length) * 52}px` }" aria-hidden="true"><td colspan="9" /></tr>
    </tbody></table><div v-if="!displayed.length" class="small-empty">暂无播放记录</div></div><footer class="playback-count">共 {{ displayed.length }} 条</footer>
  </section>
  <Teleport to="body"><div v-if="menu" class="context-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop><button @click="open(menu.link)"><Icon name="Pencil" />编辑以链</button><button @click="testLink(menu.link)"><Icon name="Activity" />测试连接</button><button @click="update(menu.link, { enabled: !menu.link.enabled }); closeMenu()"><Icon name="Power" />{{ menu.link.enabled ? '停用链接' : '启用链接' }}</button><button class="danger-text" @click="deleting = menu.link; closeMenu()"><Icon name="Trash2" />删除链接</button></div></Teleport>
  <Modal v-if="modal" compact wide :title="form.id ? '编辑以太链接' : '添加以太链接'" @close="!busy && (modal = false)">
    <div v-if="step === 1" class="modal-body link-type-picker"><button v-for="type in types" :key="type.id" @click="form.type = type.id; step = 2"><img :src="`/media/${type.icon}.png`" alt="" /><strong>{{ type.name }}</strong></button></div>
    <form v-else @submit.prevent="save"><div class="modal-body"><div class="form-grid">
      <label>以链名称<input v-model="form.name" required maxlength="60" /></label>
      <div class="field"><label>跳转模式</label><RoundedSelect v-model="form.mode" label="跳转模式" :options="modes" /></div>
      <label>服务地址<input v-model="form.address" type="url" required placeholder="http://192.168.1.10:8096" /></label>
      <label>反代端口<input v-model.number="form.port" type="number" min="1024" max="65535" required /></label>
      <template v-if="form.type === 'fnos'"><label>账号<input v-model="form.username" required autocomplete="off" /></label><label>密码<SecretInput v-model="form.password" :secret-path="form.id ? `/links/${form.id}/secret` : ''" secret-field="password" required autocomplete="new-password" /></label></template>
      <label v-else class="full">API Key<SecretInput v-model="form.apiKey" :secret-path="form.id ? `/links/${form.id}/secret` : ''" secret-field="apiKey" required autocomplete="off" /></label>
    </div><details class="link-more"><summary>更多选项</summary><label>屏蔽 UA<textarea v-model="form.blockedUA" rows="5" placeholder="一行一个关键词" /></label></details><p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button v-if="!form.id" type="button" class="btn" @click="step = 1">上一步</button><button class="btn primary" :disabled="busy">保存以链</button></footer></form>
  </Modal>
  <Modal v-if="deleting" title="删除以链" @close="deleting = null"><div class="modal-body">确认删除「{{ deleting.name }}」？</div><footer class="modal-footer"><button class="btn" @click="deleting = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">确认删除</button></footer></Modal>
</template>
