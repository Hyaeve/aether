<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, notify, state } from '../lib'
import { transferShares } from '../share-jobs'
import { loadSharePreferences, saveSharePreferences } from '../share-preferences'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
import RoundedSelect from './RoundedSelect.vue'
import { shareEntries, shareHighlight, shareInput } from '../share-links.js'
const props = defineProps({ storage: Object, parent: String, trail: Array, initialLinks: String, provider: String })
const emit = defineEmits(['close'])
const links = ref(props.initialLinks || ''), picker = ref(false)
const provider = computed(() => props.provider || shareEntries(links.value).find(entry => entry.provider)?.provider || props.storage?.type)
const pools = computed(() => state.storages.filter(s => s.enabled && (!provider.value || s.type === provider.value) && (['115', 'quark'].includes(s.type) || s.type === 'mobile' && s.config?.mode === 'native')))
const selected = ref(props.storage?.id || pools.value[0]?.id || '')
const storage = computed(() => pools.value.find(s => s.id === selected.value))
const supported = computed(() => storage.value?.enabled && (['115', 'quark'].includes(storage.value.type) || storage.value.type === 'mobile' && storage.value.config?.mode === 'native'))
const key = computed(() => `aether-share-directory:${state.username}:${storage.value?.id}`)
const target = ref('/'), targetLabel = ref('根目录'), targetHistory = ref([])
const preferences = ref({ last: {}, directories: {} }), loadingPreferences = ref(true)
watch(pools, () => {
  if (!pools.value.some(s => s.id === selected.value)) {
    const remembered = preferences.value.last[provider.value]
    selected.value = pools.value.find(s => s.id === remembered)?.id || pools.value[0]?.id || ''
  }
}, { flush: 'sync' })
function restoreTarget() {
  let saved = {}
  try { saved = JSON.parse(localStorage.getItem(key.value) || '{}') || {} } catch {}
  const shared = preferences.value.directories[storage.value?.id]
  if (shared) saved = { ...shared, path: shared.parent }
  const current = storage.value?.id === props.storage?.id
  target.value = typeof saved.path === 'string' ? saved.path : current && props.parent || '/'
  targetLabel.value = typeof saved.label === 'string' ? saved.label : current && props.trail?.at(-1)?.name || '根目录'
  targetHistory.value = (Array.isArray(saved.history) ? saved.history : typeof saved.path === 'string' ? [] : current && props.trail || []).map(c => ({ id: c.id, name: c.name }))
}
watch(key, restoreTarget, { immediate: true, flush: 'sync' })
onMounted(async () => {
  const owner = state.username
  try {
    const saved = await loadSharePreferences()
    if (!alive || !state.authenticated || state.username !== owner) return
    preferences.value = { last: saved.last || {}, directories: saved.directories || {} }
    const remembered = preferences.value.last[provider.value]
    if ((!props.storage || props.storage.type !== provider.value) && pools.value.some(s => s.id === remembered)) selected.value = remembered
    restoreTarget()
  } catch (e) { if (alive && state.authenticated && state.username === owner) notify(`读取转存目标记忆失败：${e.message}`, true) }
  finally { if (alive) loadingPreferences.value = false }
})
function rememberTarget() {
  if (!storage.value) return
  const owner = state.username
  const directory = { parent: target.value, label: targetLabel.value, history: targetHistory.value.map(c => ({ ...c })) }
  const payload = { storageId: storage.value.id, ...directory }
  preferences.value.last[storage.value.type] = storage.value.id
  preferences.value.directories[storage.value.id] = directory
  try { localStorage.setItem(key.value, JSON.stringify({ ...directory, path: directory.parent })) } catch {}
  saveSharePreferences(payload).catch(e => { if (state.authenticated && state.username === owner) notify(`保存转存目标记忆失败：${e.message}`, true) })
}
const pickerTrail = computed(() => targetHistory.value.map((c, i) => ({ id: c.id, name: i ? targetHistory.value[i - 1].name : '根目录' })))
const parsedLinks = computed(() => shareInput(links.value))
const lines = computed(() => parsedLinks.value.lines)
const highlightedLinks = computed(() => shareHighlight(links.value))
const parsing = ref(false), parsed = ref(null), parseError = ref(''), savedSelections = ref([])
let alive = true
onUnmounted(()=>{alive=false})
watch([links,selected],()=>{savedSelections.value=[]})
async function parse() {
  if(parsing.value || loadingPreferences.value || !supported.value)return
  parsing.value=true;parseError.value=''
  const input=lines.value.slice(), sid=storage.value.id, owner=state.username
  try {
    const roots=[]
    for(let i=0;i<input.length;i++) {
      if(!alive || !state.authenticated || state.username!==owner)return
      const result=await api('/files/share/tree','POST',{storageId:sid,url:input[i]})
      const single=result.items.length===1 && result.items[0].isDir ? result.items[0].name : ''
      roots.push({url:input[i],name:result.items.map(item=>item.name).slice(0,2).join('、') || `分享 ${i+1}`,single,tree:result.tree || result.items.map(item=>({...item,path:item.name})),excluded:[...(savedSelections.value[i]||[])],opened:new Set(['',...(single?[single]:[])])})
    }
    if(alive && state.authenticated && state.username===owner && storage.value?.id===sid && JSON.stringify(lines.value)===JSON.stringify(input))parsed.value=roots
  } catch(e){if(alive)parseError.value=e.message}
  finally {if(alive)parsing.value=false}
}
const selectedPath=(root,key)=>!root.excluded.some(prefix=>key===prefix||key.startsWith(prefix+'/'))
function toggleRoot(root, checked) { root.excluded=checked?[]:root.tree.filter(item=>!item.path.includes('/')).map(item=>item.path) }
function togglePath(root,key) {
  if(selectedPath(root,key))root.excluded=[...root.excluded,key]
  else {
    const ancestor=root.excluded.find(prefix=>key.startsWith(prefix+'/'))
    root.excluded=root.excluded.filter(prefix=>prefix!==key && !key.startsWith(prefix+'/'))
    if(ancestor) root.excluded.push(...root.tree.filter(item=>item.path.startsWith(ancestor+'/') && item.path!==key && !key.startsWith(item.path+'/') && !item.path.startsWith(key+'/')).map(item=>item.path))
  }
}
function shownTree(root) {return root.tree.filter(item=>{if(item.path===root.single)return false;const parts=item.path.split('/');parts.pop();return parts.every((_,i)=>root.opened.has(parts.slice(0,i+1).join('/')))})}
function applySelection(){savedSelections.value=parsed.value.map(root=>[...root.excluded]);parsed.value=null}
const placeholder = computed(() => `${{ '115': '115 网盘', quark: '夸克网盘', mobile: '移动云盘' }[storage.value?.type] || storage.value?.name || ''}分享链接，一行一条`)
function chooseTarget(value) {
  if (!pools.value.some(s => s.id === value.storageId)) return
  selected.value = value.storageId
  target.value = value.source; targetLabel.value = value.sourceLabel
  targetHistory.value = (value.sourceTrail || []).map((c, i, trail) => ({ id: c.id, name: trail[i + 1]?.name || value.sourceLabel }))
  rememberTarget()
  picker.value = false
}
function submit() {
  if (loadingPreferences.value || !supported.value || parsedLinks.value.conflict || !lines.value.length || lines.value.length > 50) return
  const history = targetHistory.value.map(c => ({ ...c }))
  const root = storage.value.config?.root || (['115', 'quark'].includes(storage.value.type) ? '0' : '/')
  if (!['/', root].includes(target.value) && !history.length) history.push({ id: '/', name: targetLabel.value })
  transferShares(storage.value, target.value, lines.value, history, savedSelections.value)
  emit('close')
}
</script>
<template>
  <Modal v-if="!parsed" title="分享转存" compact @close="emit('close')">
    <form @submit.prevent="submit">
      <div class="modal-body share-transfer">
        <div v-if="!props.storage" class="field"><label>存储池</label><RoundedSelect v-model="selected" label="转存存储池" :disabled="loadingPreferences" :options="pools.map(s => ({value:s.id,label:s.name}))" @update:model-value="rememberTarget" /></div>
        <label>分享链接<div class="share-link-editor"><pre aria-hidden="true"><template v-for="(part, index) in highlightedLinks" :key="index"><mark v-if="part.password">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></pre><textarea v-model="links" rows="5" required :placeholder="placeholder" maxlength="200000" @scroll="$event.target.previousElementSibling.scrollTop = $event.target.scrollTop" /></div></label>
        <p v-if="parsedLinks.conflict" class="error-message" role="alert">同一分享链接检测到不同提取码，请确认输入后再转存。</p>
        <div class="field"><label>转存目录</label><button type="button" class="source-trigger" :disabled="!supported || loadingPreferences" aria-label="选择转存目录" @click="picker = true"><span><template v-if="props.storage && storage?.id !== props.storage.id">{{ storage?.name }} / </template>{{ targetLabel }}</span><Icon name="FolderOpen" /></button></div>
        <p v-if="!supported" class="error-message">当前存储不支持分享转存。</p>
        <p v-if="parseError" class="error-message" role="alert">{{ parseError }}</p>
        <p v-if="lines.length > 50" class="error-message">一次最多提交 50 条分享链接。</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn cancel" @click="emit('close')">取消</button><button type="button" class="btn" :disabled="loadingPreferences || parsing || !supported || parsedLinks.conflict || !lines.length || lines.length > 50" @click="parse"><Icon name="ScanSearch" />{{ parsing?'解析中…':'解析' }}</button><button class="btn primary" :disabled="loadingPreferences || parsing || !supported || parsedLinks.conflict || !lines.length || lines.length > 50"><Icon name="FolderInput" />转存</button></footer>
    </form>
  </Modal>
  <Modal v-if="parsed" title="分享内容" standard @close="parsed=null">
    <div class="modal-body share-tree"><section v-for="root in parsed" :key="root.url"><div class="share-tree-row"><button class="icon-btn" :aria-label="`展开分享 ${root.name}`" @click="root.opened.has('')?root.opened.delete(''):root.opened.add('')"><Icon :name="root.opened.has('')?'ChevronDown':'ChevronRight'" /></button><input type="checkbox" :aria-label="`选择分享 ${root.name}`" :checked="!root.excluded.length" @change="toggleRoot(root,$event.target.checked)" /><Icon name="Folder" /><strong>{{ root.name }}</strong></div><template v-if="root.opened.has('')"><div v-for="item in shownTree(root)" :key="item.path" class="share-tree-row" :style="{paddingLeft:`${20+item.path.split('/').length*16}px`}"><button v-if="item.isDir" class="icon-btn" :aria-label="`展开 ${item.name}`" @click="root.opened.has(item.path)?root.opened.delete(item.path):root.opened.add(item.path)"><Icon :name="root.opened.has(item.path)?'ChevronDown':'ChevronRight'" /></button><span v-else class="tree-spacer" /><input type="checkbox" :aria-label="`选择 ${item.path}`" :checked="selectedPath(root,item.path)" @change="togglePath(root,item.path)" /><Icon :name="item.isDir?'Folder':'File'" /><span>{{ item.name }}</span></div></template></section></div>
