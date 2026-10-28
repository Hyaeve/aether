<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import Icon from '../components/Icon.vue'
import Modal from '../components/Modal.vue'
import SecretInput from '../components/SecretInput.vue'
import QuarkTakeover from '../components/QuarkTakeover.vue'
import StrmReplace from '../components/StrmReplace.vue'
import Pan115Simulcast from '../components/Pan115Simulcast.vue'
import RecycleCleanup from '../components/RecycleCleanup.vue'
import FolderCleanup from '../components/FolderCleanup.vue'
import Pan115Signin from '../components/Pan115Signin.vue'
import ProviderIcon from '../components/ProviderIcon.vue'
import Rapid115Icon from '../components/Rapid115Icon.vue'
import { useRouter } from 'vue-router'
import PluginSettings from '../components/PluginSettings.vue'
import { api, notify } from '../lib'
import { readPlugin, invalidatePlugin } from '../plugin-config'
import { refreshReplacementNotices } from '../replacement-notices'
import PluginMarket from '../components/PluginMarket.vue'
import PluginSearch from '../components/PluginSearch.vue'
import '../plugin-cards.css'
import '../plugin-tasks.css'
const props = defineProps({ section: { default: 'tools' } })
const query = ref('')
const busy = ref(false)
const cleanup=ref({enabled:false,rules:[]}),simulcastOn=ref(true)
const folderCleanup=ref({enabled:false,rules:[]}),signin=ref({enabled:false,rules:[]}),enableFolder=ref(false)
onMounted(()=>Promise.all([['/115-folder-cleanup',folderCleanup],['/115-signin',signin]].map(async([path,target])=>{try{target.value=await api(path)}catch{}})))
async function toggleExtra115(which,confirmed=false){
 if(busy.value)return
 const target=which==='folder'?folderCleanup:signin,path=which==='folder'?'/115-folder-cleanup':'/115-signin'
 busy.value=true
 try{
  const saved=await api(path)
  if(which==='folder'&&confirmed&&JSON.stringify(saved)!==JSON.stringify(folderCleanup.value)){folderCleanup.value=saved;notify('清理配置已变化，请重新确认',true);return}
  target.value=saved
  if(which==='folder'&&!saved.enabled&&!confirmed){enableFolder.value=true;return}
  target.value=await api(path,'PUT',{...saved,enabled:confirmed?true:!saved.enabled,...(which==='folder'?{confirmed:true}:{})})
  enableFolder.value=false
  notify(`${target.value.enabled?'已启用':'已停用'}${which==='folder'?'115文件夹清理':'115签到'}`)
 }catch(e){notify(e.message,true)}finally{busy.value=false}
}
onMounted(async()=>{try{cleanup.value=await api('/115-recycle-cleanup');simulcastOn.value=(await api('/115-simulcast/enabled')).enabled}catch{}})
async function toggle115(which){if(busy.value)return;busy.value=true;try{if(which==='cleanup'){await api('/115-recycle-cleanup','PUT',{...cleanup.value,enabled:!cleanup.value.enabled});cleanup.value.enabled=!cleanup.value.enabled;notify(cleanup.value.enabled?'已启用115回收站清理':'已停用115回收站清理')}else{await api('/115-simulcast/enabled','PUT',{enabled:!simulcastOn.value});simulcastOn.value=!simulcastOn.value;notify(simulcastOn.value?'已启用115同播复制':'已停用115同播复制')}}catch(e){notify(e.message,true)}finally{busy.value=false}}
const takeover = ref({ enabled: false, bindings: [] })
const pluginStates = ref({})
async function togglePlugin(tool) {
  busy.value = true
  try {
    const current = await readPlugin(tool.kind, true)
    await api(`/plugins/${tool.kind}`, 'PUT', { ...current, enabled: !current.enabled })
    invalidatePlugin(tool.kind)
    pluginStates.value[tool.kind] = !current.enabled
    notify(`${current.enabled ? '已停用' : '已启用'} ${tool.name}`)
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
onMounted(() => Promise.all(['emby', 'tmdb', 'ai', 'proxy'].map(async kind => {
  try { pluginStates.value[kind] = (await readPlugin(kind, true)).enabled } catch (e) { notify(e.message, true) }
})))
async function loadTakeover() {
  try { takeover.value = await api('/quark-takeover') } catch (e) { notify(e.message, true) }
}
async function toggleTakeover() {
  if (!takeover.value.bindings.length) { selected.value = tools.find(t => t.name === '夸克 STRM 接管'); return }
  busy.value = true
  try {
    await api('/quark-takeover', 'PUT', { enabled: !takeover.value.enabled })
    await loadTakeover()
    notify(takeover.value.enabled ? '已启用夸克 STRM 接管' : '已停用夸克 STRM 接管')
  } catch (e) { notify(e.message, true) } finally { busy.value = false }
}
onMounted(loadTakeover)
const flow = ref(''), password = ref(''), backupFile = ref(null), importStep = ref(1)
function start(mode) { flow.value = mode; password.value = ''; backupFile.value = null; importStep.value = 1 }
async function restore() {
  busy.value = true
  try {
    const data = new FormData(); data.append('file', backupFile.value); data.append('password', password.value)
    const res = await fetch('/api/config/import', { method: 'POST', credentials: 'same-origin', body: data })
    const result = await res.json()
    if (!res.ok) throw new Error(result.error || '导入失败')
    notify(result.message); flow.value = ''; selected.value = null
  } catch (e) { notify(e.message, true) } finally { busy.value = false; password.value = '' }
}
async function backup() {
  busy.value = true
  try {
    const response = await fetch('/api/config/backup', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ password: password.value }) })
    if (!response.ok) throw new Error((await response.json()).error || '配置备份失败')
    const url = URL.createObjectURL(await response.blob()), anchor = document.createElement('a')
    anchor.href = url; anchor.download = `aether-config-${new Date().toISOString().slice(0, 10)}.aether`; anchor.click()
    setTimeout(() => URL.revokeObjectURL(url), 30000); notify('配置备份已导出'); selected.value = null; flow.value = ''
  } catch (e) { notify(e.message, true) } finally { busy.value = false; password.value = '' }
}
const selected = ref(null)
watch(() => props.section, () => { selected.value = null; flow.value = '' })
const router=useRouter()
function selectTool(tool){if(tool.name==='115 秒传')router.push('/tools/115-rapid');else selected.value=tool}
const tools = [
  {name:'115签到',icon:'CalendarCheck2',subtitle:'115 网盘 · 每日签到',detail:'按Cron计划为115账号签到，每个账号一条任务，每日回执防止重复提交'},
  {name:'115 秒传',icon:'Zap',subtitle:'115 网盘 · SHA1 秒传',detail:'通过 SHA1 哈希秒传本地文件到 115 网盘，无需实际上传文件数据'},
  { name: '115 同播复制', icon: 'Copy', subtitle: '115 网盘 · 多客户端播放', detail: '同一文件十分钟内出现不同播放客户端时，为后续客户端复制独立文件并获取直链' },
  { name:'115文件夹清理',icon:'FolderClock',subtitle:'115 网盘 · 文件夹清理',detail:'按Cron计划清理所选文件夹中超过阈值的文件及空子目录，保留所选文件夹' },
  { name:'115回收站清理',icon:'Trash2',subtitle:'115 网盘 · 定时清理',detail:'按计划清理115回收站中超过保留天数的项目' },
  { name: '夸克 STRM 接管', icon: 'ArrowLeftRight', subtitle: '夸克网盘 · TV 版 302 直链', detail: '接管本项目生成的夸克 STRM 播放请求，按绑定策略返回 TV 版 302 直链；画质、字幕及客户端兼容性受上游限制。' },
  { name: '整理规则', icon: 'FolderTree', subtitle: '媒体整理 · 命名与目录', file: 'organize-rules.json', detail: '配置媒体命名、目录结构和整理规则' },
  { name: '二级分类', icon: 'Tags', subtitle: '媒体整理 · 分类归档', file: 'categories.json', detail: '按媒体类型、地区和分类归档文件' },
  { name: '洗版策略', icon: 'MediaUpgrade', subtitle: '媒体整理 · 版本偏好', file: 'upgrade-policies.json', detail: '根据画质与版本偏好替换已有媒体' },
  { name: 'AI 辅助识别', kind: 'ai', icon: 'BrainCircuit', subtitle: '媒体识别 · 兼容模型接口', file: 'ai.json', detail: '通过 OpenAI 兼容模型识别媒体名称与季集信息' },
  { name: '识别规则', icon: 'ListFilter', subtitle: '媒体识别 · 匹配规则', file: 'recognition-rules.json', detail: '最小视频、整理黑名单、自定义识别词、自定义匹配' },
  { name: 'TMDB 配置', kind: 'tmdb', icon: 'TMDB', subtitle: '媒体元数据 · TMDB', detail: '配置影视元数据接口、图片域名与语言偏好' },
  { name: '代理配置', kind: 'proxy', icon: 'Network', subtitle: '网络连接 · 服务代理', detail: '管理 TMDB 与 AI 请求使用的网络代理' },
  { name: 'Emby 通知', kind: 'emby', icon: 'EmbyNotice', subtitle: '媒体服务 · 事件通知', detail: '接收 Emby 中勾选的入库、播放及系统事件' },
  { name: '配置备份', icon: 'FileArchive', subtitle: '系统配置 · 加密备份', detail: '加密导入导出系统、存储池、以链与规则配置' }
]
tools.splice(tools.length - 1, 0, { name: 'STRM 替换', icon: 'FilePenLine', subtitle: 'STRM 文件 · 内容替换', detail: '批量替换容器目录内 STRM 文件内容，保留文件名与目录结构' })
const results = computed(() => tools.filter(tool => `${tool.name} ${tool.subtitle} ${tool.detail}`.toLowerCase().includes(query.value.toLowerCase())).map(tool => ({ ...tool, iconName: tool.icon })))
</script>
<template>
  <nav v-tab-scroll class="content-tabs" aria-label="辅助工具栏目"><RouterLink to="/tools" :class="{ active: section === 'tools' }"><Icon name="Blocks" />我的插件</RouterLink><RouterLink to="/tools/market" :class="{ active: section === 'market' }"><Icon name="Puzzle" />插件市场</RouterLink></nav>
  <PluginMarket v-if="section === 'market'" />
  <div v-else class="plugin-grid">
    <article v-for="tool in tools" :key="tool.name" class="plugin-card" role="button" tabindex="0" :aria-label="tool.name" @click="selectTool(tool)" @keydown.enter.self="selectTool(tool)" @keydown.space.prevent.self="selectTool(tool)">
      <Rapid115Icon v-if="tool.name==='115 秒传'" /><button v-else-if="['115签到','115文件夹清理'].includes(tool.name)" class="plugin-symbol simulcast-symbol" :aria-label="`${(tool.name==='115签到'?signin.enabled:folderCleanup.enabled)?'停用':'启用'}${tool.name}`" :aria-pressed="tool.name==='115签到'?signin.enabled:folderCleanup.enabled" :disabled="busy" @click.stop="toggleExtra115(tool.name==='115签到'?'signin':'folder')"><ProviderIcon type="115" /><Icon :name="tool.icon" :size="15" /></button><button v-else-if="['115 同播复制','115回收站清理'].includes(tool.name)" class="plugin-symbol simulcast-symbol" :aria-label="`${(tool.name==='115 同播复制'?simulcastOn:cleanup.enabled)?'停用':'启用'}${tool.name}`" :aria-pressed="tool.name==='115 同播复制'?simulcastOn:cleanup.enabled" :disabled="busy" @click.stop="toggle115(tool.name==='115 同播复制'?'simulcast':'cleanup')"><ProviderIcon type="115" /><Icon :name="tool.icon" :size="15" /></button><button v-else-if="tool.name === '夸克 STRM 接管'" class="plugin-symbol quark-takeover-symbol" :aria-label="`${takeover.enabled ? '停用' : '启用'}夸克 STRM 接管`" :aria-pressed="takeover.enabled && takeover.bindings.length > 0" :disabled="busy" @click.stop="toggleTakeover"><ProviderIcon type="quark" /><Icon name="ArrowLeftRight" :size="14" /></button><button v-else-if="tool.kind" class="plugin-symbol plugin-toggle" :aria-label="`${pluginStates[tool.kind] ? '停用' : '启用'}${tool.name}`" :aria-pressed="!!pluginStates[tool.kind]" :disabled="busy" @click.stop="togglePlugin(tool)"><Icon :name="tool.icon" :size="26" /></button><span v-else class="plugin-symbol"><Icon :name="tool.icon" :size="26" /></span>
      <div class="plugin-heading"><strong>{{ tool.name }}</strong><small class="plugin-subtitle">{{ tool.subtitle }}</small></div>
      <small class="plugin-description" :data-tooltip="tool.detail">{{ tool.detail }}</small><span v-if="!tool.kind && !['配置备份', '夸克 STRM 接管', '115 同播复制', '115回收站清理', '115文件夹清理', '115签到', 'STRM 替换','115 秒传'].includes(tool.name)" class="status pending">待实现</span>
    </article>
  </div>
  <PluginSearch v-if="section === 'tools'" :items="results" @search="query = $event" @select="selectTool" />
  <QuarkTakeover v-if="selected?.name === '夸克 STRM 接管'" @close="selected = null" @changed="takeover = $event" />
  <Pan115Simulcast v-else-if="selected?.name === '115 同播复制'" @close="selected = null" />
  <FolderCleanup v-else-if="selected?.name === '115文件夹清理'" @close="selected=null" @changed="folderCleanup=$event" />
  <Pan115Signin v-else-if="selected?.name === '115签到'" @close="selected=null" @changed="signin=$event" />
      <RecycleCleanup v-else-if="selected?.name === '115回收站清理'" @close="selected=null" @changed="cleanup=$event" />
  <StrmReplace v-else-if="selected?.name === 'STRM 替换'" @close="selected = null" @started="refreshReplacementNotices" />
  <PluginSettings v-else-if="selected?.kind" :key="selected.kind" :kind="selected.kind" :title="selected.name" @close="selected = null" @changed="pluginStates[$event.kind] = $event.enabled" />
  <Modal v-else-if="selected" :title="selected.name" @close="selected = null">
    <template v-if="selected.name === '配置备份'"><div class="modal-body"><p>备份包含账号、存储池、以链和规则配置，使用你设置的密码加密。导入后需重启容器，并使用备份中的账号登录。</p></div><footer class="modal-footer backup-actions"><button class="btn" @click="start('import')"><Icon name="ArchiveRestore" />导入配置备份</button><button class="btn primary" @click="start('export')"><Icon name="Download" />导出配置备份</button></footer></template>
    <div v-else class="modal-body"><p>该插件尚未实现，当前不能启用或执行。</p><p v-if="selected.detail">{{ selected.detail }}</p><code v-if="selected.file">/config/organize/{{ selected.file }}</code></div>
  </Modal>
  <Modal v-if="flow" :title="flow === 'export' ? '加密导出' : '导入配置'" compact @close="!busy && (flow = '')">
    <form @submit.prevent="flow === 'export' ? backup() : importStep === 1 ? (importStep = 2) : restore()">
      <div class="modal-body"><label v-if="flow === 'import' && importStep === 1">选择备份文件<input type="file" accept=".aether" required @change="backupFile = $event.target.files[0]" /></label><label v-else>备份密码<SecretInput v-model="password" required autocomplete="off" /></label><p v-if="flow === 'import'" class="muted">现有配置将由备份中的配置覆盖，重启后生效。</p></div>
      <footer class="modal-footer"><button v-if="flow === 'import' && importStep === 2" type="button" class="btn" :disabled="busy" @click="importStep = 1">上一步</button><button class="btn primary" :disabled="busy">{{ flow === 'export' ? '确认导出' : importStep === 1 ? '下一步' : '确认导入' }}</button></footer>
    </form>
  </Modal>
  <Modal v-if="enableFolder" compact confirmation title="启用文件夹清理计划" @close="!busy&&(enableFolder=false)"><div class="modal-body">启用已保存的 {{folderCleanup.rules?.length||0}} 条文件夹清理计划？阈值为0的规则将清理目录下全部文件及空子目录，文件移入回收站。</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="enableFolder=false">取消</button><button class="btn danger" :disabled="busy" @click="toggleExtra115('folder',true)">确认启用</button></footer></Modal>
