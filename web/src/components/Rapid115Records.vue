<script setup>
import { computed, ref, watch } from 'vue'
import { bytes, date } from '../lib'
import { compactDate } from '../date-time.mjs'
import Icon from './Icon.vue'
const props = defineProps({ items: {type:Array,default:()=>[]}, page:Number, pages:Number, loading:Boolean, scoped:Boolean })
const emit = defineEmits(['page'])
const jump=ref(1)
watch(()=>props.page,value=>jump.value=value,{immediate:true})
const statuses={running:'处理中',failed:'秒传失败',interrupted:'已中断',deleted:'重试3次失败，已删除'}
function status(r){return r.status==='running'?(r.phase||'处理中'):r.status==='success'?(r.retries?`重试${r.retries}次秒传成功`:'秒传成功'):r.status==='uploaded'?'传输成功':r.status==='retrying'?`等待第${(r.retries||0)+1}次重试`:statuses[r.status]||r.status}
const icon=r=>({running:'LoaderCircle',success:'CircleCheck',uploaded:'Upload',retrying:'Clock',deleted:'Trash2',failed:'CircleAlert',interrupted:'CircleAlert'})[r.status]||'CircleAlert'
const progress=r=>['success','uploaded'].includes(r.status)?1:r.total>0?Math.max(0,Math.min(1,(r.done||0)/r.total)):0
const sourceKind=r=>r.sourceKind||(r.source?.startsWith('监控')?'monitor':r.source?.startsWith('手动')?'manual':'')
const columns=computed(()=>['文件名称','文件大小','PickCode',...(!props.scoped?['触发来源']:[]),'执行时间','秒传状态'])
</script>
<template>
  <div class="rapid115-record-frame" :class="{scoped}">
    <div class="rapid115-record-table" role="table" aria-label="秒传记录">
      <div class="rapid115-record-columns" role="row"><span v-for="label in columns" :key="label" role="columnheader">{{label}}</span></div>
      <article v-for="r in items" :key="r.id" class="rapid115-record" role="row">
        <span class="rapid115-record-name" role="cell" :data-tooltip="r.name" data-tooltip-stationary data-tooltip-delay="300" tabindex="0">{{r.name}}</span>
        <span class="rapid115-record-size" role="cell">{{bytes(r.size)}}</span>
        <span class="rapid115-pickcode" role="cell">{{r.pickcode||'--'}}</span>
        <span v-if="!scoped" class="rapid115-source" :class="sourceKind(r)" role="cell" :data-tooltip="r.source" data-tooltip-stationary data-tooltip-delay="300">{{r.source}}</span>
        <time role="cell" :datetime="r.time" :data-tooltip="date(r.time)" data-tooltip-always data-tooltip-stationary>{{scoped?compactDate(r.time).slice(3):compactDate(r.time)}}</time>
        <span class="rapid115-status" role="cell" :class="r.status"><span class="rapid115-status-label" :data-tooltip="r.message||status(r)" data-tooltip-stationary data-tooltip-delay="300"><Icon :name="icon(r)" :size="14" /><span>{{status(r)}}</span></span><progress :aria-label="`${r.name}：${status(r)}`" :aria-valuetext="r.total>0?`${status(r)}，${Math.floor(progress(r)*100)}%`:status(r)" :value="progress(r)" max="1" /></span>
      </article>
    </div>
    <p v-if="!items.length" class="small-empty">暂无秒传记录</p>
  </div>
  <form class="rapid115-pagination" aria-label="秒传记录分页" @submit.prevent="emit('page',jump)"><button type="button" class="icon-btn" aria-label="第一页" :disabled="loading||page===1" @click="emit('page',1)"><Icon name="ChevronsLeft" /></button><button type="button" class="icon-btn" aria-label="上一页" :disabled="loading||page===1" @click="emit('page',page-1)"><Icon name="ChevronLeft" /></button><span>{{page}} / {{pages}}</span><button type="button" class="icon-btn" aria-label="下一页" :disabled="loading||page===pages" @click="emit('page',page+1)"><Icon name="ChevronRight" /></button><button type="button" class="icon-btn" aria-label="最后一页" :disabled="loading||page===pages" @click="emit('page',pages)"><Icon name="ChevronsRight" /></button><input v-model="jump" type="number" min="1" step="1" aria-label="跳转页码" /><button class="icon-btn" aria-label="跳转到指定页" :disabled="loading"><Icon name="CornerDownLeft" /></button></form>
