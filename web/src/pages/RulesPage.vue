<script setup>
import Icon from '../components/Icon.vue'
import PluginSettings from '../components/PluginSettings.vue'
import RulePresetCards from '../components/RulePresetCards.vue'
import RecognitionRules from '../components/RecognitionRules.vue'
import '../plugin-cards.css'
const props=defineProps({section:{default:'recognition'}})
const tabs=[{id:'recognition',name:'识别规则',icon:'ListFilter'},{id:'naming',name:'命名规则',icon:'FolderTree'},{id:'categories',name:'媒体分类',icon:'Tags'},{id:'upgrade',name:'洗版策略',icon:'MediaUpgrade'},{id:'assist',name:'辅助配置',icon:'Settings2'}]
const configs=[{name:'TMDB 配置',kind:'tmdb'},{name:'AI 辅助识别',kind:'ai'}]
</script>
<template>
 <nav v-tab-scroll class="content-tabs" aria-label="以太识律栏目"><RouterLink v-for="tab in tabs" :key="tab.id" :to="`/rules/${tab.id}`" :class="{active:section===tab.id}"><Icon :name="tab.icon" />{{tab.name}}</RouterLink></nav>
 <RecognitionRules v-if="section==='recognition'"/>
 <RulePresetCards v-else-if="['naming','categories','upgrade'].includes(section)" :key="section" :kind="section"/>
 <section v-else class="rules-panels" aria-label="辅助配置">
  <PluginSettings v-for="config in configs" :key="config.kind" :kind="config.kind" :title="config.name" embedded/>
 </section>
</template>
<style scoped>
.rules-panels{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;height:calc(100dvh - 184px);min-height:0}
@media(max-width:760px){.rules-panels{grid-template-columns:minmax(0,1fr);height:auto;min-height:0}}
</style>
