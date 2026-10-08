<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, notify, state } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import { useVirtualList } from '../virtual-list'
const props = defineProps({ storage: String, source: String, files: Array })
const emit = defineEmits(['close', 'changed'])
const rules = ref([{ kind: 'replace', find: '', replace: '', caseSensitive: false, firstOnly: false }])
const expanded = ref(0)
const sourceFiles = ref(props.files.map(f => ({ ...f })))
const ignored = ref([]), naming = ref(false), editing = ref(null), editName = ref('')
const originals = () => sourceFiles.value.map(f => ({ id: f.id, name: f.name, newName: f.name, isDir: f.isDir }))
const sets = ref([]), chosen = ref(''), setName = ref(''), items = ref([]), error = ref(''), busy = ref(false), loading = ref(false), saveBusy = ref(false)
const viewport = ref(null)
const { shown, top, bottom, reset } = useVirtualList(items, viewport, { rowHeight: 92 })
let timer, generation = 0, disposed = false
items.value = originals()
const payload = () => ({ storageId: props.storage, source: props.source, ids: sourceFiles.value.filter(f => !ignored.value.includes(f.id)).map(f => f.id), rules: rules.value.filter(r => r.find.trim()) })
const changes = computed(() => items.value.filter(f => !ignored.value.includes(f.id) && f.name !== f.newName && !f.error).length)
const invalid = computed(() => loading.value || !items.value.length || items.value.some(f => !ignored.value.includes(f.id) && f.error) || !!error.value || !changes.value)
const validRules = computed(() => rules.value.length > 0 && rules.value.every(r => r.find.trim()))
function toggleIgnore(item) {
  ignored.value = ignored.value.includes(item.id) ? ignored.value.filter(id => id !== item.id) : [...ignored.value, item.id]
  clearTimeout(timer); preview()
}
async function renameOne() {
  if (!editing.value || busy.value) return
  busy.value = true
  const item = editing.value
  try {
    await api('/files/action', 'POST', { storageId: props.storage, source: props.source, action: 'rename', ids: [item.id], name: editName.value })
    const file = sourceFiles.value.find(f => f.id === item.id)
    const type = state.storages.find(s => s.id === props.storage)?.type
    if (['local', 'webdav', 'openlist'].includes(type)) {
      const newID = item.id.slice(0, item.id.lastIndexOf('/') + 1) + editName.value
      ignored.value = ignored.value.map(id => id === file.id ? newID : id)
      file.id = newID
    }
    file.name = editName.value
    editing.value = null; emit('changed'); notify('已重命名'); await preview()
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
async function preview() {
  const id = ++generation
  if (!payload().rules.length || !payload().ids.length) { items.value = originals(); loading.value = false; error.value = ''; return }
  loading.value = true; error.value = ''
  try { const result = await api('/files/rename-preview', 'POST', payload()); if (!disposed && id === generation) { const byID = new Map(result.map(f => [f.id, f])); items.value = originals().map(f => byID.get(f.id) || f); reset() } }
  catch (e) { if (!disposed && id === generation) { error.value = e.message; items.value = [] } }
  finally { if (!disposed && id === generation) loading.value = false }
}
watch(rules, () => {
  clearTimeout(timer); generation++; loading.value = true
  timer = setTimeout(preview, 250)
}, { deep: true })
function addRule() { rules.value.push({ kind: 'replace', find: '', replace: '', caseSensitive: false, firstOnly: false }); expanded.value = rules.value.length - 1 }
function removeRule(index) { rules.value.splice(index, 1); expanded.value = Math.min(index, rules.value.length - 1) }
function applySet(value) {
  chosen.value = value
  const set = sets.value.find(s => s.id === value)
  if (set) { rules.value = JSON.parse(JSON.stringify(set.rules)); expanded.value = 0 }
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
  <section class="rename-workbench">
    <header class="rename-heading"><button class="icon-btn" aria-label="返回文件管理" :disabled="busy" @click="emit('close')"><Icon name="ArrowLeft" /></button><h2>重命名工作台</h2><span>{{ files.length }} 个项目</span><button class="btn primary" :disabled="invalid || busy" @click="execute">{{ busy ? '正在重命名…' : '确认重命名' }}</button></header>
    <div class="rename-columns">
      <section class="rename-comparison">
        <div ref="viewport" class="rename-preview">
          <p v-if="loading" class="small-empty">正在预览…</p><p v-else-if="!items.length" class="small-empty">{{ error || '等待应用规则' }}</p>
          <div :style="{ height: `${top}px` }" />
          <article v-for="item in shown" :key="item.id" class="rename-preview-row" :class="{ignored: ignored.includes(item.id)}">
            <div><small>原：</small><span :data-tooltip="item.name">{{ item.name }}</span></div>
            <div class="rename-changed" :class="{ 'error-message': item.error }"><small>新：</small><span :data-tooltip="item.error || item.newName">{{ item.error || item.newName }}</span></div>
            <div class="rename-item-actions"><button class="icon-btn" :aria-label="ignored.includes(item.id) ? '取消忽略' : '忽略'" :title="ignored.includes(item.id) ? '取消忽略' : '忽略'" :disabled="busy" @click="toggleIgnore(item)"><Icon :name="ignored.includes(item.id) ? 'RotateCcw' : 'X'" /></button><button class="icon-btn" aria-label="修改" title="修改" :disabled="busy" @click="editing = item; editName = item.name"><Icon name="Pencil" /></button></div>
          </article><div :style="{ height: `${bottom}px` }" />
        </div><p v-if="error && items.length" class="error-message">{{ error }}</p>
      </section>
      <aside class="rename-rules">
        <fieldset :disabled="busy">
          <article v-for="(rule, index) in rules" :key="index" class="rename-rule" :class="{collapsed: expanded !== index}">
            <header><strong>规则 {{ index + 1 }}</strong><button class="icon-btn" aria-label="删除规则" :disabled="rules.length === 1" @click="removeRule(index)"><Icon name="Trash2" /></button><button class="icon-btn" :aria-label="`规则 ${index + 1}`" :aria-expanded="expanded === index" @click="expanded = expanded === index ? -1 : index"><Icon :name="expanded === index ? 'ChevronDown' : 'ChevronRight'" /></button></header>
            <template v-if="expanded === index">
            <RoundedSelect v-model="rule.kind" label="规则类型" :options="[{ value: 'replace', label: '查找替换' }]" />
            <label>查找内容<input v-model="rule.find" maxlength="255" /></label><label>替换为<input v-model="rule.replace" maxlength="255" /></label>
            <label class="rename-check"><input v-model="rule.caseSensitive" type="checkbox" />区分大小写</label><label class="rename-check"><input v-model="rule.firstOnly" type="checkbox" />仅替换第一个</label>
            </template>
          </article>
          <button class="btn rename-add-rule" :disabled="rules.length >= 30" @click="addRule"><Icon name="Plus" />添加规则</button>
        </fieldset>
        <div class="rename-save"><div v-if="sets.length" class="rename-set-picker"><RoundedSelect upward :model-value="chosen" label="选择规则集" :options="[{ value: '', label: '选择规则集' }, ...sets.map(s => ({ value: s.id, label: s.name }))]" :disabled="busy" @update:model-value="applySet" /><button class="icon-btn" aria-label="删除规则集" :disabled="!chosen || busy || saveBusy" @click="deleteSet"><Icon name="X" /></button></div><button class="btn" :disabled="busy || saveBusy || !validRules" @click="naming = true"><Icon name="Save" />保存规则集</button></div>
      </aside>
    </div>
  </section>
  <Modal v-if="naming" title="保存规则集" compact @close="!saveBusy && (naming = false)"><form @submit.prevent="saveSet"><div class="modal-body"><label>规则集名称<input v-model="setName" required maxlength="80" :disabled="saveBusy" /></label></div><footer class="modal-footer"><button type="button" class="btn cancel" :disabled="saveBusy" @click="naming = false">取消</button><button class="btn primary" :disabled="saveBusy || !setName.trim()">保存</button></footer></form></Modal>
  <Modal v-if="editing" title="重命名" compact @close="!busy && (editing = null)"><form @submit.prevent="renameOne"><div class="modal-body"><label>新名称<input v-model="editName" required maxlength="255" :disabled="busy" /></label></div><footer class="modal-footer"><button type="button" class="btn cancel" :disabled="busy" @click="editing = null">取消</button><button class="btn primary" :disabled="busy || !editName.trim()">确认修改</button></footer></form></Modal>
</template>
<style scoped>
.rename-preview-row { position: relative; grid-template-columns: minmax(0,1fr); gap: 6px; padding-right: 92px; }
.rename-preview-row small { flex-shrink: 0; color: var(--muted); }
.rename-preview-row.ignored > div:not(.rename-item-actions) { opacity: .45; }
.rename-preview-row .rename-item-actions { position: absolute; right: 8px; top: 28px; opacity: 0; }
.rename-preview-row:hover .rename-item-actions, .rename-preview-row:focus-within .rename-item-actions { opacity: 1; }
.rename-rules { display: flex; flex-direction: column; }
.rename-save { margin-top: auto; padding-top: 18px; }
.rename-rule.collapsed { padding: 4px 12px; margin-bottom: 6px; }
.rename-rule.collapsed header { margin: 0; }
@media (hover: none) { .rename-preview-row .rename-item-actions { opacity: 1; } }
</style>
