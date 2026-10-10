<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { api, state, notify, date } from '../lib'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import Modal from './Modal.vue'
import RoundedSelect from './RoundedSelect.vue'
import NumberInput from './NumberInput.vue'
import TaskSourcePicker from './TaskSourcePicker.vue'
const rules = ref([]), modal = ref(false), editing = ref(''), busy = ref(false), error = ref(''), picker = ref(''), deleting = ref(null), menu = ref('')
const form = reactive({}), step = ref(0), detailId = ref(''), pickerIndex = ref(0), deleteConfirmation = ref(null)
const twoWay = computed(() => form.syncMode === 'two_way')
watch(twoWay, value => { if (value) form.completionRule = 'keep' })
const steps = ['基本设置','备份规则','扫描规则','筛选规则']
const filterTypes = [{value:'name',label:'名称'},{value:'extension',label:'扩展名'},{value:'regex',label:'正则表达式'},{value:'size',label:'文件大小'}]
const filterModes = [{value:'include',label:'白名单'},{value:'exclude',label:'黑名单'}]
const sizeUnits = ['B','KB','MB','GB'].map(value=>({value,label:value}))
const detail = computed(()=>rules.value.find(r=>r.id===detailId.value))
const locations = (r,kind) => r[kind+'s']?.length ? r[kind+'s'] : [{storageId:r[kind+'Id'],path:r[kind],label:r[kind+'Label']}]
const blankLocation = () => ({storageId:'',path:'',label:''})
const storages = computed(() => state.storages.filter(s => s.enabled))
const storage = id => state.storages.find(s => s.id === id)
const policies = [{value:'skip',label:'跳过同名文件'},{value:'overwrite',label:'覆盖同名文件'}]
const completionPolicies = [{value:'keep',label:'保留源文件'},{value:'delete_source',label:'删除已备份源文件'},{value:'delete_source_dir',label:'删除已备份源文件和空文件夹'}]
const deletionPolicies = [{value:'keep',label:'保留'},{value:'trash',label:'移至回收目录'},{value:'delete',label:'删除'}]
const conflictPolicies = [{value:'keep_both',label:'保留两个版本（另一个保存为冲突副本）'},{value:'source',label:'优先源目录版本'},{value:'newest',label:'优先较新版本'}]
const statuses = {idle:'等待执行',running:'正在备份',completed:'备份完成',failed:'备份失败',stopped:'已停止',interrupted:'已中断'}
let timer, alive = true, loading = false
async function load() {
  if (loading) return
  clearTimeout(timer); loading = true
  try { const value = await api('/backup-rules'); if (alive) rules.value = value }
  catch(e) { if (alive) notify(e.message,true) }
  finally { loading = false; if(alive) timer = setTimeout(load,3000) }
}
function closeMenu() { menu.value = '' }
function escape(event) { if(event.key === 'Escape') closeMenu() }
onMounted(() => { load(); document.addEventListener('click',closeMenu); document.addEventListener('keydown',escape) })
onUnmounted(() => { alive = false; clearTimeout(timer); document.removeEventListener('click',closeMenu); document.removeEventListener('keydown',escape) })
function open(rule) {
  closeMenu(); editing.value = rule?.id || ''; error.value = ''; step.value=0
  const defaults = {name:'',enabled:true,sourceId:'',source:'',sourceLabel:'',targetId:'',target:'',targetLabel:'',replace:'skip',completionRule:'keep',monitorEnabled:false,extensions:'',exclude:'',minSize:0,maxSize:0,cron:'',scanInterval:0,filters:[],syncMode:'one_way',deletionRule:'keep',syncDelete:false,conflictRule:'keep_both',conflictMarker:'conflict copy',historyDays:0,deleteLimit:20,targetOnly:'copy',syncMarker:true,fullScan:true,fullScanEvery:8,fullScanHours:24}
  Object.keys(form).forEach(key=>delete form[key])
  Object.assign(form,defaults,rule ? JSON.parse(JSON.stringify(rule)) : {})
  form.sources = rule ? JSON.parse(JSON.stringify(locations(rule,'source'))) : [blankLocation()]
  form.targets = rule ? JSON.parse(JSON.stringify(locations(rule,'target'))) : [blankLocation()]
  modal.value = true
}
function choose(value) {
  form[picker.value+'s'][pickerIndex.value] = {storageId:value.storageId,path:value.source,label:value.sourceLabel}
  picker.value = ''
}
function pick(kind,index) { picker.value=kind; pickerIndex.value=index }
const picked = computed(()=>picker.value ? form[picker.value+'s'][pickerIndex.value] : null)
function addFilter() { form.filters.push({type:'name',mode:'exclude',value:'',matchFile:true,matchDir:false,minSize:0,maxSize:0,unit:'MB'}) }
function copyRule(rule) { open(rule); editing.value=''; form.name=rule.name+' 副本'; form.enabled=false }
const phaseText = r => r.phase==='scan' ? '扫描源目录' : r.phase==='copy' ? '复制文件' : statuses[r.status] || '等待执行'
const percent = r => r.total > 0 ? Math.min(100,Math.round((r.processed || 0)/r.total*100)) : 0
async function save() {
  if(busy.value) return
  if (!form.name.trim()) { step.value=0; error.value='请填写备份名称'; return }
  busy.value = true; error.value = ''
  try { await api(editing.value ? `/backup-rules/${editing.value}` : '/backup-rules',editing.value ? 'PUT' : 'POST',form); modal.value=false; await load(); notify('备份规则已保存') }
  catch(e) { error.value=e.message }
  finally { busy.value=false }
}
async function command(rule, action) {
  closeMenu()
  try { await api(`/backup-rules/${rule.id}/${action}`,'POST'); await load(); notify(action==='stop'?'正在停止备份':'备份已开始') }
  catch(e) { notify(e.message,true) }
}
async function toggle(rule) {
  closeMenu()
  try { await api(`/backup-rules/${rule.id}`,'PUT',{...rule,enabled:!rule.enabled}); await load(); notify(rule.enabled?'备份规则已停用':'备份规则已启用') }
  catch(e) { notify(e.message,true) }
}
async function remove() {
  busy.value=true
  try { await api(`/backup-rules/${deleting.value.id}`,'DELETE'); deleting.value=null; await load(); notify('备份规则已删除') }
  catch(e) { notify(e.message,true) }
  finally { busy.value=false }
}
const directoryText = (id,label) => id ? `${storage(id)?.name || '存储已移除'} / ${label || '根目录'}` : ''
defineExpose({ open, load })
</script>
<template>
  <div v-if="!rules.length" class="empty-state"><Icon name="ArchiveRestore" :size="32" /><h2>暂无备份规则</h2></div>
  <div v-else class="backup-grid">
    <article v-for="rule in rules" :key="rule.id" class="backup-card" :class="{ disabled:!rule.enabled, failed:rule.status==='failed', running:rule.status==='running' }" @contextmenu.prevent.stop="menu=rule.id">
      <header><button class="provider-toggle" :aria-label="`${rule.enabled?'停用':'启用'}备份 ${rule.name}`" :aria-pressed="rule.enabled" :disabled="rule.status==='running'" @click="toggle(rule)"><ProviderIcon :type="storage(locations(rule,'source')[0]?.storageId)?.type || 'local'" /></button><button class="backup-name" :disabled="rule.status==='running'" @click="open(rule)"><strong>{{rule.name}}</strong><small>{{phaseText(rule)}}</small></button><button class="icon-btn" :class="{stopping:rule.status==='running'}" :aria-label="rule.status==='running'?'停止备份':'执行备份'" :disabled="!rule.enabled" @click="command(rule,rule.status==='running'?'stop':'run')"><Icon :name="rule.status==='running'?'Square':'Play'" /></button><div class="backup-menu" @click.stop><button class="icon-btn" :aria-label="`备份操作 ${rule.name}`" :aria-expanded="menu===rule.id" @click="menu=menu===rule.id?'':rule.id"><Icon name="EllipsisVertical" /></button><div v-if="menu===rule.id" class="storage-menu"><button @click="closeMenu();detailId=rule.id"><Icon name="List" />执行详情</button><button :disabled="rule.status==='running'" @click="open(rule)"><Icon name="Pencil" />编辑规则</button><button @click="copyRule(rule)"><Icon name="Copy" />复制规则</button><button :disabled="rule.status==='running'" @click="toggle(rule)"><Icon name="Power" />{{rule.enabled?'停用规则':'启用规则'}}</button><button class="danger-text" :disabled="rule.status==='running'" @click="closeMenu();deleting=rule"><Icon name="Trash2" />删除规则</button></div></div></header>
      <div v-for="kind in ['source','target']" :key="kind" class="backup-path" :data-tooltip="locations(rule,kind).map(p=>directoryText(p.storageId,p.label)).join('\n')"><Icon :name="kind==='source'?'FolderOpen':'ArchiveRestore'" :size="16" /><span>{{locations(rule,kind).slice(0,2).map(p=>directoryText(p.storageId,p.label)).join(' · ')}}</span><small v-if="locations(rule,kind).length>2">+{{locations(rule,kind).length-2}}</small></div>
      <div class="backup-tags"><span>{{rule.syncMode==='two_way'?'双向同步':rule.replace==='overwrite'?'覆盖同名':'跳过同名'}}</span><span>{{rule.cron || (rule.scanInterval ? rule.scanInterval+'秒扫描' : '手动执行')}}</span><span v-if="rule.filters?.length">{{rule.filters.length}} 条筛选</span><button v-if="rule.syncPending && rule.status!=='running'" class="sync-delete-confirm danger-text" @click="deleteConfirmation=rule"><Icon name="ShieldAlert" :size="14" />确认同步删除</button></div>
      <footer><span :data-tooltip="rule.message">{{rule.message || (rule.cron || '手动执行')}}</span><time>{{rule.lastRun && !rule.lastRun.startsWith('0001') ? date(rule.lastRun) : '尚未执行'}}</time></footer>
      <progress v-if="rule.status==='running'" aria-label="备份进度" :aria-valuetext="rule.phase==='scan' ? '正在扫描，已扫描'+rule.scanned+'个文件' : percent(rule)+'%'" :max="Math.max(1,rule.total || 0)" :value="rule.phase==='scan' ? undefined : (rule.processed || 0)" />
    </article>
  </div>
  <Modal v-if="modal" standard :title="editing?'编辑备份规则':'添加备份规则'" @close="!busy && (modal=false)">
    <nav class="backup-steps" aria-label="备份配置步骤"><button v-for="(title,index) in steps" :key="title" type="button" :class="{active:step===index}" :aria-current="step===index?'step':undefined" @click="step=index">{{title}}</button></nav>
    <form novalidate @submit.prevent="save"><div class="backup-editor modal-body"><div class="backup-fields">
      <section v-show="step===0" class="backup-section">
      <div class="form-grid"><label>备份名称<input v-model="form.name" :required="step===0" aria-required="true" maxlength="200" /></label><div class="field"><label>同步模式</label><RoundedSelect v-model="form.syncMode" label="同步模式" :options="[{value:'one_way',label:'单向同步'},{value:'two_way',label:'双向同步'}]" /></div></div>
      <div v-for="kind in ['source','target']" :key="kind" class="field"><div class="backup-field-heading"><label>{{kind==='source'?'源目录':'目标目录'}}</label><button class="icon-btn" type="button" :aria-label="kind==='source'?'添加源目录':'添加目标目录'" :disabled="form[kind+'s'].length>=16" @click="form[kind+'s'].push(blankLocation())"><Icon name="Plus" :size="18" /></button></div><div v-for="(loc,index) in form[kind+'s']" :key="index" class="backup-location"><button class="source-trigger" type="button" :aria-label="(kind==='source'?'选择备份源目录':'选择备份目标目录')+(index?' '+(index+1):'')" @click="pick(kind,index)"><span>{{directoryText(loc.storageId,loc.label) || '选择目录'}}</span><Icon name="FolderOpen" /></button><button v-if="form[kind+'s'].length>1" class="icon-btn danger-text" type="button" :aria-label="'移除'+(kind==='source'?'源':'目标')+'目录 '+(index+1)" @click="form[kind+'s'].splice(index,1)"><Icon name="X" :size="16" /></button></div></div>
      <label class="backup-monitor"><span>文件系统监听</span><input v-model="form.monitorEnabled" type="checkbox" role="switch" class="switch" /></label>
      </section>
      <section v-show="step===1" class="backup-section">
        <div v-if="!twoWay" class="field"><label>同名文件</label><RoundedSelect v-model="form.replace" label="同名文件策略" :options="policies" /></div>
        <div class="field"><label>完成规则</label><RoundedSelect v-model="form.completionRule" label="完成规则" :disabled="twoWay" :options="completionPolicies" /></div>
        <div class="field"><label>删除规则</label><RoundedSelect v-model="form.deletionRule" label="删除规则" :options="deletionPolicies" /></div>
        <template v-if="twoWay"><div class="field"><label>同一文件在两处都被修改时</label><RoundedSelect v-model="form.conflictRule" label="冲突规则" :options="conflictPolicies" /></div><label v-if="form.conflictRule === 'keep_both'">冲突副本标记<input v-model.trim="form.conflictMarker" aria-label="冲突副本标记" maxlength="100" /></label>
        <details class="backup-sync-advanced"><summary>双向同步高级设置<Icon name="ChevronDown" :size="16" /></summary><div class="backup-filters">
          <div class="form-grid"><div class="field"><label>被替换的版本保留天数</label><NumberInput v-model="form.historyDays" aria-label="被替换的版本保留天数" unit="天" min="0" max="36500" /></div><div class="field"><label>删除保护上限</label><NumberInput v-model="form.deleteLimit" aria-label="删除保护上限" unit="%" min="0" max="100" /></div></div>
          <div class="field"><label>只存在于目标文件夹的文件</label><RoundedSelect v-model="form.targetOnly" label="目标独有文件" :options="[{value:'copy',label:'复制到源文件夹'},{value:'keep',label:'保留在目标文件夹'}]" /></div>
          <label class="backup-monitor"><span>同步标记文件</span><input v-model="form.syncMarker" type="checkbox" role="switch" class="switch" /></label>
          <label class="backup-monitor"><span>自动彻底扫描</span><input v-model="form.fullScan" type="checkbox" role="switch" class="switch" /></label>
          <div class="form-grid"><div class="field"><label>每 N 次普通扫描</label><NumberInput v-model="form.fullScanEvery" aria-label="每 N 次普通扫描" :disabled="!form.fullScan" min="0" max="100000" /></div><div class="field"><label>至少每 N 小时</label><NumberInput v-model="form.fullScanHours" aria-label="至少每 N 小时" :disabled="!form.fullScan" unit="小时" min="0" max="876000" /></div></div>
        </div></details></template>
      </section>
      <section v-show="step===2" class="backup-section"><label>Cron 表达式<input v-model="form.cron" aria-label="Cron 表达式" /></label><div class="field"><label>自动扫描间隔</label><NumberInput v-model="form.scanInterval" aria-label="自动扫描间隔" unit="秒" min="0" max="31536000" /></div><label v-if="twoWay" class="backup-monitor"><span>同步删除</span><input v-model="form.syncDelete" type="checkbox" role="switch" class="switch" /></label></section>
      <section v-show="step===3" class="backup-section">
      <article v-for="(filter,index) in form.filters" :key="index" class="backup-filter">
        <header><strong>规则 {{index+1}}</strong><button type="button" class="icon-btn danger-text" :aria-label="'删除筛选规则 '+(index+1)" @click="form.filters.splice(index,1)"><Icon name="Trash2" :size="16" /></button></header>
        <div class="form-grid"><div class="field"><label>类型</label><RoundedSelect v-model="filter.type" :label="'筛选类型 '+(index+1)" :options="filterTypes" @update:model-value="value=>{if(value==='extension'||value==='size'){filter.matchDir=false;filter.matchFile=true}}" /></div><div class="field"><label>模式</label><RoundedSelect v-model="filter.mode" :label="'筛选模式 '+(index+1)" :options="filterModes" /></div></div>
        <label v-if="filter.type!=='size'">{{filter.type==='extension'?'扩展名':filter.type==='regex'?'正则表达式':'名称关键词'}}<input v-model="filter.value" :aria-label="'匹配内容 '+(index+1)" maxlength="2048" /></label>
        <div v-else class="backup-size"><div class="field"><label>最小值</label><NumberInput v-model="filter.minSize" :aria-label="'最小值 '+(index+1)" min="0" /></div><div class="field"><label>最大值</label><NumberInput v-model="filter.maxSize" :aria-label="'最大值 '+(index+1)" min="0" /></div><div class="field"><label>单位</label><RoundedSelect v-model="filter.unit" :label="'大小单位 '+(index+1)" :options="sizeUnits" /></div></div>
        <div class="backup-filter-range"><label><input v-model="filter.matchFile" type="checkbox" />文件</label><label><input v-model="filter.matchDir" type="checkbox" :disabled="filter.type==='extension'||filter.type==='size'" />文件夹</label></div>
      </article>
      <button class="btn backup-add-filter" type="button" :disabled="form.filters.length>=32" @click="addFilter"><Icon name="Plus" />添加筛选规则</button>
      <details><summary>兼容筛选<Icon name="ChevronDown" :size="16" /></summary><div class="backup-filters">
        <label>包含扩展名<input v-model="form.extensions" aria-label="包含扩展名" placeholder="英文分号分隔，留空为全部" /></label>
        <label>排除名称关键词<input v-model="form.exclude" aria-label="排除名称关键词" placeholder="英文分号分隔" /></label>
        <div class="form-grid"><div class="field"><label>最小文件大小</label><NumberInput v-model="form.minSize" aria-label="最小文件大小" unit="B" min="0" /></div><div class="field"><label>最大文件大小</label><NumberInput v-model="form.maxSize" aria-label="最大文件大小" unit="B" min="0" /></div></div>
      </div></details>
      </section>
      <p v-if="error" class="error-message" role="alert">{{error}}</p>
    </div></div><footer class="modal-footer"><button class="btn primary" :disabled="busy || form.sources.some(p=>!p.storageId) || form.targets.some(p=>!p.storageId)"><Icon name="Check" />保存规则</button><button type="button" class="btn" :disabled="busy" @click="modal=false">取消</button></footer></form>
  </Modal>
  <TaskSourcePicker v-if="picker" :storages="storages" :storage="picked.storageId" :initial="picked.path || '/'" :initial-label="picked.label" @select="choose" @close="picker=''" />
  <Modal v-if="detail" title="备份执行详情" @close="detailId=''"><div class="modal-body backup-detail"><h3>{{detail.name}}</h3><div class="backup-phase">{{phaseText(detail)}}<span v-if="detail.phase==='copy'">{{percent(detail)}}%</span></div><progress v-if="detail.status==='running'" aria-label="执行详情进度" :max="Math.max(1,detail.total || 0)" :value="detail.phase==='scan'?undefined:(detail.processed || 0)" /><dl><div><dt>扫描文件</dt><dd>{{detail.scanned || 0}}</dd></div><div><dt>已复制</dt><dd>{{detail.copied || 0}}</dd></div><div><dt>已跳过</dt><dd>{{detail.skipped || 0}}</dd></div><div><dt>已处理 / 复制总数</dt><dd>{{detail.processed || 0}} / {{detail.total || 0}}</dd></div></dl><p>{{detail.message || '尚未执行'}}</p><time>{{detail.lastRun && !detail.lastRun.startsWith('0001') ? date(detail.lastRun) : ''}}</time></div><footer class="modal-footer"><button class="btn" @click="detailId=''">关闭</button></footer></Modal>
  <Modal v-if="deleting" title="删除备份规则" compact confirmation @close="deleting=null"><div class="modal-body">确认删除「{{deleting.name}}」？</div><footer class="modal-footer"><button class="btn danger" :disabled="busy" @click="remove">确认</button><button class="btn" @click="deleting=null">取消</button></footer></Modal>
  <Modal v-if="deleteConfirmation" title="确认同步删除" compact confirmation @close="deleteConfirmation=null"><div class="modal-body">「{{deleteConfirmation.name}}」的删除数量超过保护上限。确认后将重新扫描；文件有变化时仍会暂停，不会执行旧的删除计划。</div><footer class="modal-footer"><button class="btn danger" @click="command(deleteConfirmation,'confirm-deletions');deleteConfirmation=null">确认</button><button class="btn" @click="deleteConfirmation=null">取消</button></footer></Modal>
