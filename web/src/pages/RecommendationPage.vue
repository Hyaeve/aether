<script setup>
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {api,notify} from '../lib'
import Icon from '../components/Icon.vue'
import MediaSubscriptionEditor from '../components/MediaSubscriptionEditor.vue'
import '../explore.css'
const props=defineProps({section:{default:'all'},mediaId:String})
const route=useRoute(),router=useRouter(),sections=ref([]),detail=ref(null),query=ref(''),group=ref(''),page=ref(1),pageInput=ref('1'),loading=ref(false),error=ref(''),editing=ref(null),targets=ref([]),subscribing=ref(false)
const tabs=[['all','全部','LayoutGrid'],['tv','剧集','Tv'],['movie','电影','Film'],['anime','动漫','Layers3'],['rankings','榜单','ListOrdered']]
const category=computed(()=>tabs.some(t=>t[0]===props.section)?props.section:'all')
let alive=true,ticket=0,searchTimer
async function load(){
 const run=++ticket;loading.value=true;error.value=''
 try{const value=await api(props.mediaId?`/discovery/${props.section}/${props.mediaId}`:`/discovery?${new URLSearchParams({category:category.value,q:query.value,group:group.value,page:String(page.value)})}`);if(!alive||run!==ticket)return;if(props.mediaId)detail.value=value;else sections.value=value.sections||[]}
 catch(e){if(alive&&run===ticket)error.value=e.message}finally{if(alive&&run===ticket)loading.value=false}
}
function search(){clearTimeout(searchTimer);searchTimer=setTimeout(()=>{group.value='';page.value=1;pageInput.value='1';load()},300)}
function submitSearch(){clearTimeout(searchTimer);group.value='';page.value=1;pageInput.value='1';load()}
function more(section){group.value=section.id;page.value=1;pageInput.value='1';load()}
function jump(){const max=sections.value[0]?.pages||1,n=Number(pageInput.value);page.value=Number.isFinite(n)?Math.min(max,Math.max(1,Math.trunc(n))):1;pageInput.value=String(page.value);load()}
function open(media){router.push({path:`/discover/${media.kind}/${media.id}`,query:{from:category.value}})}
async function subscribe(){subscribing.value=true;try{const value=await api('/media-subscriptions');targets.value=value.targets||[];const season=detail.value.seasons?.find(s=>s.number>0)?.number||1;const existing=value.items?.find(s=>s.kind===detail.value.kind&&s.id===detail.value.id&&(s.kind==='movie'||s.season===season));editing.value=existing||detail.value}catch(e){notify(e.message,true)}finally{subscribing.value=false}}
function back(){const from=tabs.some(t=>t[0]===route.query.from)?route.query.from:category.value;router.push('/discover/'+from)}
function scrollShelf(event,dir){const shelf=event.currentTarget.closest('.recommendation-section').querySelector('.recommendation-shelf');shelf.scrollBy({left:dir*shelf.clientWidth*.8,behavior:matchMedia('(prefers-reduced-motion: reduce)').matches?'instant':'smooth'})}
watch(()=>[props.section,props.mediaId],()=>{query.value='';group.value='';page.value=1;pageInput.value='1';sections.value=[];detail.value=null;load()})
onMounted(load);onUnmounted(()=>{alive=false;ticket++;clearTimeout(searchTimer)})
</script>
<template>
 <nav v-tab-scroll class="content-tabs" aria-label="资源推荐栏目"><RouterLink v-for="tab in tabs" :key="tab[0]" :to="'/discover/'+tab[0]" :class="{active:category===tab[0]}"><Icon :name="tab[2]"/>{{tab[1]}}</RouterLink></nav>
 <template v-if="!mediaId">
  <div class="explore-toolbar"><button v-if="group" class="btn" @click="group='';page=1;pageInput='1';load()"><Icon name="ArrowLeft"/>返回推荐</button><div class="search-field"><Icon name="Search"/><input v-model="query" aria-label="搜索推荐媒体" placeholder="搜索电影、剧集…" maxlength="300" @input="search" @keydown.enter.prevent="submitSearch"/></div><button class="icon-btn" aria-label="刷新推荐" :disabled="loading" @click="load"><Icon name="RefreshCw" :class="{spin:loading}"/></button></div>
  <p v-if="loading" class="explore-status" role="status">正在读取媒体推荐…</p>
  <section v-if="error" class="explore-empty" role="alert"><Icon name="CircleAlert"/><p>{{error}}</p><RouterLink class="btn" to="/rules/assist">TMDB配置</RouterLink><button class="btn" @click="load">重新加载</button></section>
  <section v-for="section in sections" v-show="!error" :key="section.id" class="recommendation-section" :aria-label="section.title">
   <header><h2>{{section.title}}</h2><div v-if="!group&&!query" class="recommendation-actions"><button class="icon-btn" :aria-label="`向前浏览${section.title}`" @click="scrollShelf($event,-1)"><Icon name="ChevronLeft"/></button><button class="icon-btn" :aria-label="`向后浏览${section.title}`" @click="scrollShelf($event,1)"><Icon name="ChevronRight"/></button><button class="text-btn" @click="more(section)">更多<Icon name="ChevronRight" :size="14"/></button></div></header>
   <div :class="group||query?'recommendation-grid':'recommendation-shelf'"><button v-for="media in section.items" :key="media.kind+media.id" class="recommendation-poster" :aria-label="`${media.title} 详情`" @click="open(media)"><img v-if="media.poster" :src="media.poster" alt="" loading="lazy" decoding="async" referrerpolicy="no-referrer"/><Icon v-else :name="media.kind==='tv'?'Tv':'Film'" :size="36"/><span class="poster-kind">{{media.kind==='tv'?'剧集':'电影'}}</span><span class="poster-score">{{Number(media.score||0).toFixed(1)}}</span><span class="poster-caption"><strong>{{media.title}}</strong><small>{{media.year||'年份待定'}}</small></span></button></div>
   <p v-if="!loading&&!section.items.length" class="explore-status">暂无匹配的媒体</p>
  </section>
  <footer v-if="(group||query)&&(sections[0]?.pages||1)>1" class="explore-pagination"><button class="btn" :disabled="page<=1||loading" @click="pageInput=String(page-1);jump()">上一页</button><span>{{page}} / {{sections[0].pages}}</span><button class="btn" :disabled="page>=sections[0].pages||loading" @click="pageInput=String(page+1);jump()">下一页</button><input v-model="pageInput" type="number" min="1" :max="sections[0].pages" aria-label="推荐跳转页码" @change="jump" @keydown.enter.prevent="jump"/></footer>
 </template>
 <template v-else>
  <div class="explore-toolbar"><button class="btn" @click="back"><Icon name="ArrowLeft"/>返回推荐</button></div><p v-if="loading" class="explore-status" role="status">正在读取媒体详情…</p><p v-if="error" class="error-message" role="alert">{{error}}</p>
  <template v-if="detail"><section class="discovery-hero"><img v-if="detail.backdrop" class="discovery-backdrop" :src="detail.backdrop" alt="" decoding="async" referrerpolicy="no-referrer"/><div class="discovery-hero-content"><img v-if="detail.poster" class="discovery-detail-poster" :src="detail.poster" :alt="detail.title" referrerpolicy="no-referrer"/><span v-else class="discovery-detail-poster poster-fallback"><Icon name="Film" :size="48"/></span><div><small>{{detail.kind==='tv'?'剧集':'电影'}} · {{detail.year}}</small><h1>{{detail.title}}</h1><p>{{detail.runtime?`${detail.runtime} 分钟 · `:''}}{{detail.genres?.join(' / ')}}</p><span class="detail-rating"><Icon name="Star"/>{{Number(detail.score||0).toFixed(1)}}</span><button class="btn primary" :disabled="subscribing" @click="subscribe"><Icon name="Bell"/>{{subscribing?'正在读取…':'添加订阅'}}</button></div></div></section><div class="discovery-detail-layout"><section class="discovery-overview"><h2>简介</h2><p>{{detail.overview||'暂无简介'}}</p><h2 v-if="detail.cast?.length">主要演员</h2><div class="discovery-cast"><span v-for="name in detail.cast" :key="name">{{name}}</span></div></section><aside class="discovery-facts"><dl><div v-for="[label,value] in [['TMDB ID',detail.id],['原始标题',detail.originalTitle],['状态',detail.status],['原始语言',detail.language],['出品国家',detail.countries?.join(' / ')],['制作公司',detail.companies?.join(' / ')]]" :key="label"><dt>{{label}}</dt><dd>{{value||'—'}}</dd></div></dl><a :href="`https://www.themoviedb.org/${detail.kind}/${detail.id}`" target="_blank" rel="noopener noreferrer">The Movie Database<Icon name="ExternalLink" :size="14"/></a></aside></div></template>
 </template>
 <MediaSubscriptionEditor v-if="editing" :media="editing" :targets="targets" @close="editing=null" @saved="editing=null"/>
</template>
