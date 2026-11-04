<script setup>
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {date} from '../lib'
import {useVirtualList} from '../virtual-list'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import RoundedSelect from './RoundedSelect.vue'
import PageJumpInput from './PageJumpInput.vue'

const props=defineProps({cards:{type:Array,default:()=>[]},provider:{type:String,default:'quark'},running:{default:''},busy:Boolean,loading:Boolean})
const emit=defineEmits(['edit','remove','run'])
const label=computed(()=>props.provider==='cas'?'CAS':props.provider==='mobile'?'移动':props.provider==='115'?'115':'夸克')
const page=ref(1),jump=ref(1),viewport=ref(null)
const search=ref(''),filter=ref('all'),shell=ref(null),minHeight=ref(240)
const statusOptions=computed(()=>[{value:'all',label:'全部状态',icon:'ListFilter'},{value:'invalid',label:'失效项',icon:'Link2Off'},{value:'running',label:'执行中',icon:'LoaderCircle'},...(props.provider==='115'?[]:[{value:'archived',label:'封存中',icon:'Archive'}]),{value:'failed',label:'失败',icon:'CircleAlert'}])
const filtered=computed(()=>props.cards.filter(card=>{
 const text=search.value.trim().toLocaleLowerCase()
 if(text&&![card.name,card.shareName,card.url,card.directory,card.directoryName].some(value=>value?.toLocaleLowerCase().includes(text)))return false
 return filter.value==='all'||(filter.value==='running'?generating(card):filter.value==='invalid'?card.result?.status==='invalid':filter.value==='archived'?!card.cron?.trim():card.result?.status==='failed')
}))
const pages=computed(()=>Math.max(1,Math.ceil(filtered.value.length/20)))
watch([search,filter],()=>{go(1);reset()})
let observer
function fit(){const el=shell.value;if(!el)return;const host=el.closest('.thin-scroll-area');const offset=el.getBoundingClientRect().top-(host?.getBoundingClientRect().top||0)+(host?.scrollTop||window.scrollY);const main=el.closest('.page-content'),gap=main?parseFloat(getComputedStyle(main).paddingBottom)||24:24;minHeight.value=Math.max(240,(host?.clientHeight||innerHeight)-offset-gap)}
onMounted(()=>{observer=new ResizeObserver(fit);if(shell.value){observer.observe(shell.value);if(shell.value.parentElement)observer.observe(shell.value.parentElement);const host=shell.value.closest('.thin-scroll-area');if(host)observer.observe(host)}window.addEventListener('resize',fit);fit()})
onUnmounted(()=>{observer?.disconnect();window.removeEventListener('resize',fit)})

const pageCards=computed(()=>filtered.value.slice((page.value-1)*20,page.value*20))
const rowHeight=computed(()=>width.value<700?152:112)
const {shown,top,bottom,width,reset}=useVirtualList(pageCards,viewport,{rowHeight,window:true})
watch(pages,value=>{if(page.value>value)page.value=value})
watch(page,value=>{jump.value=value;reset()})
function go(value){page.value=Math.max(1,Math.min(pages.value,Math.trunc(Number(value))||1));jump.value=page.value}
function hasTime(value){return value&&!value.startsWith('0001')}
function generating(card){return props.running===card.id||card.result?.status==='running'}
function initials(card){return Array.from((card.name||card.shareName||'分享').trim()).slice(0,2).join('')||'分享'}
</script>

