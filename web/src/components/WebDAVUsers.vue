<script setup>
import { onMounted, reactive, ref } from 'vue'
import { api, state, notify } from '../lib'
import Icon from './Icon.vue'
import Modal from './Modal.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
const users = ref([]), open = ref(false), picker = ref(false), busy = ref(false), error = ref(''), removing = ref(null)
const form = reactive({ id: '', username: '', password: '', enabled: true, grants: [] })
async function load() { try { users.value = await api('/webdav/users') } catch (e) { notify(e.message, true) } }
async function toggle(user) {
  busy.value = true
  try { await api(`/webdav/users/${user.id}`, 'PATCH', { enabled: !user.enabled }); await load() }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
onMounted(load)
function edit(user) {
  Object.assign(form, user ? JSON.parse(JSON.stringify(user)) : { id: '', username: '', password: '', enabled: true, grants: [] })
  form.password = ''; error.value = ''; open.value = true
}
function addGrant(value) {
  if (!form.grants.some(g => g.storageId === value.storageId && g.directory === value.source)) {
    const pool = state.storages.find(s => s.id === value.storageId)
    const label = value.sourceLabel === '根目录' ? pool?.name || '根目录' : value.sourceLabel || '已选目录'
    const base = label.replace(/[\\/:*?"<>|]/g, '_')
    let name = base, index = 2
    while (form.grants.some(g => g.name.toLowerCase() === name.toLowerCase())) name = `${base} (${index++})`
    form.grants.push({ storageId: value.storageId, directory: value.source, directoryLabel: label, name })
  }
  picker.value = false
}
async function save() {
  busy.value = true; error.value = ''
  try {
    await api('/webdav/users' + (form.id ? `/${form.id}` : ''), form.id ? 'PUT' : 'POST', form)
    open.value = false; await load(); notify('WebDAV 用户已保存')
  } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function remove() {
  busy.value = true
  try { await api(`/webdav/users/${removing.value.id}`, 'DELETE'); removing.value = null; await load(); notify('WebDAV 用户已删除') }
  catch (e) { notify(e.message, true) } finally { busy.value = false }
}
</script>
<template>
  <section class="dav-users">
    <div class="section-label"><h2>WebDAV 用户</h2><button type="button" class="btn primary" @click="edit()"><Icon name="UserPlus" />添加用户</button></div>
    <p v-if="!users.length" class="small-empty">暂无独立用户</p>
    <div class="dav-user-grid"><article v-for="user in users" :key="user.id" class="dav-user-card">
      <header><Icon name="UserRound" :size="28" /><strong>{{ user.username }}</strong><span class="status" :class="user.enabled ? 'success' : 'muted'">{{ user.enabled ? '已启用' : '已停用' }}</span></header>
      <span class="dav-readonly">只读</span>
      <div class="dav-paths"><div v-for="grant in user.grants" :key="grant.id"><Icon name="Folder" :size="15" /><span>{{ grant.directoryLabel || grant.name }}</span></div><span v-if="!user.grants.length" class="muted">未授权目录</span></div>
      <footer><button type="button" class="icon-btn" title="编辑用户" aria-label="编辑用户" @click="edit(user)"><Icon name="Pencil" /></button><button type="button" class="icon-btn" :disabled="busy" :title="user.enabled ? '停用用户' : '启用用户'" :aria-label="user.enabled ? '停用用户' : '启用用户'" @click="toggle(user)"><Icon :name="user.enabled ? 'Pause' : 'Play'" /></button><button type="button" class="icon-btn danger-text" title="删除用户" aria-label="删除用户" @click="removing = user"><Icon name="Trash2" /></button></footer>
    </article></div>
  </section>
  <Modal v-if="open" :title="form.id ? '编辑 WebDAV 用户' : '添加 WebDAV 用户'" wide @close="!busy && (open = false)">
    <form @submit.prevent="save"><div class="modal-body">
      <div class="form-grid"><label>账号<input v-model="form.username" required maxlength="150" autocomplete="off" /></label><label>密码<input v-model="form.password" type="password" :required="!form.id" maxlength="72" autocomplete="new-password" :placeholder="form.id ? '留空保持原密码' : ''" /></label><label class="toggle-line full"><span>启用用户</span><input v-model="form.enabled" type="checkbox" class="switch" role="switch" /></label></div>
      <div class="section-label"><h3>授权目录</h3><button type="button" class="btn" @click="picker = true"><Icon name="FolderPlus" />添加目录</button></div>
      <div v-for="(grant, index) in form.grants" :key="index" class="dav-grant"><Icon name="Folder" /><span>{{ grant.directoryLabel || grant.name }}<small>{{ state.storages.find(s => s.id === grant.storageId)?.name || '存储已删除' }}</small></span><button type="button" class="icon-btn danger-text" aria-label="移除授权目录" @click="form.grants.splice(index, 1)"><Icon name="Trash2" /></button></div>
      <p v-if="!form.grants.length" class="small-empty">未授权任何目录</p><p v-if="error" class="error-message" role="alert">{{ error }}</p>
    </div><footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="open = false">取消</button><button class="btn primary" :disabled="busy">{{ busy ? '保存中…' : '保存用户' }}</button></footer></form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="state.storages.filter(s => s.enabled)" @select="addGrant" @close="picker = false" />
  <Modal v-if="removing" title="删除 WebDAV 用户" @close="!busy && (removing = null)"><div class="modal-body">确认删除「{{ removing.username }}」？该用户将无法继续访问。</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="removing = null">取消</button><button class="btn danger" :disabled="busy" @click="remove">删除用户</button></footer></Modal>
</template>
