<script setup>
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import Icon from '../components/Icon.vue'
import ProviderLineIcon from '../components/ProviderLineIcon.vue'
import QuarkLibrary from '../components/QuarkLibrary.vue'
const route=useRoute()
const tabs=[{id:'115',name:'115链库'},{id:'mobile',name:'移动链库'},{id:'quark',name:'夸克链库'},{id:'cas',name:'CAS链库',icon:'Box'}]
const kind=computed(()=>route.path.split('/')[2]||'115')
const quark=ref(null)
</script>
<template>
 <section v-if="!route.path.endsWith('/settings')" class="task-heading"><nav v-tab-scroll class="content-tabs" aria-label="虚拟链库栏目"><RouterLink v-for="tab in tabs" :key="tab.id" :to="`/virtual/${tab.id}`" :class="{active:kind===tab.id}"><Icon v-if="tab.icon" :name="tab.icon" :size="19" /><ProviderLineIcon v-else :type="tab.id" />{{tab.name}}</RouterLink></nav><button class="btn primary" @click="quark?.open()"><Icon name="Plus"/>添加链接</button></section>
 <QuarkLibrary :key="kind" ref="quark" :provider="kind"/>
</template>
<style scoped>
.virtual-library-panel { border:1px solid var(--border);border-radius:8px;padding:20px;background:var(--surface);min-height:calc(100dvh - 185px);box-sizing:border-box; }
.virtual-fields { display:grid;grid-template-columns:1fr 1fr;gap:20px; }.virtual-library-panel label { display:grid;gap:8px;margin-bottom:16px;min-width:0; }.virtual-library-panel textarea { width:100%;resize:vertical; }
.virtual-actions { display:flex;justify-content:flex-end;gap:10px;align-items:center; }.virtual-actions p { margin-right:auto; }
.virtual-default-hint{font-size:12px;line-height:1.7;margin:14px 0 0}.virtual-actions{flex-wrap:wrap}
.virtual-results { margin-top:20px;border-top:1px solid var(--border); }.virtual-results header { padding:12px;color:var(--muted); }.virtual-results > div { display:flex;align-items:center;gap:12px;padding:12px;border-top:1px solid var(--border); }.virtual-results > div > span:first-of-type { flex:1;min-width:0; }.virtual-results strong,.virtual-results small { display:block;overflow-wrap:anywhere; }.virtual-results small { color:var(--muted);font-size:12px;margin-top:5px; }.virtual-results > div > span:last-child { white-space:nowrap; }
.virtual-planned { display:flex;align-items:center;justify-content:center;gap:12px;color:var(--muted); }
:global([data-theme=dark] .virtual-library-panel .source-trigger),:global([data-theme=dark] .virtual-library-panel .rounded-select-trigger) { background:var(--input);color:var(--text); }
@media(max-width:600px){.virtual-fields { grid-template-columns:1fr;gap:0; }}
</style>
