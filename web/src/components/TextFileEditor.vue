<script setup>
import { ref } from 'vue'
import { api, notify } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
const props = defineProps({ file:Object, endpoint:String, content:String, revision:String, disabled:Boolean })
const emit = defineEmits(['close','saved'])
const text = ref(props.content), saving = ref(false), error = ref('')
async function save() {
  if (saving.value || props.disabled) return
  saving.value = true; error.value = ''
  try { await api(props.endpoint, 'PUT', { content:text.value, revision:props.revision }); notify('文件已保存'); emit('saved'); emit('close') }
  catch (e) { error.value = e.message }
  finally { saving.value = false }
}
</script>
<template>
  <Modal :title="file.name" standard @close="!saving && emit('close')"><form @submit.prevent="save"><div class="modal-body"><textarea v-model="text" class="scrape-text-editor" aria-label="文件内容" spellcheck="false" :disabled="saving || disabled" /><p v-if="error" class="error-message" role="alert">{{ error }}</p></div><footer class="modal-footer"><button class="btn primary" :disabled="saving || disabled"><Icon name="Save" />保存</button><button type="button" class="btn" :disabled="saving" @click="emit('close')">取消</button></footer></form></Modal>
</template>
<style scoped>
.scrape-text-editor { width:100%; height:min(32dvh,260px); min-height:120px; resize:vertical; font:14px/1.6 Consolas,monospace; }
</style>
