<script setup>
import {onUnmounted,ref,watch} from 'vue'

const props=defineProps({target:Object})
const visible=ref(false),position=ref({})
let frame=0,observer,element
function update(){
 frame=0
 if(!element)return
 const {clientHeight,scrollHeight,scrollTop,offsetTop}=element
 const overflow=scrollHeight-clientHeight,rail=Math.max(0,clientHeight-20)
 visible.value=overflow>1&&rail>0
 if(!visible.value)return
 const height=Math.min(96,Math.max(28,rail*clientHeight/scrollHeight),rail)
 const progress=Math.max(0,Math.min(1,scrollTop/overflow))
 position.value={top:`${offsetTop+8}px`,height:`${height}px`,transform:`translateY(${progress*(rail-height)}px)`}
}
function schedule(){if(!frame)frame=requestAnimationFrame(update)}
function scroll(){
 if(frame)cancelAnimationFrame(frame)
 update()
}
function detach(){
 observer?.disconnect()
 element?.removeEventListener('scroll',scroll)
 window.removeEventListener('resize',schedule)
 if(frame)cancelAnimationFrame(frame)
 frame=0;observer=null;element=null
}
watch(()=>props.target,value=>{
 detach();visible.value=false
 if(!value)return
 element=value
 element.addEventListener('scroll',scroll,{passive:true})
 window.addEventListener('resize',schedule,{passive:true})
 observer=new ResizeObserver(schedule)
 for(const node of [element,element.parentElement,...element.children])if(node)observer.observe(node)
 schedule()
},{flush:'post',immediate:true})
onUnmounted(detach)
</script>

<template>
 <span v-show="visible" class="sidebar-scroll-hint" :style="position" aria-hidden="true"/>
</template>

<style scoped>
.sidebar-scroll-hint{position:absolute;right:0;width:2px;pointer-events:none;border-radius:2px;background:linear-gradient(to bottom,transparent,color-mix(in srgb,var(--sidebar-brand) 34%,transparent) 24%,color-mix(in srgb,var(--sidebar-brand) 34%,transparent) 76%,transparent);contain:layout style;z-index:1}
</style>
