<script setup>
import {computed,ref,watch} from 'vue'
import {date} from '../lib'
import {useVirtualList} from '../virtual-list'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import PageJumpInput from './PageJumpInput.vue'

const props=defineProps({cards:{type:Array,default:()=>[]},provider:{type:String,default:'quark'},running:{default:''},busy:Boolean,loading:Boolean})
const emit=defineEmits(['edit','remove','run'])
const label=computed(()=>props.provider==='mobile'?'移动':'夸克')
const page=ref(1),jump=ref(1),viewport=ref(null)
const pages=computed(()=>Math.max(1,Math.ceil(props.cards.length/20)))
const pageCards=computed(()=>props.cards.slice((page.value-1)*20,page.value*20))
const rowHeight=computed(()=>width.value<700?152:112)
const {shown,top,bottom,width,reset}=useVirtualList(pageCards,viewport,{rowHeight,window:true})
watch(pages,value=>{if(page.value>value)page.value=value})
watch(page,value=>{jump.value=value;reset()})
function go(value){page.value=Math.max(1,Math.min(pages.value,Math.trunc(Number(value))||1));jump.value=page.value}
function hasTime(value){return value&&!value.startsWith('0001')}
function generating(card){return props.running===card.id||card.result?.status==='running'}
</script>

<template>
  <section class="share-library-window" :class="{compact:width<700}" :aria-label="`${label}分享链接`">
    <div ref="viewport" class="share-library-rows" :style="{height:`${pageCards.length*rowHeight}px`}">
      <div v-if="top" :style="{height:`${top}px`}" aria-hidden="true" />
      <article v-for="card in shown" :key="card.id" :data-config-path="`/virtual-library/${provider}/${card.id}`" class="quark-library-card share-library-row" :style="{height:`${rowHeight}px`}" :class="{invalid:hasTime(card.result?.invalidSince),disabled:!card.enabled}" :aria-label="card.name">
        <div class="share-row-main">
          <header><ProviderIcon :type="provider" small /><h3 :data-tooltip="card.name">{{card.name}}</h3><span class="share-row-status" :class="card.result?.status">{{!card.enabled?(card.directory?'已停用':'未启用'):generating(card)?'执行中':card.result?.status==='invalid'?'分享失效':card.result?.status==='failed'?'执行失败':'活链'}}</span></header>
          <div class="share-row-location" :data-tooltip="card.directory?`${card.directory}/${card.directoryName}`:'未设置生成位置'"><Icon name="FolderOpen" :size="15" /><span>{{card.directory?`${card.directory}/${card.directoryName}`:'未设置生成位置'}}</span></div>
          <p class="quark-result share-row-result" :class="card.result?.status" :data-tooltip="card.result?.message"><Icon :name="card.result?.status==='failed'?'CircleAlert':generating(card)?'LoaderCircle':'FileSymlink'" :class="{spin:generating(card)}" :size="14" /><span>{{card.result?.message||'等待执行'}}</span></p>
        </div>
        <dl class="share-row-times"><div><dt><Icon name="Clock3" :size="14" />Cron</dt><dd :data-tooltip="card.cron||'手动执行'">{{card.cron||'手动执行'}}</dd></div><div><dt>最近执行</dt><dd :data-tooltip="hasTime(card.result?.lastRun)?date(card.result.lastRun):'尚未执行'">{{hasTime(card.result?.lastRun)?date(card.result.lastRun):'尚未执行'}}</dd></div></dl>
        <div class="share-row-actions"><button class="icon-btn" :aria-label="`编辑 ${card.name}`" :disabled="busy" @click="emit('edit',card)"><Icon name="Pencil" /></button><button class="icon-btn danger-text" :aria-label="`移除 ${card.name}`" :disabled="busy" @click="emit('remove',card)"><Icon name="Trash2" /></button><button class="btn" :disabled="!card.enabled||busy" @click="emit('run',card)"><Icon :name="generating(card)?'LoaderCircle':'Play'" :class="{spin:generating(card)}" />{{generating(card)?'生成中':'立即生成'}}</button></div>
      </article>
      <div v-if="bottom" :style="{height:`${bottom}px`}" aria-hidden="true" />
    </div>
    <div v-if="!cards.length" class="share-library-empty"><ProviderIcon :type="provider" small /><span>{{loading?'正在读取分享链接…':'暂无分享链接'}}</span></div>
    <footer class="share-library-pagination">
      <span>共 {{cards.length}} 条</span>
      <div class="share-page-controls"><button class="icon-btn" aria-label="上一页" :disabled="page<=1" @click="go(page-1)"><Icon name="ChevronLeft" /></button><span role="status" aria-label="当前页">{{page}} / {{pages}}</span><button class="icon-btn" aria-label="下一页" :disabled="page>=pages" @click="go(page+1)"><Icon name="ChevronRight" /></button>
        <form @submit.prevent="go(jump)"><PageJumpInput v-model="jump" :pages="pages" /><button class="icon-btn" aria-label="跳转页面"><Icon name="CornerDownLeft" /></button></form>
      </div>
    </footer>
  </section>
