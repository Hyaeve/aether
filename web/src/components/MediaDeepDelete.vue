<script setup>
import {computed,onMounted,onBeforeUnmount,ref} from 'vue'
import {api,notify,state} from '../lib'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import PageJumpInput from './PageJumpInput.vue'
const emit=defineEmits(['close'])
const loaded=ref(false),busy=ref(false),tab=ref('settings'),confirmation=ref(false),records=ref([]),error=ref('')
const settings=ref({enabled:false,linkIds:[]}),saved=ref({enabled:false,linkIds:[]})
const page=ref(1),pages=computed(()=>Math.max(1,Math.ceil(records.value.length/20))),rows=computed(()=>records.value.slice((Math.max(1,Number(page.value)||1)-1)*20,Math.max(1,Number(page.value)||1)*20))
const links=computed(()=>state.links||[])
const names={emby:'Emby',audiobookshelf:'Audiobookshelf',fnos:'飞牛影视'}
const valid=link=>link.enabled&&!!link.mediaRoot
let alive=true
onBeforeUnmount(()=>alive=false)
async function load(){try{const data=await api('/media-deep-delete');if(!alive)return;settings.value={enabled:!!data.settings.enabled,linkIds:data.settings.linkIds||[]};saved.value=JSON.parse(JSON.stringify(settings.value));records.value=data.records||[];loaded.value=true;error.value=''}catch(e){if(alive)error.value=e.message}}
onMounted(load)
function ask(){if(settings.value.enabled&&(!saved.value.enabled||settings.value.linkIds.some(id=>!saved.value.linkIds.includes(id))))confirmation.value=true;else save()}
async function save(){if(busy.value)return;busy.value=true;error.value='';try{const result=await api('/media-deep-delete','PUT',{...settings.value,confirmed:confirmation.value});if(alive){settings.value=result;saved.value=JSON.parse(JSON.stringify(result));confirmation.value=false;notify('媒体深度删除配置已保存')}}catch(e){if(alive){confirmation.value=false;error.value=e.message}}finally{if(alive)busy.value=false}}
const status=record=>({success:'已完成',failed:'未完成',skipped:'未联动',prepared:'待确认',deleting:'处理中'})[record.status]||'待核对'
</script>
<template>
 <Modal standard title="媒体深度删除" @close="!busy&&emit('close')">
  <nav class="content-tabs deep-tabs" aria-label="媒体深度删除栏目"><button :class="{active:tab==='settings'}" @click="tab='settings'"><Icon name="Settings2" />联动配置</button><button :class="{active:tab==='records'}" @click="tab='records'"><Icon name="History" />删除记录</button></nav>
  <form v-if="tab==='settings'" @submit.prevent="ask"><div class="modal-body deep-body">
   <p class="muted">通过所选以链的反代地址主动删除媒体时，联动删除普通STRM指向的真实源文件。删除整部作品会处理作品下的全部普通STRM；CAS、分享和ED2K专用STRM不参与。</p>
   <label class="deep-enable">启用联动删除<input v-model="settings.enabled" type="checkbox" class="switch" role="switch" aria-label="启用媒体深度删除" :disabled="!loaded||busy" /></label>
   <div class="deep-links"><label v-for="link in links" :key="link.id" class="deep-link"><Icon :name="link.type==='audiobookshelf'?'BookOpen':'Tv'" :size="24"/><span><strong>{{link.name}}</strong><small>{{names[link.type]}} · {{valid(link)?link.mediaRoot:'请先启用以链并设置Aether媒体目录'}}</small></span><input v-model="settings.linkIds" type="checkbox" :value="link.id" :aria-label="`联动 ${link.name}`" :disabled="!loaded||busy||!valid(link)" /></label><p v-if="!links.length" class="muted">请先在以太链接创建媒体服务。</p></div>
   <p class="muted deep-help">目录映射在以链编辑窗口的“更多选项”设置。客户端直连原服务、扫库移除或重命名不会触发。源文件沿用存储池的删除方式；无法确认路径、范围或删除结果时保留源文件，并记录原因。</p>
  </div><p v-if="error" role="alert" class="error-message deep-error">{{error}}</p><footer class="modal-footer"><button class="btn primary" :disabled="!loaded||busy||settings.enabled&&!settings.linkIds.length"><Icon :name="busy?'LoaderCircle':'Save'" :class="{spin:busy}" />保存配置</button></footer></form>
  <template v-else><div class="modal-body deep-body"><p v-if="!records.length" class="muted">暂无深度删除记录</p><article v-for="record in rows" :key="record.id" class="deep-record"><div><strong>{{record.linkName}}</strong><span class="status" :class="record.status==='success'?'success':record.status==='failed'?'error':'pending'">{{status(record)}}</span><time>{{new Date(record.time).toLocaleString()}}</time></div><small>{{record.name}}</small><small>{{record.message}} · 已处理{{record.count}}个文件</small></article></div><footer class="modal-footer deep-pagination"><button class="btn" :disabled="busy" @click="load"><Icon name="RefreshCw" />刷新记录</button><button class="icon-btn" aria-label="上一页" :disabled="Number(page)<=1" @click="page=Number(page)-1"><Icon name="ChevronLeft"/></button><PageJumpInput v-model="page" :pages="pages"/><span>/ {{pages}}</span><button class="icon-btn" aria-label="下一页" :disabled="Number(page)>=pages" @click="page=Number(page)+1"><Icon name="ChevronRight"/></button></footer></template>
 </Modal>
 <Modal v-if="confirmation" compact confirmation title="启用媒体深度删除" @close="!busy&&(confirmation=false)"><div class="modal-body">为所选的{{settings.linkIds.length}}个以链启用联动删除？之后从这些以链主动删除普通STRM或整部作品时，对应的真实源文件也会按存储池删除方式处理。</div><footer class="modal-footer"><button class="btn" :disabled="busy" @click="confirmation=false">返回</button><button class="btn danger" :disabled="busy" @click="save">确认启用</button></footer></Modal>
