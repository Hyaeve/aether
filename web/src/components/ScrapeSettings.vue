<script setup>
import {onMounted,onUnmounted,reactive,ref} from 'vue'
import {api,notify} from '../lib'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
const settings=reactive({writeMode:'missing',episodes:true,fanart:false,actors:false,excluded:''})
const loading=ref(true),ready=ref(false),busy=ref(false),error=ref('')
let alive=true
async function load(){
  loading.value=true;error.value=''
  try{const value=await api('/strm-scrape/settings');if(alive){Object.assign(settings,value);ready.value=true}}
  catch(e){if(alive)error.value=e.message}
  finally{if(alive)loading.value=false}
}
async function save(){
  if(!ready.value||busy.value)return
  busy.value=true;error.value=''
  try{await api('/strm-scrape/settings','PUT',{...settings});if(alive)notify('刮削设置已保存')}
  catch(e){if(alive){error.value=e.message;notify(e.message,true)}}
  finally{if(alive)busy.value=false}
}
onMounted(load)
onUnmounted(()=>{alive=false})
</script>
<template>
 <section class="scrape-settings-panel" aria-label="刮削设置" :aria-busy="loading">
  <header class="settings-section-title"><Icon name="Settings2"/><h2>刮削设置</h2></header>
  <p v-if="loading" class="muted" role="status">正在读取刮削设置…</p>
  <p v-if="error" class="error-message" role="alert">{{error}} <button v-if="!ready&&!loading" type="button" class="text-btn" @click="load">重新加载</button></p>
  <form @submit.prevent="save">
   <fieldset :disabled="!ready||busy">
    <div class="field"><label>写入策略</label><RoundedSelect v-model="settings.writeMode" label="写入策略" :disabled="!ready||busy" :options="[{value:'missing',label:'仅补缺'},{value:'overwrite',label:'覆盖已有'}]"/></div>
    <label>排除目录<input v-model="settings.excluded" aria-label="排除目录" placeholder="英文分号分隔"/></label>
    <label class="switch-label">分集 NFO 与预览图<input v-model="settings.episodes" class="switch" type="checkbox" role="switch"/></label>
    <label class="switch-label">背景图<input v-model="settings.fanart" class="switch" type="checkbox" role="switch"/></label>
    <label class="switch-label">演员信息<input v-model="settings.actors" class="switch" type="checkbox" role="switch"/></label>
   </fieldset>
   <footer><button class="btn primary" :disabled="!ready||busy"><Icon :name="busy?'LoaderCircle':'Save'" :class="{spin:busy}"/>{{busy?'保存中…':'保存设置'}}</button></footer>
  </form>
 </section>
</template>
<style scoped>
.scrape-settings-panel{width:100%;max-width:640px;align-self:flex-start;padding:20px;background:var(--surface);border:1px solid var(--border);border-radius:12px;box-sizing:border-box}
.scrape-settings-panel h2{font-size:16px;margin:0}.scrape-settings-panel fieldset{display:grid;gap:20px;border:0;margin:20px 0 0;padding:0;min-width:0}.scrape-settings-panel fieldset>label{margin:0}.scrape-settings-panel .field{min-width:0}.scrape-settings-panel .switch-label{display:flex;flex-direction:row;align-items:center;justify-content:space-between;gap:16px;min-height:32px}.scrape-settings-panel footer{display:flex;justify-content:flex-end;margin-top:24px}.scrape-settings-panel .switch{flex:none}
@media(max-width:600px){.scrape-settings-panel{padding:16px}}
</style>