<template>
  <section ref="shell" class="share-library-window" :style="{minHeight: minHeight+'px'}" :class="{compact:width<700}" :aria-label="`${label}分享链接`">
    <header class="share-library-toolbar"><RoundedSelect v-model="filter" icon="ListFilter" label="筛选链库状态" :options="statusOptions" /><label class="share-library-search"><Icon name="Search" :size="16"/><input v-model="search" type="search" aria-label="搜索链库" placeholder="搜索分享链接" /></label></header>
    <div ref="viewport" class="share-library-rows" :style="{height:`${pageCards.length*rowHeight}px`}">
      <div v-if="top" :style="{height:`${top}px`}" aria-hidden="true" />
      <article v-for="card in shown" :key="card.id" :data-config-path="`/virtual-library/${provider}/${card.id}`" class="quark-library-card share-library-row" :style="{height:`${rowHeight}px`}" :class="{invalid:hasTime(card.result?.invalidSince),disabled:!card.enabled}" :aria-label="card.name">
        <div class="share-row-main">
          <header><span class="share-row-avatar" aria-hidden="true">{{initials(card)}}</span><h3 :data-tooltip="card.name">{{card.name}}</h3><span class="share-row-status" :class="card.result?.status">{{!card.enabled?(card.directory?'已停用':'未启用'):generating(card)?'执行中':card.result?.status==='invalid'?'分享失效':card.result?.status==='failed'?'执行失败':provider==='115'?'分享':!card.cron?.trim()?'封存中':provider==='cas'?'CAS':'活链'}}</span></header>
          <div class="share-row-location" :data-tooltip="card.directory?`${card.directory}/${card.directoryName}`:'未设置生成位置'"><Icon name="FolderOpen" :size="15" /><span>{{card.directory?`${card.directory}/${card.directoryName}`:'未设置生成位置'}}</span></div>
          <p class="quark-result share-row-result" :class="card.result?.status" :data-tooltip="card.result?.message"><Icon :name="card.result?.status==='failed'?'CircleAlert':generating(card)?'LoaderCircle':'FileSymlink'" :class="{spin:generating(card)}" :size="14" /><span>{{card.result?.message||'等待执行'}}</span></p>
        </div>
        <dl class="share-row-times"><div v-if="provider!=='115'"><dt><Icon name="Clock3" :size="14" />Cron</dt><dd :data-tooltip="card.cron||'手动执行'">{{card.cron||'手动执行'}}</dd></div><div><dt>最近执行</dt><dd :data-tooltip="hasTime(card.result?.lastRun)?date(card.result.lastRun):'尚未执行'">{{hasTime(card.result?.lastRun)?date(card.result.lastRun):'尚未执行'}}</dd></div></dl>
        <div class="share-row-actions"><button class="btn" :disabled="!card.enabled||busy" @click="emit('run',card)"><Icon :name="generating(card)?'LoaderCircle':'Play'" :class="{spin:generating(card)}" />{{generating(card)?'执行中':'执行'}}</button><button class="icon-btn" :aria-label="`编辑 ${card.name}`" :disabled="busy" @click="emit('edit',card)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :aria-label="`移除 ${card.name}`" :disabled="busy" @click="emit('remove',card)"><Icon name="Trash2" /></button></div>
      </article>
      <div v-if="bottom" :style="{height:`${bottom}px`}" aria-hidden="true" />
    </div>
    <div v-if="!filtered.length" class="share-library-empty"><Icon v-if="provider==='cas'" name="Box"/><ProviderIcon v-else :type="provider" small /><span>{{loading?'正在读取分享链接…':search||filter!=='all'?'没有匹配的分享链接':'暂无分享链接'}}</span></div>
    <footer class="share-library-pagination">
      <span>共 {{filtered.length}} 条</span>
      <div v-if="pages>1" class="share-page-controls"><button class="icon-btn" aria-label="上一页" :disabled="page<=1" @click="go(page-1)"><Icon name="ChevronLeft" /></button><span role="status" aria-label="当前页">{{page}} / {{pages}}</span><button class="icon-btn" aria-label="下一页" :disabled="page>=pages" @click="go(page+1)"><Icon name="ChevronRight" /></button>
        <form @submit.prevent="go(jump)"><PageJumpInput v-model="jump" :pages="pages" /><button class="icon-btn" aria-label="跳转页面"><Icon name="CornerDownLeft" /></button></form>
      </div>
    </footer>
  </section>
</template>

