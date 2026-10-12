<script setup>
import { onMounted, ref } from 'vue'
import { api, notify } from '../lib'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
const address=ref(''),format=ref('pickcode_name'),busy=ref(false),ready=ref(false),error=ref(''),saved=ref('')
async function save(){if(!ready.value||busy.value||saved.value===JSON.stringify([address.value,format.value]))return;busy.value=true;error.value='';try{const value=await api('/strm/settings','PUT',{address:address.value,format:format.value});address.value=value.address;format.value=value.format;saved.value=JSON.stringify([address.value,format.value]);notify('STRM 配置已保存')}catch(e){error.value=e.message}finally{busy.value=false}}
onMounted(async()=>{try{const value=await api('/strm/settings');address.value=value.address;format.value=value.format;saved.value=JSON.stringify([address.value,format.value]);ready.value=true}catch(e){error.value=e.message}})
</script>
<template><section class="strm-settings settings-form"><div class="settings-section-title"><Icon name="FileSymlink" /><h2>STRM 配置</h2></div><label>STRM IP 地址<input v-model="address" aria-label="STRM IP 地址" type="url" :disabled="busy||!ready" @blur="save" @keydown.enter.prevent="save" /></label><div class="field"><label>115 STRM 格式</label><RoundedSelect v-model="format" label="115 STRM 格式" :disabled="busy||!ready" :options="[{value:'pickcode_name',label:'pickcode_name'},{value:'pickcode',label:'pickcode'}]" @update:model-value="save" /></div><p v-if="error" class="error-message" role="alert">{{error}}</p></section></template>
<style scoped>.strm-settings{padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}.strm-settings h2{font-size:16px}.strm-settings>.field{margin-top:20px}</style>