</template>
<style scoped>
.quark-takeover-symbol { position: relative; background: transparent; padding: 0; border: 0; }
.plugin-card .simulcast-symbol { position:relative; background:transparent; border-radius:50%; }
.simulcast-symbol :deep(.provider-icon) { width:44px; height:44px; background:transparent; border-radius:50%; }
.simulcast-symbol :deep(.provider-logo) { width:100%; height:100%; }
.simulcast-symbol > svg { position:absolute; right:1px; bottom:1px; color:var(--primary); background:var(--surface); border-radius:4px; }
.simulcast-symbol[aria-pressed=false]{opacity:.5}
.plugin-toggle { border: 0; padding: 0; }
.plugin-toggle[aria-pressed=false] { opacity: .5; }
.quark-takeover-symbol :deep(.provider-icon), .quark-takeover-symbol :deep(.provider-logo) { width: 100%; height: 100%; padding: 0; border-radius: 0; background:transparent; border:0; }
.plugin-card .plugin-symbol:has(.emby-notice-icon) { background:transparent; border:0; padding:0; }.plugin-symbol :deep(.emby-notice-icon) { width:44px; height:44px; }
.quark-takeover-symbol[aria-pressed=false] { opacity: .55; }
.plugin-card { position:relative;height:130px;min-height:130px;padding:16px;grid-template-columns:44px minmax(0,1fr);grid-template-rows:44px 40px;gap:12px 14px;align-content:start;overflow:hidden;cursor:pointer }
.plugin-card>.plugin-symbol,.plugin-card>button.plugin-symbol,.plugin-card>.rapid115-icon{grid-column:1;grid-row:1}
.plugin-heading{grid-column:2;grid-row:1;min-width:0;display:grid;grid-template-columns:minmax(0,1fr);grid-template-rows:24px 18px;gap:2px}
.plugin-heading strong{grid-column:1;grid-row:1;display:block;font-size:16px;line-height:24px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.plugin-heading .plugin-subtitle{grid-column:1;grid-row:2;display:block;margin:0;color:var(--muted);font-size:12px;line-height:18px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.plugin-card .plugin-description{grid-column:1/-1;grid-row:2;margin:0;line-height:20px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.plugin-card .status{position:absolute;top:4px;bottom:auto;right:8px;font-size:10px;line-height:12px;background:var(--surface)}
.quark-takeover-symbol > svg { position: absolute; bottom: -2px; right: -4px; background: var(--surface); color: var(--primary); border-radius: 4px; }
</style>
