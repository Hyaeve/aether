<script setup>
import { computed } from 'vue'
import { changedNameParts } from '../name-diff'
const props = defineProps({ name: { type: String, default: '' }, other: { type: String, default: '' } })
const parts = computed(() => {
  const diff = changedNameParts(props.other, props.name)
  const changes=diff.filter(part=>part.changed).length, contextSize=changes===1?6:changes===2?3:0
  const fixedChanges=diff.filter(part=>part.changed).reduce((n,part)=>n+Array.from(part.text).length,0)<=12
  return diff.flatMap((part,index) => {
    if (part.changed) return [{...part,priority:true,fixed:fixedChanges}]
    const chars=Array.from(part.text), before=diff[index-1]?.changed?Math.min(contextSize,chars.length):0
    const after=diff[index+1]?.changed?Math.min(contextSize,chars.length-before):0
    if(before+after===chars.length)return [{...part,priority:true}]
    return [
      {text:chars.slice(0,before).join(''),priority:true},
      {text:chars.slice(before,chars.length-after).join(''),priority:false},
      {text:after?chars.slice(-after).join(''):'',priority:true}
    ].filter(p=>p.text)
  })
})
</script>
<template>
  <span class="rename-name" :data-tooltip="name" data-tooltip-fragments data-tooltip-stationary data-tooltip-delay="300"><span v-for="(part,index) in parts" :key="index" class="rename-name-part" :class="{changed:part.changed,priority:part.priority,fixed:part.fixed}">{{part.text}}</span></span>
</template>
<style scoped>
.rename-name{display:flex;align-items:baseline;min-width:0;width:100%;white-space:nowrap;overflow:hidden}
.rename-name-part{flex:0 1000 auto;min-width:0;overflow:hidden;text-overflow:ellipsis}
.rename-name-part.priority{flex-shrink:0}.rename-name-part.changed{flex-shrink:1;color:var(--orange)}.rename-name-part.fixed{flex-shrink:0}
</style>
