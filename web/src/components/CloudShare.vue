<script setup>
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,notify,state} from '../lib'
import {copyOwnShare,rememberOwnShare} from '../own-shares'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import NumberInput from './NumberInput.vue'
import {cloudListings} from '../cloud-session-cache'
const props=defineProps({storage:String,parent:String,files:Array,editing:Object})
const emit=defineEmits(['close','changed'])
const pool=computed(()=>state.storages.find(s=>s.id===props.storage)),kind=computed(()=>pool.value?.type)
const options=ref({days:kind.value==='115'?15:0,codeMode:'random',code:'',limit:0,autofill:kind.value==='115',encrypted:false}),result=ref(null),busy=ref(false),ready=ref(false),error=ref('')
const periods=computed(()=>(kind.value==='115'?[15,0,1,3,5,7]:[0,1,7,30]).map(value=>({value,label:value?`${value}天`:'长期'})))
let alive=true
const owner=state.username
const active=()=>alive&&state.authenticated&&state.username===owner
onBeforeUnmount(()=>{alive=false})
onMounted(async()=>{try{const saved=await api(`/cloud/preferences?storage=${encodeURIComponent(props.storage)}`);if(active()){options.value={...saved,code:''};ready.value=true}}catch(e){if(active())error.value=e.message}})
async function submit(){if(busy.value||!ready.value||!active())return;busy.value=true;error.value='';let writing=false;try{await api(`/cloud/preferences?storage=${encodeURIComponent(props.storage)}`,'PUT',options.value);if(!active())return;writing=true;if(props.editing){await api(`/cloud/manage?storage=${encodeURIComponent(props.storage)}&kind=shares`,'POST',{action:'edit',ids:[props.editing.id],options:options.value,confirm:true});cloudListings.clear();if(!active())return;emit('changed');emit('close');notify('分享设置已更新')}else{const created=await api('/cloud/shares','POST',{storageId:props.storage,parent:props.parent,ids:props.files.map(f=>f.id),options:options.value});cloudListings.clear();if(!active())return;result.value=created;rememberOwnShare(created.url);emit('changed')}}catch(e){if(writing)cloudListings.clear();if(active())error.value=e.message}finally{if(alive)busy.value=false}}
async function copy(content){try{await copyOwnShare(result.value,kind.value,content);notify('分享已复制')}catch{notify('浏览器未允许复制，请选中文本复制',true)}}
</script>
<template>
<Modal standard :title="result?'分享链接':editing?'编辑分享':'创建分享'" @close="!busy&&emit('close')"><form @submit.prevent="submit"><div class="modal-body cloud-share-form"><template v-if="result"><p>{{result.name}}</p><textarea readonly rows="3" aria-label="分享链接" :value="result.url" /><p v-if="result.code">访问码：{{result.code}}</p></template><template v-else><div class="cloud-share-files"><div v-for="file in files||[editing]" :key="file.id"><Icon :name="file.isDir?'Folder':'File'" />{{file.name}}</div></div><div class="cloud-share-pair"><label>有效期<RoundedSelect v-model="options.days" label="分享有效期" :options="periods" /></label><label v-if="kind==='115'">访问码<RoundedSelect v-model="options.codeMode" label="访问码模式" :options="[{value:'random',label:'随机访问码'},{value:'fixed',label:'固定访问码'}]" /></label><label v-else>分享方式<RoundedSelect v-model="options.encrypted" label="分享方式" :options="[{value:false,label:'公开链接'},{value:true,label:'加密链接'}]" /></label></div><label v-if="kind==='115'">接收次数限制<NumberInput v-model="options.limit" aria-label="接收次数限制" :min="0" :max="999999" /></label><label v-if="kind==='115'||options.encrypted" class="toggle-line"><span>自动填充{{kind==='115'?'访问码':'提取码'}}</span><input v-model="options.autofill" type="checkbox" role="switch" class="switch" /></label></template><p v-if="error" class="error-message" role="alert">{{error}}</p></div><footer class="modal-footer"><template v-if="result"><button type="button" class="btn" @click="copy(true)"><Icon name="Copy" />复制分享内容</button><button type="button" class="btn primary" @click="copy(false)"><Icon name="Link" />仅复制链接</button></template><template v-else><button type="button" class="btn" :disabled="busy" @click="emit('close')">取消</button><button class="btn primary" :disabled="busy||!ready"><Icon :name="busy?'LoaderCircle':'Share2'" />{{editing?'保存设置':'创建分享'}}</button></template></footer></form></Modal>
</template>
<style scoped>.cloud-share-form{display:grid;gap:16px}.cloud-share-pair{display:grid;grid-template-columns:1fr 1fr;gap:14px}.cloud-share-form label{display:grid;gap:8px}.cloud-share-files{max-height:150px;overflow:auto;scrollbar-width:none;background:var(--input);border:1px solid var(--border);border-radius:8px;padding:10px}.cloud-share-files>div{display:flex;align-items:center;gap:8px;overflow-wrap:anywhere}.cloud-share-files svg{width:18px;flex:none}.cloud-share-form .toggle-line{display:flex;justify-content:space-between}.cloud-share-form textarea{width:100%;resize:vertical;overflow-wrap:anywhere}</style>
