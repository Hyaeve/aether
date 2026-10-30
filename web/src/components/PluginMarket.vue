<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { api, notify } from '../lib'
import Icon from './Icon.vue'
import Modal from './Modal.vue'
import PluginSearch from './PluginSearch.vue'
import '../plugin-cards.css'

const plugins = ref([]), total = ref(0), page = ref(1), loaded = ref(false), loading = ref(false)
const repositories = ref([]), revision = ref(''), repositoryOpen = ref(false), editing = ref(null), deleting = ref(null), saving = ref(false), selected = ref(null)
const searchItems = ref([]), searching = ref(false)
let alive = true, generation = 0, searchGeneration = 0
onUnmounted(() => { alive = false; generation++; searchGeneration++ })
function reportFailures(value) { for (const failure of value.failures || []) notify(`${failure.name}：${failure.message}`, true) }
async function load(refresh = false, append = false) {
  const run = ++generation
  loading.value = true
  const nextPage = append ? page.value + 1 : 1
  try {
    const value = await api(`/plugin-market?page=${nextPage}${refresh ? '&refresh=1' : ''}`)
    if (!alive || run !== generation) return
    plugins.value = append ? [...plugins.value, ...value.plugins] : value.plugins
    total.value = value.total; page.value = nextPage; loaded.value = true; reportFailures(value)
  } catch (e) { if (alive && run === generation) notify(e.message, true) }
  finally { if (alive && run === generation) loading.value = false }
}
onMounted(() => load())
async function find(query) {
  const run = ++searchGeneration
  searching.value = true
  try {
    const value = await api(`/plugin-market?q=${encodeURIComponent(query)}&count=100`)
    if (alive && run === searchGeneration) { searchItems.value = value.plugins; reportFailures(value) }
  } catch (e) { if (alive && run === searchGeneration) notify(e.message, true) }
  finally { if (alive && run === searchGeneration) searching.value = false }
}
async function openRepositories() {
  try {
    const value = await api('/plugin-market/repositories')
    if (alive) { repositories.value = value.repositories; revision.value = value.revision; repositoryOpen.value = true }
  } catch (e) { if (alive) notify(e.message, true) }
}
async function persist(next) {
  if (saving.value) return
  saving.value = true
  try {
    const value = await api('/plugin-market/repositories', 'PUT', { repositories: next, revision: revision.value })
    if (!alive) return
    repositories.value = value.repositories; revision.value = value.revision
    editing.value = null; deleting.value = null; notify('插件仓库已保存'); await load()
  } catch (e) { if (alive) notify(e.message, true) }
  finally { if (alive) saving.value = false }
}
function saveRepository() {
  const value = { ...editing.value }
  persist(value.id ? repositories.value.map(repo => repo.id === value.id ? value : repo) : [...repositories.value, value])
}
function imageFailed(event) { event.target.hidden = true }
</script>
<template>
  <div class="market-toolbar"><span class="muted">{{ loaded ? `${total} 个插件` : '' }}</span><div><button class="btn" @click="openRepositories"><Icon name="Warehouse" />插件仓库</button><button class="icon-btn" aria-label="刷新插件市场" :disabled="loading" @click="load(true)"><Icon name="RefreshCw" :class="{ spin: loading }" /></button></div></div>
  <div class="plugin-grid market-grid" :aria-busy="loading">
    <article v-for="plugin in plugins" :key="plugin.repository + ':' + plugin.id" class="plugin-card" role="button" tabindex="0" :aria-label="plugin.name" @click="selected = plugin" @keydown.enter.self="selected = plugin" @keydown.space.prevent.self="selected = plugin">
      <span class="plugin-symbol market-symbol"><Icon name="Puzzle" :size="26" /><img v-if="plugin.icon" :src="plugin.icon" alt="" loading="lazy" referrerpolicy="no-referrer" @error="imageFailed" /></span>
      <div class="plugin-heading"><strong>{{ plugin.name }}</strong><small class="plugin-subtitle">{{ plugin.subtitle || plugin.source }} · v{{ plugin.version }}</small></div>
      <small class="plugin-description" :data-tooltip="plugin.description" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300">{{ plugin.description }}</small>
    </article>
  </div>
  <p v-if="loaded && !plugins.length" class="small-empty">暂无插件</p>
  <div v-if="plugins.length < total" class="market-more"><button class="btn" :disabled="loading" @click="load(false, true)"><Icon name="ChevronDown" />加载更多</button></div>
  <PluginSearch market :items="searchItems" :busy="searching" @search="find" @select="selected = $event" @repositories="openRepositories" />
  <Modal v-if="selected" standard :title="selected.name" @close="selected = null"><div class="modal-body market-detail"><dl><dt>仓库</dt><dd>{{ selected.source }}</dd><dt>插件 ID</dt><dd>{{ selected.id }}</dd><dt>版本</dt><dd>{{ selected.version }}</dd><dt>作者</dt><dd>{{ selected.author || '-' }}</dd><dt>状态</dt><dd>未安装</dd></dl><p>{{ selected.description }}</p><p class="muted">{{ selected.aether ? '已声明 Aether 协议，外部插件运行时尚未开放。' : '仅展示仓库目录，不支持直接运行 MoviePilot 插件。' }}</p></div></Modal>
  <Modal v-if="repositoryOpen" standard title="插件仓库" @close="!saving && (repositoryOpen = false)">
    <div class="modal-body market-repositories"><p v-if="!repositories.length" class="small-empty">暂无仓库</p><div v-for="repo in repositories" :key="repo.id" class="market-repository"><div><strong>{{ repo.name }}</strong><small :data-tooltip="repo.url + ' · ' + repo.ref">{{ repo.url }} · {{ repo.ref }}</small></div><button class="switch" role="switch" :aria-label="'启用仓库 ' + repo.name" :aria-checked="repo.enabled" :class="{ on: repo.enabled }" :disabled="saving" @click="persist(repositories.map(item => item.id === repo.id ? { ...item, enabled: !item.enabled } : item))"></button><button class="icon-btn" :aria-label="'编辑仓库 ' + repo.name" :disabled="saving" @click="editing = { ...repo }"><Icon name="Pencil" :size="17" /></button><button class="icon-btn danger-text" :aria-label="'删除仓库 ' + repo.name" :disabled="saving" @click="deleting = repo"><Icon name="Trash2" :size="17" /></button></div></div>
    <footer class="modal-footer"><button class="btn primary" :disabled="saving || repositories.length >= 20" @click="editing = { id: '', name: '', url: '', ref: 'main', enabled: true }"><Icon name="Plus" />添加仓库</button></footer>
  </Modal>
  <Modal v-if="editing" standard :title="editing.id ? '编辑插件仓库' : '添加插件仓库'" @close="!saving && (editing = null)"><form @submit.prevent="saveRepository"><div class="modal-body repository-form"><label>名称<input v-model="editing.name" aria-label="仓库名称" maxlength="80" :disabled="saving" /></label><label>仓库地址<input v-model="editing.url" type="url" aria-label="仓库地址" required :disabled="saving" /></label><label>分支 / 提交<input v-model="editing.ref" aria-label="分支 / 提交" maxlength="128" :disabled="saving" /></label></div><footer class="modal-footer"><button type="button" class="btn" :disabled="saving" @click="editing = null">取消</button><button class="btn primary" :disabled="saving"><Icon :name="saving ? 'LoaderCircle' : 'Check'" :class="{ spin: saving }" />{{ saving ? '正在校验' : '保存' }}</button></footer></form></Modal>
  <Modal v-if="deleting" confirmation title="删除插件仓库" @close="!saving && (deleting = null)"><div class="modal-body">确认删除仓库 {{ deleting.name }}？</div><footer class="modal-footer"><button class="btn" :disabled="saving" @click="deleting = null">取消</button><button class="btn danger" :disabled="saving" @click="persist(repositories.filter(repo => repo.id !== deleting.id))">确认删除</button></footer></Modal>
