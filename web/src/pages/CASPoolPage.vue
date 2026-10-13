<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, bytes, date, notify, state } from '../lib'
import CASPool from '../components/CASPool.vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import CacheHitChart from '../components/CacheHitChart.vue'
import ThinScroll from '../components/ThinScroll.vue'
import { usePageSlide } from '../page-slide'
const props=defineProps({kind:{default:'cas'}})
const {leaving,back}=usePageSlide(`/tasks/${props.kind}`)
const pool=ref(null),backButton=ref(null),busy=ref(false),ready=ref(false),items=ref([]),stats=ref({}),temporary=ref(false),selected=ref([]),cleaning=ref(false),confirm=ref(false)
let alive=true,timer,anchor=-1
const metrics=computed(()=>[['成功数',stats.value.success||0,'CircleCheck'],['失败数',stats.value.failed||0,'CircleAlert'],...(props.kind==='ed2k'?[]:[['移动秒传数',stats.value.mobile||0,'mobile'],['天翼秒传数',stats.value.tianyi||0,'tianyi']])])
const recent=computed(()=>[...items.value].sort((a,b)=>Date.parse(b.lastUsed||b.expiresAt)-Date.parse(a.lastUsed||a.expiresAt)).slice(0,4))
function escape(event){if(!temporary.value&&!event.target.closest('.rounded-select'))back()}
async function load(){try{const value=await api(`/${props.kind}/status`);if(alive){items.value=value.items||[];stats.value=value.stats||{};selected.value=selected.value.filter(key=>items.value.some(i=>i.key===key))}}catch(e){if(alive)notify(e.message,true)}}
function select(event,item){const index=items.value.indexOf(item);if(event.shiftKey&&anchor>=0){selected.value=[...new Set([...selected.value,...items.value.slice(Math.min(index,anchor),Math.max(index,anchor)+1).map(i=>i.key)])]}else{selected.value=selected.value.includes(item.key)?selected.value.filter(key=>key!==item.key):[...selected.value,item.key];anchor=index}}
function all(){selected.value=selected.value.length===items.value.length?[]:items.value.map(i=>i.key)}
async function cleanup(){cleaning.value=true;try{const result=await api(`/${props.kind}/cleanup`,'POST',{keys:selected.value});notify(`已清理 ${result.removed||0} 项${result.error?' · '+result.error:''}`,!!result.error);selected.value=[];confirm.value=false;await load()}catch(e){notify(e.message,true)}finally{cleaning.value=false}}
const storageName=id=>state.storages.find(s=>s.id===id)?.name||'存储已移除'
onMounted(()=>{backButton.value?.focus({preventScroll:true});load();timer=setInterval(load,10000)})
onBeforeUnmount(()=>{alive=false;clearInterval(timer)})
</script>
<template>
  <div class="cas-pool-page page-slide" :class="{leaving}" @keydown.esc="escape">
    <header class="task-heading jump-heading"><div class="jump-title"><button ref="backButton" class="icon-btn" :aria-label="`返回 ${kind.toUpperCase()} 任务`" @click="back"><Icon name="ChevronRight" :size="24" /></button><h2 class="module-title">存储绑定池</h2></div><button class="btn primary" :disabled="busy||!ready" @click="pool?.save()"><Icon name="Save" />保存设置</button></header>
    <section class="cas-pool-workspace">
      <section class="cas-temporary-strip" aria-label="CAS 秒传统计"><CacheHitChart task label="秒传率" :show-value="false" :hits="stats.success||0" :misses="stats.failed||0" /><dl><div v-for="[label,value,icon] in metrics" :key="label"><dt><ProviderIcon v-if="['mobile','tianyi'].includes(icon)" :type="icon" small /><Icon v-else :name="icon" :size="22" :class="icon==='CircleAlert'?'danger-text':'success-text'" />{{label}}</dt><dd>{{value}}</dd></div></dl><button class="temporary-preview" aria-label="查看秒传临时文件" @click="temporary=true;load()"><span v-for="item in recent" :key="item.key" class="temporary-preview-entry"><strong>{{item.name}}</strong><small>{{storageName(item.storageId)}} · {{bytes(item.size||0)}}</small></span><span v-if="!recent.length" class="temporary-preview-empty"><Icon name="FileClock" :size="22" />临时文件</span></button></section>
      <CASPool ref="pool" :kind="kind" @close="back" @busy="busy=$event" @ready="ready=$event" />
    </section>
  </div>
  <Modal v-if="temporary" standard title="秒传临时文件" @close="!cleaning && (temporary=false)"><div class="modal-body temporary-body"><div class="temporary-toolbar"><span>{{items.length}} 项</span><button class="btn" :disabled="cleaning" @click="all"><Icon name="CheckCheck" />{{selected.length===items.length&&items.length?'全不选':'全选'}}</button><button class="btn danger" :disabled="!selected.length||cleaning" @click="confirm=true"><Icon name="Trash2" />清理</button></div><ThinScroll :thickness="3" class="temporary-scroll"><article v-for="item in items" :key="item.key" class="temporary-item" :class="{selected:selected.includes(item.key)}" tabindex="0" @click="select($event,item)" @keydown.space.prevent="select($event,item)"><Icon :name="selected.includes(item.key)?'CircleCheck':'Circle'" /><div><strong>{{item.name}}</strong><small>{{storageName(item.storageId)}} · {{bytes(item.size||0)}}</small><time>{{date(item.expiresAt)}} 到期{{item.active?' · 播放中':''}}</time></div></article></ThinScroll></div></Modal>
  <Modal v-if="confirm" compact confirmation title="清理秒传文件" @close="!cleaning && (confirm=false)"><div class="modal-body">确认删除所选 {{selected.length}} 个秒传临时文件？正在播放的文件不会删除。</div><footer class="modal-footer"><button class="btn danger" :disabled="cleaning" @click="cleanup">确认清理</button><button class="btn" :disabled="cleaning" @click="confirm=false">取消</button></footer></Modal>