</template>
<style scoped>
.deep-tabs{padding:0 22px;margin:0}.deep-body{max-height:min(58vh,620px);overflow:auto;scrollbar-width:none}.deep-body::-webkit-scrollbar{display:none}.deep-body>p{font-size:13px;line-height:1.8}.deep-enable{display:flex;align-items:center;justify-content:space-between;margin:16px 0}.deep-links{display:grid;gap:10px}.deep-link{display:flex;align-items:center;gap:12px;padding:12px;border:1px solid var(--border);border-radius:12px;background:var(--surface)}.deep-link>span{flex:1;min-width:0}.deep-link strong,.deep-link small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.deep-link small{margin-top:5px;font-size:12px;color:var(--muted)}.deep-link input{width:18px;height:18px;flex:none;accent-color:var(--primary)}.deep-record{padding:12px;border-bottom:1px solid var(--border)}.deep-record>div{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.deep-record time{font-size:12px;color:var(--muted);margin-left:auto}.deep-record small{display:block;line-height:1.7;margin-top:8px;overflow-wrap:anywhere;color:var(--muted)}.deep-error{margin:0 22px 12px}
</style>
<style scoped>.deep-pagination{gap:8px}.deep-pagination>.btn{margin-right:auto}.deep-pagination>input{width:58px;height:34px}.deep-pagination>span{white-space:nowrap}</style>
<style scoped>
.deep-tabs{display:flex;gap:22px;flex:none;align-items:center}.deep-tabs>button{display:inline-flex;align-items:center;gap:6px;min-height:44px;padding:8px 0;background:transparent;border:0;border-bottom:2px solid transparent;color:var(--muted);font:inherit;font-size:14px;cursor:pointer;white-space:nowrap}.deep-tabs>button.active{color:var(--primary);border-bottom-color:var(--primary)}
.modal label.deep-enable{display:flex;flex-direction:row;align-items:center;justify-content:space-between;gap:12px;text-align:left;min-height:40px}.modal label.deep-link{display:flex;flex-direction:row;align-items:center;gap:12px;text-align:left;min-height:68px}.modal label.deep-link>span{text-align:left;display:block}.modal label.deep-link>input{margin:0;width:18px;height:18px}
.modal .deep-body{min-width:0;width:100%;max-width:100%;box-sizing:border-box;overflow-x:hidden}.deep-links{min-width:0;width:100%;grid-template-columns:minmax(0,1fr)}.modal label.deep-link{min-width:0;width:100%;max-width:100%;box-sizing:border-box}.modal label.deep-link>span{min-width:0;flex:1}.modal label.deep-link>svg{flex:none}.modal label.deep-link>input{min-width:18px;max-width:18px;padding:0;box-sizing:border-box}
</style>
