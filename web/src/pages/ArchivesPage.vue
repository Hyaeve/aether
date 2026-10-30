<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, notify } from '../lib'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import RoundedSelect from '../components/RoundedSelect.vue'
import ArchiveScrapeRow from '../components/ArchiveScrapeRow.vue'
import PageJumpInput from '../components/PageJumpInput.vue'
import { noticeTime } from '../library-notices'
import { archiveStatuses, archiveTriggers, archiveKinds, archiveStatusClass } from '../archive-display'
import '../archives.css'

const props = defineProps({ section: String })
const mode = ref('list'), query = ref(''), status = ref(''), trigger = ref('')
const result = ref({items:[],total:0,page:1,pages:1}), loading = ref(false), loaded = ref(false), jumpPage = ref(1)
const groups = ref({}), retry = ref(null), submitting = ref(false), scrapeRunning = ref(false)
const retryTask = ref('')
const statusOptions = computed(() => [{value:'',label:'全部状态'}, ...['success','doubt','failed','running','interrupted','cancelled'].filter(s => props.section === 'scrape' || s !== 'doubt').map(value=>({value,label:archiveStatuses[value]}))])
let alive=true, request=0, timer, searchTimer
function parameters(page=1) {
  return new URLSearchParams({page:String(page),q:query.value.trim(),status:status.value,trigger:trigger.value,mode:mode.value})
}
async function load(page=1, polling=false) {
  clearTimeout(timer)
  if(props.section==='organize') return
  const generation=++request, section=props.section
  if(!polling) loading.value=true
  try {
    const data=await api(`/archives/${section}?${parameters(page)}`)
    if(!alive || generation!==request) return
    result.value=data;loaded.value=true
    if(document.activeElement?.getAttribute('aria-label')!=='跳转页码') jumpPage.value=data.page
    if(section==='scrape') {
      const progress=await api('/strm-scrape/status')
      if(alive && generation===request) {
        scrapeRunning.value=!!progress.running
        if(retryTask.value && progress.taskId===retryTask.value && !progress.running) {
          retryTask.value=''
          notify(progress.message || '识别刮削已结束',!!progress.error || progress.failed>0 || progress.unmatched>0)
        }
      }
    }
  } catch(e) { if(alive && generation===request) notify(e.message,true) }
  finally {
    if(alive && generation===request) {
      loading.value=false
      timer=setTimeout(()=>load(result.value.page,true),5000)
    }
  }
}
async function loadGroup(group,page=1) {
  const ticket=Symbol()
  groups.value={...groups.value,[group.id]:{...groups.value[group.id],loading:true,ticket}}
  try {
    const params=parameters(page);params.set('mode','list');params.set('group',group.id)
    const data=await api(`/archives/scrape?${params}`)
    if(alive && props.section==='scrape' && groups.value[group.id]?.ticket===ticket) groups.value={...groups.value,[group.id]:{...data,loading:false}}
  } catch(e) {
    if(alive && groups.value[group.id]?.ticket===ticket) { delete groups.value[group.id]; notify(e.message,true) }
  }
}
function toggleGroup(group) {
  if(groups.value[group.id]) { delete groups.value[group.id]; return }
  groups.value={}
  loadGroup(group)
}
function changePage(page) {
  const next=Math.min(Math.max(1,Math.trunc(Number(page))||1),result.value.pages)
  jumpPage.value=next
  groups.value={};load(next)
}
async function confirmRetry() {
  if(!retry.value || submitting.value) return
  const item=retry.value
  submitting.value=true
  try {
    await api(`/archives/scrape/${encodeURIComponent(item.id)}/retry`,'POST',{confirmed:true})
    if(!alive || props.section!=='scrape' || retry.value?.id!==item.id) return
    retry.value=null;scrapeRunning.value=true;retryTask.value=item.taskId
    const characters=Array.from(item.filename || '')
    const name=characters.slice(0,48).join('')+(characters.length>48?'…':'')
    groups.value={};notify(`正在重新识别刮削：${name}`);load(result.value.page)
  } catch(e) { if(alive) notify(e.message,true) }
  finally { if(alive) submitting.value=false }
}
function reset() { request++;groups.value={};result.value={items:[],total:0,page:1,pages:1};loaded.value=false;load() }
watch(()=>props.section,()=>{clearTimeout(searchTimer);retry.value=null;query.value='';status.value='';trigger.value='';reset()},{immediate:true})
watch([mode,status,trigger],reset)
watch(query,()=>{clearTimeout(searchTimer);searchTimer=setTimeout(reset,250)})
onUnmounted(()=>{alive=false;request++;clearTimeout(timer);clearTimeout(searchTimer)})
</script>
<template>
  <nav v-tab-scroll class="content-tabs" aria-label="档案中心栏目">
    <RouterLink to="/archives/organize" :class="{active:section==='organize'}"><Icon name="FolderTree" />整理档案</RouterLink>
    <RouterLink to="/archives/scrape" :class="{active:section==='scrape'}"><Icon name="ScanSearch" />刮削档案</RouterLink>
    <RouterLink to="/archives/tasks" :class="{active:section==='tasks'}"><Icon name="FileClock" />任务档案</RouterLink>
  </nav>
  <section v-if="section==='organize'" class="archive-empty workspace-empty" aria-label="整理档案"><Icon name="FolderTree" :size="32" /><h2>整理档案</h2><span class="muted">暂未实装</span></section>
  <section v-else class="archive-panel" :aria-label="section==='scrape'?'刮削档案':'任务档案'">
    <div class="archive-toolbar">
      <div class="search-field"><Icon name="Search" /><input v-model="query" aria-label="搜索档案" placeholder="搜索名称或原文件" /></div>
      <RoundedSelect v-model="status" label="档案状态" :options="statusOptions" />
      <RoundedSelect v-model="trigger" label="触发来源" :options="[{value:'',label:'全部来源'},...Object.entries(archiveTriggers).map(([value,label])=>({value,label}))]" />
      <div v-if="section==='scrape'" class="archive-view" role="group" aria-label="档案视图"><button class="icon-btn" aria-label="列表模式" data-tooltip="列表模式" data-tooltip-always :aria-pressed="mode==='list'" @click="mode='list'"><Icon name="List" /></button><button class="icon-btn" aria-label="分组模式" data-tooltip="分组模式" data-tooltip-always :aria-pressed="mode==='group'" @click="mode='group'"><Icon name="FolderTree" /></button></div>
      <span class="archive-count" role="status">{{ result.total }} {{ section==='scrape'&&mode==='group'?'组':'项' }}</span>
      <button class="icon-btn" aria-label="刷新档案" :disabled="loading" @click="groups={};load(result.page)"><Icon name="RefreshCw" :class="{spin:loading}" /></button>
    </div>
    <div v-if="section==='scrape' && mode==='list'" class="archive-columns" aria-hidden="true"><span>影视 / 原文件名称</span><span>触发来源</span><span>执行时间</span><span>操作状态</span><span>操作</span></div>
    <div v-if="!result.items.length" class="small-empty">{{ loading&&!loaded?'正在读取档案…':loaded?'暂无档案':'档案读取失败，可刷新重试' }}</div>
    <div v-else-if="section==='scrape' && mode==='list'" role="list" aria-label="刮削文件"><ArchiveScrapeRow v-for="item in result.items" :key="item.id" :item="item" :disabled="scrapeRunning || submitting" @retry="retry=$event" /></div>
    <div v-else-if="section==='scrape'" class="archive-groups">
      <div v-for="group in result.items" :key="group.id" class="archive-group">
        <button class="archive-group-toggle" :aria-expanded="!!groups[group.id]" @click="toggleGroup(group)"><Icon :name="groups[group.id]?'ChevronDown':'ChevronRight'" :size="17" /><Icon :name="group.kind==='tv'?'Tv':'Film'" /><span><strong :data-tooltip="group.title">{{ group.title || group.folder }}</strong><small :data-tooltip="`${group.taskName} / ${group.folder}`">{{ group.kind==='tv'?'剧集':'电影' }} · {{ group.taskName }} / {{ group.folder }}<template v-if="group.tmdb"> · TMDB {{ group.tmdb }}</template></small></span><b>{{ group.count }} 项</b><span class="archive-status" :class="archiveStatusClass(group.status)">{{ archiveStatuses[group.status] || '未知状态' }}</span></button>
        <div v-if="groups[group.id]" class="archive-group-content" role="list" :aria-label="`${group.title}的文件`">
          <p v-if="groups[group.id].loading" class="small-empty">正在读取…</p>
          <ArchiveScrapeRow v-for="item in groups[group.id].items || []" :key="item.id" :item="item" :disabled="scrapeRunning || submitting" @retry="retry=$event" />
          <div v-if="groups[group.id].pages>1" class="archive-group-pages"><button class="icon-btn" aria-label="组内上一页" :disabled="groups[group.id].loading || groups[group.id].page<=1" @click="loadGroup(group,groups[group.id].page-1)"><Icon name="ChevronLeft" /></button><span>{{groups[group.id].page}} / {{groups[group.id].pages}}</span><button class="icon-btn" aria-label="组内下一页" :disabled="groups[group.id].loading || groups[group.id].page>=groups[group.id].pages" @click="loadGroup(group,groups[group.id].page+1)"><Icon name="ChevronRight" /></button></div>
        </div>
      </div>
    </div>
    <div v-else role="list" aria-label="任务执行记录">
      <div class="archive-task-columns" aria-hidden="true"><span>任务名称 / 类型</span><span>触发来源</span><span>执行时间</span><span>执行结果</span></div>
      <article v-for="item in result.items" :key="item.id" class="archive-task-row" role="listitem"><div class="archive-task-name"><Icon name="ListTodo" /><span><strong :data-tooltip="item.taskName">{{ item.taskName }}</strong><small>{{ archiveKinds[item.taskKind] || item.taskKind }} · {{ item.processed || 0 }} {{item.taskKind==='automation'?'步':'项'}}</small></span></div><span class="archive-trigger">{{ archiveTriggers[item.trigger] || '未知来源' }}</span><span class="archive-task-time"><time :datetime="item.startedAt">{{ noticeTime(item.startedAt) }}</time><small v-if="item.finishedAt">结束 {{ noticeTime(item.finishedAt) }}</small></span><span class="archive-status" :class="archiveStatusClass(item.status)" :data-tooltip="item.message" data-tooltip-always><Icon :name="item.status==='success'?'CircleCheck':'CircleAlert'" :size="15" />{{ archiveStatuses[item.status] || '未知状态' }}</span></article>
    </div>
    <form class="archive-pagination" aria-label="档案分页" @submit.prevent="changePage(jumpPage)"><button type="button" class="icon-btn" aria-label="第一页" :disabled="loading || result.page===1" @click="changePage(1)"><Icon name="ChevronsLeft" /></button><button type="button" class="icon-btn" aria-label="上一页" :disabled="loading || result.page===1" @click="changePage(result.page-1)"><Icon name="ChevronLeft" /></button><span>{{ result.page }} / {{ result.pages }}</span><button type="button" class="icon-btn" aria-label="下一页" :disabled="loading || result.page===result.pages" @click="changePage(result.page+1)"><Icon name="ChevronRight" /></button><button type="button" class="icon-btn" aria-label="最后一页" :disabled="loading || result.page===result.pages" @click="changePage(result.pages)"><Icon name="ChevronsRight" /></button><PageJumpInput v-model="jumpPage" :pages="result.pages" /><button class="icon-btn" aria-label="跳转到指定页" :disabled="loading"><Icon name="CornerDownLeft" /></button></form>
  </section>
  <Modal v-if="retry" title="重新识别刮削" compact confirmation @close="!submitting && (retry=null)"><div class="modal-body"><p>重新识别并刮削「{{ retry.filename }}」？元数据按当前刮削设置补缺或覆盖，原 STRM 保留。</p></div><footer class="modal-footer"><button class="btn primary" :disabled="submitting" @click="confirmRetry">确认识别</button><button class="btn" :disabled="submitting" @click="retry=null">取消</button></footer></Modal>
</template>