</template>
<style scoped>
.cas-pool-page{min-width:0;flex:1;display:flex;flex-direction:column}.cas-pool-workspace{flex:1;border:1px solid var(--border);border-radius:8px;background:var(--surface);padding:18px}
.cas-temporary-strip{display:grid;grid-template-columns:104px minmax(220px,.8fr) minmax(260px,1fr);align-items:center;gap:24px;padding:4px 4px 20px;border-bottom:1px solid var(--border);margin-bottom:8px}.cas-temporary-strip dl{display:grid;grid-auto-flow:column;grid-template-rows:repeat(2,1fr);grid-template-columns:repeat(2,minmax(0,1fr));gap:12px 16px;margin:0}.cas-temporary-strip dt{display:flex;gap:6px;align-items:center;font-size:12px;color:var(--muted)}.cas-temporary-strip dd{margin:3px 0 0 28px;font-size:17px}.cas-temporary-strip :deep(.provider-icon){width:22px;height:22px;flex:none;background:none;border:0;padding:0}.cas-temporary-strip :deep(.provider-logo){width:100%;height:100%;object-fit:contain;transform:scale(1.16)}
.temporary-preview{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));grid-template-rows:repeat(2,minmax(44px,1fr));gap:8px;padding:10px;border:1px solid var(--border);border-radius:8px;background:var(--input);text-align:left;color:var(--text);min-width:0;min-height:110px}.temporary-preview:hover{border-color:var(--primary)}.temporary-preview-entry{display:grid;gap:4px;min-width:0;align-content:center}.temporary-preview-entry strong{font-size:13px;font-weight:500;overflow:hidden;white-space:nowrap;text-overflow:ellipsis}.temporary-preview-entry small{font-size:12px;color:var(--muted);overflow:hidden;white-space:nowrap;text-overflow:ellipsis}.temporary-preview-empty{grid-column:1 / -1;grid-row:1 / -1;display:flex;align-items:center;justify-content:center;gap:8px;color:var(--muted);font-size:14px}
.temporary-toolbar{display:flex;gap:8px;align-items:center;margin-bottom:14px}.temporary-toolbar>span{margin-right:auto;color:var(--muted)}.temporary-scroll{height:min(55dvh,480px)}.temporary-item{display:flex;gap:10px;padding:10px;border:1px solid var(--border);border-radius:8px;margin-bottom:6px;cursor:pointer;background:var(--input)}.temporary-item.selected{border-color:var(--primary);background:var(--primary-soft)}.temporary-item>div{min-width:0;display:grid;gap:4px}.temporary-item strong{font-size:14px;overflow-wrap:anywhere}.temporary-item small,.temporary-item time{font-size:12px;color:var(--muted)}
@media(max-width:1050px){.cas-temporary-strip{grid-template-columns:104px minmax(0,1fr);gap:12px}.temporary-preview{grid-column:1 / -1}}@media(max-width:600px){.cas-pool-workspace{padding:12px}.cas-temporary-strip dt{font-size:12px}}
</style>
