<script setup>
import { computed, ref, onUnmounted } from 'vue'
import { api, bytes, notify, state } from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import LocalDirectoryPicker from './LocalDirectoryPicker.vue'
const props=defineProps({kind:String})
const emit=defineEmits(['close','queued'])
const targetKey=`aether-generation-target:${state.username}:${props.kind}`
function previousTarget(){
  try{return localStorage.getItem(targetKey)?.trim()||state.strmRoot||''}catch{return state.strmRoot||''}
}
const mode=ref('text'),text=ref(''),files=ref([]),target=ref(previousTarget()),picker=ref(false),input=ref(null),step=ref(1),busy=ref(false),error=ref(''),preview=ref(null),signature=ref('')
const links=ref('')
const confirm=ref(false)
const title=computed(()=>props.kind.toUpperCase())
let alive=true
onUnmounted(()=>{alive=false})
const currentSignature=()=>JSON.stringify([mode.value,text.value,links.value,files.value.map(f=>[f.name,f.size,f.lastModified])])
async function parse(){
  busy.value=true;error.value=''
  try{
    const key=currentSignature()
    if(preview.value&&signature.value===key){step.value=2;return}
    let value
    if(mode.value==='text'||mode.value==='links')value=await api('/generation/preview','POST',{kind:props.kind,text:mode.value==='links'?links.value:text.value,...(mode.value==='links'?{mode:'links'}:{})})
    else{
      const body=new FormData();body.set('kind',props.kind);for(const f of files.value)body.append('files',f)
      const response=await fetch('/api/generation/preview',{method:'POST',credentials:'same-origin',body});value=await response.json();if(!response.ok)throw new Error(value.error||'解析失败')
    }
    if(alive){preview.value=value;signature.value=key;step.value=2;if(!value.valid)notify(value.items?.find(item=>item.error)?.error||'含无效内容，请检查后重新解析',true)}
  }catch(e){if(alive){error.value=e.message;notify(e.message,true)}}finally{if(alive)busy.value=false}
}
function submit(){if(preview.value?.temporary)confirm.value=true;else queue()}
async function queue(){busy.value=true;error.value='';try{await api('/generation/jobs','POST',{preview:preview.value.preview,target:target.value,...(preview.value.temporary?{confirm:true}:{})});if(alive){try{localStorage.setItem(targetKey,target.value.trim())}catch{}notify('已加入生成队列');emit('queued');emit('close')}}catch(e){if(alive){error.value=e.message;notify(e.message,true);if(/过期|配置已变化|不可用/.test(e.message)){signature.value='';confirm.value=false}}}finally{if(alive)busy.value=false}}
</script>
<template>
  <Modal :title="`添加 ${title} 任务`" standard @close="!busy&&emit('close')">
    <form @submit.prevent="step===1?parse():submit()"><div class="modal-body generation-import">
      <template v-if="step===1">
        <div class="segmented" role="tablist" aria-label="解析方式"><button v-for="[value,label] in [...[['text','文本解析'],['files','文件解析']],...(kind==='cas'?[['links','链接解析']]:[])]" :key="value" type="button" role="tab" :aria-selected="mode===value" :class="{active:mode===value}" :disabled="busy" @click="mode=value">{{label}}</button></div>
        <label v-if="mode==='text'">{{title}} 信息<textarea v-model="text" :aria-label="`${title} 信息`" rows="7" :disabled="busy" /></label>
        <label v-else-if="mode==='links'">移动分享链接<textarea v-model="links" aria-label="移动分享链接" rows="7" :disabled="busy" /></label>
        <div v-else><input ref="input" hidden type="file" multiple :accept="kind==='cas'?'.txt,.cas,.zip,.rar,.7z':'.txt,.ed2k,.zip,.rar,.7z'" @change="files=[...$event.target.files]" /><button type="button" class="offline-file-picker" :disabled="busy" @click="input.click()"><Icon name="FolderOpen" />{{files.length?`已选择 ${files.length} 个文件`:'选择解析文件'}}</button><ul v-if="files.length" class="generation-files"><li v-for="(f,i) in files" :key="i">{{f.name}} <small>{{bytes(f.size)}}</small></li></ul></div>
        <div class="field"><label for="generation-target">生成目录</label><div class="directory-input"><input id="generation-target" v-model="target" :disabled="busy" required /><button type="button" class="icon-btn" aria-label="选择生成目录" :disabled="busy" @click="picker=true"><Icon name="FolderOpen" /></button></div></div>
      </template>
      <template v-else><div class="generation-preview" aria-label="生成预览"><article v-for="(item,i) in preview.items" :key="i" :class="{invalid:item.error}"><Icon :name="item.error?'CircleAlert':'FileSymlink'" /><div><strong>{{item.path}}</strong><small>{{item.error||bytes(item.size)}}</small></div></article></div><p v-if="!preview.valid" class="error-message" role="alert">含无效内容，请返回上一步修正后重新解析。</p></template>
      <p v-if="error" class="error-message" role="alert">{{error}}</p>
    </div><footer class="modal-footer generation-footer"><button type="button" class="btn" :disabled="busy" @click="step===2?step=1:emit('close')"><Icon :name="step===2?'ArrowLeft':'X'" />{{step===2?'上一步':'取消'}}</button><button class="btn primary" :disabled="busy||!target||(step===1?(mode==='text'?!text.trim():mode==='links'?!links.trim():!files.length):!preview?.valid)"><Icon :name="step===1?'ScanSearch':'ListTodo'" />{{busy?'处理中…':step===1?'解析':'加入生成队列'}}</button></footer></form>
  </Modal>
  <LocalDirectoryPicker v-if="picker" :initial="target" @select="target=$event;picker=false" @close="picker=false" />
  <Modal v-if="confirm" compact confirmation title="确认临时转存生成" @close="!busy&&(confirm=false)">
    <div class="modal-body generation-consent"><p>其中 {{preview.temporary.count}} 个文件需要临时转存以读取 SHA256，不下载媒体内容。</p><dl><dt>移动存储池</dt><dd><ProviderIcon type="mobile" compact />{{preview.temporary.storageName}}</dd><dt>临时目录</dt><dd>{{preview.temporary.directory}}</dd><dt>生成目录</dt><dd>{{target}}</dd></dl><p>STRM 生成并校验成功后立即清理对应临时文件，按存储删除方式处理。失败或结果未确认的文件保留，远程空目录保留；不会删除分享者的源文件。</p></div>
    <footer class="modal-footer"><button class="btn" :disabled="busy" @click="confirm=false">取消</button><button class="btn primary" :disabled="busy" @click="queue"><Icon :name="busy?'LoaderCircle':'Check'" />{{busy?'提交中…':'确认生成'}}</button></footer>
  </Modal>
</template>
<style scoped>
.generation-footer{justify-content:space-between}
.generation-consent{overflow-wrap:anywhere}.generation-consent p{margin:0 0 16px}.generation-consent p:last-child{margin:16px 0 0}.generation-consent dl{display:grid;grid-template-columns:88px minmax(0,1fr);gap:12px;margin:0}.generation-consent dt{color:var(--muted)}.generation-consent dd{margin:0;display:flex;align-items:center;gap:8px;min-width:0}
.generation-import{display:grid;gap:18px;min-width:0}.segmented button{font-size:15px;font-weight:600}.generation-import textarea{resize:vertical;min-height:120px}.generation-preview{max-height:55dvh;overflow:auto;scrollbar-width:thin}.generation-preview article{display:flex;gap:10px;padding:12px;border:1px solid var(--border);background:var(--input);border-radius:8px;margin-bottom:8px}.generation-preview article>div{display:grid;gap:5px;min-width:0}.generation-preview strong{font-size:14px;overflow-wrap:anywhere}.generation-preview small,.generation-files small{color:var(--muted);font-size:12px}.generation-preview .invalid{border-color:var(--red);color:var(--red)}.generation-files{margin:10px 0 0;padding-left:20px;font-size:13px}
</style>
