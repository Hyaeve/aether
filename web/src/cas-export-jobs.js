import {api,notices,notify,state} from './lib'
const tracked=new Map()
let timer,reading=false
export function trackCASExport(job){
 if(tracked.has(job.id))return
 const notice={id:`cas-export:${job.id}`,message:'CAS导出 · 排队等待',progress:0,persistent:true}
 notices.push(notice);tracked.set(job.id,{notice:notices.at(-1),owner:state.username})
 if(!timer)timer=setInterval(refresh,1000)
 refresh()
}
async function refresh(){
 if(reading)return;reading=true
 try{for(const [id,item] of tracked){
  if(!state.authenticated||item.owner!==state.username){remove(id,item);continue}
  try{
   const job=await api(`/files/cas-export/jobs/${id}`)
   if(!state.authenticated||item.owner!==state.username){remove(id,item);continue}
   item.notice.message=`CAS导出 · ${job.status==='queued'?'排队等待':job.total?`${job.processed} / ${job.total}`:job.message}`
   item.notice.progress=job.total?job.processed/job.total:0
   if(['success','error'].includes(job.status)){
    remove(id,item)
    if(job.status==='success'&&job.download){const anchor=document.createElement('a');anchor.href=`/api/files/cas-export/jobs/${id}/download`;anchor.download=job.name||'CAS.zip';document.body.append(anchor);anchor.click();anchor.remove()}
    notify(job.status==='success'?'CAS导出完成':'CAS导出失败，请查看系统日志',job.status==='error')
   }
  }catch(e){if(e.status===404){remove(id,item);notify('CAS导出任务已失效，服务可能已重启；请检查日志和导出目录',true)}else{item.notice.message='CAS导出 · 状态查询失败，稍后重试';if(!state.authenticated)remove(id,item)}}
 }}finally{reading=false;if(!tracked.size){clearInterval(timer);timer=null}}
}
function remove(id,item){tracked.delete(id);const index=notices.findIndex(n=>n.id===item.notice.id);if(index>=0)notices.splice(index,1)}
