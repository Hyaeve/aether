<script setup>
import Icon from './Icon.vue'
const props=defineProps({modelValue:{type:Array,default:()=>[]},title:String,field:String,disabled:Boolean,errorField:String})
const emit=defineEmits(['update:modelValue'])
function add(){emit('update:modelValue',[...props.modelValue,{from:'',to:'',regex:false,_key:Symbol()}])}
function update(index,key,value){emit('update:modelValue',props.modelValue.map((r,i)=>i===index?{...r,[key]:value}:r))}
function remove(index){emit('update:modelValue',props.modelValue.filter((_,i)=>i!==index))}
function move(index,delta){const rows=[...props.modelValue],to=index+delta;if(to<0||to>=rows.length)return;[rows[index],rows[to]]=[rows[to],rows[index]];emit('update:modelValue',rows)}
</script>
<template>
 <div class="replacement-rules">
  <div v-for="(rule,i) in modelValue" :key="rule._key" class="replacement-rule" :aria-label="`${title}第${i+1}条`" role="group">
   <div class="replacement-heading"><span>规则 {{i+1}}</span><label class="regex-option"><input type="checkbox" :checked="rule.regex" :aria-label="`${title}正则 ${i+1}`" :disabled="disabled" @change="update(i,'regex',$event.target.checked)"/>正则</label><div class="replacement-actions"><button type="button" class="icon-btn" :aria-label="`上移${title} ${i+1}`" :disabled="disabled||i===0" data-tooltip="上移" data-tooltip-always @click="move(i,-1)"><Icon name="ArrowUp" :size="16"/></button><button type="button" class="icon-btn" :aria-label="`下移${title} ${i+1}`" :disabled="disabled||i===modelValue.length-1" data-tooltip="下移" data-tooltip-always @click="move(i,1)"><Icon name="ArrowDown" :size="16"/></button><button type="button" class="icon-btn danger-text" :aria-label="`删除${title} ${i+1}`" :disabled="disabled" data-tooltip="删除" data-tooltip-always @click="remove(i)"><Icon name="Trash2" :size="16"/></button></div></div>
   <div class="replacement-fields"><label>原文本<input :value="rule.from" :aria-label="`${title}原文本 ${i+1}`" :aria-invalid="errorField===`${field}.${i}.from`" maxlength="1024" :disabled="disabled" @input="update(i,'from',$event.target.value)"/></label><Icon name="ArrowRight" :size="16"/><label>{{field==='namingRules'?'重命名字段':'替换文本'}}<input :value="rule.to" :aria-label="`${title}替换文本 ${i+1}`" placeholder="留空即移除" maxlength="1024" :disabled="disabled" @input="update(i,'to',$event.target.value)"/></label></div>
  </div>
  <button type="button" class="replacement-add" :disabled="disabled||modelValue.length>=512" :aria-label="`添加${title}`" @click="add"><Icon name="Plus" :size="17"/>添加条目</button>
 </div>
</template>
<style scoped>
.replacement-rules{display:grid;gap:12px;min-width:0}.replacement-rule{min-width:0;padding:12px;border:1px solid var(--border);border-radius:8px;background:var(--layer-item)}.replacement-heading{display:flex;align-items:center;gap:12px;font-size:12px;color:var(--muted);margin-bottom:10px}.regex-option{margin:0 0 0 auto;display:flex;align-items:center;gap:6px;color:var(--text);font-size:12px;cursor:pointer}.regex-option input{width:15px;height:15px;margin:0;accent-color:var(--primary)}.replacement-actions{display:flex;gap:4px}.replacement-actions .icon-btn{width:32px;height:32px;min-height:32px}.replacement-fields{display:grid;grid-template-columns:minmax(0,1fr) 16px minmax(0,1fr);gap:10px;align-items:end;min-width:0}.replacement-fields>label{display:grid;gap:7px;min-width:0;margin:0;font-size:12px}.replacement-fields input{width:100%;height:40px;min-width:0;padding:8px 10px}.replacement-fields>svg{align-self:end;margin-bottom:12px;color:var(--muted)}.replacement-fields input[aria-invalid=true]{border-color:var(--red)}.replacement-add{width:100%;min-height:40px;display:flex;justify-content:center;align-items:center;gap:8px;border:1px dashed var(--border);border-radius:8px;background:transparent;color:var(--muted);cursor:pointer}.replacement-add:hover:enabled{color:var(--primary);border-color:var(--primary);background:var(--primary-soft)}.replacement-add:disabled{opacity:.6;cursor:default}.replacement-add:focus-visible{outline:2px solid var(--primary);outline-offset:2px}
.regex-option{flex-direction:row}
@media(pointer:coarse){.replacement-actions .icon-btn{width:36px;height:40px;min-height:40px}.regex-option{min-height:40px}}
@media(max-width:480px){.replacement-rule{padding:10px}.replacement-heading{gap:8px}.replacement-fields{gap:6px;grid-template-columns:minmax(0,1fr) 12px minmax(0,1fr)}.replacement-fields>svg{width:12px}.replacement-actions{gap:0}}
</style>
