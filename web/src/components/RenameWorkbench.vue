<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, notify } from '../lib'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import { useVirtualList } from '../virtual-list'
const props = defineProps({ storage: String, source: String, files: Array })
const emit = defineEmits(['close', 'changed'])
const rules = ref([{ kind: 'replace', find: '', replace: '', caseSensitive: false, firstOnly: false }])
const expanded = ref(0)
const originals = () => props.files.map(f => ({ id: f.id, name: f.name, newName: f.name, isDir: f.isDir }))
const sets = ref([]), chosen = ref(''), setName = ref(''), items = ref([]), error = ref(''), busy = ref(false), loading = ref(false), saveBusy = ref(false)
const viewport = ref(null)
const { shown, top, bottom, reset } = useVirtualList(items, viewport, { rowHeight: 92 })
let timer, generation = 0, disposed = false
items.value = originals()
const payload = () => ({ storageId: props.storage, source: props.source, ids: props.files.map(f => f.id), rules: rules.value.filter(r => r.find) })
const changes = computed(() => items.value.filter(f => f.name !== f.newName && !f.error).length)
const invalid = computed(() => loading.value || !items.value.length || items.value.some(f => f.error) || !!error.value || !changes.value)
async function preview() {
  const id = ++generation
  if (!payload().rules.length) { items.value = originals(); loading.value = false; error.value = ''; return }
  loading.value = true; error.value = ''
  try { const result = await api('/files/rename-preview', 'POST', payload()); if (!disposed && id === generation) { items.value = result; reset() } }
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
  saveBusy.value = true
  try { sets.value = await api('/files/rename-rules', 'POST', { name: setName.value, rules: payload().rules }); chosen.value = sets.value.at(-1).id; setName.value = ''; notify('规则集已保存') }
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
    const result = await api('/files/rename', 'POST', { ...payload(), expected: items.value })
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
      <section class="rename-comparison"><header><span>原名称</span><Icon name="ArrowRight" /><span>新名称</span></header>
        <div ref="viewport" class="rename-preview">
          <p v-if="loading" class="small-empty">正在预览…</p><p v-else-if="!items.length" class="small-empty">{{ error || '等待应用规则' }}</p>
          <div :style="{ height: `${top}px` }" />
          <article v-for="item in shown" :key="item.id" class="rename-preview-row">
            <div><Icon :name="item.isDir ? 'Folder' : 'FileVideo'" /><span :data-tooltip="item.name">{{ item.name }}</span></div>
            <Icon name="ArrowRight" />
            <div class="rename-changed" :class="{ 'error-message': item.error }"><span :data-tooltip="item.error || item.newName">{{ item.error || item.newName }}</span></div>
          </article><div :style="{ height: `${bottom}px` }" />
        </div><p v-if="error && items.length" class="error-message">{{ error }}</p>
      </section>
      <aside class="rename-rules"><div class="rename-set-picker"><RoundedSelect :model-value="chosen" label="选择规则集" :options="[{ value: '', label: '选择规则集' }, ...sets.map(s => ({ value: s.id, label: s.name }))]" :disabled="busy" @update:model-value="applySet" /><button class="icon-btn" aria-label="删除规则集" :disabled="!chosen || busy || saveBusy" @click="deleteSet"><Icon name="Trash2" /></button></div>
        <fieldset :disabled="busy">
          <article v-for="(rule, index) in rules" :key="index" class="rename-rule">
            <header><button class="text-btn rename-rule-toggle" :aria-expanded="expanded === index" @click="expanded = expanded === index ? -1 : index"><Icon :name="expanded === index ? 'ChevronDown' : 'ChevronRight'" />规则 {{ index + 1 }}</button><button class="icon-btn" aria-label="删除规则" :disabled="rules.length === 1" @click="removeRule(index)"><Icon name="Trash2" /></button></header>
            <template v-if="expanded === index">
            <RoundedSelect v-model="rule.kind" label="规则类型" :options="[{ value: 'replace', label: '查找替换' }]" />
            <label>查找内容<input v-model="rule.find" maxlength="255" /></label><label>替换为<input v-model="rule.replace" maxlength="255" /></label>
            <label class="rename-check"><input v-model="rule.caseSensitive" type="checkbox" />区分大小写</label><label class="rename-check"><input v-model="rule.firstOnly" type="checkbox" />仅替换第一个</label>
            </template>
          </article>
          <button class="btn rename-add-rule" :disabled="rules.length >= 30" @click="addRule"><Icon name="Plus" />添加规则</button>
        </fieldset>
        <form class="rename-save" @submit.prevent="saveSet"><input v-model="setName" aria-label="规则集名称" placeholder="规则集名称" required :disabled="busy || saveBusy" /><button class="btn" :disabled="busy || saveBusy || !setName.trim() || loading || !!error"><Icon name="Save" />保存规则集</button></form>
      </aside>
    </div>
  </section>
</template>
