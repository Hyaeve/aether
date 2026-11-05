<script setup>
import {computed,onMounted,onUnmounted,ref,watch} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {api,notify} from '../lib'
import Icon from '../components/Icon.vue'
import MediaSubscriptionEditor from '../components/MediaSubscriptionEditor.vue'
import DiscoveryPoster from '../components/DiscoveryPoster.vue'
import PosterImage from '../components/PosterImage.vue'
import Modal from '../components/Modal.vue'
import '../explore.css'
import '../discovery-revision.css'
const props=defineProps({section:{default:'all'},mediaId:String})
const route=useRoute(),router=useRouter(),sections=ref([]),detail=ref(null),query=ref(''),group=ref(''),page=ref(1),pageInput=ref('1'),loading=ref(false),error=ref(''),editing=ref(null),targets=ref([]),subscribing=ref(false)
const tabs=[['all','全部','LayoutGrid'],['tv','剧集','Tv'],['movie','电影','Film'],['anime','动漫','Layers3'],['rankings','榜单','ListOrdered']]
const category=computed(()=>tabs.some(t=>t[0]===props.section)?props.section:'all')
const resourceSearch=ref(null)
async function searchResources(media){resourceSearch.value={media,loading:true,ready:false};try{const data=await api('/media-subscriptions');if(resourceSearch.value?.media===media)Object.assign(resourceSearch.value,{loading:false,ready:!!data.sourceReady})}catch(e){if(resourceSearch.value?.media===media)Object.assign(resourceSearch.value,{loading:false,error:e.message})}}
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
async function subscribe(media=detail.value){subscribing.value=true;try{const [value,full]=await Promise.all([api('/media-subscriptions'),media.seasons?Promise.resolve(media):api(`/discovery/${media.kind}/${media.id}`)]);targets.value=value.targets||[];const season=full.seasons?.find(s=>s.number>0)?.number||1;const existing=value.items?.find(s=>s.kind===full.kind&&s.id===full.id&&(s.kind==='movie'||s.season===season));editing.value=existing||full}catch(e){notify(e.message,true)}finally{subscribing.value=false}}
function back(){const from=tabs.some(t=>t[0]===route.query.from)?route.query.from:category.value;router.push('/discover/'+from)}
function scrollShelf(event,dir){const shelf=event.currentTarget.closest('.recommendation-section').querySelector('.recommendation-shelf');shelf.scrollBy({left:dir*shelf.clientWidth*.8,behavior:matchMedia('(prefers-reduced-motion: reduce)').matches?'instant':'smooth'})}
watch(()=>[props.section,props.mediaId],()=>{query.value='';group.value='';page.value=1;pageInput.value='1';sections.value=[];detail.value=null;load()})
onMounted(load);onUnmounted(()=>{alive=false;ticket++;clearTimeout(searchTimer)})
</script>
<template>
 <header class="recommendation-heading"><nav v-tab-scroll class="content-tabs" aria-label="资源推荐栏目"><RouterLink v-for="tab in tabs" :key="tab[0]" :to="'/discover/'+tab[0]" :class="{active:category===tab[0]}"><Icon :name="tab[2]"/>{{tab[1]}}</RouterLink></nav><div v-if="!mediaId" class="search-field"><Icon name="Search"/><input v-model="query" aria-label="搜索推荐媒体" placeholder="搜索电影、剧集…" maxlength="300" @input="search" @keydown.enter.prevent="submitSearch"/></div><button v-else class="btn" @click="back"><Icon name="ArrowLeft"/>返回推荐</button></header>
 <template v-if="!mediaId">
  <div v-if="group" class="explore-toolbar"><button class="btn" @click="group='';page=1;pageInput='1';load()"><Icon name="ArrowLeft"/>返回推荐</button></div>
  <p v-if="loading" class="explore-status" role="status">正在读取媒体推荐…</p>
  <section v-if="error" class="explore-empty" role="alert"><Icon name="CircleAlert"/><p>{{error}}</p><RouterLink class="btn" to="/rules/assist">TMDB配置</RouterLink><button class="btn" @click="load">重新加载</button></section>
  <section v-for="section in sections" v-show="!error" :key="section.id" class="recommendation-section" :aria-label="section.title">
   <header><h2>{{section.title}}</h2><div v-if="!group&&!query" class="recommendation-actions"><button class="icon-btn" :aria-label="`向前浏览${section.title}`" @click="scrollShelf($event,-1)"><Icon name="ChevronLeft"/></button><button class="icon-btn" :aria-label="`向后浏览${section.title}`" @click="scrollShelf($event,1)"><Icon name="ChevronRight"/></button><button class="text-btn" @click="more(section)">更多<Icon name="ChevronRight" :size="14"/></button></div></header>
   <div :class="group||query?'recommendation-grid':'recommendation-shelf'"><DiscoveryPoster v-for="media in section.items" :key="media.kind+media.id" :media="media" @open="open" @search="searchResources" @subscribe="subscribe"/></div>
   <p v-if="!loading&&!section.items.length" class="explore-status">暂无匹配的媒体</p>
  </section>
  <footer v-if="(group||query)&&(sections[0]?.pages||1)>1" class="explore-pagination"><button class="btn" :disabled="page<=1||loading" @click="pageInput=String(page-1);jump()">上一页</button><span>{{page}} / {{sections[0].pages}}</span><button class="btn" :disabled="page>=sections[0].pages||loading" @click="pageInput=String(page+1);jump()">下一页</button><input v-model="pageInput" type="number" min="1" :max="sections[0].pages" aria-label="推荐跳转页码" @change="jump" @keydown.enter.prevent="jump"/></footer>
 </template>
 <template v-else>
  <p v-if="loading" class="explore-status" role="status">正在读取媒体详情…</p><p v-if="error" class="error-message" role="alert">{{error}}</p>
  <template v-if="detail">
   <section class="discovery-hero"><img v-if="detail.backdrop" class="discovery-backdrop" :src="detail.backdrop" alt="" decoding="async" referrerpolicy="no-referrer"/><div class="discovery-hero-content"><PosterImage class="discovery-detail-poster" :src="detail.poster" :title="detail.title" :kind="detail.kind"/><div class="discovery-title"><h1>{{detail.title}} <small>({{detail.year}})</small></h1><p>{{detail.runtime?detail.runtime+' 分钟 | ':''}}{{detail.genres?.join('、')}}</p></div><div class="discovery-hero-actions"><button class="btn discovery-search" @click="searchResources(detail)"><Icon name="Search"/>搜索资源</button><button class="btn discovery-subscribe" :disabled="subscribing" @click="subscribe(detail)"><Icon name="Heart"/>添加订阅</button></div></div></section>
   <div class="discovery-detail-layout"><section class="discovery-overview"><h2>简介</h2><p>{{detail.overview||'暂无简介'}}</p><div class="discovery-crew"><div v-for="(person,i) in detail.crew||[]" :key="i"><strong>{{person.role}}</strong><span>{{person.name}}</span></div></div><div class="discovery-external"><a :href="`https://www.themoviedb.org/${detail.kind}/${detail.id}`" target="_blank" rel="noopener noreferrer"><Icon name="Link" :size="16"/>TheMovieDb</a><a v-if="/^tt[0-9]+$/.test(detail.imdb||'')" :href="'https://www.imdb.com/title/'+detail.imdb" target="_blank" rel="noopener noreferrer"><Icon name="Link" :size="16"/>IMDb</a></div></section><aside class="discovery-facts"><div class="discovery-rating" :aria-label="'评分 '+Number(detail.score||0).toFixed(1)"><Icon v-for="i in 10" :key="i" name="Star" :class="{rated:i<=Math.round(detail.score||0)}"/><strong>{{Number(detail.score||0).toFixed(1)}}</strong></div><dl><div v-for="[label,value] in [['TMDB ID',detail.id],['原始标题',detail.originalTitle],['状态',detail.status],['上映日期',detail.release],['原始语言',detail.language],['出品国家',detail.countries?.join(' / ')],['制作公司',detail.companies?.join(' / ')]]" :key="label"><dt>{{label}}</dt><dd>{{value||'—'}}</dd></div></dl></aside></div>
   <section v-if="detail.people?.length||detail.cast?.length" class="recommendation-section discovery-people"><header><h2>演员阵容</h2></header><div class="discovery-person-shelf"><article v-for="(person,i) in detail.people?.length?detail.people:detail.cast.map(name=>({name}))" :key="i"><img v-if="person.photo" :src="person.photo" :alt="person.name" loading="lazy" referrerpolicy="no-referrer"/><span v-else class="person-fallback"><Icon name="UserRound" :size="36"/></span><p>{{person.name}}</p><small>{{person.role||'演员'}}</small></article></div></section>
   <section v-for="[label,items] in [['推荐',detail.recommendations],['类似',detail.similar]]" v-show="items?.length" :key="label" class="recommendation-section"><header><h2>{{label}}</h2></header><div class="recommendation-shelf"><DiscoveryPoster v-for="media in items" :key="media.kind+media.id" :media="media" @open="open" @search="searchResources" @subscribe="subscribe"/></div></section>
  </template>
 </template>
 <MediaSubscriptionEditor v-if="editing" :media="editing" :targets="targets" @close="editing=null" @saved="editing=null"/>
 <Modal v-if="resourceSearch" standard title="搜索资源" :eyebrow="resourceSearch.media.title" @close="resourceSearch=null"><div class="modal-body"><p role="status">{{resourceSearch.loading?'正在读取资源来源…':resourceSearch.error||'资源来源尚未接入，配置来源后可在这里搜索对应云盘资源。'}}</p><RouterLink class="btn" to="/subscriptions/sources" @click="resourceSearch=null">资源来源</RouterLink></div><footer class="modal-footer"><button class="btn" @click="resourceSearch=null">关闭</button></footer></Modal>
</template>
