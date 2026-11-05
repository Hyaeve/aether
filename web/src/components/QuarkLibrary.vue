<script setup>
import CronInput from './CronInput.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, notify, state } from '../lib'
import { extensionError } from '../extension-rules'
import { usePageSlide } from '../page-slide'
import { revealOptions } from '../expanded-options'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import Modal from './Modal.vue'
import LocalDirectoryPicker from './LocalDirectoryPicker.vue'
import ExtensionPresets from './ExtensionPresets.vue'
import NumberInput from './NumberInput.vue'
import QuarkLibraryOverview from './QuarkLibraryOverview.vue'
import RoundedSelect from './RoundedSelect.vue'
import ShareLibraryRows from './ShareLibraryRows.vue'
const props=defineProps({provider:{type:String,default:'quark'}})
const pan115=computed(()=>props.provider==='115')
const cas=computed(()=>props.provider==='cas')
const mobile=computed(()=>props.provider==='mobile'||cas.value)
const providerLabel=computed(()=>cas.value?'CAS':mobile.value?'移动':pan115.value?'115':'夸克')
const apiRoot=`/virtual-library/${props.provider}`
const route = useRoute()
const settingsPage = computed(() => route.path.endsWith('/settings'))
const { leaving, back } = usePageSlide(`/virtual/${props.provider}`)
const cards = ref([]), stats = ref({}), cfg = reactive({scrapeEnabled:false,storageIds:[],directory:'',invalidDays:15,retentionHours:12,rotationHours:24,updateDays:7,storageId:'',mediaExtensions:'',metadataExtensions:'',playbackMode:'share'})
const form = reactive({}), modal = ref(false), picker = ref(false), deleting = ref(null), loading = ref(false), saving = ref(false), running = ref('')
const settingsPicker = ref(false)
const confirmingSync = ref(null)
const originalForm=ref(null)
const originalCascade=ref('')
const generationFields=['storageId','url','directory','mediaExtensions','metadataExtensions','enabled','cascadeCleanup','scrapeEnabled','excludeDirs','excludeFiles','excludeTypes']
const generationChanged=computed(()=>!originalForm.value||generationFields.some(key=>form[key]!==originalForm.value[key]))
const more = ref(false)
const directoryError = ref('')
const duplicateDirectory = computed(()=>cards.value.some(card=>card.id!==form.id&&card.directory===form.directory&&card.directoryName?.toLocaleLowerCase()===form.directoryName?.trim().toLocaleLowerCase()))
const invalidDirectory = computed(()=>duplicateDirectory.value||!!directoryError.value)
watch(()=>[form.directory,form.directoryName],()=>{directoryError.value=''})
const accounts = computed(() => state.storages.filter(s => s.enabled && s.type === props.provider && (!mobile.value||s.config?.mode==='native')))
const firstAccount = computed(() => accounts.value[0]?.id)
const rotationEnabled=computed(()=>accounts.value.filter(s=>cfg.storageIds.includes(s.id)).length>1)
const backgroundBusy = computed(() => !!running.value || cards.value.some(card => card.result?.status === 'running'))
watch(firstAccount, id => { if(pan115.value){if(id&&!cfg.storageId)cfg.storageId=id;return}if(id&&!cfg.storageIds.includes(id))cfg.storageIds=[id,...cfg.storageIds] })
let active = true, timer
let resolveSequence = 0, resolvedName = '', resolvePending = null
watch(settingsPage,value=>{if(value){leaving.value=false;load()}})
async function load() {
  if (loading.value) return
  loading.value = true
  try {
    const value = await api(apiRoot)
    if (!active) return
    cards.value = value.cards || []
    stats.value = value.stats || {}
    Object.assign(cfg, value.settings)
    if(pan115.value&&!cfg.storageId)cfg.storageId=firstAccount.value||''
    if(cfg.playbackMode==='redirect')cfg.playbackMode='share'
    if(!pan115.value&&firstAccount.value&&!cfg.storageIds.includes(firstAccount.value))cfg.storageIds=[firstAccount.value,...cfg.storageIds]
  } catch(e) { if(active) notify(e.message,true) } finally { if(active) loading.value=false }
}
function open(card) {
  directoryError.value=''
  resolveSequence++;resolvedName='';resolvePending=null
  more.value=false
  Object.assign(form,{id:'',storageId:cfg.storageId||firstAccount.value||'',url:'',name:'',cron:'',directory:cfg.directory||'',directoryName:'',mediaExtensions:'',metadataExtensions:'',enabled:!!cfg.directory},
    card ? Object.fromEntries(['id','storageId','url','name','cron','directory','directoryName','mediaExtensions','metadataExtensions','enabled'].map(k=>[k,card[k]])) : {},
    {cascadeCleanup:card?.cascadeCleanup||(mobile.value||pan115.value?'off':'on'),scrapeEnabled:card?card.scrapeEnabled===true:cfg.scrapeEnabled===true,excludeDirs:card?.excludeDirs||'',excludeFiles:card?.excludeFiles||'',excludeTypes:card?.excludeTypes||''})
  originalCascade.value=form.cascadeCleanup
  form.cascadeCleanup=form.cascadeCleanup==='off'?'':form.cascadeCleanup==='on'?'jpg;jpeg;png;webp;nfo;srt;ass;ssa;sub;vtt':form.cascadeCleanup
  if(!form.directory)form.directory=cfg.directory||''
  originalForm.value=JSON.parse(JSON.stringify(form))
  modal.value=true
}
function resolveName() {
  if(!form.url.trim()||saving.value)return
  if(form.id&&form.url===originalForm.value?.url&&form.name&&form.directoryName)return
  if(resolvePending?.url===form.url)return resolvePending.promise
  const sequence=++resolveSequence,url=form.url
  const promise=(async()=>{try{
    const value=await api(apiRoot+'/resolve','POST',{url,...(pan115.value?{storageId:form.storageId}:{})})
    if(!active||!modal.value||sequence!==resolveSequence||url!==form.url||!value.name)return
    if(!form.name||form.name===resolvedName)form.name=value.name
    if(!form.directoryName||form.directoryName===resolvedName)form.directoryName=value.name
    resolvedName=value.name
  }catch(e){if(active&&modal.value&&sequence===resolveSequence&&!saving.value)notify(e.message,true)}
  finally{if(resolvePending?.sequence===sequence)resolvePending=null}})()
  resolvePending={url,sequence,promise}
  return promise
}
function validExtensions(value) {
  const error=extensionError('媒体扩展名',value.mediaExtensions)||extensionError('元数据扩展名',value.metadataExtensions)||extensionError('排除类型',value.excludeTypes||'')||(value.cascadeCleanup!==undefined?extensionError('级联删除',value.cascadeCleanup):'')
  if(error) notify(error,true)
  return !error
}
async function save(confirmed = false) {
  if(saving.value)return
  if(invalidDirectory.value){notify('保存目录名称已存在，请修改目录名称',true);return}
  if(!validExtensions(form))return
  if(form.id&&form.enabled&&!cas.value&&generationChanged.value&&confirmed!==true){confirmingSync.value={mode:'save'};return}
  saving.value=true
  try{
    if(resolvePending?.url===form.url)await resolvePending.promise
    if(!active)return
    const value=await api(`${apiRoot}${form.id?'/'+form.id:''}`,form.id?'PUT':'POST',{...Object.fromEntries(Object.entries(form).filter(([key])=>pan115.value||key!=='storageId')),cascadeCleanup:form.cascadeCleanup===originalForm.value?.cascadeCleanup?originalCascade.value:form.cascadeCleanup.trim()||'off'})
    if(active){modal.value=false;notify(value.generationError?`链接已保存，生成失败：${value.generationError}`:value.message||value.result?.message||`${providerLabel.value}${form.id?'链库已更新':'链接已添加'}`,!!value.generationError);await load()}
  }catch(e){if(active){if(e.field==='directoryName')directoryError.value=e.message;notify(e.message,true)}}finally{if(active)saving.value=false}
}
async function run(card, confirmed = false) {
  if(confirmed!==true){confirmingSync.value={mode:'run',card};return}
  running.value=card.id
  try{
    const result=await api(`${apiRoot}/${card.id}/run`,'POST')
    if(active)notify(`${card.name}：${result.message}`)
  }catch(e){if(active)notify(e.message,true)}finally{if(active){running.value='';await load()}}
}
function confirmSync(){const action=confirmingSync.value;confirmingSync.value=null;if(action.mode==='save')save(true);else run(action.card,true)}
async function remove() {
  saving.value=true
  try{await api(`${apiRoot}/${deleting.value.id}`,'DELETE');if(active){deleting.value=null;notify('链接记录已移除，生成文件保留');await load()}}
  catch(e){if(active)notify(e.message,true)}finally{if(active)saving.value=false}
}
async function saveSettings() {
  if(!validExtensions(cfg))return
  saving.value=true
  const payload=pan115.value?{scrapeEnabled:cfg.scrapeEnabled,storageId:cfg.storageId||firstAccount.value,directory:cfg.directory}:cas.value?{scrapeEnabled:cfg.scrapeEnabled,directory:cfg.directory,mediaExtensions:cfg.mediaExtensions,updateDays:cfg.updateDays}:mobile.value?Object.fromEntries(['scrapeEnabled','storageIds','directory','mediaExtensions','metadataExtensions','playbackMode','invalidDays','retentionHours','rotationHours','updateDays'].map(key=>[key,cfg[key]])):Object.fromEntries(Object.entries(cfg).filter(([key])=>!['playbackMode','storageId'].includes(key)))
  try{const value=await api(apiRoot+'/settings','PUT',payload);if(active){Object.assign(cfg,value);notify(`${providerLabel.value}链库设置已保存`)}}
  catch(e){if(active)notify(e.message,true)}finally{if(active)saving.value=false}
}
function toggle(id){if(id===firstAccount.value)return;cfg.storageIds=cfg.storageIds.includes(id)?cfg.storageIds.filter(v=>v!==id):[...cfg.storageIds,id]}
watch(()=>form.directory,value=>{if(!value?.trim())form.enabled=false})
onMounted(()=>{load();timer=setInterval(()=>{if(!modal.value&&!settingsPage.value&&!saving.value)load()},10000)})
onBeforeUnmount(()=>{active=false;clearInterval(timer)})
defineExpose({open})
</script>
<template>
  <div v-if="settingsPage" class="quark-library-settings page-slide" :class="{leaving}">
    <header class="task-heading jump-heading"><h2 class="module-title"><Icon v-if="cas" name="Box"/><ProviderIcon v-else :type="provider" small />{{providerLabel}}链库设置</h2><button class="btn" @click="back"><Icon name="ArrowLeft" />返回页面</button></header>
    <form class="quark-settings-form" @submit.prevent="saveSettings">
      <section v-if="!cas&&!pan115" class="quark-account-section" aria-label="链库存储池"><h3><Icon name="Database" />链库存储池</h3>
        <div class="quark-account-list"><article v-for="storage in accounts" :key="storage.id" class="quark-account" :class="{selected:cfg.storageIds.includes(storage.id)}"><button type="button" :aria-label="`绑定 ${storage.name}`" :aria-pressed="cfg.storageIds.includes(storage.id)" :disabled="saving||loading||storage.id===firstAccount" @click="toggle(storage.id)"><ProviderIcon :type="provider" /></button><div><strong>{{storage.name}}</strong><small>{{storage.usage?.username||storage.config?.username||`${providerLabel}云盘`}}</small></div></article></div>
        <div v-if="!accounts.length" class="small-empty quark-account-empty"><ProviderIcon :type="provider" small /><span>{{mobile?'暂无移动原生存储':'暂无夸克存储'}}</span></div>
        <div v-for="id in cfg.storageIds.filter(id=>!accounts.some(s=>s.id===id))" :key="id" class="quark-unavailable"><span>不可用的存储池</span><button type="button" class="icon-btn" aria-label="移除不可用的存储池" @click="toggle(id)"><Icon name="X" /></button></div>
      </section>
      <section v-else-if="pan115" class="quark-account-section" aria-label="115解析存储池"><h3><Icon name="Database"/>115解析存储池</h3><label>默认解析存储池<RoundedSelect v-model="cfg.storageId" label="默认解析存储池" :options="accounts.map(s=>({value:s.id,label:s.name}))"/></label><p class="muted">115分享按快照手动执行，不设置活链或更新周期。</p></section>
      <section v-else class="quark-account-section" aria-label="CAS生成说明"><h3><Icon name="Box"/>移动分享CAS</h3><p>仅接受移动云盘分享链接，生成“原文件名.扩展名.cas.strm”。已有文件直接跳过。</p><p>分享没有返回SHA256时，会临时转存到CAS绑定的移动存储，取得特征并确认生成后清理本次副本。结果未确认时保留回执，不重复转存。</p><RouterLink class="btn" to="/tasks/cas/pool"><Icon name="Database"/>CAS存储绑定池</RouterLink></section>
      <section class="quark-settings-fields" aria-label="链库生成设置"><h3><Icon name="Settings2" />链库设置</h3><div class="form-grid">
        <label :class="{full:mobile}">生成位置<div class="quark-directory-field"><input v-model="cfg.directory" aria-label="默认生成位置" :disabled="saving||loading" /><button type="button" class="icon-btn" aria-label="选择默认生成位置" :disabled="saving||loading" @click="settingsPicker=true"><Icon name="FolderOpen" /></button></div></label>
        <label v-if="mobile&&!cas">播放模式<RoundedSelect v-model="cfg.playbackMode" label="播放模式" :options="[{value:'relay',label:'以太中继'},{value:'share',label:'分享直链'},{value:'transfer',label:'转存直链'}]" /></label>
        <label v-if="!cas&&!pan115" class="library-rotation-field">轮换时间<NumberInput v-model="cfg.rotationHours" aria-label="轮换时间" unit="h" :min="1" :max="8760" :disabled="!rotationEnabled||saving||loading" required /></label>
        <label v-if="!cas&&!pan115"><span class="library-time-title">失效链接保留时间</span><NumberInput v-model="cfg.invalidDays" aria-label="失效链接保留时间" unit="天" :min="1" :max="3650" required /></label>
        <label v-if="!cas&&!pan115"><span class="library-time-title">临时转存保活期</span><NumberInput v-model="cfg.retentionHours" aria-label="临时转存保活期" unit="h" :min="1" :max="8760" required /></label>
        <label v-if="!pan115">更新周期<NumberInput v-model="cfg.updateDays" aria-label="更新周期" unit="天" :min="1" :max="106751" required /></label>
        <label>链库刮削<RoundedSelect :model-value="cfg.scrapeEnabled?'include':'exclude'" label="默认链库刮削" :options="[{value:'exclude',label:'不参与'},{value:'include',label:'参与'}]" @update:model-value="cfg.scrapeEnabled=$event==='include'" /></label>
        <label v-if="!pan115" class="full"><span class="quark-extension-label">默认媒体扩展名<ExtensionPresets v-model="cfg.mediaExtensions" :groups="['video','audio']" /></span><input v-model="cfg.mediaExtensions" aria-label="默认媒体扩展名" /></label>
        <label v-if="!cas&&!pan115" class="full"><span class="quark-extension-label">默认元数据扩展名<ExtensionPresets v-model="cfg.metadataExtensions" :groups="['image','data']" /></span><input v-model="cfg.metadataExtensions" aria-label="默认元数据扩展名" /></label>
      </div><footer><button class="btn primary" :disabled="saving||loading"><Icon name="Save" />保存设置</button></footer></section>
    </form>
  </div>
  <template v-else>
    <QuarkLibraryOverview :provider="provider" :cards="cards" :stats="stats" :running="running" />
    <ShareLibraryRows :provider="provider" :cards="cards" :running="running" :busy="saving||backgroundBusy" :loading="loading" @edit="open" @remove="deleting=$event" @run="run" />
  </template>
  <Modal v-if="modal" standard compact :title="`${form.id?'编辑':'添加'}${providerLabel}链接`" @close="!saving&&(modal=false)">
    <form class="quark-library-form" @submit.prevent="save" @keydown.enter="['INPUT'].includes($event.target.tagName)&&$event.preventDefault()"><div class="modal-body quark-library-editor"><fieldset :disabled="saving"><div class="form-grid">
      <label>分享链接<input v-model="form.url" aria-label="分享链接" required autocomplete="off" @blur="resolveName" /></label>
      <label>分享名称<input v-model="form.name" aria-label="分享名称" maxlength="85" /></label>
      <label>生成位置<div class="quark-directory-field"><input v-model="form.directory" aria-label="生成位置" :required="form.enabled" /><button type="button" class="icon-btn" aria-label="选择生成位置" @click="picker=true"><Icon name="FolderOpen" /></button></div></label>
      <label>目录名称<input v-model="form.directoryName" aria-label="目录名称" maxlength="255" :aria-invalid="invalidDirectory||undefined" :aria-describedby="invalidDirectory?'library-directory-error':undefined" @blur="duplicateDirectory&&notify('保存目录名称已存在，请修改目录名称',true)" /><small v-if="invalidDirectory" id="library-directory-error" class="error-message">{{directoryError||'保存目录名称已存在，请修改目录名称'}}</small></label>
      <label v-if="!pan115">Cron表达式<CronInput v-model="form.cron" share-library aria-label="Cron表达式" /></label><label v-else>解析存储池<RoundedSelect v-model="form.storageId" label="解析存储池" :options="accounts.map(s=>({value:s.id,label:s.name}))"/></label>
      <label class="full"><span class="quark-extension-label">媒体扩展名<ExtensionPresets v-model="form.mediaExtensions" :groups="['video','audio']" /></span><input v-model="form.mediaExtensions" aria-label="媒体扩展名" /></label>
      <label v-if="!cas&&!pan115" class="full"><span class="quark-extension-label">元数据扩展名<ExtensionPresets v-model="form.metadataExtensions" :groups="['image','data']" /></span><input v-model="form.metadataExtensions" aria-label="元数据扩展名" /></label>
      <label class="switch-label full">启用链接<input v-model="form.enabled" class="switch" type="checkbox" role="switch" aria-label="启用链接" :disabled="!form.directory?.trim()" /></label>
    </div>
    <button type="button" class="disclosure" :aria-expanded="more" aria-controls="quark-library-more" @click="more=!more;more&&revealOptions($event.currentTarget)"><Icon :name="more?'ChevronDown':'ChevronRight'" />更多选项</button>
    <div v-if="more" id="quark-library-more" class="form-grid quark-library-more">
      <label>链库刮削<RoundedSelect :model-value="form.scrapeEnabled?'include':'exclude'" label="链库刮削" :options="[{value:'exclude',label:'不参与'},{value:'include',label:'参与'}]" @update:model-value="form.scrapeEnabled=$event==='include'" /></label>
      <label class="full cascade-delete-field"><span class="quark-extension-label">级联删除<ExtensionPresets v-model="form.cascadeCleanup" :groups="['image','data']" label-prefix="级联删除" /></span><input v-model="form.cascadeCleanup" aria-label="级联删除扩展名" placeholder="留空不启用，多个扩展名用英文分号隔开" /></label>
      <label class="full">排除目录<input v-model="form.excludeDirs" aria-label="排除目录" maxlength="4096" /></label>
      <label>排除文件<input v-model="form.excludeFiles" aria-label="排除文件" maxlength="4096" /></label>
      <label>排除类型<input v-model="form.excludeTypes" aria-label="排除类型" /></label>
    </div></fieldset></div><footer class="modal-footer"><button type="button" class="btn" :disabled="saving" @click="modal=false">取消</button><button class="btn primary" :disabled="saving||invalidDirectory"><Icon name="Save" />{{saving?'保存中':'保存链接'}}</button></footer></form>
  </Modal>
  <LocalDirectoryPicker v-if="picker" :initial="form.directory" @close="picker=false" @select="form.directory=$event;picker=false" />
  <LocalDirectoryPicker v-if="settingsPicker" :initial="cfg.directory" @close="settingsPicker=false" @select="cfg.directory=$event;settingsPicker=false" />
  <Modal v-if="deleting" compact confirmation :title="`移除${providerLabel}链接`" @close="!saving&&(deleting=null)"><div class="modal-body">确认移除「{{deleting.name}}」？已生成的本地文件保留<template v-if="!cas">，分享播放授权将失效</template>。</div><footer class="modal-footer"><button class="btn" :disabled="saving" @click="deleting=null">取消</button><button class="btn danger" :disabled="saving" @click="remove">确认移除</button></footer></Modal>
  <Modal v-if="confirmingSync" compact confirmation :title="`同步${providerLabel}分享`" @close="confirmingSync=null"><div class="modal-body"><template v-if="cas">将生成缺少的CAS文件，已有文件直接跳过。分享没有SHA256时，将临时转存到CAS绑定的移动存储；确认生成后清理本次副本，未知结果保留回执。</template><template v-else>同步生成目录，清理过期登记产物；保留未登记或已修改的文件。</template></div><footer class="modal-footer"><button class="btn" @click="confirmingSync=null">取消</button><button class="btn primary" @click="confirmSync">{{cas?'确认生成':'确认同步'}}</button></footer></Modal>
