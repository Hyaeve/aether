<script setup>
import {nextTick,reactive,ref} from 'vue'
import {CodeXml,File,FileText,FolderTree,Route,Weight} from 'lucide-vue-next'
import Icon from './Icon.vue'
import Modal from './Modal.vue'
import ExtensionPresets from './ExtensionPresets.vue'
import {notify} from '../lib'
import {extensionTokens,invalidExtensionTokens} from '../extension-rules'
const props=defineProps({form:Object})
const form=props.form, error=ref(''), filterPicker=ref(false), tokenDrafts=reactive(new Map()), filterKeys=new WeakMap(), root=ref(null)
let nextFilterKey=0
const filterKey=filter=>{if(!filterKeys.has(filter))filterKeys.set(filter,++nextFilterKey);return filterKeys.get(filter)}
const filterTypes = [{value:'extension',label:'扩展名',icon:FileText},{value:'name',label:'文件名',icon:File},{value:'regex',label:'正则',icon:CodeXml},{value:'path_prefix',label:'路径前缀',icon:FolderTree},{value:'path_regex',label:'路径正则',icon:Route},{value:'size',label:'最大最小',icon:Weight}]

const filterType = filter => filterTypes.find(t=>t.value===filter.type) || filterTypes[1]
const sizeUnits = ['KB','MB','GB']
function cycleUnit(filter, key) { filter[key] = sizeUnits[(sizeUnits.indexOf(filter[key] || filter.unit || 'MB') + 1) % sizeUnits.length] }
async function addFilter(type) {
  form.filters.push({type,mode:type==='extension'||type==='size'?'include':'exclude',value:'',matchFile:true,matchDir:['path_prefix','path_regex'].includes(type),minSize:0,maxSize:0,unit:'MB',minUnit:'MB',maxUnit:'MB'})
  filterPicker.value=false
  await nextTick()
  root.value?.querySelector('.backup-filter:last-of-type input:not([type=radio]):not([type=checkbox])')?.focus()
}
const tokens = filter => (filter.value || '').split(';').map(value=>value.trim()).filter(Boolean)
function addToken(filter) {
  const draft = tokenDrafts.get(filter) || ''
  let values = filter.type==='extension'?extensionTokens(draft):draft.split(';').map(value=>value.trim()).filter(Boolean)
  if(filter.type==='extension')values=values.map(value=>value.replace(/^\./,'')).filter(Boolean).map(value=>form.caseSensitive?value:value.toLowerCase())
  const invalid=filter.type==='extension'?invalidExtensionTokens(values.join(';')):[]
  values=values.filter(value=>!invalid.includes(value))
  const all=[...tokens(filter),...values], seen=new Set()
  const unique=all.filter(value=>{const key=form.caseSensitive?value:value.toLowerCase();if(seen.has(key))return false;seen.add(key);return true})
  if(new TextEncoder().encode(unique.join(';')).length>2048){error.value='筛选匹配内容最多2048字节';return false}
  filter.value=unique.join(';');tokenDrafts.delete(filter);error.value=''
  if(invalid.length)notify(`未添加不合规范的扩展名：${invalid.join('、')}。仅允许英文字母、数字、下划线和连字符`,true)
  return true
}

function removeToken(filter,index) { const values=tokens(filter);values.splice(index,1);filter.value=values.join(';') }
function setScope(filter,key,event) {
  filter[key]=event.target.checked
  if(!filter.matchFile && !filter.matchDir)filter[key]=true
  event.target.checked=filter[key]
}

