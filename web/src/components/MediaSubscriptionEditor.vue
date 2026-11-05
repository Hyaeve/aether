<script setup>
import {computed,reactive,ref} from 'vue'
import {api,notify} from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import NumberInput from './NumberInput.vue'
const props=defineProps({media:Object,targets:{type:Array,default:()=>[]}}),emit=defineEmits(['close','saved'])
const tab=ref('basic'),busy=ref(false),error=ref('')
const initialSeason=props.media.kind==='tv'?(props.media.season??props.media.seasons?.find(s=>s.number>0)?.number??1):0
const form=reactive({season:initialSeason,keyword:'',totalEpisodes:props.media.seasons?.find(s=>s.number===initialSeason)?.episodes||0,startEpisode:1,quality:'',resolution:'',effect:'',sourceIds:[],targetId:'',searchHours:null,include:'',exclude:'',mediaCategory:'',customWords:'',...Object.fromEntries(['season','keyword','totalEpisodes','startEpisode','quality','resolution','effect','targetId','searchHours','include','exclude','mediaCategory','customWords'].filter(k=>props.media[k]!==undefined).map(k=>[k,props.media[k]]))})
const searchMode=computed({get:()=>form.searchHours==null?'system':'custom',set:value=>form.searchHours=value==='custom'?24:null})
const targetOptions=computed(()=>[{value:'',label:'暂不指定'},...props.targets.map(t=>({value:t.id,label:`${t.name} · ${t.path}`})),...(form.targetId&&!props.targets.some(t=>t.id===form.targetId)?[{value:form.targetId,label:'原转存目录已不可用，请重新选择'}]:[])])
const options=values=>[{value:'',label:'不限制'},...values.map(value=>({value,label:value}))]
async function save(){
 if(busy.value)return;busy.value=true;error.value=''
 try{const result=await api(`/media-subscriptions${props.media.key?'/'+props.media.key:''}`,props.media.key?'PUT':'POST',{id:props.media.id,kind:props.media.kind,...form});emit('saved',result);notify(props.media.key?'订阅已保存':'已添加订阅')}
 catch(e){error.value=e.message;notify(e.message,true)}finally{busy.value=false}
}
</script>
<template>
 <Modal :title="media.key?'编辑订阅':'添加订阅'" :eyebrow="`${media.title}${media.kind==='tv'?' · S'+String(form.season).padStart(2,'0'):''}`" wide @close="!busy&&emit('close')">
  <form class="subscription-editor" @submit.prevent="save">
   <div class="subscription-editor-tabs" role="tablist" aria-label="订阅编辑栏目"><button v-for="item in [['basic','基础'],['advanced','进阶']]" :key="item[0]" type="button" role="tab" :aria-selected="tab===item[0]" :class="{active:tab===item[0]}" @click="tab=item[0]">{{item[1]}}</button></div>
   <div class="modal-body"><fieldset :disabled="busy" class="subscription-fields">
    <div v-if="tab==='basic'" class="form-grid subscription-basic">
     <label :class="{full:media.kind==='movie'}">搜索关键词<input v-model="form.keyword" aria-label="搜索关键词" maxlength="300" :placeholder="media.title"/></label>
     <label v-if="media.kind==='tv'">总集数<NumberInput v-model="form.totalEpisodes" aria-label="总集数" min="0" max="100000" step="1" required/></label>
     <label v-if="media.kind==='tv'">开始集数<NumberInput v-model="form.startEpisode" aria-label="开始集数" min="1" max="100000" step="1" required/></label>
     <label>质量<RoundedSelect v-model="form.quality" label="质量" :options="options(['REMUX','BluRay','WEB-DL','WEBRip','HDTV','DVD'])" fixed-popup/></label>
     <label>分辨率<RoundedSelect v-model="form.resolution" label="分辨率" :options="options(['8K','4K','1440p','1080p','720p','480p'])" fixed-popup/></label>
     <label>特效<RoundedSelect v-model="form.effect" label="特效" :options="options(['Dolby Vision','HDR10+','HDR Vivid','HDR10','HLG','HDR','SDR'])" fixed-popup/></label>
     <label class="full">订阅站点<RoundedSelect model-value="" label="订阅站点" :options="[]" placeholder="资源来源暂未开放" disabled/><small class="muted">资源来源栏目接入后，在这里选择对应云盘的资源站点。</small></label>
     <div class="subscription-search-period full"><label>搜索周期<RoundedSelect v-model="searchMode" label="搜索周期" :options="[{value:'system',label:'跟随系统'},{value:'custom',label:'自定义'}]" fixed-popup/></label>
     <label v-if="searchMode==='custom'">间隔小时<NumberInput v-model="form.searchHours" aria-label="间隔小时" unit="h" min="1" max="8760" step="1" required/></label></div>
     <label class="full">转存目录<RoundedSelect v-model="form.targetId" label="转存目录" :options="targetOptions" fixed-popup/><small class="muted">按目标云盘筛选分享资源；115目录可用于已支持的离线资源。<RouterLink v-if="!targets.length" to="/notifications/transfers" @click="emit('close')">添加转存目录</RouterLink></small></label>
    </div>
    <div v-else class="form-grid subscription-advanced">
     <label>包含<input v-model="form.include" aria-label="包含" maxlength="1024"/></label><label>排除<input v-model="form.exclude" aria-label="排除" maxlength="1024"/></label>
     <label v-if="media.kind==='tv'">指定季号<NumberInput v-model="form.season" aria-label="指定季号" min="0" max="1000" step="1" required/></label>
     <label :class="{full:media.kind==='movie'}">媒体分类<input v-model="form.mediaCategory" aria-label="媒体分类" maxlength="255"/></label>
     <label class="full">自定义识别词<textarea v-model="form.customWords" aria-label="自定义识别词" rows="5" maxlength="4096"/></label>
    </div>
   </fieldset><p class="subscription-pending-note"><Icon name="Info" :size="15"/>订阅配置将保留，资源来源接入后再搜索和转存。</p><p v-if="error" class="error-message" role="alert">{{error}}</p></div>
   <footer class="modal-footer"><button type="button" class="btn" :disabled="busy" @click="emit('close')">取消</button><button class="btn primary" :disabled="busy"><Icon :name="busy?'LoaderCircle':'Save'" :class="{spin:busy}"/>{{busy?'保存中…':'保存订阅'}}</button></footer>
  </form>
 </Modal>