</template>

<style scoped>
.share-library-window{min-width:0;border:1px solid var(--border);border-radius:8px;background:var(--surface);overflow:clip}
.share-library-rows{min-width:0}
.share-library-row{box-sizing:border-box;display:grid;grid-template-columns:minmax(0,1fr) minmax(175px,.4fr) auto;align-items:center;gap:20px;padding:12px 16px;border-bottom:1px solid var(--border);min-width:0}
.share-library-row.disabled{opacity:.65}.share-library-row.invalid{box-shadow:inset 3px 0 var(--red)}
.share-row-main{min-width:0}.share-row-main header{display:flex;align-items:center;gap:8px;min-width:0}.share-row-main h3{font-size:14px;font-weight:500;margin:0;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.share-row-status{flex:none;font-size:12px;color:var(--green);margin-left:4px}.share-row-status.failed,.share-row-status.invalid,.share-row-result.failed,.share-row-result.invalid{color:var(--red)}.share-row-status.running{color:var(--primary)}
.share-row-location,.share-row-result{display:flex;align-items:center;gap:6px;min-width:0;font-size:12px;color:var(--muted);margin:7px 0 0}.share-row-location>svg,.share-row-result>svg{flex:none}.share-row-location>span,.share-row-result>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.share-row-times{display:grid;gap:10px;margin:0;font-size:12px;min-width:0}.share-row-times>div{display:flex;align-items:center;gap:8px;min-width:0}.share-row-times dt{display:flex;align-items:center;gap:4px;flex:none;color:var(--muted)}.share-row-times dd{margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.share-row-actions{display:flex;align-items:center;gap:4px;flex:none}.share-row-actions .icon-btn{width:30px;height:32px}.share-row-actions .btn{height:34px;padding:0 9px;font-size:12px}
.share-library-empty{display:flex;align-items:center;justify-content:center;gap:10px;min-height:100px;color:var(--muted);font-size:13px}
.share-library-pagination{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:8px 12px;font-size:12px;color:var(--muted)}.share-page-controls,.share-page-controls form{display:flex;align-items:center;gap:6px}.share-page-controls .icon-btn{width:30px;height:32px}.share-page-controls [aria-label="当前页"]{min-width:44px;text-align:center;white-space:nowrap}.share-page-controls input{width:52px;height:32px;padding:0 6px;text-align:center}
@media(max-width:1000px){.share-library-row{gap:10px}.share-row-times{font-size:11px;gap:8px}.share-library-row{grid-template-columns:minmax(0,1fr) 160px auto}}
.compact .share-library-row{grid-template-columns:minmax(0,1fr) auto;grid-template-rows:1fr auto;padding:12px;gap:8px}.compact .share-row-main{grid-column:1/-1}.compact .share-row-times{gap:5px;font-size:11px}.compact .share-row-actions{align-self:end}.compact .share-row-main h3{font-size:13px}.compact .share-row-status{margin-left:auto}.compact .share-library-pagination{padding:8px;gap:4px}.compact .share-page-controls,.compact .share-page-controls form{gap:3px}
</style>
