<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../lib'
import Icon from './Icon.vue'
defineOptions({inheritAttrs:false})
const props=defineProps({modelValue:{type:String,default:''},retry:Boolean,label:{type:String,default:'Cron表达式'}})
const emit=defineEmits(['update:modelValue'])
const error=ref('')
let timer,request=0,alive=true
watch(()=>[props.modelValue,props.retry],()=>{
 clearTimeout(timer);const ticket=++request;error.value=''
 if(!props.modelValue.trim())return
 timer=setTimeout(async()=>{
  try{const result=await api('/cron/validate','POST',{expression:props.modelValue,retry:props.retry});if(alive&&ticket===request)error.value=result.error||''}
  catch(e){if(alive&&ticket===request)error.value=e.message}
 },250)
},{immediate:true})
onBeforeUnmount(()=>{alive=false;request++;clearTimeout(timer)})
</script>
<template><div class="cron-input"><input v-bind="$attrs" :aria-label="$attrs['aria-label']||label" :value="modelValue" :aria-invalid="error?'true':undefined" maxlength="128" @input="emit('update:modelValue',$event.target.value)" @keydown.enter.prevent /><button v-if="error" type="button" class="cron-warning icon-btn" aria-label="Cron计划不可用原因" :data-tooltip="error" data-tooltip-always data-tooltip-stationary data-tooltip-delay="300"><Icon name="CircleAlert" :size="17" /></button></div></template>
<style scoped>.cron-input{position:relative;min-width:0;width:100%}.cron-input input{width:100%;height:var(--control-height);padding-right:38px}.cron-input input[aria-invalid=true]{border-color:var(--red)}.cron-warning{position:absolute;right:4px;top:4px;color:var(--red);width:32px;height:32px}</style>
