<script setup>
import { onMounted, ref } from 'vue'
import { api, notify } from '../lib'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
const address=ref(''),format=ref('pickcode_name'),overwriteMode=ref('skip'),busy=ref(false),ready=ref(false),saved=ref('')
function signature(){return JSON.stringify([address.value,format.value,overwriteMode.value])}
function apply(value){address.value=value.address;format.value=value.format;overwriteMode.value=value.overwriteMode||'skip';saved.value=signature()}
async function save(){
  if(!ready.value||busy.value||saved.value===signature())return
  busy.value=true
  try{apply(await api('/strm/settings','PUT',{address:address.value,format:format.value,overwriteMode:overwriteMode.value}));notify('STRM 配置已保存')}
  catch(e){notify(e.message,true)}finally{busy.value=false}
}
onMounted(async()=>{try{apply(await api('/strm/settings'));ready.value=true}catch(e){notify(e.message,true)}})
</script>
<template><section class="strm-settings settings-form"><div class="settings-section-title"><Icon name="FileSymlink" /><h2>STRM 配置</h2></div><label>STRM IP 地址<input v-model="address" aria-label="STRM IP 地址" type="url" :disabled="busy||!ready" @blur="save" @keydown.enter.prevent="save" /></label><div class="field"><label>115 STRM 格式</label><RoundedSelect v-model="format" label="115 STRM 格式" :disabled="busy||!ready" :options="[{value:'pickcode_name',label:'pickcode_name'},{value:'pickcode',label:'pickcode'}]" @update:model-value="save" /></div><div class="field strm-overwrite-field"><label>STRM覆盖模式</label><RoundedSelect v-model="overwriteMode" label="STRM覆盖模式" :disabled="busy||!ready" :options="[{value:'skip',label:'直接跳过'},{value:'overwrite',label:'覆盖生成'}]" @update:model-value="save" /></div></section></template>
<style scoped>.strm-settings{width:min(100%,380px);max-width:380px;padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}.strm-settings h2{font-size:16px}.strm-settings>.field{margin-top:20px}.strm-overwrite-field{display:grid;grid-template-columns:max-content minmax(0,1fr);align-items:center;gap:16px}.strm-overwrite-field>label{margin:0}</style>
