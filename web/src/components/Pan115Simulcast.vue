<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, state, notify } from '../lib'
import Icon from './Icon.vue'
import Modal from './Modal.vue'
import ProviderIcon from './ProviderIcon.vue'
import RoundedSelect from './RoundedSelect.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'

const emit = defineEmits(['close'])
const configs = ref({}), loaded = ref(false), busy = ref(false), error = ref('')
const editing = ref(false), original = ref(''), picker = ref(false), removing = ref('')
const form = reactive({ storageId: '', enabled: true, directory: '', directoryLabel: '' })
const storages = computed(() => state.storages.filter(s => s.type === '115' && s.enabled))
const options = computed(() => storages.value.filter(s => !configs.value[s.id] || s.id === original.value).map(s => ({ value: s.id, label: s.name })))
const canAdd = computed(() => storages.value.some(s => !configs.value[s.id]))
const poolName = id => state.storages.find(s => s.id === id)?.name || '存储已删除'
watch(() => form.storageId, () => { if (form.storageId !== original.value) { form.directory = ''; form.directoryLabel = '' } })
async function load() {
  busy.value = true; error.value = ''
  try { configs.value = await api('/115-simulcast'); loaded.value = true }
  catch (e) { error.value = e.message } finally { busy.value = false }
}
onMounted(load)
function edit(id = '') {
  original.value = id
  Object.assign(form, { storageId: id, enabled: true, directory: '', directoryLabel: '' }, configs.value[id] || {})
  error.value = ''; editing.value = true
}
async function persist(next) {
  if (busy.value || !loaded.value) return false
  busy.value = true; error.value = ''
  try { configs.value = await api('/115-simulcast', 'PUT', next); notify('115 同播配置已保存'); return true }
  catch (e) { error.value = e.message; return false } finally { busy.value = false }
}
async function save() {
  if (!form.storageId || !form.directory) { error.value = '请选择存储和复制目录'; return }
  const next = { ...configs.value }
  delete next[original.value]
  next[form.storageId] = { enabled: form.enabled, directory: form.directory, directoryLabel: form.directoryLabel }
  if (await persist(next)) editing.value = false
}
async function remove() {
  const next = { ...configs.value }; delete next[removing.value]
  if (await persist(next)) removing.value = ''
}
function select(value) {
  form.directory = value.source; form.directoryLabel = value.sourceLabel; picker.value = false
}
</script>

<template>
  <Modal title="115 同播复制" @close="!busy && emit('close')">
  <section class="modal-body simulcast-settings" :aria-busy="busy">
    <p v-if="error && !editing && !removing" class="error-message" role="alert">{{ error }}</p>
    <div class="simulcast-list">
      <article v-for="(config, id) in configs" :key="id" class="simulcast-row">
        <button class="simulcast-toggle" :aria-label="`${config.enabled ? '停用' : '启用'} ${poolName(id)} 同播复制`" :aria-pressed="config.enabled" :disabled="busy" @click="persist({ ...configs, [id]: { ...config, enabled: !config.enabled } })"><ProviderIcon type="115" /></button>
        <div class="simulcast-name"><strong>{{ poolName(id) }}</strong><span>{{ config.directoryLabel || config.directory }}</span></div>
        <button class="icon-btn" title="编辑" :aria-label="`编辑 ${poolName(id)}`" :disabled="busy" @click="edit(id)"><Icon name="Pencil" /></button>
        <button class="icon-btn danger-text" title="删除" :aria-label="`删除 ${poolName(id)}`" :disabled="busy" @click="removing = id; error = ''"><Icon name="Trash2" /></button>
      </article>
      <button class="simulcast-add" :disabled="busy || !loaded || !canAdd" @click="edit()"><Icon name="Plus" />存储绑定</button>
    </div>
  </section>
  </Modal>
  <Modal v-if="editing" :title="original ? '编辑同播复制' : '添加同播复制'" @close="!busy && (editing = false)">
    <form @submit.prevent="save">
      <div class="modal-body simulcast-form">
        <label>115 存储<RoundedSelect v-model="form.storageId" label="115 存储" placeholder="选择存储" :options="options" :disabled="busy || !!original" /></label>
        <label>复制目录<button type="button" class="btn simulcast-directory" aria-label="选择复制目录" :disabled="busy || !form.storageId" @click="picker = true"><Icon name="Folder" /><span>{{ form.directoryLabel || form.directory || '选择目录' }}</span></button></label>
        <p v-if="error" class="error-message" role="alert">{{ error }}</p>
      </div>
      <footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="editing = false">取消</button><button class="btn primary" :disabled="busy || !form.storageId || !form.directory">{{ busy ? '保存中…' : '保存' }}</button></footer>
    </form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="storages.filter(s => s.id === form.storageId)" :storage="form.storageId" :initial="form.directory" :initial-label="form.directoryLabel" @select="select" @close="picker = false" />
  <Modal v-if="removing" title="删除同播配置" @close="!busy && (removing = '')">
    <div class="modal-body">确认删除「{{ poolName(removing) }}」的配置？已生成的云端副本将保留。<p v-if="error" class="error-message" role="alert">{{ error }}</p></div>
    <footer class="modal-footer"><button class="btn" :disabled="busy" @click="removing = ''">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除配置</button></footer>
  </Modal>
</template>

<style scoped>
.simulcast-settings { min-width: 0; }
.simulcast-list { display: grid; gap: 12px; }
.simulcast-row { display: flex; align-items: center; gap: 8px; padding: 12px 0; border-bottom: 1px solid var(--border); min-width: 0; }
.simulcast-name { flex: 1; min-width: 0; display: grid; gap: 4px; overflow-wrap: anywhere; }
.simulcast-name span { color: var(--muted); font-size: 13px; }
.simulcast-toggle { padding:0; border:0; background:transparent; flex:0 0 40px; width:40px; height:40px; }
.simulcast-toggle[aria-pressed=false] { opacity:.45; }
.simulcast-toggle :deep(.provider-icon) { width:40px; height:40px; background:transparent; border-radius:0; }
.simulcast-toggle :deep(img) { width:100%; height:100%; }
.simulcast-row .icon-btn { width: 36px; height: 36px; flex-shrink: 0; }
.simulcast-add { display: flex; justify-content: center; align-items: center; gap: 8px; border: 1px dashed var(--border); border-radius: 8px; min-height: 48px; color: var(--muted); background: transparent; cursor: pointer; }
.simulcast-add:disabled { opacity: .5; cursor: default; }
.simulcast-form { display: grid; gap: 18px; }
.simulcast-form label { display: grid; gap: 8px; min-width: 0; }
.simulcast-form .toggle-line { display: flex; }
.simulcast-directory { width: 100%; justify-content: flex-start; min-width: 0; }
.simulcast-directory span { white-space: normal; overflow-wrap: anywhere; min-width: 0; }
@media (max-width: 480px) { .simulcast-row { flex-wrap: wrap; } .simulcast-name { flex-basis: calc(100% - 44px); } .simulcast-row .switch { margin-right: auto; } }
</style>