<style scoped>
.share-library-toolbar{display:flex;align-items:center;justify-content:flex-end;gap:8px;padding:12px;border-bottom:1px solid var(--border)}
.share-library-toolbar :deep(.rounded-select-trigger){height:36px;width:36px;padding:8px;justify-content:center}
.share-library-toolbar :deep(.rounded-select-popup){min-width:160px}
.share-library-toolbar .share-library-search{display:flex;flex-direction:row;margin:0;align-items:center;gap:8px;width:min(260px,calc(100% - 44px));min-width:0;color:var(--muted)}
.share-library-search input{height:36px;width:100%;min-width:0}

.share-library-window{display:flex;flex-direction:column;min-width:0;border:1px solid var(--border);border-radius:8px;background:var(--surface);overflow:visible}
.share-library-rows{min-width:0;flex:none}
.share-library-row{box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) minmax(175px,.4fr) auto;align-items:center;gap:20px;padding:12px 16px;border-bottom:1px solid var(--border);min-width:0}
.share-library-row.disabled{opacity:.65}.share-library-row.invalid{box-shadow:inset 3px 0 var(--red)}
.share-row-main{min-width:0}.share-row-main header{display:flex;align-items:center;gap:8px;min-width:0}.share-row-main h3{font-size:14px;font-weight:500;margin:0;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.share-row-avatar{display:grid;place-items:center;width:32px;height:32px;flex:none;box-sizing:border-box;border-radius:8px;background:var(--primary-soft);color:var(--primary);font-size:14px;font-weight:600;line-height:1;white-space:nowrap}
.share-row-status{flex:none;font-size:12px;color:var(--green);margin-left:4px}.share-row-status.failed,.share-row-status.invalid,.share-row-result.failed,.share-row-result.invalid{color:var(--red)}.share-row-status.running{color:var(--primary)}
.share-row-location,.share-row-result{display:flex;align-items:center;gap:6px;min-width:0;font-size:12px;color:var(--muted);margin:7px 0 0}.share-row-location>svg,.share-row-result>svg{flex:none}.share-row-location>span,.share-row-result>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.share-row-times{display:grid;gap:10px;margin:0;font-size:12px;min-width:0}.share-row-times>div{display:flex;align-items:center;gap:8px;min-width:0}.share-row-times dt{display:flex;align-items:center;gap:4px;flex:none;color:var(--muted)}.share-row-times dd{margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.share-row-actions{display:flex;align-items:center;gap:4px;flex:none}.share-row-actions .icon-btn{width:30px;height:32px}.share-row-actions .btn{height:34px;padding:0 9px;font-size:12px}
.share-library-empty{display:flex;align-items:center;justify-content:center;gap:10px;min-height:100px;color:var(--muted);font-size:13px}
.share-library-pagination{margin-top:auto;display:flex;align-items:center;justify-content:space-between;gap:8px;padding:8px 12px;font-size:12px;color:var(--muted)}.share-page-controls,.share-page-controls form{display:flex;align-items:center;gap:6px}.share-page-controls .icon-btn{width:30px;height:32px}.share-page-controls [aria-label="当前页"]{min-width:44px;text-align:center;white-space:nowrap}.share-page-controls input{width:52px;height:32px;padding:0 6px;text-align:center}
@media(max-width:1000px){.share-library-row{gap:10px}.share-row-times{font-size:11px;gap:8px}.share-library-row{grid-template-columns:minmax(0,1fr) 160px auto}}
.compact .share-library-row{grid-template-columns:minmax(0,1fr) auto;grid-template-rows:1fr auto;padding:12px;gap:8px}.compact .share-row-main{grid-column:1/-1}.compact .share-row-times{gap:5px;font-size:11px}.compact .share-row-actions{align-self:end}.compact .share-row-main h3{font-size:13px}.compact .share-row-status{margin-left:auto}.compact .share-library-pagination{padding:8px;gap:4px}.compact .share-page-controls,.compact .share-page-controls form{gap:3px}
</style>
