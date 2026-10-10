<script setup>
import { ref, watch, onUnmounted } from 'vue'
const props = defineProps({ element:Object, thickness:{type:Number,default:3} })
const thumb = ref(null), visible = ref(false)
let observer, mutation, cleanup, drag, frame
function update() {
 cancelAnimationFrame(frame)
 frame = requestAnimationFrame(() => {
  const el=props.element;if(!el || !thumb.value)return
  const h=el.clientHeight,total=el.scrollHeight,size=Math.min(h,Math.max(24,h*h/Math.max(1,total)))
  visible.value=total>h+1
  const scale=(devicePixelRatio || 1)*(visualViewport?.scale || 1)
  Object.assign(thumb.value.style,{height:`${size}px`,transform:`translateY(${el.scrollTop/Math.max(1,total-h)*(h-size)}px)`})
  thumb.value.style.setProperty('--rail-width',`${props.thickness/scale}px`)
 })
}
watch(()=>props.element,el=>{
 cleanup?.();observer?.disconnect();mutation?.disconnect()
 if(!el)return
 observer=new ResizeObserver(update);observer.observe(el)
 mutation=new MutationObserver(update);mutation.observe(el,{childList:true,subtree:true,attributes:true})
 el.addEventListener('scroll',update);window.addEventListener('resize',update);visualViewport?.addEventListener('resize',update)
 cleanup=()=>{el.removeEventListener('scroll',update);window.removeEventListener('resize',update);visualViewport?.removeEventListener('resize',update)}
 update()
},{flush:'post',immediate:true})
function start(e){drag={y:e.clientY,top:props.element.scrollTop};e.currentTarget.setPointerCapture(e.pointerId)}
function move(e){if(!drag)return;const el=props.element,travel=el.clientHeight-thumb.value.clientHeight;if(travel>0)el.scrollTop=drag.top+(e.clientY-drag.y)*(el.scrollHeight-el.clientHeight)/travel}
onUnmounted(()=>{cleanup?.();observer?.disconnect();mutation?.disconnect();cancelAnimationFrame(frame)})
</script>
<template><div v-show="visible" class="scroll-rail" aria-hidden="true"><div ref="thumb" class="scroll-rail-thumb" @pointerdown.prevent="start" @pointermove="move" @pointerup="drag=null" @pointercancel="drag=null" @lostpointercapture="drag=null" /></div></template>
<style scoped>
.scroll-rail{position:absolute;inset:0 0 0 auto;width:8px;pointer-events:none;z-index:3}.scroll-rail-thumb{position:absolute;right:0;top:0;width:8px;pointer-events:auto;touch-action:none}.scroll-rail-thumb::after{content:'';position:absolute;right:1px;inset-block:0;width:var(--rail-width,3px);border-radius:4px;background:color-mix(in srgb,var(--primary) 30%,transparent)}.scroll-rail-thumb:hover::after{background:color-mix(in srgb,var(--primary) 60%,transparent)}
</style>