<footer class="modal-footer"><button class="btn" @click="parsed=null"><Icon name="ArrowLeft" />返回</button><button class="btn primary" @click="applySelection"><Icon name="Check" />应用</button></footer>
  </Modal>
  <TaskSourcePicker v-if="picker && storage" :storages="pools" :storage="storage.id" :initial="target" :initial-label="targetLabel" :initial-trail="pickerTrail" @close="picker = false" @select="chooseTarget" />
</template>
<style scoped>
.share-transfer { display: grid; gap: 16px; }
.share-tree { height:min(440px,55dvh);overflow:auto;scrollbar-width:none; }.share-tree::-webkit-scrollbar { display:none; }
:global(.modal:has(.share-tree)) { scrollbar-width:none; }:global(.modal:has(.share-tree)::-webkit-scrollbar) { display:none; }
.share-tree-row { display:flex;align-items:center;gap:8px;min-height:38px; }.share-tree-row > span,.share-tree-row > strong { overflow:hidden;text-overflow:ellipsis;white-space:nowrap; }.share-tree-row input { width:16px;height:16px;flex:none; }.share-tree-row svg { flex:none; }.tree-spacer { width:32px;flex:none; }
.share-link-editor { position:relative; min-width:0; }
.share-link-editor pre, .share-link-editor textarea { box-sizing:border-box; width:100%; min-height:130px; margin:0; padding:10px 12px; border:1px solid var(--border); border-radius:8px; font:inherit; line-height:1.5; letter-spacing:inherit; white-space:pre-wrap; overflow-wrap:anywhere; tab-size:2; scrollbar-gutter:stable; }
.share-link-editor pre { position:absolute; inset:0; overflow:auto; pointer-events:none; color:var(--text); background:var(--input); }
.share-link-editor pre::-webkit-scrollbar { visibility:hidden; }
.share-link-editor mark { color:inherit; background:color-mix(in srgb,#7775ee 25%,transparent); box-shadow:inset 0 0 0 1px color-mix(in srgb,#8b82ff 52%,transparent),0 0 9px color-mix(in srgb,#8075ff 24%,transparent); border-radius:4px; }
.share-link-editor textarea { position:relative; display:block; resize:vertical; color:transparent; caret-color:var(--primary); background:transparent; }
.share-link-editor textarea::placeholder { color:var(--muted); }
.share-link-editor textarea::selection { color:transparent; background:color-mix(in srgb,var(--primary) 28%,transparent); }
</style>
