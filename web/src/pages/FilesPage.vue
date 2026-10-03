<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, state, bytes, notify } from '../lib'
import Icon from '../components/Icon.vue'
const route = useRoute()
const selected = ref(route.query.storage || state.storages.find(s => s.enabled)?.id || '')
const current = ref('/'), history = ref([]), files = ref([]), busy = ref(false), error = ref(''), query = ref('')
const visible = computed(() => files.value.filter(f => f.name.toLowerCase().includes(query.value.toLowerCase())).sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name)))
let requestId = 0
async function load(refresh = false) {
  const id = ++requestId
  if (!selected.value) { files.value = []; return }
  busy.value = true; error.value = ''
  try {
    const result = await api(`/files?storage=${encodeURIComponent(selected.value)}&path=${encodeURIComponent(current.value)}&refresh=${refresh}`)
    if (id === requestId) files.value = result
  } catch (e) { if (id === requestId) { error.value = e.message; files.value = [] } }
  finally { if (id === requestId) busy.value = false }
}
function enter(f) { if (f.isDir) { history.value.push({ id: current.value, name: f.name }); current.value = f.id; load() } }
function back() { current.value = history.value.pop()?.id || '/'; load() }
watch(selected, () => { current.value = '/'; history.value = []; load() }, { immediate: true })
async function copy(f) { try { await navigator.clipboard.writeText(f.url); notify('播放链接已复制') } catch { notify('当前浏览器不允许访问剪贴板', true) } }
</script>
<template>
  <section class="page-head"><div><div class="eyebrow">FILE EXPLORER</div><h1>文件管理<span class="title-dot">.</span></h1><p>所有存储，在一个空间里浏览。</p></div><button class="btn" :disabled="busy || !selected" @click="load(true)"><Icon name="RefreshCw" />刷新目录</button></section>
  <div class="file-toolbar"><select v-model="selected" aria-label="选择存储池"><option value="" disabled>选择存储池</option><option v-for="s in state.storages.filter(s => s.enabled)" :key="s.id" :value="s.id">{{ s.name }}</option></select><div class="path-bar"><button class="icon-btn" :disabled="!history.length || busy" aria-label="上级目录" @click="back"><Icon name="ArrowUp" /></button><Icon name="Folder" :size="16" /><span>{{ history.length ? history.map(h => h.name).join(' / ') : '根目录' }}</span></div><div class="search-field"><Icon name="Search" :size="16" /><input v-model="query" aria-label="搜索当前目录" placeholder="搜索当前目录…" /></div></div>
  <div v-if="error" class="error-message" role="alert">{{ error }}</div>
  <div v-if="busy" class="empty-state"><Icon name="LoaderCircle" class="spin" :size="30" /><p>正在读取目录…</p></div>
  <div v-else-if="!selected || !visible.length" class="empty-state"><span class="empty-icon"><Icon name="FolderOpen" :size="36" /></span><h3>{{ !selected ? '尚未连接存储' : '目录为空' }}</h3><button v-if="!selected" class="btn" @click="$router.push('/storage')">前往存储管理</button></div>
  <div v-else class="table-wrap"><table><thead><tr><th>名称</th><th>大小</th><th>类型</th><th>修改时间</th><th class="right">操作</th></tr></thead><tbody><tr v-for="f in visible" :key="f.id"><td><button class="file-name" :disabled="!f.isDir" @click="enter(f)"><Icon :name="f.isDir ? 'Folder' : 'FileVideo'" :class="{ 'folder-color': f.isDir }" :size="21" /><strong>{{ f.name }}</strong></button></td><td>{{ f.isDir ? '—' : bytes(f.size) }}</td><td>{{ f.isDir ? '文件夹' : f.name.split('.').at(-1).toUpperCase() }}</td><td>{{ f.modified?.startsWith('0001') ? '—' : new Date(f.modified).toLocaleString('zh-CN') }}</td><td><div v-if="!f.isDir" class="row-actions"><button class="icon-btn" title="复制播放链接" aria-label="复制播放链接" @click="copy(f)"><Icon name="Link" :size="16" /></button><a class="icon-btn" :href="f.url" target="_blank" rel="noreferrer" title="下载文件" aria-label="下载文件"><Icon name="Download" :size="16" /></a></div></td></tr></tbody></table></div>
  <div class="page-foot"><span>{{ visible.length }} 个项目</span><span>目录缓存 {{ state.settings.cacheEnabled ? '已开启' : '已关闭' }}</span></div>
</template>
