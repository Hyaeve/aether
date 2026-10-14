<script setup>
import Icon from './Icon.vue'
import ExtensionPresets from './ExtensionPresets.vue'
import RoundedSelect from './RoundedSelect.vue'
import {useId} from 'vue'
const id=useId()
const props=defineProps({modelValue:Object,manual:Boolean})
const emit=defineEmits(['update:modelValue','source','target','commit'])
function update(key,value){emit('update:modelValue',{...props.modelValue,[key]:value})}
</script>
<template>
  <div class="rapid115-fields" :class="{manual}">
    <div class="rapid115-pair"><label>待传目录<button type="button" class="source-trigger directory-choice" aria-label="选择待传目录" @click="emit('source')"><span>{{modelValue.source||'选择容器目录'}}</span><Icon name="FolderOpen" /></button></label><label>目标目录<button type="button" class="source-trigger directory-choice" aria-label="选择秒传目标目录" @click="emit('target')"><span>{{modelValue.targetLabel||'选择115目录'}}</span><Icon name="FolderOpen" /></button></label></div>
    <div class="rapid115-pair"><label>秒传成功处理<RoundedSelect :model-value="modelValue.successAction" label="秒传成功处理" :options="[{value:'delete',label:'清理源文件'},{value:'keep',label:'保留源文件'}]" @update:model-value="update('successAction',$event);emit('commit')" /></label><label>秒传失败处理<RoundedSelect :model-value="modelValue.failureAction" label="秒传失败处理" :options="[{value:'skip',label:'跳过'},{value:'upload',label:'传输'}]" @update:model-value="update('failureAction',$event);emit('commit')" /></label></div>
    <div class="rapid115-extensions"><div><label :for="id">秒传文件扩展名</label><ExtensionPresets :model-value="modelValue.extensions" @update:model-value="update('extensions',$event)" @commit="emit('commit')" /></div><input :id="id" :value="modelValue.extensions" aria-label="秒传文件扩展名" @input="update('extensions',$event.target.value)" @keydown.enter.prevent="emit('commit')" @blur="emit('commit')" /></div>
  </div>
</template>
<style scoped>
.rapid115-fields{display:grid;gap:16px}.rapid115-pair{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.rapid115-fields label{display:grid;gap:8px;min-width:0;font-size:14px}.directory-choice{justify-content:flex-start;width:100%;height:40px;background:var(--input)}.directory-choice svg{flex:none}.directory-choice span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.rapid115-extensions>div{display:flex;align-items:center;justify-content:space-between;margin-bottom:8px}.rapid115-extensions>div>span{display:flex;gap:4px}.rapid115-extensions .icon-btn{width:30px;height:30px;min-height:30px}.rapid115-extensions .active{color:var(--primary);background:var(--primary-soft)}.rapid115-extensions input{width:100%}
@media(max-width:440px){.rapid115-pair{gap:10px}}
.manual .rapid115-pair:nth-child(2){order:3}.manual .rapid115-extensions{order:2}
</style>
<style scoped>.directory-choice{justify-content:space-between;height:var(--control-height);min-height:var(--control-height);padding:0 12px}.rapid115-fields :deep(.rounded-select-trigger){height:var(--control-height)}.rapid115-extensions input{height:var(--control-height)}</style>
