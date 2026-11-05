<script setup>
import {computed,ref} from 'vue'
import Icon from './Icon.vue'
import {reorderRuleItems} from '../naming-template'
import {useRuleReorder} from '../useRuleReorder'
const props=defineProps({modelValue:{type:Object,required:true},catalog:{type:Object,required:true},disabled:Boolean})
const emit=defineEmits(['update:modelValue'])
const selected=ref(null),groups=computed(()=>props.modelValue.groups)
const meta=id=>props.catalog.groups.find(g=>g.id===id)
const extras=g=>[...new Set([...meta(g.id).defaults,...meta(g.id).extras])].filter(v=>!g.values.includes(v))
function patch(id,value){emit('update:modelValue',{groups:groups.value.map(g=>g.id===id?{...g,...value}:g)})}
function moveGroup(from,to){const i=groups.value.findIndex(g=>g.id===from),j=groups.value.findIndex(g=>g.id===to);emit('update:modelValue',{groups:reorderRuleItems(groups.value,i,j)})}
function shiftGroup(i,d){if(i+d>=0&&i+d<groups.value.length)moveGroup(groups.value[i].id,groups.value[i+d].id)}
function moveValue(from,to){const [id,index]=from.split(':'),[target,dest]=to.split(':');const g=groups.value.find(g=>g.id===id);if(!g||id!==target||+dest<0||+dest>=g.values.length)return;patch(id,{values:reorderRuleItems(g.values,+index,+dest)});selected.value={id,index:+dest}}
function removeValue(id,index){const g=groups.value.find(g=>g.id===id);patch(id,{values:g.values.filter((v,i)=>i!==index)});selected.value=null}
const valueDrag=useRuleReorder('.upgrade-value',moveValue,()=>props.disabled)
function reset(){emit('update:modelValue',JSON.parse(JSON.stringify(props.catalog.defaults)));selected.value=null}
defineExpose({reset})
</script>
<template>
 <section class="upgrade-rule-editor" aria-label="洗版评分规则">
  <article v-for="(group,i) in groups" :key="group.id" class="upgrade-group" :class="{disabled:!group.enabled}" :data-sort-id="group.id" :aria-label="`评分分组 ${meta(group.id).label}`">
   <header><span class="upgrade-number">#{{i+1}}</span><input type="checkbox" class="switch" role="switch" :aria-label="`启用评分 ${meta(group.id).label}`" :checked="group.enabled" :disabled="disabled" @change="patch(group.id,{enabled:$event.target.checked})"/><strong>{{meta(group.id).label}}</strong><code class="upgrade-code">（{{group.id}}）</code><div class="upgrade-group-actions"><button class="icon-btn" type="button" :aria-label="`上移分组 ${meta(group.id).label}`" :disabled="disabled||i===0" @click="shiftGroup(i,-1)"><Icon name="ArrowUp" :size="16"/></button><button class="icon-btn" type="button" :aria-label="`下移分组 ${meta(group.id).label}`" :disabled="disabled||i===groups.length-1" @click="shiftGroup(i,1)"><Icon name="ArrowDown" :size="16"/></button></div></header>
   <p v-if="group.id==='file_size'" class="upgrade-size-note">文件越大得分越高</p>
   <template v-else><div class="upgrade-values" :aria-label="`${meta(group.id).label}优先级`"><div v-for="(value,n) in group.values" :key="value" class="upgrade-value" :class="{'sort-target':valueDrag.target.value===`${group.id}:${n}`,selected:selected?.id===group.id&&selected.index===n}" :data-sort-id="`${group.id}:${n}`"><button type="button" class="rule-sort-handle" :aria-label="`拖动${meta(group.id).label}标签 ${value}`" :disabled="disabled" @pointerdown="valueDrag.begin($event,`${group.id}:${n}`)" @keydown.alt.left.prevent="moveValue(`${group.id}:${n}`,`${group.id}:${n-1}`)" @keydown.alt.right.prevent="moveValue(`${group.id}:${n}`,`${group.id}:${n+1}`)"><Icon name="GripVertical" :size="13"/></button><button type="button" class="upgrade-value-label" :aria-label="`选择${meta(group.id).label}标签 ${value}`" :disabled="disabled" @click="selected={id:group.id,index:n}">{{value}}</button><button type="button" class="upgrade-value-remove" :aria-label="`移除${meta(group.id).label}标签 ${value}`" :disabled="disabled" @click="removeValue(group.id,n)"><Icon name="X" :size="13"/></button></div><span v-if="!group.values.length" class="muted">尚未添加优先标签</span></div>
   <div v-if="selected?.id===group.id&&group.values[selected.index]" class="upgrade-value-actions" role="group" aria-label="标签调序"><span>{{group.values[selected.index]}}</span><button type="button" class="btn" :disabled="disabled||selected.index===0" @click="moveValue(`${group.id}:${selected.index}`,`${group.id}:${selected.index-1}`)">前移标签</button><button type="button" class="btn" :disabled="disabled||selected.index===group.values.length-1" @click="moveValue(`${group.id}:${selected.index}`,`${group.id}:${selected.index+1}`)">后移标签</button></div>
   <div class="upgrade-extras"><button v-for="value in extras(group)" :key="value" type="button" :aria-label="`添加${meta(group.id).label}标签 ${value}`" :disabled="disabled" @click="patch(group.id,{values:[...group.values,value]})">+ {{value}}</button></div></template>
  </article>
  <p class="rule-editor-help">落败版本归入整理归档任务设置的冗余归类目录；未设置目录时保留。整理执行流程接入后生效。</p>
 </section>
