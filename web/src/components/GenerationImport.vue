<script setup>
import { computed, ref, onUnmounted } from 'vue'
import { api, bytes, notify, state } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import LocalDirectoryPicker from './LocalDirectoryPicker.vue'
const props=defineProps({kind:String})
const emit=defineEmits(['close','queued'])
const targetKey=`aether-generation-target:${state.username}:${props.kind}`
function previousTarget(){
  try{return localStorage.getItem(targetKey)?.trim()||state.strmRoot||''}catch{return state.strmRoot||''}
}
const mode=ref('text'),text=ref(''),files=ref([]),target=ref(previousTarget()),picker=ref(false),input=ref(null),step=ref(1),busy=ref(false),error=ref(''),preview=ref(null),signature=ref('')
const title=computed(()=>props.kind.toUpperCase())
let alive=true
onUnmounted(()=>{alive=false})
const currentSignature=()=>JSON.stringify([mode.value,text.value,files.value.map(f=>[f.name,f.size,f.lastModified])])
async function parse(){
  busy.value=true;error.value=''
  try{
    const key=currentSignature()
    if(preview.value&&signature.value===key){step.value=2;return}
    let value
    if(mode.value==='text')value=await api('/generation/preview','POST',{kind:props.kind,text:text.value})
    else{
      const body=new FormData();body.set('kind',props.kind);for(const f of files.value)body.append('files',f)
      const response=await fetch('/api/generation/preview',{method:'POST',credentials:'same-origin',body});value=await response.json();if(!response.ok)throw new Error(value.error||'解析失败')
    }
    if(alive){preview.value=value;signature.value=key;step.value=2}
  }catch(e){if(alive)error.value=e.message}finally{if(alive)busy.value=false}
}
async function queue(){busy.value=true;error.value='';try{await api('/generation/jobs','POST',{preview:preview.value.preview,target:target.value});if(alive){try{localStorage.setItem(targetKey,target.value.trim())}catch{}notify('已加入生成队列');emit('queued');emit('close')}}catch(e){if(alive){error.value=e.message;if(/过期/.test(e.message))signature.value=''}}finally{if(alive)busy.value=false}}
</script>
<template>
  <Modal :title="`添加 ${title} 任务`" standard @close="!busy&&emit('close')">
    <form @submit.prevent="step===1?parse():queue()"><div class="modal-body generation-import">
      <template v-if="step===1">
        <div class="segmented" role="tablist" aria-label="解析方式"><button v-for="[value,label] in [['text','文本解析'],['files','文件解析']]" :key="value" type="button" role="tab" :aria-selected="mode===value" :class="{active:mode===value}" :disabled="busy" @click="mode=value">{{label}}</button></div>
        <label v-if="mode==='text'">{{title}} 信息<textarea v-model="text" :aria-label="`${title} 信息`" rows="7" :disabled="busy" /></label>
        <div v-else><input ref="input" hidden type="file" multiple :accept="kind==='cas'?'.txt,.cas,.zip,.rar,.7z':'.txt,.ed2k,.zip,.rar,.7z'" @change="files=[...$event.target.files]" /><button type="button" class="offline-file-picker" :disabled="busy" @click="input.click()"><Icon name="FolderOpen" />{{files.length?`已选择 ${files.length} 个文件`:'选择解析文件'}}</button><ul v-if="files.length" class="generation-files"><li v-for="(f,i) in files" :key="i">{{f.name}} <small>{{bytes(f.size)}}</small></li></ul></div>
        <div class="field"><label for="generation-target">生成目录</label><div class="directory-input"><input id="generation-target" v-model="target" :disabled="busy" required /><button type="button" class="icon-btn" aria-label="选择生成目录" :disabled="busy" @click="picker=true"><Icon name="FolderOpen" /></button></div></div>
      </template>
      <template v-else><div class="generation-preview" aria-label="生成预览"><article v-for="(item,i) in preview.items" :key="i" :class="{invalid:item.error}"><Icon :name="item.error?'CircleAlert':'FileSymlink'" /><div><strong>{{item.path}}</strong><small>{{item.error||bytes(item.size)}}</small></div></article></div><p v-if="!preview.valid" class="error-message" role="alert">含无效内容，请返回上一步修正后重新解析。</p></template>
      <p v-if="error" class="error-message" role="alert">{{error}}</p>
    </div><footer class="modal-footer generation-footer"><button type="button" class="btn" :disabled="busy" @click="step===2?step=1:emit('close')"><Icon :name="step===2?'ArrowLeft':'X'" />{{step===2?'上一步':'取消'}}</button><button class="btn primary" :disabled="busy||!target||(step===1?(mode==='text'?!text.trim():!files.length):!preview?.valid)"><Icon :name="step===1?'ScanSearch':'ListTodo'" />{{busy?'处理中…':step===1?'解析':'加入生成队列'}}</button></footer></form>
  </Modal>
  <LocalDirectoryPicker v-if="picker" :initial="target" @select="target=$event;picker=false" @close="picker=false" />
</template>
<style scoped>
.generation-footer{justify-content:space-between}
.generation-import{display:grid;gap:18px;min-width:0}.segmented button{font-size:15px;font-weight:600}.generation-import textarea{resize:vertical;min-height:120px}.generation-preview{max-height:55dvh;overflow:auto;scrollbar-width:thin}.generation-preview article{display:flex;gap:10px;padding:12px;border:1px solid var(--border);background:var(--input);border-radius:8px;margin-bottom:8px}.generation-preview article>div{display:grid;gap:5px;min-width:0}.generation-preview strong{font-size:14px;overflow-wrap:anywhere}.generation-preview small,.generation-files small{color:var(--muted);font-size:12px}.generation-preview .invalid{border-color:var(--red);color:var(--red)}.generation-files{margin:10px 0 0;padding-left:20px;font-size:13px}
</style>