</template>
<style scoped>
.rapid115-record-frame{min-height:calc(44px + 30 * 56px);background:var(--input);border:1px solid var(--border);border-radius:6px;overflow:clip}
.rapid115-record-columns,.rapid115-record{display:grid;grid-template-columns:minmax(120px,1fr) 80px 19ch 100px 126px 128px;gap:12px;padding:10px 12px;align-items:center;font-size:14px}
.rapid115-record-columns{height:42px;color:var(--muted);background:var(--layer-item);border-bottom:1px solid var(--border)}
.rapid115-record{height:56px;border-bottom:1px solid var(--border);background:var(--input)}
.rapid115-record>[role=cell]{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.rapid115-pickcode{font-family:"Cascadia Code",Consolas,monospace;font-size:13px}
time{font-variant-numeric:tabular-nums}
.rapid115-source.monitor{color:var(--info)}.rapid115-source.manual{color:var(--orange)}
.rapid115-status{display:grid;gap:6px;--status-color:var(--amber);color:var(--status-color)}
.rapid115-status-label{display:flex;align-items:center;gap:5px;min-width:0}.rapid115-status-label>span{overflow:hidden;text-overflow:ellipsis}.rapid115-status-label svg{flex:none}
.rapid115-status.success,.rapid115-status.uploaded{--status-color:var(--green)}
.rapid115-status.failed,.rapid115-status.interrupted,.rapid115-status.deleted{--status-color:var(--red)}
.rapid115-status progress{display:block;appearance:none;width:104px;max-width:100%;height:4px;border:1px solid var(--status-color);border-radius:3px;background:var(--input);overflow:hidden;accent-color:var(--status-color)}
.rapid115-status progress::-webkit-progress-bar{background:var(--input)}.rapid115-status progress::-webkit-progress-value,.rapid115-status progress::-moz-progress-bar{background:var(--status-color)}
.rapid115-status progress::-webkit-progress-value{background:var(--status-color)}.rapid115-status progress::-moz-progress-bar{background:var(--status-color)}
.rapid115-pagination{display:flex;align-items:center;justify-content:flex-end;gap:6px;padding-top:12px;font-variant-numeric:tabular-nums}
.rapid115-pagination>span{min-width:54px;text-align:center;font-size:13px}.rapid115-pagination input{width:64px;height:32px;min-height:32px;padding:5px 8px;text-align:center}
.scoped{min-height:calc(44px + 10 * 56px)}.scoped .rapid115-record,.scoped .rapid115-record-columns{grid-template-columns:minmax(60px,1fr) 56px 124px 96px 104px;gap:6px;padding-inline:8px;font-size:13px}.scoped .rapid115-pickcode{font-size:12px}
@media(max-width:1100px){
 .rapid115-record-columns{display:none}.rapid115-record-frame{min-height:calc(2px + 30 * 124px)}
 .rapid115-record{height:124px;grid-template-columns:minmax(0,1fr) minmax(0,1fr);grid-template-rows:24px 22px 22px 24px;gap:2px 12px}
 .rapid115-record-name{grid-column:1/-1}.rapid115-pickcode{grid-column:1/-1;grid-row:3}.rapid115-record time{grid-row:4;font-size:13px}.rapid115-status{grid-column:2;grid-row:4;justify-self:end;width:128px}.rapid115-source{text-align:right}
 .scoped{min-height:calc(2px + 10 * 124px)}.scoped .rapid115-record{grid-template-columns:minmax(0,1fr) minmax(0,1fr);grid-template-rows:24px 22px 22px 24px}.scoped .rapid115-record-size{grid-row:2}.scoped .rapid115-pickcode{grid-row:3}
}
@media(max-width:440px){.rapid115-pagination{gap:3px;justify-content:center}.rapid115-pagination .icon-btn{width:30px;height:36px}.rapid115-pagination input{width:52px}}
</style>