</template>
<style scoped>
.upgrade-code{font-size:12px;font-weight:400;color:var(--muted);overflow-wrap:anywhere}
.upgrade-group{border:1px solid var(--border);background:var(--layer-item);border-radius:10px;padding:14px;margin:14px 0}.upgrade-group>header{display:flex;align-items:center;gap:9px;flex-wrap:wrap;min-width:0}.upgrade-group>header strong{font-size:14px}.upgrade-number{color:var(--muted);font-weight:600;font-size:13px}.upgrade-weight{font-size:12px;color:var(--muted)}.upgrade-group-actions{display:flex;margin-left:auto;gap:2px}.upgrade-group-actions .icon-btn{width:30px;height:32px;min-height:32px}.upgrade-group .switch{width:36px;height:20px;min-height:20px;flex:none}.upgrade-values{display:flex;gap:7px;flex-wrap:wrap;margin-top:16px;min-width:0}.upgrade-value{display:flex;align-items:center;max-width:100%;border:1px solid transparent;border-radius:6px;background:var(--primary-soft);color:var(--primary)}.upgrade-value-label,.upgrade-value-remove{border:0;background:none;color:inherit;cursor:pointer;padding:6px 2px;min-height:30px;font-size:13px;overflow-wrap:anywhere;min-width:0}.upgrade-value-remove{display:grid;place-items:center;padding:6px}.upgrade-value.selected{border-color:var(--primary)}.upgrade-value .rule-sort-handle{width:20px;min-height:30px;padding:3px}.upgrade-extras{display:flex;gap:6px;flex-wrap:wrap;margin-top:12px;padding-top:10px;border-top:1px dashed var(--border)}.upgrade-extras button{border:0;background:none;color:var(--muted);font-size:12px;min-height:32px;padding:5px;cursor:pointer;overflow-wrap:anywhere}.upgrade-extras button:hover:enabled{color:var(--primary);background:var(--primary-soft);border-radius:5px}.upgrade-group.disabled .upgrade-values,.upgrade-group.disabled .upgrade-extras,.upgrade-group.disabled .upgrade-size-note{opacity:.48}.upgrade-size-note{font-size:12px;color:var(--muted);line-height:1.8;margin:14px 0 0}.upgrade-value-actions{display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-top:10px;font-size:12px}.upgrade-value-actions .btn{font-size:12px;min-height:32px;padding:5px 9px}.sort-target{outline:2px dashed var(--primary);outline-offset:2px}.sort-active{opacity:.6}
@media(max-width:480px){.upgrade-group{padding:10px}.upgrade-group>header{gap:6px}.upgrade-weight{flex-basis:100%;margin-left:30px;order:1}}
</style>
<style scoped>.upgrade-number{display:grid;place-items:center;min-width:36px;height:30px;padding:0 5px;border-radius:6px;color:var(--primary);background:var(--primary-soft);font-size:16px;font-weight:700;font-variant-numeric:tabular-nums}</style>
