<script setup>
import CronInput from './CronInput.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, date, notify, state } from '../lib'
import { extensionError } from '../extension-rules'
import { usePageSlide } from '../page-slide'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import Modal from './Modal.vue'
import LocalDirectoryPicker from './LocalDirectoryPicker.vue'
import ExtensionPresets from './ExtensionPresets.vue'
import NumberInput from './NumberInput.vue'
import QuarkLibraryOverview from './QuarkLibraryOverview.vue'
import RoundedSelect from './RoundedSelect.vue'
const route = useRoute()
const settingsPage = computed(() => route.path.endsWith('/settings'))
const { leaving, back } = usePageSlide('/virtual/quark')
const cards = ref([]), stats = ref({}), cfg = reactive({storageIds:[],directory:'',invalidDays:15,retentionHours:12,mediaExtensions:'',metadataExtensions:''})
const form = reactive({}), modal = ref(false), picker = ref(false), deleting = ref(null), loading = ref(false), saving = ref(false), running = ref('')
const settingsPicker = ref(false)
const confirmingSync = ref(null)
const accounts = computed(() => state.storages.filter(s => s.enabled && s.type === 'quark'))
const firstAccount = computed(() => accounts.value[0]?.id)
const backgroundBusy = computed(() => !!running.value || cards.value.some(card => card.result?.status === 'running'))
watch(firstAccount, id => { if(id&&!cfg.storageIds.includes(id)) cfg.storageIds=[id,...cfg.storageIds] })
let active = true, timer
let resolveSequence = 0, resolvedName = ''
watch(settingsPage,value=>{if(value){leaving.value=false;load()}})
async function load() {
  if (loading.value) return
  loading.value = true
  try {
    const value = await api('/virtual-library/quark')
    if (!active) return
    cards.value = value.cards || []
    stats.value = value.stats || {}
    Object.assign(cfg, value.settings)
    if(firstAccount.value&&!cfg.storageIds.includes(firstAccount.value))cfg.storageIds=[firstAccount.value,...cfg.storageIds]
  } catch(e) { if(active) notify(e.message,true) } finally { if(active) loading.value=false }
}
function open(card) {
  resolveSequence++;resolvedName=''
  Object.assign(form,{id:'',url:'',name:'',cron:'',directory:cfg.directory||'',directoryName:'',mediaExtensions:'',metadataExtensions:'',enabled:!!cfg.directory},
    card ? Object.fromEntries(['id','url','name','cron','directory','directoryName','mediaExtensions','metadataExtensions','enabled'].map(k=>[k,card[k]])) : {},
    {cascadeCleanup:card?.cascadeCleanup==='off'?'off':'on'})
  if(!form.directory)form.directory=cfg.directory||''
  modal.value=true
}
async function resolveName() {
  if(!form.url.trim()||saving.value)return
  const sequence=++resolveSequence,url=form.url
  try{
    const value=await api('/virtual-library/quark/resolve','POST',{url})
    if(!active||!modal.value||sequence!==resolveSequence||url!==form.url||!value.name)return
    if(!form.name||form.name===resolvedName)form.name=value.name
    if(!form.directoryName||form.directoryName===resolvedName)form.directoryName=value.name
    resolvedName=value.name
  }catch(e){if(active&&modal.value&&sequence===resolveSequence)notify(e.message,true)}
}
function validExtensions(value) {
  const error=extensionError('媒体扩展名',value.mediaExtensions)||extensionError('元数据扩展名',value.metadataExtensions)
  if(error) notify(error,true)
  return !error
}
async function save(confirmed = false) {
  if(!validExtensions(form))return
  if(form.id&&form.enabled&&confirmed!==true){confirmingSync.value={mode:'save'};return}
  saving.value=true
  try{
    const value=await api(`/virtual-library/quark${form.id?'/'+form.id:''}`,form.id?'PUT':'POST',{...form})
    if(active){modal.value=false;notify(value.generationError?`链接已保存，生成失败：${value.generationError}`:value.message||value.result?.message||(form.id?'夸克链库已更新':'夸克链接已添加'),!!value.generationError);await load()}
  }catch(e){if(active)notify(e.message,true)}finally{if(active)saving.value=false}
}
async function run(card, confirmed = false) {
  if(confirmed!==true){confirmingSync.value={mode:'run',card};return}
  running.value=card.id
  try{
    const result=await api(`/virtual-library/quark/${card.id}/run`,'POST')
    if(active)notify(`${card.name}：${result.message}`)
  }catch(e){if(active)notify(e.message,true)}finally{if(active){running.value='';await load()}}
}
function confirmSync(){const action=confirmingSync.value;confirmingSync.value=null;if(action.mode==='save')save(true);else run(action.card,true)}
async function remove() {
  saving.value=true
  try{await api(`/virtual-library/quark/${deleting.value.id}`,'DELETE');if(active){deleting.value=null;notify('链接卡片已移除，生成文件保留');await load()}}
  catch(e){if(active)notify(e.message,true)}finally{if(active)saving.value=false}
}
async function saveSettings() {
  if(!validExtensions(cfg))return
  saving.value=true
  try{const value=await api('/virtual-library/quark/settings','PUT',{...cfg});if(active){Object.assign(cfg,value);notify('夸克链库设置已保存')}}
  catch(e){if(active)notify(e.message,true)}finally{if(active)saving.value=false}
}
function toggle(id){if(id===firstAccount.value)return;cfg.storageIds=cfg.storageIds.includes(id)?cfg.storageIds.filter(v=>v!==id):[...cfg.storageIds,id]}
watch(()=>form.directory,value=>{if(!value?.trim())form.enabled=false})
function hasTime(value){return value&&!value.startsWith('0001')}
onMounted(()=>{load();timer=setInterval(()=>{if(!modal.value&&!settingsPage.value&&!saving.value)load()},10000)})
onBeforeUnmount(()=>{active=false;clearInterval(timer)})
defineExpose({open})
</script>
<template>
  <div v-if="settingsPage" class="quark-library-settings page-slide" :class="{leaving}">
    <header class="task-heading jump-heading"><h2 class="module-title"><ProviderIcon type="quark" small />夸克链库设置</h2><button class="btn" @click="back"><Icon name="ArrowLeft" />返回页面</button></header>
    <form class="quark-settings-form" @submit.prevent="saveSettings">
      <section class="quark-account-section" aria-label="链库存储池"><h3><Icon name="Database" />链库存储池</h3>
        <div class="quark-account-list"><article v-for="storage in accounts" :key="storage.id" class="quark-account" :class="{selected:cfg.storageIds.includes(storage.id)}"><button type="button" :aria-label="`绑定 ${storage.name}`" :aria-pressed="cfg.storageIds.includes(storage.id)" :disabled="saving||loading||storage.id===firstAccount" @click="toggle(storage.id)"><ProviderIcon type="quark" /></button><div><strong>{{storage.name}}</strong><small>{{storage.usage?.username||storage.config?.username||'夸克网盘'}}</small></div></article></div>
        <div v-if="!accounts.length" class="small-empty quark-account-empty"><ProviderIcon type="quark" small /><span>暂无夸克存储</span></div>
        <div v-for="id in cfg.storageIds.filter(id=>!accounts.some(s=>s.id===id))" :key="id" class="quark-unavailable"><span>不可用的存储池</span><button type="button" class="icon-btn" aria-label="移除不可用的存储池" @click="toggle(id)"><Icon name="X" /></button></div>
      </section>
      <section class="quark-settings-fields" aria-label="链库生成设置"><h3><Icon name="Settings2" />链库设置</h3><div class="form-grid">
        <label class="full">生成位置<div class="quark-directory-field"><input v-model="cfg.directory" aria-label="默认生成位置" :disabled="saving||loading" /><button type="button" class="icon-btn" aria-label="选择默认生成位置" :disabled="saving||loading" @click="settingsPicker=true"><Icon name="FolderOpen" /></button></div></label>
        <label>失效链接保留时间<NumberInput v-model="cfg.invalidDays" aria-label="失效链接保留时间" unit="天" :min="1" :max="3650" required /></label>
        <label>临时转存保活期<NumberInput v-model="cfg.retentionHours" aria-label="临时转存保活期" unit="h" :min="1" :max="8760" required /></label>
        <label class="full"><span class="quark-extension-label">默认媒体扩展名<ExtensionPresets v-model="cfg.mediaExtensions" :groups="['video','audio','comic']" /></span><input v-model="cfg.mediaExtensions" aria-label="默认媒体扩展名" /></label>
        <label class="full"><span class="quark-extension-label">默认元数据扩展名<ExtensionPresets v-model="cfg.metadataExtensions" :groups="['image','data']" /></span><input v-model="cfg.metadataExtensions" aria-label="默认元数据扩展名" /></label>
      </div><footer><button class="btn primary" :disabled="saving||loading"><Icon name="Save" />保存设置</button></footer></section>
    </form>
  </div>
  <template v-else>
    <QuarkLibraryOverview :cards="cards" :stats="stats" :running="running" />
    <div class="quark-library-grid" aria-label="夸克分享链接">
      <article v-for="card in cards" :key="card.id" class="quark-library-card" :class="{invalid:hasTime(card.result?.invalidSince),disabled:!card.enabled}" :aria-label="card.name">
        <header><ProviderIcon type="quark" small /><h3 :data-tooltip="card.name">{{card.name}}</h3><button class="icon-btn" :aria-label="`编辑 ${card.name}`" :disabled="saving||backgroundBusy" @click="open(card)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :aria-label="`移除 ${card.name}`" :disabled="saving||backgroundBusy" @click="deleting=card"><Icon name="Trash2" /></button></header>
        <div class="quark-card-directory" :data-tooltip="card.directory?`${card.directory}/${card.directoryName}`:'未设置生成位置'"><Icon name="FolderOpen" :size="17" /><span v-if="card.directory">{{card.directory}}<strong>/{{card.directoryName}}</strong></span><span v-else>未设置生成位置</span></div>
        <dl><div><dt><Icon name="Clock3" :size="14" />Cron</dt><dd>{{card.cron||'手动执行'}}</dd></div><div><dt>最近执行：</dt><dd>{{hasTime(card.result?.lastRun)?date(card.result.lastRun):'尚未执行'}}</dd></div></dl>
        <p class="quark-result" :class="card.result?.status" :data-tooltip="card.result?.message"><Icon :name="card.result?.status==='invalid'?'Link2Off':card.result?.status==='failed'?'CircleAlert':'FileSymlink'" :size="16" /><span>{{card.result?.message||'等待执行'}}</span></p>
        <footer><span class="quark-card-status" :class="card.result?.status">{{!card.enabled?(card.directory?'已停用':'未启用'):card.result?.status==='invalid'?'分享失效':card.result?.status==='running'||running===card.id?'执行中':card.result?.status==='failed'?'执行失败':'活链'}}</span><button class="btn" :disabled="!card.enabled||saving||backgroundBusy" @click="run(card)"><Icon :name="running===card.id||card.result?.status==='running'?'LoaderCircle':'Play'" />{{running===card.id||card.result?.status==='running'?'生成中':'立即生成'}}</button></footer>
      </article>
    </div>
    <div v-if="!cards.length&&!loading" class="small-empty quark-library-empty"><ProviderIcon type="quark" /><span>暂无分享链接</span></div>
  </template>
  <Modal v-if="modal" standard compact :title="form.id?'编辑夸克链接':'添加夸克链接'" @close="!saving&&(modal=false)">
    <form @submit.prevent="save" @keydown.enter="['INPUT'].includes($event.target.tagName)&&$event.preventDefault()"><div class="modal-body quark-library-editor"><fieldset :disabled="saving"><div class="form-grid">
      <label>分享链接<input v-model="form.url" aria-label="分享链接" required autocomplete="off" @blur="resolveName" /></label>
      <label>分享名称<input v-model="form.name" aria-label="分享名称" maxlength="85" /></label>
      <label>Cron表达式<CronInput v-model="form.cron" aria-label="Cron表达式" /></label>
      <label>级联清理<RoundedSelect v-model="form.cascadeCleanup" label="级联清理" hide-check :options="[{value:'on',label:'开启'},{value:'off',label:'关闭'}]" /></label>
      <label>生成位置<div class="quark-directory-field"><input v-model="form.directory" aria-label="生成位置" :required="form.enabled" /><button type="button" class="icon-btn" aria-label="选择生成位置" @click="picker=true"><Icon name="FolderOpen" /></button></div></label>
      <label>目录名称<input v-model="form.directoryName" aria-label="目录名称" maxlength="255" /></label>
      <label class="full"><span class="quark-extension-label">媒体扩展名<ExtensionPresets v-model="form.mediaExtensions" :groups="['video','audio','comic']" /></span><input v-model="form.mediaExtensions" aria-label="媒体扩展名" /></label>
      <label class="full"><span class="quark-extension-label">元数据扩展名<ExtensionPresets v-model="form.metadataExtensions" :groups="['image','data']" /></span><input v-model="form.metadataExtensions" aria-label="元数据扩展名" /></label>
      <label class="switch-label full">启用链接<input v-model="form.enabled" class="switch" type="checkbox" role="switch" aria-label="启用链接" :disabled="!form.directory?.trim()" /></label>
    </div></fieldset></div><footer class="modal-footer"><button type="button" class="btn" :disabled="saving" @click="modal=false">取消</button><button class="btn primary" :disabled="saving"><Icon name="Save" />{{saving?'保存中':'保存链接'}}</button></footer></form>
  </Modal>
  <LocalDirectoryPicker v-if="picker" :initial="form.directory" @close="picker=false" @select="form.directory=$event;picker=false" />
  <LocalDirectoryPicker v-if="settingsPicker" :initial="cfg.directory" @close="settingsPicker=false" @select="cfg.directory=$event;settingsPicker=false" />
  <Modal v-if="deleting" compact confirmation title="移除夸克链接" @close="!saving&&(deleting=null)"><div class="modal-body">确认移除「{{deleting.name}}」？已生成的本地文件保留，分享播放授权将失效。</div><footer class="modal-footer"><button class="btn" :disabled="saving" @click="deleting=null">取消</button><button class="btn danger" :disabled="saving" @click="remove">确认移除</button></footer></Modal>
  <Modal v-if="confirmingSync" compact confirmation title="同步夸克分享" @close="confirmingSync=null"><div class="modal-body">将按分享当前内容更新本卡片的生成目录，并删除分享中已不存在的登记产物。级联清理开启时，同时清理对应刮削文件及无 STRM 的子目录；连续失效达到保留期限后清理卡片及其生成目录。未登记或已修改的文件会保留。</div><footer class="modal-footer"><button class="btn" @click="confirmingSync=null">取消</button><button class="btn primary" @click="confirmSync">确认同步</button></footer></Modal>