</template>
<style scoped>
.quark-settings-fields :deep(.rounded-select),.quark-settings-fields :deep(.rounded-select-trigger){min-width:0;width:100%}
@media(min-width:701px) and (max-width:1100px){.quark-library-settings .quark-settings-form{grid-template-columns:minmax(0,1fr)}}
.quark-settings-form>section{min-width:0;border:1px solid var(--border);border-radius:14px;padding:22px;background:var(--surface)}
.quark-library-editor input[aria-invalid=true]{border-color:var(--red);box-shadow:0 0 0 1px var(--red)}.quark-library-editor .error-message{margin:2px 0 0;font-size:12px}
:global(.modal:has(>.quark-library-form)){display:flex;flex-direction:column;overflow:hidden}
:global(.modal:has(>.quark-library-form)>.modal-header){flex:none}
.quark-library-form{display:flex;flex-direction:column;min-height:0;overflow:hidden}.quark-library-editor{min-height:0;overflow-y:auto;scrollbar-width:none;scroll-padding-block:18px;padding-bottom:24px}.quark-library-editor::-webkit-scrollbar{display:none}.quark-library-form>.modal-footer{flex:none;position:static}
.quark-library-settings{flex:1;min-width:0}.quark-library-settings .module-title{display:flex;align-items:center;gap:8px}.quark-settings-form{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:32px;padding:12px 0}.quark-settings-form h3{display:flex;align-items:center;gap:8px;font-size:15px;margin:0 0 20px}.quark-account-list{display:grid;gap:8px;grid-template-columns:repeat(2,minmax(0,1fr))}.quark-account{display:flex;align-items:center;gap:10px;border:1px solid var(--border);border-radius:8px;padding:12px;background:var(--surface);min-width:0;opacity:.55}.quark-account.selected{opacity:1;border-color:var(--primary)}.quark-account>button{background:none;border:0;padding:0;flex:none;cursor:pointer}.quark-account>div{display:grid;gap:5px;min-width:0}.quark-account strong,.quark-account small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.quark-account strong{font-size:14px}.quark-account small{font-size:12px;color:var(--muted)}.quark-unavailable{display:flex;align-items:center;gap:8px;color:var(--red);font-size:13px}.quark-settings-fields footer{display:flex;justify-content:flex-end;margin-top:20px}.quark-settings-fields input{width:100%}
.quark-library-editor fieldset{padding:0;margin:0;border:0;min-width:0}.quark-library-editor .form-grid{gap:14px 16px}.quark-library-editor label{min-width:0}.quark-directory-field{position:relative;min-width:0}.quark-directory-field input{width:100%;padding-right:40px;height:40px}.quark-directory-field .icon-btn{position:absolute;right:2px;top:2px;width:36px;height:36px}.quark-extension-label{display:flex;align-items:center;justify-content:space-between;gap:6px;min-height:28px}.quark-library-editor input:not(.switch){height:40px}.switch-label{display:flex!important;align-items:center;justify-content:space-between}.quark-library-editor .switch-label{margin-top:4px}
.quark-library-editor .form-grid>.full{grid-column:1/-1}.quark-library-editor .form-grid>label:not(.full){grid-column:auto}.quark-library-editor .switch-label{flex-direction:row}
.quark-account>button:disabled{cursor:default}.quark-account-empty{display:flex;align-items:center;gap:10px;padding:24px 0}.quark-settings-fields .full{grid-column:1/-1}
.quark-settings-fields .form-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.quark-settings-fields .form-grid>label:not(.full){grid-column:auto;min-width:0}.library-rotation-field:has(input:disabled){opacity:.55}.library-rotation-field :deep(input:disabled),.library-rotation-field :deep(button:disabled){cursor:not-allowed}
.quark-settings-fields :deep(.number-control){width:100%}
.quark-settings-fields :deep(.rounded-select-trigger){height:40px;min-height:40px}
@media(max-width:480px){.library-time-title{min-height:40px}}
@media(hover:none){.quark-settings-fields :deep(.number-control input){padding-right:66px}.quark-settings-fields :deep(.number-accessory){width:60px;justify-items:start;padding-left:4px}.quark-settings-fields :deep(.number-unit){opacity:1}.quark-settings-fields :deep(.number-steppers){left:26px}}
@media(max-width:1100px){.quark-settings-form{gap:20px}.quark-account-list{grid-template-columns:minmax(0,1fr)}}@media(max-width:700px){.quark-settings-form{grid-template-columns:minmax(0,1fr)}.quark-library-editor .form-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>
