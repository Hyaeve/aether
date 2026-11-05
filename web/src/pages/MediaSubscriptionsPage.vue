<script setup>
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {api,notify} from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import MediaSubscriptionEditor from '../components/MediaSubscriptionEditor.vue'
import '../explore.css'
const props=defineProps({section:{default:'tv'}}),cards=ref([]),targets=ref([]),loading=ref(false),error=ref(''),query=ref(''),editing=ref(null),deleting=ref(null),busy=ref(false)
let alive=true
const page=ref(1),pageInput=ref('1')
const filtered=computed(()=>cards.value.filter(c=>c.kind===props.section&&`${c.title} ${c.year}`.toLowerCase().includes(query.value.toLowerCase())))
const pages=computed(()=>Math.max(1,Math.ceil(filtered.value.length/20))),visible=computed(()=>filtered.value.slice((page.value-1)*20,page.value*20))
function jump(){const n=Number(pageInput.value);page.value=Number.isFinite(n)?Math.min(pages.value,Math.max(1,Math.trunc(n))):1;pageInput.value=String(page.value)}
watch(query,()=>{page.value=1;pageInput.value='1'})
watch(pages,()=>jump())
async function load(){loading.value=true;error.value='';try{const value=await api('/media-subscriptions');if(alive){cards.value=value.items||[];targets.value=value.targets||[]}}catch(e){if(alive)error.value=e.message}finally{if(alive)loading.value=false}}
async function remove(){busy.value=true;try{await api('/media-subscriptions/'+deleting.value.key,'DELETE');deleting.value=null;await load();notify('订阅已取消')}catch(e){notify(e.message,true)}finally{busy.value=false}}
onMounted(load);onUnmounted(()=>{alive=false})
</script>
<template>
 <nav v-tab-scroll class="content-tabs" aria-label="媒体订阅栏目"><RouterLink v-for="tab in [['tv','剧集订阅','Tv'],['movie','电影订阅','Film'],['sources','资源来源','Globe']]" :key="tab[0]" :to="'/subscriptions/'+tab[0]" :class="{active:section===tab[0]}"><Icon :name="tab[2]"/>{{tab[1]}}</RouterLink></nav>
 <section v-if="section==='sources'" class="explore-empty" aria-label="资源来源"><Icon name="Globe" :size="32"/><h2>资源来源</h2><p>资源站点接入功能待开放。</p></section>
 <template v-else>
  <div class="explore-toolbar"><span class="muted">{{filtered.length}} 项订阅</span><div class="search-field"><Icon name="Search"/><input v-model="query" aria-label="搜索订阅" placeholder="搜索订阅名称"/></div><button class="icon-btn" aria-label="刷新订阅" :disabled="loading" @click="load"><Icon name="RefreshCw" :class="{spin:loading}"/></button></div>
  <p v-if="error" class="error-message" role="alert">{{error}}</p><p v-if="loading" class="explore-status" role="status">正在读取订阅…</p>
  <div class="subscription-grid"><article v-for="card in visible" :key="card.key" class="subscription-card" :data-config-path="`/media-subscriptions/${card.key}`"><img v-if="card.backdrop" class="subscription-backdrop" :src="card.backdrop" alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer"/><button class="subscription-open" :aria-label="`编辑订阅 ${card.title}`" @click="editing=card"><img v-if="card.poster" :src="card.poster" alt="" loading="lazy" referrerpolicy="no-referrer"/><span v-else class="subscription-small-poster"><Icon :name="section==='tv'?'Tv':'Film'" :size="28"/></span><span class="subscription-card-title"><small>{{card.year}}</small><strong>{{card.title}}<em v-if="card.kind==='tv'">S{{String(card.season).padStart(2,'0')}}</em></strong></span><span class="subscription-progress"><Icon name="Clock3" :size="17"/>{{card.kind==='tv'?`0 / ${card.totalEpisodes||'—'}`:'待转存'}}</span><span class="subscription-state">待接入资源来源</span></button><button class="icon-btn subscription-remove" :aria-label="`取消订阅 ${card.title}`" @click="deleting=card"><Icon name="Trash2" :size="16"/></button><span class="subscription-progress-line" aria-hidden="true"/></article></div>
  <footer v-if="pages>1" class="explore-pagination"><button class="btn" :disabled="page<=1" @click="pageInput=String(page-1);jump()">上一页</button><span>{{page}} / {{pages}}</span><button class="btn" :disabled="page>=pages" @click="pageInput=String(page+1);jump()">下一页</button><input v-model="pageInput" type="number" min="1" :max="pages" aria-label="订阅跳转页码" @change="jump" @keydown.enter.prevent="jump"/></footer>
  <section v-if="!loading&&!filtered.length&&!error" class="explore-empty"><Icon :name="section==='tv'?'Tv':'Film'" :size="32"/><h2>暂无{{section==='tv'?'剧集':'电影'}}订阅</h2><p>在资源推荐的媒体详情中添加订阅。</p><RouterLink class="btn primary" :to="'/discover/'+section">浏览资源推荐</RouterLink></section>
 </template>
 <MediaSubscriptionEditor v-if="editing" :media="editing" :targets="targets" @close="editing=null" @saved="editing=null;load()"/>
 <Modal v-if="deleting" title="取消订阅" confirmation @close="!busy&&(deleting=null)"><p class="modal-body">确认取消「{{deleting.title}}」的订阅？已转存的文件不受影响。</p><footer class="modal-footer"><button class="btn" :disabled="busy" @click="deleting=null">返回</button><button class="btn danger" :disabled="busy" @click="remove">确认取消</button></footer></Modal>
</template>
