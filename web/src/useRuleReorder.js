import {onUnmounted,ref} from 'vue'
// Handles only: normal touch scrolling and input interaction remain available.
export function useRuleReorder(selector,onMove,disabled=()=>false){
 const target=ref(''),active=ref('');let pointer
 function cancel(){pointer?.ghost.remove();if(pointer?.handle.hasPointerCapture?.(pointer.id))pointer.handle.releasePointerCapture(pointer.id);pointer=null;target.value='';active.value=''}
 function begin(event,id){
  if(event.button!==0||event.isPrimary===false||disabled())return
  event.preventDefault();event.stopPropagation();cancel()
  const handle=event.currentTarget,row=handle.closest(selector),r=row.getBoundingClientRect(),ghost=row.cloneNode(true),css=getComputedStyle(row)
  ghost.removeAttribute('id');ghost.querySelectorAll('[id]').forEach(node=>node.removeAttribute('id'));ghost.setAttribute('aria-hidden','true');ghost.inert=true;ghost.classList.add('rule-drag-ghost')
  Object.assign(ghost.style,{position:'fixed',left:`${r.left}px`,top:`${r.top}px`,width:`${r.width}px`,height:`${r.height}px`,boxSizing:'border-box',margin:'0',pointerEvents:'none',zIndex:'10050',opacity:'.94',background:css.backgroundColor,color:css.color,border:css.border,borderRadius:css.borderRadius,boxShadow:'0 5px 18px #0002',transform:'translate3d(0,0,0)'})
  Object.assign(ghost.style,{display:css.display,padding:css.padding,gap:css.gap,alignItems:css.alignItems,font:css.font,lineHeight:css.lineHeight})
  document.body.append(ghost);pointer={id:event.pointerId,key:String(id),handle,parent:row.parentElement,ghost,x:event.clientX,y:event.clientY};active.value=String(id);handle.setPointerCapture?.(event.pointerId)
 }
 function move(event){if(!pointer||pointer.id!==event.pointerId)return;if(disabled()){cancel();return}event.preventDefault();pointer.ghost.style.transform=`translate3d(${event.clientX-pointer.x}px,${event.clientY-pointer.y}px,0)`;const row=document.elementFromPoint(event.clientX,event.clientY)?.closest(selector);target.value=row?.parentElement===pointer.parent?row.dataset.sortId||'':''}
 function up(event){if(!pointer||pointer.id!==event.pointerId)return;const from=pointer.key,to=target.value,locked=disabled();cancel();if(!locked&&to!==''&&to!==from)onMove(from,to)}
 function key(event){if(event.key==='Escape'&&pointer){event.preventDefault();event.stopPropagation();cancel()}}
 document.addEventListener('pointermove',move,{passive:false});document.addEventListener('pointerup',up);document.addEventListener('pointercancel',cancel);document.addEventListener('keydown',key,true);window.addEventListener('blur',cancel)
 onUnmounted(()=>{cancel();document.removeEventListener('pointermove',move);document.removeEventListener('pointerup',up);document.removeEventListener('pointercancel',cancel);document.removeEventListener('keydown',key,true);window.removeEventListener('blur',cancel)})
 return {begin,target,active,cancel}
}