</template>
<style scoped>
.backup-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}
.sync-delete-confirm{display:inline-flex;align-items:center;gap:5px;border:0;background:none;padding:0;font-size:12px}
.backup-card{position:relative;padding:16px;background:var(--surface);border:1px solid var(--border);border-radius:8px;min-width:0;transition:border-color .2s}
.backup-card:hover{border-color:color-mix(in srgb,var(--primary) 30%,var(--border))}.backup-card.failed{border-color:color-mix(in srgb,var(--danger) 45%,var(--border))}.backup-card.disabled{opacity:.65}
.backup-card header{display:flex;align-items:center;gap:10px;margin-bottom:14px}.provider-toggle{flex:none}.backup-name{flex:1;min-width:0;background:none;border:0;text-align:left;color:var(--text);display:grid;gap:5px}.backup-name strong{font-size:15px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.backup-name small{font-size:12px;color:var(--muted)}
.backup-path{display:flex;gap:8px;align-items:center;margin:7px 0;font-size:13px;color:var(--muted)}.backup-path span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.backup-menu{position:relative}.backup-card footer{display:flex;gap:10px;justify-content:space-between;font-size:12px;color:var(--muted);margin-top:14px}.backup-card footer span{overflow:hidden;white-space:nowrap;text-overflow:ellipsis}.backup-card footer time{flex:none}.backup-card progress{width:100%;height:6px;margin-top:10px;accent-color:var(--primary)}
.backup-editor{height:min(65dvh,520px)}.backup-editor :deep(.backup-fields){display:grid;gap:14px;align-content:start}.backup-editor :deep(input),.backup-editor :deep(.rounded-select-trigger),.backup-editor :deep(.source-trigger){height:36px;min-height:36px;font-size:14px}.backup-editor :deep(.form-grid){gap:14px}.backup-editor :deep(label){font-size:14px}.backup-editor :deep(summary){display:flex;align-items:center;gap:8px;color:var(--muted);cursor:pointer;font-size:14px;list-style:none}.backup-editor :deep(summary:hover){color:var(--primary)}.backup-editor :deep(summary svg:last-child){margin-left:auto}.backup-filters{display:grid;gap:14px;margin-top:14px}
.backup-editor :deep(.backup-filter-range label){flex-direction:row}
.backup-steps{display:flex;gap:4px;margin:0 24px;padding:8px 0 12px;border-bottom:1px solid var(--border)}.backup-steps button{flex:1;display:flex;align-items:center;justify-content:center;gap:6px;border:0;background:none;color:var(--text);font-size:15px;font-weight:600;min-height:36px;padding:4px}.backup-steps button.active{color:var(--primary);background:color-mix(in srgb,var(--primary) 8%,transparent);border-radius:6px}
.backup-section{display:grid;gap:14px;min-width:0}.backup-field-heading{display:flex;align-items:center;justify-content:space-between;margin-bottom:4px}.backup-location{display:flex;align-items:center;gap:6px;margin:5px 0;min-width:0}.backup-location .source-trigger{flex:1;min-width:0}.backup-location .source-trigger span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.backup-filter{padding:12px;border:1px solid var(--border);border-radius:8px;background:var(--surface);display:grid;gap:10px;min-width:0}.backup-filter header{display:flex;align-items:center;justify-content:space-between}.backup-filter strong{font-size:13px;font-weight:500}.backup-filter-range{display:flex;gap:20px}.backup-filter-range label{display:flex;align-items:center;gap:6px}.backup-filter-range input[type=checkbox]{height:16px;min-height:16px;width:16px}.backup-size{display:grid;grid-template-columns:1fr 1fr 90px;gap:8px;min-width:0}.backup-size>*{min-width:0}.backup-add-filter{width:100%;font-size:14px;min-height:36px}.backup-tags{display:flex;flex-wrap:wrap;gap:12px;color:var(--muted);font-size:12px;margin-top:10px}.backup-detail h3{font-size:16px;margin:0 0 16px}.backup-phase{display:flex;justify-content:space-between;font-size:14px}.backup-detail progress{width:100%;height:6px;accent-color:var(--primary)}.backup-detail dl{display:grid;grid-template-columns:1fr 1fr;gap:16px}.backup-detail dl div{display:grid;gap:6px}.backup-detail dt,.backup-detail time{font-size:13px;color:var(--muted)}.backup-detail dd{margin:0;font-size:18px}.backup-detail p{font-size:14px;overflow-wrap:anywhere}.backup-detail .backup-phase span{color:var(--primary)}
@media(max-width:420px){.backup-steps{margin:0 16px;gap:0}.backup-steps button{font-size:12px;gap:3px}.backup-size{grid-template-columns:1fr 1fr}.backup-size>*:last-child{grid-column:1/-1}}
@media(max-width:760px){.backup-grid{grid-template-columns:1fr}.backup-card footer{flex-wrap:wrap}.backup-editor{max-height:60dvh}}
.backup-modal :deep(.modal) { width:min(640px,calc(100vw - 32px)); background:var(--surface); }.backup-editor :deep(.thin-scroll-rail) { display:none; }.backup-editor :deep(.backup-monitor) { flex-direction:row; align-items:center; gap:8px; }.backup-editor :deep(.backup-monitor input) { width:16px; height:16px; min-height:16px; }
.backup-editor { overflow:auto; scrollbar-width:none; }.backup-editor::-webkit-scrollbar { display:none; }.backup-fields { display:grid; gap:14px; }.backup-editor .backup-monitor { display:flex; flex-direction:row; align-items:center; justify-content:flex-start; gap:10px; }.backup-editor .backup-monitor input.switch { width:32px; height:18px; min-height:18px; }.backup-sync-advanced { border-top:1px solid var(--border); padding-top:12px; }
</style>
