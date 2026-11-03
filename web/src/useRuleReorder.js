import {onUnmounted,ref} from 'vue'
// Handles only: normal touch scrolling and input interaction remain available.
export function useRuleReorder(selector,onMove,disabled=()=>false){
 const target=ref(''),active=ref('');let pointer
 function cancel(){if(pointer?.handle.hasPointerCapture?.(pointer.id))pointer.handle.releasePointerCapture(pointer.id);pointer=null;target.value='';active.value=''}
 function begin(event,id){if(event.button!==0||event.isPrimary===false||disabled())return;event.preventDefault();event.stopPropagation();cancel();const handle=event.currentTarget,row=handle.closest(selector);pointer={id:event.pointerId,key:String(id),handle,parent:row.parentElement};active.value=String(id);handle.setPointerCapture?.(event.pointerId)}
 function move(event){if(!pointer||pointer.id!==event.pointerId)return;if(disabled()){cancel();return}event.preventDefault();const row=document.elementFromPoint(event.clientX,event.clientY)?.closest(selector);target.value=row?.parentElement===pointer.parent?row.dataset.sortId||'':''}
 function up(event){if(!pointer||pointer.id!==event.pointerId)return;const from=pointer.key,to=target.value,locked=disabled();cancel();if(!locked&&to!==''&&to!==from)onMove(from,to)}
 function key(event){if(event.key==='Escape'&&pointer){event.preventDefault();event.stopPropagation();cancel()}}
 document.addEventListener('pointermove',move,{passive:false});document.addEventListener('pointerup',up);document.addEventListener('pointercancel',cancel);document.addEventListener('keydown',key,true);window.addEventListener('blur',cancel)
 onUnmounted(()=>{cancel();document.removeEventListener('pointermove',move);document.removeEventListener('pointerup',up);document.removeEventListener('pointercancel',cancel);document.removeEventListener('keydown',key,true);window.removeEventListener('blur',cancel)})
 return {begin,target,active,cancel}
}