</template>
<style scoped>
.market-toolbar,.market-toolbar>div{display:flex;align-items:center;gap:12px}.market-toolbar{justify-content:space-between;margin-bottom:16px;font-size:13px}.market-toolbar .btn{height:36px;min-height:36px;padding-block:0}.market-symbol{position:relative}.market-symbol img{position:absolute;inset:0;width:44px;height:44px;object-fit:contain;background:var(--surface)}.market-symbol img[hidden]{display:none}.market-more{display:flex;justify-content:center;margin-top:18px}.market-repositories{max-height:55vh;overflow:auto}.market-repository{display:flex;align-items:center;gap:8px;padding:12px 0;border-bottom:1px solid var(--border)}.market-repository>div{min-width:0;flex:1;display:grid;gap:6px}.market-repository strong,.market-repository small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.market-repository small{font-size:12px;color:var(--muted)}.market-repository .switch{flex:none}.market-repository .icon-btn{width:30px;flex:none}.repository-form{display:grid;gap:16px}.repository-form input{height:40px}.market-detail dl{display:grid;grid-template-columns:80px minmax(0,1fr);gap:12px;margin:0}.market-detail dd{margin:0;overflow-wrap:anywhere}.market-detail p{white-space:pre-wrap;overflow-wrap:anywhere}
</style>