</template>
<style scoped>
:global(.modal.wide:has(.subscription-editor)){width:min(820px,calc(100vw - 32px));max-height:min(90dvh,860px);display:flex;flex-direction:column;overflow:hidden}
.subscription-editor{display:flex;flex-direction:column;min-height:0;overflow:hidden}.subscription-editor>.modal-body{overflow-y:auto;scrollbar-width:none;min-height:0}.subscription-editor>.modal-body::-webkit-scrollbar{display:none}.subscription-fields{padding:0;margin:0;border:0;min-width:0}.subscription-basic{grid-template-columns:repeat(3,minmax(0,1fr));gap:20px 16px}.subscription-basic>.full{grid-column:1/-1}.subscription-basic>label:has([aria-label="搜索周期"]):not(.full),.subscription-basic>label:has([aria-label="间隔小时"]){grid-column:auto}.subscription-editor small{font-size:12px;line-height:1.5;margin-top:5px}.subscription-editor-tabs{display:flex;flex:none;padding:0 24px;border-bottom:1px solid var(--border)}.subscription-editor-tabs button{flex:1;padding:14px;border:0;border-bottom:2px solid transparent;background:none;color:var(--muted);cursor:pointer}.subscription-editor-tabs button.active{border-bottom-color:var(--primary);color:var(--primary)}.subscription-pending-note{display:flex;gap:8px;align-items:flex-start;color:var(--muted);font-size:12px;margin:20px 0 0}.subscription-pending-note svg{flex:none}.subscription-editor>.modal-footer{flex:none}.subscription-editor textarea{resize:vertical}
.subscription-search-period{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.subscription-search-period:not(:has([aria-label="间隔小时"]))>label{grid-column:1/-1}
.subscription-fields :deep(.number-control){width:100%}
@media(max-width:600px){.subscription-basic,.subscription-search-period{grid-template-columns:minmax(0,1fr)}.subscription-editor-tabs{padding-inline:16px}}
</style>
