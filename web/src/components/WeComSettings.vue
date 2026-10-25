<script setup>
import {onUnmounted,ref} from 'vue'
import {api,notify} from '../lib'
import Icon from './Icon.vue'
import SecretInput from './SecretInput.vue'

const props=defineProps({initial:Object})
const emit=defineEmits(['saved'])
const secretFields=['secret','token','encodingAESKey']
const flags={secret:'hasSecret',token:'hasToken',encodingAESKey:'hasEncodingAESKey'}
const masked=value=>{
 const result={enabled:false,corpId:'',secret:'',agentId:'',apiURL:'https://qyapi.weixin.qq.com',token:'',encodingAESKey:'',userIds:[],...value}
 for(const field of secretFields)result[field]=result[flags[field]]?'********':''
 return result
}
const form=ref(masked(props.initial)),members=ref((props.initial?.userIds||[]).join(';'))
const busy=ref(false),testing=ref(false)
const callbackURL=new URL('/notify/wecom',location.origin).href
const fields=[
 {key:'corpId',label:'企业 ID',tip:'企业微信管理后台的企业 ID'},
 {key:'secret',label:'应用 Secret',secret:true,tip:'自建应用的 Secret，按需显示已保存值'},
 {key:'agentId',label:'Agent ID',tip:'自建应用的数字 Agent ID'},
 {key:'apiURL',label:'API 地址',tip:'默认使用企业微信官方 HTTPS API，也可指定受信任的 HTTPS 代理'},
 {key:'token',label:'Token',secret:true,tip:'接收消息回调使用的 Token，与 EncodingAESKey 同时填写或留空'},
 {key:'encodingAESKey',label:'EncodingAESKey',secret:true,tip:'接收消息回调使用的43位编码密钥'},
]
let alive=true
onUnmounted(()=>{alive=false})
async function save(){
 if(busy.value||testing.value)return
 busy.value=true
 try{
  const value={...form.value,userIds:members.value.split(/[;,\s|]+/).filter(Boolean),clearSecrets:[]}
  for(const field of secretFields){
   if(value[field]===''&&value[flags[field]])value.clearSecrets.push(field)
   if(value[field]==='********')value[field]=''
  }
  const result=await api('/notification-service/wecom','PUT',{wecom:value})
  if(!alive)return
  form.value=masked(result.wecom);members.value=(result.wecom.userIds||[]).join(';')
  emit('saved',result.wecom);notify('企业微信设置已保存')
 }catch(e){if(alive)notify(e.message,true)}finally{if(alive)busy.value=false}
}
async function test(){
 if(busy.value||testing.value)return
 testing.value=true
 try{await api('/notification-service/wecom/test','POST',{});if(alive)notify('企业微信测试消息已发送')}
 catch(e){if(alive)notify(e.message,true)}finally{if(alive)testing.value=false}
}
</script>
<template>
 <form class="wecom-form" aria-label="企业微信" novalidate @submit.prevent="save">
  <header><h2><Icon name="Building2" />企业微信</h2><input v-model="form.enabled" type="checkbox" role="switch" class="switch" aria-label="启用企业微信" :disabled="busy||testing" /></header>
  <div v-for="field in fields" :key="field.key" class="field">
   <div class="wecom-label"><label :for="`wecom-${field.key}`">{{field.label}}</label><button type="button" class="icon-btn" :aria-label="`${field.label}说明`" :data-tooltip="field.tip"><Icon name="CircleHelp" :size="14" /></button></div>
   <SecretInput v-if="field.secret" :id="`wecom-${field.key}`" v-model="form[field.key]" secret-path="/notification-service/wecom/secret" :secret-field="field.key" :aria-label="field.label" autocomplete="new-password" :disabled="busy||testing" />
   <input v-else :id="`wecom-${field.key}`" v-model.trim="form[field.key]" :aria-label="field.label" :inputmode="field.key==='agentId'?'numeric':undefined" autocomplete="off" :disabled="busy||testing" />
  </div>
  <div class="field"><div class="wecom-label"><label for="wecom-members">接收成员</label><button type="button" class="icon-btn" aria-label="接收成员说明" data-tooltip="成员 UserID，用分号分隔；仅这些成员可查询状态。@all 为向应用可见范围全员发送，须明确填写。"><Icon name="CircleHelp" :size="14" /></button></div><input id="wecom-members" v-model="members" aria-label="接收成员" autocomplete="off" :disabled="busy||testing" /></div>
  <label class="wecom-callback">回调地址<input :value="callbackURL" aria-label="企业微信回调地址" readonly /></label>
  <footer><button type="button" class="btn" :disabled="busy||testing||!form.hasSecret" @click="test"><Icon :name="testing?'LoaderCircle':'Send'" :class="{spin:testing}" />测试通知</button><button class="btn primary" :disabled="busy||testing"><Icon :name="busy?'LoaderCircle':'Save'" :class="{spin:busy}" />保存设置</button></footer>
 </form>
</template>
<style scoped>
.wecom-form{display:grid;gap:18px;width:100%;min-width:0;padding:20px;border:1px solid var(--border);border-radius:8px;background:var(--surface)}
header,h2,.wecom-label{display:flex;align-items:center;gap:10px}header{justify-content:space-between}h2{margin:0;font-size:15px}
.wecom-label{gap:5px;min-height:20px}.wecom-label label{margin:0;font-size:13px}.wecom-label .icon-btn{width:24px;height:24px;flex:none;color:var(--muted)}
.field{display:grid;gap:8px;min-width:0}.wecom-callback{gap:8px}.wecom-form :deep(.secret-input),.wecom-form :deep(input:not([type=checkbox])){width:100%;min-width:0;height:var(--control-height);min-height:var(--control-height)}
footer{display:flex;gap:10px}footer .btn{flex:1;height:var(--toolbar-height);min-height:var(--toolbar-height);padding-block:0}
</style>
