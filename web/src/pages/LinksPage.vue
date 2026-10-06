<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, notify, date } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import SecretInput from '../components/SecretInput.vue'
const types = [{ id: 'audiobookshelf', name: 'Audiobookshelf', icon: 'abs' }, { id: 'emby', name: 'Emby', icon: 'emby' }, { id: 'fnos', name: '飞牛影视', icon: 'fnmovie' }]
const modes = [{ value: 'always', label: '始终跳转' }, { value: 'public', label: '公网跳转' }, { value: 'private', label: '内网跳转' }, { value: 'never', label: '始终中继' }]
const links = ref([]), tab = ref('manage'), modal = ref(false), step = ref(1), busy = ref(false), error = ref(''), events = ref([]), query = ref(''), deleting = ref(null)
const form = reactive({})
const menu = ref(null)
const outcome = ref('all')
const outcomes = [{ value: 'all', label: '全部类型' }, { value: 'redirect', label: '302 跳转' }, { value: 'proxy', label: '中继' }, { value: 'transcode', label: '音频适配' }, { value: 'passthrough', label: '透传' }, { value: 'local', label: '本地' }, { value: 'error', label: '失败' }, { value: 'unauthorized', label: '未授权' }]
const armed = ref(''), dragging = ref(''), dropTarget = ref(''), sorting = ref(false)
let holdTimer, releaseTimer, suppressClick = false
const interactive = event => event.target.closest('button, a, input, .rounded-select')
function hold(event, link) {
  if (event.button !== 0 || interactive(event)) return
  clearTimeout(holdTimer); clearTimeout(releaseTimer)
  holdTimer = setTimeout(() => { armed.value = link.id; suppressClick = true }, 450)
}
function release() {
  clearTimeout(holdTimer)
  releaseTimer = setTimeout(() => { armed.value = ''; suppressClick = false }, 100)
}
function cardClick(event, link) { if (!suppressClick && !interactive(event)) open(link) }
function dragStart(event, link) {
  if (sorting.value || armed.value !== link.id || interactive(event)) { event.preventDefault(); return }
  closeMenu(); dragging.value = link.id
  event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', link.id)
}
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
const displayed = computed(() => events.value.filter(e => (outcome.value === 'all' || e.outcome === outcome.value) && `${linkName(e.upstream)} ${e.mediaPath} ${e.path} ${e.client}`.toLowerCase().includes(query.value.toLowerCase())))
const typeOf = type => types.find(t => t.id === type) || types[0]
const linkName = id => links.value.find(l => l.id === id)?.name || id
const endpoint = link => { const u = new URL(location.href); u.port = String(link.port); u.pathname = '/'; u.search = ''; u.hash = ''; u.protocol = 'http:'; return u.href }
async function load() {
  try { links.value = await api('/links'); events.value = (await api('/link-playback')).recentEvents || [] }
  catch (e) { notify(e.message, true) }
}
function open(link) { closeMenu(); Object.assign(form, { id: '', name: '', type: '', address: '', port: '', apiKey: '', username: '', password: '', mode: 'always', enabled: true, blockedUA: '' }, link || {}); error.value = ''; step.value = link ? 2 : 1; modal.value = true }
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
onMounted(() => { document.addEventListener('click', closeMenu); document.addEventListener('keydown', escapeMenu); load(); timer = setInterval(() => { if (tab.value === 'cache') load() }, 5000) })
onUnmounted(() => { clearInterval(timer); clearTimeout(holdTimer); clearTimeout(releaseTimer); document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', escapeMenu) })
</script>
<template>
  <nav class="content-tabs" aria-label="以太链接栏目"><a href="#" :class="{ active: tab === 'manage' }" @click.prevent="tab = 'manage'"><Icon name="Waypoints" :size="17" />以链管理</a><a href="#" :class="{ active: tab === 'cache' }" @click.prevent="tab = 'cache'; load()"><Icon name="ListVideo" :size="17" />直链缓存</a></nav>
  <div v-if="tab === 'manage'" class="link-grid">
    <article v-for="link in links" :key="link.id" class="link-card" tabindex="0" :aria-label="link.name" :class="{ 'drag-armed': armed === link.id, dragging: dragging === link.id, 'drop-target': dropTarget === link.id }" :draggable="armed === link.id && !sorting" @pointerdown="hold($event, link)" @pointerup="release" @pointerleave="!dragging && release()" @click="cardClick($event, link)" @keydown="cardKey($event, link)" @dragstart="dragStart($event, link)" @dragend="dragEnd" @dragover.prevent="dragging && (dropTarget = link.id)" @drop.prevent="moveLink(dragging, link.id)" @contextmenu.prevent.stop="context($event, link)">
      <div class="link-identity"><button class="link-toggle" :aria-label="`${link.enabled ? '停用' : '启用'}以链 ${link.name}`" :aria-pressed="link.enabled" :disabled="busy" @click.stop="update(link, { enabled: !link.enabled })"><img :src="`/media/${typeOf(link.type).icon}.png`" :alt="typeOf(link.type).name" /></button><strong>{{ link.name }}</strong></div>
      <div class="link-card-controls"><RoundedSelect :model-value="link.mode" :label="`${link.name}跳转模式`" :options="modes" :disabled="busy" @update:model-value="update(link, { mode: $event })" /><div class="link-port-actions"><a :href="endpoint(link)" target="_blank" rel="noopener noreferrer"><Icon name="ArrowUpRight" :size="15" />{{ link.port }}</a><button class="icon-btn" :aria-label="`以链操作 ${link.name}`" :aria-expanded="menu?.link.id === link.id" @click.stop="context($event, link)"><Icon name="Ellipsis" /></button></div></div>
    </article>
    <button class="add-storage-tile link-add" @click="open()"><Icon name="Plus" :size="28" /><strong>添加以太链接</strong></button>
  </div>
  <section v-else class="link-playback">
    <div class="playback-toolbar"><RoundedSelect v-model="outcome" label="筛选播放类型" :options="outcomes" /><div class="toolbar-right"><button class="icon-btn" aria-label="刷新播放流水" @click="load"><Icon name="RefreshCw" /></button><div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索播放流水" placeholder="搜索播放流水…" /></div></div></div>
    <div class="table-wrap"><table><thead><tr><th>时间</th><th>以链</th><th>媒体</th><th>处理方式</th><th>缓存</th><th>有效期</th><th>客户端</th></tr></thead><tbody><tr v-for="(event, i) in displayed" :key="i"><td>{{ date(event.time) }}</td><td>{{ linkName(event.upstream) }}</td><td class="playback-path" :title="event.mediaPath || event.path">{{ event.mediaPath || event.path }}</td><td>{{ ({ redirect: '302 跳转', proxy: '中继', transcode: '音频适配', passthrough: '透传', local: '本地', error: '失败', unauthorized: '未授权' })[event.outcome] || event.outcome }}</td><td>{{ event.cacheHit ? '命中' : '首次解析' }}</td><td>{{ event.cacheTtlSeconds || 0 }} s</td><td>{{ event.client }}</td></tr></tbody></table><div v-if="!displayed.length" class="small-empty">暂无播放记录</div></div>
  </section>
  <Teleport to="body"><div v-if="menu" class="context-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop><button @click="open(menu.link)"><Icon name="Pencil" />编辑以链</button><button @click="testLink(menu.link)"><Icon name="Activity" />测试连接</button><button @click="update(menu.link, { enabled: !menu.link.enabled }); closeMenu()"><Icon name="Power" />{{ menu.link.enabled ? '停用链接' : '启用链接' }}</button><button class="danger-text" @click="deleting = menu.link; closeMenu()"><Icon name="Trash2" />删除链接</button></div></Teleport>
  <Modal v-if="modal" compact wide :title="form.id ? '编辑以太链接' : '添加以太链接'" @close="!busy && (modal = false)">
    <div v-if="step === 1" class="modal-body link-type-picker"><button v-for="type in types" :key="type.id" @click="form.type = type.id; step = 2"><img :src="`/media/${type.icon}.png`" alt="" /><strong>{{ type.name }}</strong></button></div>
    <form v-else @submit.prevent="save"><div class="modal-body"><div class="form-grid">
      <label class="full">以链名称<input v-model="form.name" required maxlength="60" /></label>
      <label class="full">服务地址<input v-model="form.address" type="url" required placeholder="http://192.168.1.10:8096" /></label>
      <label>反代端口<input v-model.number="form.port" type="number" min="1024" max="65535" required /></label>
      <div class="field"><label>跳转模式</label><RoundedSelect v-model="form.mode" label="跳转模式" :options="modes" /></div>
      <template v-if="form.type === 'fnos'"><label>账号<input v-model="form.username" required autocomplete="off" /></label><label>密码<SecretInput v-model="form.password" required autocomplete="new-password" /></label></template>
      <label v-else class="full">API Key<SecretInput v-model="form.apiKey" required autocomplete="off" /></label>
    </div><details class="link-more"><summary>更多选项</summary><label>屏蔽 UA<textarea v-model="form.blockedUA" rows="5" placeholder="一行一个关键词" /></label></details><p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button v-if="!form.id" type="button" class="btn" @click="step = 1">上一步</button><button class="btn primary" :disabled="busy">保存以链</button></footer></form>
  </Modal>
  <Modal v-if="deleting" title="删除以链" @close="deleting = null"><div class="modal-body">确认删除「{{ deleting.name }}」？</div><footer class="modal-footer"><button class="btn" @click="deleting = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">确认删除</button></footer></Modal>
</template>
