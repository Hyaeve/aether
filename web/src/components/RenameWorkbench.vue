<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, notify, state } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import ThinScroll from './ThinScroll.vue'
import { useVirtualList } from '../virtual-list'
import { changedNameParts } from '../name-diff'
import { entryIcon } from '../file-icon'
import ProviderIcon from './ProviderIcon.vue'
import VisitPath from './VisitPath.vue'
import RenameHistory from './RenameHistory.vue'
import RenameRuleFields from './RenameRuleFields.vue'
import { newRenameRule, renameRuleTypes, serializedRenameRule } from '../rename-rules'
const historyOpen=ref(false)
const props = defineProps({ storage: String, source: String, sourceLabel: String, files: Array })
const storageInfo=computed(()=>state.storages.find(s=>s.id===props.storage))
const pathParts=computed(()=>(props.sourceLabel||props.source||'/').split('/').filter(Boolean))
const emit = defineEmits(['close', 'changed'])
const rules = ref([newRenameRule()])
const expanded = ref(0)
const addOpen = ref(false), addButton = ref(null), choices = ref(null)
const sourceFiles = ref(props.files.map(f => ({ ...f })))
const ignored = ref([]), naming = ref(false), editing = ref(null), editName = ref('')
const originals = () => sourceFiles.value.map(f => ({ id: f.id, name: f.name, newName: f.name, isDir: f.isDir }))
const sets = ref([]), chosen = ref(''), setName = ref(''), items = ref([]), error = ref(''), busy = ref(false), loading = ref(false), saveBusy = ref(false)
const viewport = ref(null)
const { shown, top, bottom } = useVirtualList(items, viewport, { rowHeight: 72 })
let timer, generation = 0, disposed = false
items.value = originals()
const effectiveRules = computed(() => rules.value.map(serializedRenameRule).filter(Boolean))
const ruleSignature = computed(() => JSON.stringify(effectiveRules.value))
const payload = () => ({ storageId: props.storage, source: props.source, sourceLabel:props.sourceLabel, ids: sourceFiles.value.filter(f => !ignored.value.includes(f.id)).map(f => f.id), rules: effectiveRules.value })
const changes = computed(() => items.value.filter(f => !ignored.value.includes(f.id) && f.name !== f.newName && !f.error).length)
const invalid = computed(() => loading.value || !items.value.length || items.value.some(f => !ignored.value.includes(f.id) && f.error) || !!error.value || !changes.value)
function historyChanged(records,directory){
  if(directory?.storageId===props.storage && directory.source===props.source){
    for(const record of records.filter(r=>r.status==='success')){
      const file=sourceFiles.value.find(f=>f.id===record.fileId && f.name===record.newName)
      if(!file)continue
      if(['local','webdav','openlist'].includes(storageInfo.value?.type)){
        const oldID=file.id;file.id=oldID.slice(0,oldID.lastIndexOf('/')+1)+record.name
        ignored.value=ignored.value.map(id=>id===oldID?file.id:id)
      }
      file.name=record.name
    }
    preview()
  }
  emit('changed')
}
const validRules = computed(() => effectiveRules.value.length > 0 && !loading.value && !error.value)
function toggleIgnore(item) {
  ignored.value = ignored.value.includes(item.id) ? ignored.value.filter(id => id !== item.id) : [...ignored.value, item.id]
  clearTimeout(timer); preview()
}
async function renameOne() {
  if (!editing.value || busy.value) return
  busy.value = true
  const item = editing.value
  try {
    const input={storageId:props.storage,source:props.source,sourceLabel:props.sourceLabel,ids:[item.id],rules:[{kind:'replace',findType:'regex',find:'^'+item.name.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'$',replace:editName.value.replace(/\$/g,'$$$$'),caseSensitive:true}]}
    const expected=await api('/files/rename-preview','POST',input)
    const result=await api('/files/rename','POST',{...input,expected})
    if(result.error)throw new Error(result.error)
    const file = sourceFiles.value.find(f => f.id === item.id)
    const type = state.storages.find(s => s.id === props.storage)?.type
    const renamed = result.renamed?.find(f => f.id === item.id)
    if (renamed?.newId || ['local', 'webdav', 'openlist'].includes(type)) {
      const newID = renamed?.newId || item.id.slice(0, item.id.lastIndexOf('/') + 1) + editName.value
      ignored.value = ignored.value.map(id => id === file.id ? newID : id)
      file.id = newID
    }
    if (renamed?.pickCode) file.pickCode = renamed.pickCode
    file.name = editName.value
    editing.value = null; emit('changed'); notify('已重命名'); await preview()
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
async function preview() {
  const id = ++generation
  if (!payload().rules.length || !payload().ids.length) { updatePreview([]); loading.value = false; error.value = ''; return }
  loading.value = true; error.value = ''
  try { const result = await api('/files/rename-preview', 'POST', payload()); if (!disposed && id === generation) updatePreview(result) }
  catch (e) { if (!disposed && id === generation) error.value = e.message }
  finally { if (!disposed && id === generation) loading.value = false }
}
function updatePreview(result) {
  const byID = new Map(result.map(f => [f.id, f]))
  const existing = new Map(items.value.map(f => [f.id, f]))
  items.value = originals().map(f => Object.assign(existing.get(f.id) || f, { ...f, ...byID.get(f.id), error: byID.get(f.id)?.error || '' }))
}
watch(ruleSignature, () => {
  clearTimeout(timer); generation++; loading.value = true
  timer = setTimeout(preview, 250)
})
async function toggleAdd() {
  addOpen.value = !addOpen.value
  if (addOpen.value) {
    await nextTick()
    choices.value?.scrollIntoView({ block: 'nearest' })
  }
}
async function addRule(kind) {
  if (busy.value || rules.value.length >= 30) return
  rules.value.push(newRenameRule(kind)); expanded.value = rules.value.length - 1; addOpen.value = false
  await nextTick()
  document.querySelector('.rename-rule:last-of-type .rounded-select-trigger')?.focus({ preventScroll:true })
}
function changeRule(index, kind) { rules.value[index] = newRenameRule(kind) }
function removeRule(index) { if (rules.value.length === 1) return; rules.value.splice(index, 1); expanded.value = Math.min(index, rules.value.length - 1) }
function closeChoices() { addOpen.value = false; addButton.value?.focus({preventScroll:true}) }
function nameParts(item) {
  return changedNameParts(item.name, item.newName || '')
}
function applySet(value) {
  chosen.value = value
  const set = sets.value.find(s => s.id === value)
  if (set) { rules.value = set.rules.map(r => newRenameRule(r.kind, JSON.parse(JSON.stringify(r)))); expanded.value = 0; addOpen.value = false }
}
async function saveSet() {
  if (!validRules.value || !setName.value.trim()) return
  saveBusy.value = true
  try { sets.value = await api('/files/rename-rules', 'POST', { name: setName.value, rules: payload().rules }); chosen.value = sets.value.at(-1).id; setName.value = ''; naming.value = false; notify('规则集已保存') }
  catch (e) { notify(e.message, true) } finally { saveBusy.value = false }
}
async function deleteSet() {
  saveBusy.value = true
  try { sets.value = await api('/files/rename-rules', 'DELETE', { id: chosen.value }); chosen.value = ''; notify('规则集已删除') }
  catch (e) { notify(e.message, true) } finally { saveBusy.value = false }
}
async function execute() {
  if (invalid.value || busy.value) return
  busy.value = true
  try {
    const result = await api('/files/rename', 'POST', { ...payload(), expected: items.value.filter(f => !ignored.value.includes(f.id)) })
    emit('changed')
    if (result.error) { error.value = result.error; items.value = []; notify(result.error, true) }
    else { notify(`已重命名 ${result.processed} 项`); emit('close') }
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
onMounted(async () => {
  try { sets.value = await api('/files/rename-rules') } catch (e) { notify(e.message, true) }
})
onUnmounted(() => { disposed = true; clearTimeout(timer); generation++ })
</script>
<template>
  <RenameHistory v-if="historyOpen" @close="historyOpen=false" @changed="historyChanged" />
  <section v-else class="rename-workbench">
    <header class="rename-heading task-heading"><button class="icon-btn" aria-label="返回文件管理" :disabled="busy" @click="emit('close')"><Icon name="ArrowLeft" /></button><h2 class="module-title">命名工作台</h2><div class="rename-current-directory"><ProviderIcon :type="storageInfo?.type||'local'" /><strong>{{storageInfo?.name}}</strong><VisitPath :parents="pathParts.slice(0,-1)" :deepest="pathParts.at(-1)||'根目录'" /></div><div class="rename-heading-actions"><button class="btn" :disabled="busy" @click="historyOpen=true"><Icon name="History" />历史记录</button><button class="btn primary" :disabled="invalid || busy" @click="execute"><Icon name="Check" />{{ busy ? '正在重命名…' : '确认重命名' }}</button></div></header>
    <div class="rename-columns">
      <section class="rename-comparison">
        <ThinScroll :ref="el => viewport = el?.element || null" :thickness="3" class="rename-preview-scroll" content-class="rename-preview">
          <div class="rename-preview-content">
          <p v-if="!items.length" class="small-empty">{{ error || '等待应用规则' }}</p>
          <div :style="{ height: `${top}px` }" />
          <article v-for="item in shown" :key="item.id" class="rename-preview-row" :class="{ignored: ignored.includes(item.id)}">
            <div><small>原：</small><Icon :name="entryIcon(item)" :class="{'folder-color':item.isDir}" :size="18" /><span :data-tooltip="item.name">{{ item.name }}</span></div>
            <div class="rename-changed" :class="{ 'error-message': item.error }"><small>新：</small><Icon :name="entryIcon({name:item.newName,isDir:item.isDir})" :class="{'folder-color':item.isDir}" :size="18" /><span :data-tooltip="item.error || item.newName"><template v-if="item.error">{{ item.error }}</template><template v-else><template v-for="(part, index) in nameParts(item)" :key="index"><mark v-if="part.changed">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></template></span></div>
            <div class="rename-item-actions"><button class="icon-btn" :aria-label="ignored.includes(item.id) ? '取消忽略' : '忽略'" :data-tooltip="ignored.includes(item.id) ? '取消忽略' : '忽略'" data-tooltip-always :disabled="busy" @click="toggleIgnore(item)"><Icon :name="ignored.includes(item.id) ? 'RotateCcw' : 'X'" /></button><button class="icon-btn" aria-label="修改" data-tooltip="修改" data-tooltip-always :disabled="busy" @click="editing = item; editName = item.name"><Icon name="TextCursorInput" /></button></div>
          </article><div :style="{ height: `${bottom}px` }" /><small class="rename-count">{{files.length}} 项</small>
          </div>
        </ThinScroll><p v-if="error && items.length" class="error-message">{{ error }}</p>
      </section>
      <aside class="rename-rules">
        <ThinScroll :thickness="3" class="rename-rules-scroll">
        <fieldset :disabled="busy">
          <article v-for="(rule, index) in rules" :key="index" class="rename-rule" :class="{collapsed: expanded !== index}">
            <header><strong>规则 {{ index + 1 }}</strong><button v-if="rules.length > 1" class="icon-btn" aria-label="删除规则" @click="removeRule(index)"><Icon name="Trash2" /></button><button class="icon-btn" :aria-label="`规则 ${index + 1}`" :aria-expanded="expanded === index" @click="expanded = expanded === index ? -1 : index"><Icon :name="expanded === index ? 'ChevronDown' : 'ChevronRight'" /></button></header>
            <Transition name="rule-expand"><div v-if="expanded === index" class="rule-collapse"><div class="rule-fields">
            <RoundedSelect :model-value="rule.kind" label="规则类型" :options="renameRuleTypes" @update:model-value="changeRule(index,$event)" />
            <RenameRuleFields :rule="rule" />
            </div></div></Transition>
          </article>
          <button ref="addButton" class="btn rename-add-rule" :disabled="rules.length >= 30" :aria-expanded="addOpen" aria-controls="rename-rule-choices" @click="toggleAdd"><Icon name="Plus" />添加规则<Icon :name="addOpen ? 'ChevronUp' : 'ChevronDown'" class="rename-add-chevron" /></button>
          <div v-if="addOpen" id="rename-rule-choices" ref="choices" class="rename-rule-choices" role="group" aria-label="可选规则" @keydown.esc.stop.prevent="closeChoices"><button v-for="type in renameRuleTypes" :key="type.value" type="button" @click="addRule(type.value)"><Icon :name="type.icon" :size="16" />{{type.label}}</button></div>
        </fieldset>
        </ThinScroll>
        <div class="rename-save"><div v-if="sets.length" class="rename-set-picker"><RoundedSelect upward :model-value="chosen" label="常用规则集" :options="[{ value: '', label: '常用规则集' }, ...sets.map(s => ({ value: s.id, label: s.name }))]" :disabled="busy" @update:model-value="applySet" /><button class="icon-btn" aria-label="删除规则集" :disabled="!chosen || busy || saveBusy" @click="deleteSet"><Icon name="X" /></button></div><button class="btn" :disabled="busy || saveBusy || !validRules" @click="naming = true"><Icon name="Save" />保存为规则集</button></div>
      </aside>
    </div>
  </section>
  <Modal v-if="naming" title="保存规则集" compact @close="!saveBusy && (naming = false)"><form @submit.prevent="saveSet"><div class="modal-body"><label>规则集名称<input v-model="setName" required maxlength="80" :disabled="saveBusy" /></label></div><footer class="modal-footer"><button type="button" class="btn cancel" :disabled="saveBusy" @click="naming = false">取消</button><button class="btn primary" :disabled="saveBusy || !setName.trim()">保存</button></footer></form></Modal>
  <Modal v-if="editing" title="重命名" compact @close="!busy && (editing = null)"><form @submit.prevent="renameOne"><div class="modal-body"><label>新名称<input v-model="editName" required maxlength="255" :disabled="busy" /></label></div><footer class="modal-footer"><button type="button" class="btn cancel" :disabled="busy" @click="editing = null">取消</button><button class="btn primary" :disabled="busy || !editName.trim()">确认修改</button></footer></form></Modal>
</template>
<style scoped>
.rename-workbench {height:calc(100dvh - 80px);padding-bottom:var(--workspace-end-gap)}.rename-heading {--rename-heading-font-size:14px;min-height:48px;margin-bottom:20px;align-items:center;justify-content:flex-start;gap:10px}.rename-heading-actions{margin-left:auto;display:flex;gap:8px;flex:none}.rename-comparison{position:relative}.rename-count{position:absolute;bottom:4px;left:4px;font-size:12px;color:var(--muted);pointer-events:none}.rename-current-directory{display:flex;align-items:center;gap:6px;min-width:0;font-size:var(--rename-heading-font-size);flex:1}.rename-current-directory strong{max-width:100px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.rename-current-directory :deep(.provider-icon){width:18px;height:18px;background:none;border:0;padding:0;flex:none;font-size:18px}
.rename-heading-actions .btn { font-size:var(--rename-heading-font-size); }
.rename-current-directory :deep(.provider-icon > svg), .rename-current-directory :deep(.provider-icon > .provider-logo) { width:18px; height:18px; }
.rename-current-directory :deep(.provider-icon .dav-symbol) { width:18px !important; height:18px !important; }
.rename-current-directory :deep(.provider-icon .dav-symbol > span) { font-size:5px; }
.rename-workbench { padding-bottom:0; }.rename-preview-scroll { flex:1; min-height:0; }
.rename-heading h2.module-title { font-size:15px; }
.rename-preview-scroll :deep(.rename-preview) { border:0; border-radius:0; padding-right:6px; }.rename-preview-content { position:relative;min-height:100%;padding-bottom:24px; }
.rename-preview-row { position: relative; grid-template-columns: minmax(0,1fr); gap: 2px; padding:8px 92px 8px 12px; font-size:12px; height:66px; margin-bottom:6px; border:1px solid color-mix(in srgb,var(--border) 65%,transparent); border-radius:8px; background:var(--input); }
.rename-comparison, .rename-rules { padding:10px; border:1px solid var(--border); border-radius:8px; background:var(--surface); }
.rename-preview-row > div:first-child { color:var(--muted); }
.rename-preview-row small { flex-shrink: 0; color: var(--muted); }
.rename-preview-row.ignored > div:not(.rename-item-actions) { opacity: .45; }
.rename-preview-row .rename-item-actions { position: absolute; right: 8px; top: 18px; opacity: 0; }
.rename-preview-row:hover .rename-item-actions, .rename-preview-row:focus-within .rename-item-actions { opacity: 1; }
.rename-rules { display: flex; flex-direction: column; overflow:hidden; }
.rename-rules-scroll { flex:1; min-height:0; }
.rename-rules-scroll :deep(.thin-scroll-area) { padding-right:6px; }
.rename-save { flex-shrink:0; margin-top:0; padding-top:10px; background:var(--surface); }
.rename-rule.collapsed { padding: 4px 12px; margin-bottom: 6px; }
.rename-rule.collapsed header { margin: 0; }
.rule-collapse { display:grid; grid-template-rows:1fr; opacity:1; }.rule-fields { min-height:0; }.rule-expand-enter-active .rule-fields, .rule-expand-leave-active .rule-fields { overflow:hidden; }.rule-expand-enter-active, .rule-expand-leave-active { transition:grid-template-rows .24s ease, opacity .2s ease; }.rule-expand-enter-from, .rule-expand-leave-to { grid-template-rows:0fr; opacity:0; }
.rename-rules, .rename-rules :deep(button), .rename-rules label, .rename-rules input, .rename-rules :deep(.rounded-select), .rename-rule header strong { font-size:12px; }
.rename-rule label:not(.rename-check), .rule-inline { display:grid; grid-template-columns:64px minmax(0,1fr); align-items:center; gap:8px; margin-top:8px; }.rule-inline { margin-bottom:8px; }
.rename-rule input:not([type=checkbox]), .rename-rules :deep(.rounded-select-trigger), .rename-rules .btn { min-height:28px; height:28px; padding-top:3px; padding-bottom:3px; }
.rule-checks { display:flex; gap:18px; }.rule-checks label { margin:10px 0 2px; }
.rename-rule { padding:10px; margin-bottom:8px; background:var(--surface); }.rename-rule header { margin-bottom:6px; }.rename-rule .icon-btn { width:28px; height:28px; min-height:28px; }
.rename-add-rule { justify-content:flex-start;gap:8px; }.rename-add-chevron { margin-left:auto; }
.rename-rule-choices { display:grid;gap:2px;padding:6px;margin-top:6px;border:1px solid var(--border);border-radius:8px;background:var(--input); }
.rename-rule-choices button { display:flex;align-items:center;gap:8px;text-align:left;padding:8px;border:0;border-radius:4px;background:transparent;color:var(--text);min-height:34px; }
.rename-rule-choices button:hover, .rename-rule-choices button:focus-visible { background:color-mix(in srgb,var(--primary) 12%,var(--surface));color:var(--primary); }
.rename-rule-choices button:focus-visible { outline:2px solid var(--primary);outline-offset:-2px; }
.rename-rules .rule-fields { min-width:0; }
.rename-changed small { color:var(--green); }.rename-changed > span { color:var(--text); }.rename-changed mark { color:#bf690b; background:#df921c12; border-radius:3px; }
:global([data-theme=dark] .rename-changed mark) { color:#f4b45a; }
@media(prefers-reduced-motion:reduce) { .rule-expand-enter-active, .rule-expand-leave-active { transition:none; } }
@media (hover: none) { .rename-preview-row .rename-item-actions { opacity: 1; } }
@media (max-width:1100px) { .rename-heading { --rename-heading-font-size:12px; } }
@media (max-width:760px) { .rename-columns{grid-template-columns:minmax(0,1fr);grid-template-rows:minmax(0,1fr) minmax(0,1fr);gap:8px}.rename-heading{flex-wrap:wrap}.rename-heading-actions{margin-left:auto}.rename-current-directory{flex-basis:40%}.rename-preview-row .rename-item-actions{opacity:1}.rename-preview-scroll :deep(.rename-preview){min-height:0;max-height:none}.rename-rules{height:auto}.rule-checks{flex-wrap:wrap;gap:6px} }
</style>