function flush(){for(const filter of form.filters){if(filter.type==='extension'&&invalidExtensionTokens(filter.value).length){const invalid=invalidExtensionTokens(filter.value);const message=`扩展名不合规范：${invalid.join('、')}。仅允许英文字母、数字、下划线和连字符`;error.value=message;notify(message,true);return false}if(['extension','name'].includes(filter.type)&&!addToken(filter))return false;if(filter.type!=='size'&&!filter.value.trim()){error.value='请填写筛选规则的匹配内容';return false}if(filter.type==='size'&&['minSize','maxSize'].some(key=>!Number.isSafeInteger(filter[key])||filter[key]<0)){error.value='大小仅允许0或正整数';return false}}return true}
defineExpose({flush})
</script>
<template>
      <div ref="root" class="backup-filter-frame">
      <article v-for="(filter,index) in form.filters" :key="filterKey(filter)" class="backup-filter">
        <header><strong><component :is="filterType(filter).icon" :size="16" aria-hidden="true" /><span>{{index+1}}. {{filterType(filter).label}}</span></strong><div class="backup-filter-controls"><label><input v-model="filter.mode" type="radio" :name="'backup-filter-mode-'+index" value="exclude" :aria-label="'黑名单 '+(index+1)" />黑名单</label><label><input v-model="filter.mode" type="radio" :name="'backup-filter-mode-'+index" value="include" :aria-label="'白名单 '+(index+1)" />白名单</label><template v-if="!['path_prefix','path_regex'].includes(filter.type)"><label><input :checked="filter.matchDir" type="checkbox" :disabled="filter.type==='extension'||filter.type==='size'" :aria-label="'文件夹 '+(index+1)" @change="setScope(filter,'matchDir',$event)" />文件夹</label><label><input :checked="filter.matchFile" type="checkbox" :aria-label="'文件 '+(index+1)" @change="setScope(filter,'matchFile',$event)" />文件</label></template><button type="button" class="icon-btn danger-text" :aria-label="'删除筛选规则 '+(index+1)" @click="tokenDrafts.delete(filter);form.filters.splice(index,1)"><Icon name="Trash2" :size="16" /></button></div></header>
        <div v-if="filter.type==='extension'||filter.type==='name'" class="backup-token-editor"><div v-if="tokens(filter).length" class="backup-tokens"><span v-for="(token,tokenIndex) in tokens(filter)" :key="token" class="backup-token"><span>{{token}}</span><button type="button" :aria-label="'删除 '+token+' 规则 '+(index+1)" @click="removeToken(filter,tokenIndex)"><Icon name="X" :size="14" /></button></span></div><div class="backup-extension-input-row"><div class="backup-token-input"><input :value="tokenDrafts.get(filter) || ''" :aria-label="'匹配内容 '+(index+1)" :placeholder="filter.type==='extension'?'添加扩展名':'添加文件名'" maxlength="2048" @input="tokenDrafts.set(filter,$event.target.value)" @keydown.enter.prevent="!$event.isComposing && addToken(filter)" @blur="addToken(filter)" /></div><ExtensionPresets v-if="filter.type==='extension'" v-model="filter.value" class="backup-filter-presets" :groups="['video','audio','image','data','document']" /></div></div>
        <textarea v-else-if="filter.type==='path_prefix'" v-model="filter.value" class="backup-path-prefix" rows="3" :aria-label="'匹配内容 '+(index+1)" maxlength="2048" />
        <div v-else-if="filter.type!=='size'"><input v-model="filter.value" :aria-label="'匹配内容 '+(index+1)" maxlength="2048" /></div>
        <div v-else class="backup-size"><label v-for="bound in ['min','max']" :key="bound">{{bound==='min'?'最小值':'最大值'}}<span class="backup-size-input"><input v-model.number="filter[bound+'Size']" type="number" min="0" step="1" required :aria-label="(bound==='min'?'最小值 ':'最大值 ')+(index+1)" @keydown="['e','E','-','+','.'].includes($event.key) && $event.preventDefault()" /><button type="button" :aria-label="(bound==='min'?'最小值单位 ':'最大值单位 ')+(index+1)" @click="cycleUnit(filter,bound+'Unit')">{{filter[bound+'Unit'] || filter.unit || 'MB'}}</button></span></label></div>
      </article>
      <button class="btn backup-add-filter" type="button" :disabled="form.filters.length>=32" @click="filterPicker=true"><Icon name="Plus" />添加规则</button>
      </div>
<p v-if="error" class="error-message" role="alert">{{error}}</p>
  <Modal v-if="filterPicker" compact title="选择规则类型" @close="filterPicker=false"><div class="backup-filter-types modal-body"><button v-for="type in filterTypes" :key="type.value" type="button" @click="addFilter(type.value)"><component :is="type.icon" :size="20" aria-hidden="true" /><span>{{type.label}}</span></button></div></Modal>

