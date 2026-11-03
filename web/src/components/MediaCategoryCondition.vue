<script setup>
import {computed} from 'vue'
import Icon from './Icon.vue'
import RoundedSelect from './RoundedSelect.vue'
import NumberInput from './NumberInput.vue'
import {mediaCategoryOperators,mediaCategoryLeaf,mediaCategoryNoValue,mediaCategoryListValue} from '../media-category'
defineOptions({name:'MediaCategoryCondition'})
const props=defineProps({modelValue:{type:Object,required:true},fields:{type:Array,default:()=>[]},disabled:Boolean,depth:{type:Number,default:0},label:{default:'分类条件'}})
const emit=defineEmits(['update:modelValue','remove'])
const field=computed(()=>props.fields.find(f=>f.id===props.modelValue.field))
const kinds=computed(()=>[{value:'condition',label:'普通条件'},...(props.depth<6?[{value:'all',label:'全部满足'},{value:'any',label:'任一满足'},{value:'not',label:'取反'}]:[])])
const operators=computed(()=>(field.value?.operators||[]).map(value=>({value,label:mediaCategoryOperators[value]||value})))
const isList=computed(()=>mediaCategoryListValue(field.value,props.modelValue.operator))
const valueText=computed(()=>Array.isArray(props.modelValue.value)?props.modelValue.value.join(';'):props.modelValue.value??'')
function patch(value){emit('update:modelValue',{...props.modelValue,...value})}
function kind(value){const node=props.modelValue;if(value===node.kind)return;if(value==='condition'){emit('update:modelValue',mediaCategoryLeaf());return}const children=node.kind==='condition'?[JSON.parse(JSON.stringify(node))]:node.children||[mediaCategoryLeaf()];emit('update:modelValue',{kind:value,children:value==='not'?[{...node}]:children})}
function changeField(value){const selected=props.fields.find(f=>f.id===value),op=selected?.operators?.[0]||'exists';emit('update:modelValue',{kind:'condition',field:value,operator:op,...(mediaCategoryNoValue(op)?{}:{value:['list','integer_list'].includes(selected?.type)?[]:['number','integer'].includes(selected?.type)?0:''})})}
function changeOperator(operator){patch({operator,value:mediaCategoryNoValue(operator)?undefined:mediaCategoryListValue(field.value,operator)?[]:['number','integer'].includes(field.value?.type)?0:''})}
function changeValue(text){let value=text;if(isList.value){value=text.split(';').map(s=>s.trim()).filter(Boolean);if(['number','integer','integer_list'].includes(field.value?.type))value=value.map(s=>Number.isFinite(Number(s))?Number(s):s)}patch({value})}
function child(index,value){patch({children:props.modelValue.children.map((node,i)=>i===index?value:node)})}
function add(){patch({children:[...(props.modelValue.children||[]),mediaCategoryLeaf()]})}
function remove(index){patch({children:props.modelValue.children.filter((_,i)=>i!==index)})}
</script>
<template>
 <div class="media-condition" :class="{'is-group':modelValue.kind!=='condition'}" role="group" :aria-label="label">
  <div class="media-condition-heading"><RoundedSelect :model-value="modelValue.kind" :label="`${label}组合`" :options="kinds" fixed-popup :disabled="disabled" @update:model-value="kind"/><button v-if="depth>0" type="button" class="icon-btn danger-text" :aria-label="`删除${label}`" :disabled="disabled" @click="emit('remove')"><Icon name="X" :size="16"/></button></div>
  <div v-if="modelValue.kind==='condition'" class="media-condition-fields"><div class="field"><label>匹配字段</label><RoundedSelect :model-value="modelValue.field" :label="`${label}字段`" :options="fields.map(f=>({value:f.id,label:f.label}))" fixed-popup :disabled="disabled" @update:model-value="changeField"/></div><div class="field"><label>比较方式</label><RoundedSelect :model-value="modelValue.operator" :label="`${label}比较方式`" :options="operators" fixed-popup :disabled="disabled" @update:model-value="changeOperator"/></div><label v-if="!mediaCategoryNoValue(modelValue.operator)" class="media-condition-value">匹配值<NumberInput v-if="['number','integer'].includes(field?.type)&&!isList" :model-value="modelValue.value" :aria-label="`${label}匹配值`" :step="field?.type==='integer'?1:0.1" :disabled="disabled" @update:model-value="patch({value:$event})"/><input v-else :value="valueText" :aria-label="`${label}匹配值`" :placeholder="isList?'多个值用英文分号隔开':field?.example" maxlength="8192" :disabled="disabled" required @input="changeValue($event.target.value)"/><small v-if="isList">{{modelValue.operator==='between'?'填写起点;终点':'多个值用英文分号隔开'}}<template v-if="field?.example">，如 {{field.example}}</template></small></label></div>
  <div v-else class="media-condition-children"><MediaCategoryCondition v-for="(node,i) in modelValue.children" :key="i" :model-value="node" :fields="fields" :depth="depth+1" :label="`${label}.${i+1}`" :disabled="disabled" @update:model-value="child(i,$event)" @remove="remove(i)"/><p v-if="!modelValue.children?.length" class="error-message">请添加至少一个条件</p><button v-if="modelValue.kind!=='not'||!modelValue.children?.length" class="btn media-condition-add" type="button" :disabled="disabled||(modelValue.children?.length||0)>=128" :aria-label="`添加${label}子条件`" @click="add"><Icon name="Plus" :size="15"/>添加条件</button></div>
 </div>
</template>
<style scoped>
.media-condition{min-width:0;border:1px solid var(--border);border-radius:8px;padding:12px;background:var(--layer-rule)}.media-condition-heading{display:flex;align-items:center;gap:8px;margin-bottom:10px}.media-condition-heading>.rounded-select{width:148px;min-width:0}.media-condition-heading>.icon-btn{margin-left:auto;width:32px;height:32px;min-height:32px}.media-condition-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.media-condition-fields>.field{min-width:0}.media-condition-fields label{font-size:12px;margin:0 0 7px}.media-condition-value{grid-column:1/-1;min-width:0}.media-condition-value>input{width:100%;height:40px}.media-condition-value small{font-size:12px;color:var(--muted);font-weight:400;line-height:1.6}.media-condition-children{display:grid;gap:10px;min-width:0}.is-group>.media-condition-children{padding-left:10px;border-left:2px solid var(--border)}.media-condition-add{justify-self:start;min-height:36px;font-size:12px}.media-condition :deep(.rounded-select){min-width:0;max-width:100%}.media-condition :deep(.rounded-select-trigger){height:40px;min-width:0;max-width:100%}.media-condition :deep(.select-label){overflow:hidden;min-width:0}.media-condition :deep(.select-label+svg){flex:none}.media-condition :deep(.number-control){height:40px;width:100%}
@media(max-width:480px){.media-condition{padding:10px}.media-condition-fields{grid-template-columns:minmax(0,1fr)}.is-group>.media-condition-children{padding-left:4px}.media-condition-heading>.rounded-select{width:140px}}
</style>
