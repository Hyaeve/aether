<script setup>
import {ref,watch,onBeforeUnmount} from 'vue'
import Icon from '../components/Icon.vue'
import PluginSettings from '../components/PluginSettings.vue'
import {api,notify} from '../lib'
import {readPlugin,invalidatePlugin} from '../plugin-config'
import '../plugin-cards.css'
const props=defineProps({section:{default:'naming'}})
const tabs=[{id:'naming',name:'命名规则',icon:'FolderTree'},{id:'categories',name:'二级分类',icon:'Tags'},{id:'upgrade',name:'洗版策略',icon:'MediaUpgrade'},{id:'assist',name:'辅助配置',icon:'Settings2'}]
const placeholders={naming:[{name:'整理规则',icon:'FolderTree',detail:'媒体命名模板与目录结构'},{name:'识别规则',icon:'ListFilter',detail:'最小视频、整理黑名单、自定义识别词、自定义匹配'}],categories:[{name:'二级分类',icon:'Tags',detail:'按媒体类型、地区和分类归档文件'}],upgrade:[{name:'洗版策略',icon:'MediaUpgrade',detail:'根据画质与版本偏好替换已有媒体'}]}
const configs=[{name:'TMDB 配置',kind:'tmdb',icon:'TMDB',detail:'配置影视元数据接口、图片域名与语言偏好'},{name:'AI 辅助识别',kind:'ai',icon:'BrainCircuit',detail:'通过兼容模型接口识别媒体名称与季集信息'}]
const selected=ref(null),states=ref({}),busy=ref(false),error=ref('')
let generation=0
onBeforeUnmount(()=>generation++)
watch(()=>props.section,async section=>{
 const stamp=++generation;selected.value=null;error.value=''
 if(section!=='assist')return
 await Promise.all(configs.map(async config=>{try{const value=await readPlugin(config.kind,true);if(stamp===generation)states.value[config.kind]=value.enabled}catch(e){if(stamp===generation)error.value=e.message}}))
},{immediate:true})
async function toggle(config){
 if(busy.value)return;busy.value=true;const stamp=generation
 try{const current=await readPlugin(config.kind,true);await api(`/plugins/${config.kind}`,'PUT',{...current,enabled:!current.enabled});invalidatePlugin(config.kind);if(stamp===generation)states.value[config.kind]=!current.enabled;notify(`${current.enabled?'已停用':'已启用'} ${config.name}`)}catch(e){notify(e.message,true)}finally{busy.value=false}
}
</script>
<template>
 <nav v-tab-scroll class="content-tabs" aria-label="以太识律栏目"><RouterLink v-for="tab in tabs" :key="tab.id" :to="`/rules/${tab.id}`" :class="{active:section===tab.id}"><Icon :name="tab.icon" />{{tab.name}}</RouterLink></nav>
 <section v-if="section!=='assist'" class="rules-panels" :aria-label="tabs.find(tab=>tab.id===section)?.name">
  <article v-for="item in placeholders[section]||[]" :key="item.name" class="rule-placeholder" :aria-label="item.name"><Icon :name="item.icon" :size="28"/><div><h2>{{item.name}}</h2><p>{{item.detail}}</p></div><span class="status pending">待实装</span></article>
 </section>
 <section v-else class="rules-panels" aria-label="辅助配置">
  <article v-for="config in configs" :key="config.kind" class="rule-config-card" role="button" tabindex="0" :aria-label="config.name" @click="selected=config" @keydown.enter.self="selected=config" @keydown.space.self.prevent="selected=config">
   <button class="plugin-symbol" :aria-label="`${states[config.kind]?'停用':'启用'}${config.name}`" :aria-pressed="!!states[config.kind]" :disabled="busy||typeof states[config.kind]!=='boolean'" @click.stop="toggle(config)"><Icon :name="config.icon" :size="28"/></button>
   <div><h2>{{config.name}}</h2><p>{{config.detail}}</p></div><Icon name="ChevronRight" class="rule-config-open" :size="18"/>
  </article><p v-if="error" class="error-message" role="alert">{{error}}</p>
 </section>
 <PluginSettings v-if="selected" :key="selected.kind" :kind="selected.kind" :title="selected.name" @close="selected=null" @changed="states[$event.kind]=$event.enabled"/>
</template>
<style scoped>
.rules-panels{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;align-items:start}
.rule-placeholder,.rule-config-card{display:flex;align-items:center;gap:16px;min-width:0;padding:22px;border:1px solid var(--border);border-radius:14px;background:var(--surface);min-height:116px}
.rule-placeholder>svg{color:var(--primary);flex:none}.rule-placeholder>div,.rule-config-card>div{flex:1;min-width:0}
.rules-panels h2{font-size:16px;line-height:24px;margin:0}.rule-placeholder p,.rule-config-card p{font-size:13px;line-height:22px;color:var(--muted);margin:7px 0 0;overflow-wrap:anywhere}.rule-placeholder>.status{flex:none}
.rule-config-card{cursor:pointer;transition:border-color .18s,background .18s}.rule-config-card:hover{border-color:var(--primary)}.rule-config-card:focus-visible{outline:2px solid var(--primary);outline-offset:3px}
.rule-config-card>.plugin-symbol{width:44px;height:44px;flex:none;border:0;cursor:pointer;padding:0}.rule-config-card>.plugin-symbol[aria-pressed=false]{opacity:.55}.rule-config-card>.plugin-symbol:disabled{cursor:wait}.rule-config-open{flex:none;color:var(--muted)}
.rules-panels>.error-message{grid-column:1/-1}
@media(max-width:760px){.rules-panels{grid-template-columns:minmax(0,1fr)}.rule-placeholder,.rule-config-card{padding:18px;gap:12px}.rule-placeholder>.status{font-size:11px}}
@media(prefers-reduced-motion:reduce){.rule-config-card{transition:none}}
</style>
