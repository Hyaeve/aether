// Move the page's short header first; only then consume wheel input in the list.
export function listWheel(event) {
 if (event.ctrlKey || Math.abs(event.deltaX)>Math.abs(event.deltaY)) return
 const list=event.currentTarget,page=list.closest('.page-scroll .thin-scroll-area')
 if(!page)return
 const delta=event.deltaY*(event.deltaMode===1?16:event.deltaMode===2?page.clientHeight:1)
 const max=page.scrollHeight-page.clientHeight
 let remainder=delta
 if(delta>0 && page.scrollTop<max-1){const used=Math.min(delta,max-page.scrollTop);page.scrollTop+=used;remainder-=used}
 else if(delta<0 && list.scrollTop<=0 && page.scrollTop>0){const used=Math.min(-delta,page.scrollTop);page.scrollTop-=used;remainder+=used}
 if(remainder!==delta){event.preventDefault();if(remainder)list.scrollTop+=remainder}
}