</template>
<style scoped>
.quark-library-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}.quark-library-card{min-width:0;border:1px solid var(--border);border-radius:8px;padding:16px;background:var(--surface)}.quark-library-card.invalid{border-color:var(--red)}.quark-library-card.disabled{opacity:.65}
.quark-library-card header{display:flex;align-items:center;gap:8px;min-width:0;margin-bottom:14px}.quark-library-card h3{flex:1;min-width:0;font-size:15px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.quark-library-card header .icon-btn{width:28px;height:28px;flex:none}.quark-card-directory{display:flex;align-items:center;gap:8px;font-size:13px;color:var(--muted);min-width:0}.quark-card-directory>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.quark-card-directory strong{font-weight:500;color:var(--text)}.quark-library-card dl{display:grid;gap:9px;margin:15px 0;font-size:12px}.quark-library-card dl>div{display:flex;gap:8px;min-width:0}.quark-library-card dt{display:flex;align-items:center;gap:5px;color:var(--muted);flex:none}.quark-library-card dd{margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.quark-result{display:flex;align-items:center;gap:6px;font-size:12px;min-height:20px;margin:0 0 12px;color:var(--muted)}.quark-result span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.quark-result.invalid,.quark-result.failed,.quark-card-status.invalid,.quark-card-status.failed{color:var(--red)}.quark-library-card footer{display:flex;align-items:center;justify-content:space-between;gap:8px}.quark-card-status{font-size:12px;color:var(--green)}.quark-library-card footer .btn{height:36px;padding:0 10px;font-size:12px}
.quark-library-empty{display:flex;gap:12px;align-items:center;justify-content:center;min-height:260px}
.quark-library-settings{flex:1;min-width:0}.quark-library-settings .module-title{display:flex;align-items:center;gap:8px}.quark-settings-form{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:32px;padding:12px 0}.quark-settings-form h3{display:flex;align-items:center;gap:8px;font-size:15px;margin:0 0 20px}.quark-account-list{display:grid;gap:8px;grid-template-columns:repeat(2,minmax(0,1fr))}.quark-account{display:flex;align-items:center;gap:10px;border:1px solid var(--border);border-radius:8px;padding:12px;background:var(--surface);min-width:0;opacity:.55}.quark-account.selected{opacity:1;border-color:var(--primary)}.quark-account>button{background:none;border:0;padding:0;flex:none;cursor:pointer}.quark-account>div{display:grid;gap:5px;min-width:0}.quark-account strong,.quark-account small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.quark-account strong{font-size:14px}.quark-account small{font-size:12px;color:var(--muted)}.quark-unavailable{display:flex;align-items:center;gap:8px;color:var(--red);font-size:13px}.quark-settings-fields footer{display:flex;justify-content:flex-end;margin-top:20px}.quark-settings-fields input{width:100%}
.quark-library-editor fieldset{padding:0;margin:0;border:0;min-width:0}.quark-library-editor .form-grid{gap:14px 16px}.quark-library-editor label{min-width:0}.quark-directory-field{position:relative;min-width:0}.quark-directory-field input{width:100%;padding-right:40px;height:40px}.quark-directory-field .icon-btn{position:absolute;right:2px;top:2px;width:36px;height:36px}.quark-extension-label{display:flex;align-items:center;justify-content:space-between;gap:6px;min-height:28px}.quark-library-editor input:not(.switch){height:40px}.switch-label{display:flex!important;align-items:center;justify-content:space-between}.quark-library-editor .switch-label{margin-top:4px}
.quark-library-editor .form-grid>.full{grid-column:1/-1}.quark-library-editor .form-grid>label:not(.full){grid-column:auto}.quark-library-editor .switch-label{flex-direction:row}
.quark-account>button:disabled{cursor:default}.quark-account-empty{display:flex;align-items:center;gap:10px;padding:24px 0}.quark-settings-fields .full{grid-column:1/-1}
@media(max-width:1100px){.quark-library-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.quark-settings-form{gap:20px}.quark-account-list{grid-template-columns:minmax(0,1fr)}}@media(max-width:700px){.quark-library-grid,.quark-settings-form{grid-template-columns:minmax(0,1fr)}.quark-library-editor .form-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>
