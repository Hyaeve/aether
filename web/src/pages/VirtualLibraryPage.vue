<script setup>
import { computed, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, bytes, notify, state } from '../lib'
import Icon from '../components/Icon.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import ProviderLineIcon from '../components/ProviderLineIcon.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import LocalDirectoryPicker from '../components/LocalDirectoryPicker.vue'
const route=useRoute(),router=useRouter()
const tabs=[{id:'115',name:'115链库'},{id:'mobile',name:'移动链库'},{id:'quark',name:'夸克链库'},{id:'cas',name:'CAS链库',icon:'Box'}]
const kind=computed(()=>route.path.split('/').at(-1)==='virtual'?'115':route.path.split('/').at(-1))
const pools=computed(()=>state.storages.filter(s=>s.type==='115'&&s.enabled))
const storage=ref(pools.value[0]?.id||''),link=ref(''),directory=ref(''),picker=ref(false),preview=ref(null),busy=ref(false),error=ref('')
let alive=true
onUnmounted(()=>{alive=false})
async function parse(){
 busy.value=true;error.value='';preview.value=null
 try{const result=await api('/virtual-library/115/preview','POST',{storageId:storage.value,url:link.value});if(alive)preview.value=result}catch(e){if(alive)error.value=e.message}finally{if(alive)busy.value=false}
}
async function generate(){
 busy.value=true;error.value=''
 try{const result=await api('/virtual-library/115/generate','POST',{storageId:storage.value,preview:preview.value.preview,directory:directory.value});if(alive){notify(`生成 ${result.generated} 个 STRM，跳过 ${result.skipped} 个已有文件`);preview.value=null}}catch(e){if(alive)error.value=e.message}finally{if(alive)busy.value=false}
}
</script>
<template>
 <section class="task-heading"><nav v-tab-scroll class="content-tabs" aria-label="虚拟链库栏目"><RouterLink v-for="tab in tabs" :key="tab.id" :to="`/virtual/${tab.id}`" :class="{active:kind===tab.id}"><Icon v-if="tab.icon" :name="tab.icon" :size="19" /><ProviderLineIcon v-else :type="tab.id" />{{ tab.name }}</RouterLink></nav><button class="btn" @click="router.push('/tasks/ed2k/pool')"><Icon name="Settings2" />存储绑定池</button></section>
 <section v-if="kind==='115'" class="virtual-library-panel"><form @submit.prevent="parse"><div class="virtual-fields"><label>解析存储池<RoundedSelect v-model="storage" label="解析存储池" :disabled="busy" :options="pools.map(s=>({value:s.id,label:s.name}))" /></label><label>生成目录<button type="button" class="source-trigger" :disabled="busy" @click="picker=true"><span>{{ directory || '选择容器目录' }}</span><Icon name="FolderOpen" /></button></label></div><label>115分享链接<textarea v-model="link" rows="3" :disabled="busy" required @input="preview=null" /></label><div class="virtual-actions"><p v-if="error" class="error-message" role="alert">{{ error }}</p><button class="btn" :disabled="busy || !storage || !link"><Icon name="ScanSearch" />{{ busy?'处理中…':'解析' }}</button><button type="button" class="btn primary" :disabled="busy || !directory || !preview?.files.length" @click="generate"><Icon name="FileSymlink" />生成 STRM</button></div></form><div v-if="preview" class="virtual-results"><header>{{ preview.files.length }} 项</header><div v-for="item in preview.files" :key="item.path"><Icon name="FileSymlink" /><span><strong>{{ item.path }}</strong><small>{{ item.content }}</small></span><span>{{ bytes(item.size) }}</span></div></div></section>
 <section v-else class="virtual-library-panel virtual-planned"><Icon v-if="kind==='cas'" name="Box" :size="27" /><ProviderIcon v-else :type="kind" /><span>尚未开放</span></section>
 <LocalDirectoryPicker v-if="picker" :initial="directory" @close="picker=false" @select="directory=$event;picker=false" />
</template>
<style scoped>
.virtual-library-panel { border:1px solid var(--border);border-radius:8px;padding:20px;background:var(--surface);min-height:calc(100dvh - 185px);box-sizing:border-box; }
.virtual-fields { display:grid;grid-template-columns:1fr 1fr;gap:20px; }.virtual-library-panel label { display:grid;gap:8px;margin-bottom:16px;min-width:0; }.virtual-library-panel textarea { width:100%;resize:vertical; }
.virtual-actions { display:flex;justify-content:flex-end;gap:10px;align-items:center; }.virtual-actions p { margin-right:auto; }
.virtual-results { margin-top:20px;border-top:1px solid var(--border); }.virtual-results header { padding:12px;color:var(--muted); }.virtual-results > div { display:flex;align-items:center;gap:12px;padding:12px;border-top:1px solid var(--border); }.virtual-results > div > span:first-of-type { flex:1;min-width:0; }.virtual-results strong,.virtual-results small { display:block;overflow-wrap:anywhere; }.virtual-results small { color:var(--muted);font-size:12px;margin-top:5px; }.virtual-results > div > span:last-child { white-space:nowrap; }
.virtual-planned { display:flex;align-items:center;justify-content:center;gap:12px;color:var(--muted); }
:global([data-theme=dark] .virtual-library-panel .source-trigger),:global([data-theme=dark] .virtual-library-panel .rounded-select-trigger) { background:var(--input);color:var(--text); }
@media(max-width:600px){.virtual-fields { grid-template-columns:1fr;gap:0; }}
</style>