</template>
<style scoped>
.backup-filter{padding:12px;border:1px solid var(--border);border-radius:8px;display:grid;gap:10px;min-width:0;background:var(--layer-item)}.backup-filter header{display:flex;align-items:center;justify-content:space-between}.backup-filter label{font-size:14px}.backup-size{display:grid;gap:8px;min-width:0}.backup-add-filter{width:100%;font-size:14px;height:36px}
.backup-filter-frame { display:grid;gap:14px;padding:14px;border:1px solid var(--border);border-radius:8px;background:var(--layer-frame); }
.backup-filter { background:var(--layer-item); }
.backup-filter header { flex-wrap:wrap; gap:8px; padding-bottom:10px; border-bottom:1px solid var(--border); }
.backup-filter header strong { display:flex; align-items:center; gap:6px; font-size:14px; font-weight:600; }
.backup-filter-controls { display:flex; align-items:center; flex-wrap:wrap; gap:8px; margin-left:auto; }
.backup-filter-controls label,.backup-editor .backup-case-sensitive { display:flex; flex-direction:row; align-items:center; gap:4px; white-space:nowrap; }
.backup-filter-controls input[type=radio],.backup-filter-controls input[type=checkbox],.backup-editor .backup-case-sensitive input { width:16px; height:16px; min-height:16px; margin:0; accent-color:var(--primary); }
.backup-filter-controls .icon-btn { width:30px; height:30px; min-height:30px; }
.backup-token-editor { display:grid; gap:10px; min-width:0; }
.backup-tokens { display:flex; flex-wrap:wrap; gap:6px; min-width:0; }
.backup-token { position:relative; display:inline-flex; align-items:center; max-width:100%; padding:4px 7px; border-radius:6px; background:var(--input); font-size:14px; }
.backup-token > span { min-width:0; overflow-wrap:anywhere; }
.backup-token button { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; padding:0; border:0; border-radius:inherit; background:#0006; color:#fff; opacity:0; transition:opacity .15s; }
.backup-token:hover button,.backup-token:focus-within button { opacity:1; }
.backup-token button :deep(svg) { width:11px; height:11px; }
.backup-token-input { width:50%; min-width:0; }
.backup-extension-input-row{display:flex;align-items:center;gap:8px;min-width:0}.backup-extension-input-row .backup-token-input{flex:0 1 50%;width:50%}.backup-filter-presets{display:flex;align-items:center;gap:3px;flex:none;margin-left:auto}.backup-filter-presets .icon-btn{width:30px;height:30px;min-height:30px;padding:0}.backup-filter-presets .extension-preset{color:var(--muted)}
.backup-token-input input { width:100%; min-width:0; }
.backup-filter-types { display:grid; gap:4px; }
.backup-filter-types button { display:flex; align-items:center; gap:12px; min-height:44px; padding:10px 12px; border:0; border-radius:6px; background:none; color:var(--text); text-align:left; font-size:14px; }
.backup-filter-types button:hover,.backup-filter-types button:focus-visible { background:var(--primary-soft); color:var(--primary); }
.backup-filter-types svg,.backup-filter header strong svg { flex:none; }
.backup-size { grid-template-columns:repeat(2,minmax(0,1fr)); }.backup-size>*:last-child { grid-column:auto; }
.backup-size-input { display:block; position:relative; min-width:0; }.backup-size-input input { width:100%; padding-right:44px; appearance:textfield; }.backup-size-input input::-webkit-inner-spin-button { appearance:none; }.backup-size-input button { position:absolute; inset:0 0 0 auto; border:0; background:transparent; color:var(--muted); width:42px; font-size:14px; }.backup-size-input button:hover { color:var(--primary); }
.backup-path-prefix { resize:vertical; height:78px; min-height:78px; line-height:22px; padding:5px 9px; font-size:14px; overflow:auto; }
.backup-filter > div > input { width:100%; }
.backup-editor .backup-case-sensitive { gap:10px; }
.backup-editor .backup-case-sensitive input.switch { width:32px; height:18px; min-height:18px; }
@media(max-width:600px){.backup-filter header strong{width:100%}.backup-filter-controls{margin-left:0;gap:7px}.backup-filter-controls label{font-size:13px}.backup-filter-controls .icon-btn{margin-left:auto}.backup-token-input{width:50%}}
@media(prefers-reduced-motion:reduce){.backup-token button{transition:none}}

.backup-filter :is(input:not([type=radio]):not([type=checkbox])){height:var(--control-height);min-height:var(--control-height)}
</style>
<style scoped>
.backup-filter-frame{background:var(--layer-item)}.backup-filter{background:var(--layer-rule)}.backup-filter :is(input:not([type=radio]):not([type=checkbox]),textarea){background:var(--input)}.backup-token{background:var(--token-background)}
</style>
