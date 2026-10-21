import {test,expect} from '@playwright/test'

async function mock(page, handler=()=>({})) {
 await page.route('**/api/**',async r=>{
  const u=new URL(r.request().url());let value
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'document-requirements',storages:[{id:'p',name:'115主账号',type:'115',enabled:true,config:{}}],tasks:[],settings:{},cache:{},logs:[]}
  else value=await handler(u,r.request())
  return r.fulfill({json:value??{}})
 })
}

test('shared extension filter accepts valid tokens from mixed input and has all five presets',async({page},info)=>{
 await mock(page,u=>u.pathname==='/api/backup-rules'||u.pathname==='/api/transfers'?[]:{})
 await page.goto('/transfer/backup');await page.locator('.task-heading').getByRole('button',{name:'添加规则',exact:true}).click()
 const modal=page.getByRole('dialog',{name:'添加备份规则',exact:true})
 await modal.getByRole('button',{name:'筛选规则',exact:true}).click();await modal.getByRole('button',{name:'添加规则',exact:true}).click()
 await page.getByRole('dialog',{name:'选择规则类型'}).getByRole('button',{name:'扩展名',exact:true}).click()
 const input=modal.getByRole('textbox',{name:'匹配内容 1',exact:true})
 await input.fill('MP4;中文;my-ext;_extra;bad/name');await input.press('Enter')
 await expect(modal.locator('.backup-token>span')).toHaveText(['mp4','my-ext','_extra'])
 await expect(page.locator('.toast.error').last()).toContainText('中文、bad/name')
 const buttons=modal.locator('.extension-presets button');await expect(buttons).toHaveCount(5)
 expect(await buttons.evaluateAll(els=>els.every(el=>el.type==='button'))).toBe(true)
 await modal.getByRole('button',{name:'数据扩展名',exact:true}).click()
 await expect(modal.locator('.backup-token>span')).toContainText(['ssa','sub'])
 await modal.getByRole('button',{name:'文档扩展名',exact:true}).click()
 await expect(modal.locator('.backup-token>span')).toContainText(['pdf','docx','rar'])
 await page.screenshot({path:info.outputPath('extension-presets-light.png')})
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.dataset.theme='dark')
 expect(await modal.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 await page.screenshot({path:info.outputPath('extension-presets-dark-mobile.png')})
})

test('download cleanup previews terminal records only and confirms their exact IDs',async({page})=>{
 const posts=[]
 await mock(page,(u,req)=>{
  if(u.pathname!=='/api/cloud/manage')return {}
  if(req.method()==='POST'){posts.push(req.postDataJSON());return {count:req.postDataJSON().ids.length}}
  if(u.searchParams.get('prepare')==='cleanup')return {items:[{id:'ended1',status:'success'},{id:'ended2',status:'interrupted'}]}
  return {items:[{id:'active',name:'正在下载.mkv',status:'running'}],nextPage:0,total:9000}
 })
 await page.goto('/storage/downloads');await expect(page.locator('.cloud-total')).toHaveText('1 项')
 await page.locator('.cloud-toolbar').getByRole('button',{name:'清理',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'确认清理记录',exact:true});await expect(dialog).toContainText('2 项');expect(posts).toHaveLength(0)
 await dialog.getByRole('button',{name:'确认',exact:true}).click()
 await expect.poll(()=>posts.length).toBe(1);expect(posts[0]).toMatchObject({action:'cleanup',ids:['ended1','ended2'],confirm:true})
})

test('recycle confirmation uses six cleartext digits, rejects incomplete PIN and resets on reopen',async({page},info)=>{
 const posts=[]
 await mock(page,(u,req)=>u.pathname==='/api/cloud/manage'?(req.method()==='POST'?(posts.push(req.postDataJSON()),{count:1}):{items:[{id:'r',name:'影片.mkv',size:100,path:'/剧集/Season 1'}],password:true}):{})
 await page.goto('/storage/trash');await page.getByRole('button',{name:'清空',exact:true}).click()
 let dialog=page.getByRole('dialog',{name:'确认清空',exact:true}),inputs=dialog.locator('.pin-input input')
 await expect(inputs).toHaveCount(6);await expect(inputs.first()).toHaveAttribute('type','text')
 await inputs.nth(5).fill('9');await expect(inputs.first()).toHaveValue('');await expect(inputs.nth(5)).toHaveValue('9')
 await expect(dialog.getByRole('button',{name:'确认',exact:true})).toBeDisabled();await inputs.nth(5).fill('')
 await inputs.first().fill('00123');await expect(dialog.getByRole('button',{name:'确认',exact:true})).toBeDisabled()
 await inputs.nth(5).fill('4');await expect.poll(()=>inputs.evaluateAll(els=>els.map(el=>el.value))).toEqual(['0','0','1','2','3','4'])
 await page.screenshot({path:info.outputPath('recycle-pin.png')})
 await dialog.getByRole('button',{name:'确认',exact:true}).click();await expect.poll(()=>posts[0]?.password).toBe('001234')
 await page.getByRole('button',{name:'清空',exact:true}).click();await expect.poll(()=>page.locator('.pin-input input').evaluateAll(els=>els.map(el=>el.value))).toEqual(['','','','','',''])
})

test('video audio tracks can switch where supported and media information remains until explicitly closed',async({page},info)=>{
 await mock(page,u=>u.pathname==='/api/files'?[{id:'v',name:'视频.mp4',url:'/missing-video.mp4',size:1024}]:u.pathname==='/api/files/video-info'?{size:1024,format:{format_name:'matroska',bit_rate:'8000000'},streams:[{index:0,codec_type:'video',codec_name:'hevc',width:1920,height:1080,avg_frame_rate:'24000/1001',color_transfer:'smpte2084'},{index:1,codec_type:'audio',codec_name:'aac',channels:2,sample_rate:'48000'}]}:{})
 await page.goto('/files');await page.getByRole('button',{name:'视频.mp4',exact:true}).dblclick()
 const viewer=page.locator('.video-viewer'),video=viewer.locator('video')
 await video.evaluate(el=>{const list=[{label:'中文',enabled:true},{label:'英文',enabled:false}];Object.defineProperty(el,'audioTracks',{value:list,configurable:true});window.testAudioTracks=list})
 await viewer.getByRole('button',{name:'音轨',exact:true}).hover();await page.getByRole('menuitemradio',{name:'英文',exact:true}).click()
 expect(await page.evaluate(()=>window.testAudioTracks.map(t=>t.enabled))).toEqual([false,true])
 await viewer.focus();await page.keyboard.press('ArrowDown');await expect(viewer.locator('.video-volume-notice')).toContainText('95')
 await viewer.getByRole('button',{name:'全屏',exact:true}).click();await viewer.getByRole('button',{name:'视频信息',exact:true}).click()
 const details=page.getByRole('region',{name:'视频信息',exact:true});await expect(details).toContainText('hevc');await expect(details).toContainText('HDR PQ');await expect(details).toContainText('23.98 fps')
 await viewer.click({position:{x:20,y:20}});await expect(details).toBeVisible()
 await page.screenshot({path:info.outputPath('video-info.png')})
 await details.getByRole('button',{name:'关闭视频信息'}).click();await expect(details).toHaveCount(0)
})

test('background log polling preserves filters and the visible record anchor',async({page})=>{
 const records=Array.from({length:250},(_,i)=>({time:new Date(Date.UTC(2026,9,21,0,0,i)).toISOString(),module:'system',level:'info',message:`persistent-log-${i}`}))
 await mock(page,u=>u.pathname==='/api/logs'?records:{})
 await page.goto('/logs');await expect(page.locator('.log-panel footer')).toHaveText('250 条记录')
 await expect.poll(()=>page.locator('.page-content').evaluate(el=>el.getAnimations().every(a=>a.playState==='finished'))).toBe(true)
 await page.getByRole('textbox',{name:'搜索日志'}).fill('persistent-log')
 const host=page.locator('.page-scroll>.thin-scroll-area')
 await host.evaluate(el=>{el.scrollTop=1800})
 await expect.poll(()=>page.locator('.log-entry').first().getAttribute('data-index')).not.toBe('0')
 const before=await page.locator('.log-entry').first().textContent()
 const y=await page.locator('.log-entry').first().evaluate(el=>el.getBoundingClientRect().top)
 await page.clock.install()
 records.push({time:'2026-10-22T00:00:00Z',module:'system',level:'info',message:'persistent-log-latest'})
 await page.clock.runFor(5100)
 await expect(page.locator('.log-panel footer')).toHaveText('251 条记录')
 await expect(page.getByRole('textbox',{name:'搜索日志'})).toHaveValue('persistent-log')
 await expect(page.locator('.log-entry').first()).toHaveText(before)
 expect(await page.locator('.log-entry').first().evaluate(el=>el.getBoundingClientRect().top)).toBeCloseTo(y,0)
})

test('cloud row operations fit narrow viewports without hiding buttons',async({page},info)=>{
 await mock(page,u=>u.pathname==='/api/cloud/manage'?{items:[{id:'f',name:'媒体.mkv',url:'https://example.com/very-long-link',path:'/剧集/Season 1',status:'error',size:1024}],nextPage:0}:[])
 await page.goto('/storage/downloads')
 for(const width of [390,320]){
  await page.setViewportSize({width,height:844})
  const row=page.locator('.cloud-management-row').first();await expect(row).toBeVisible()
  const right=await row.evaluate(el=>el.getBoundingClientRect().right)
  for(const button of await row.locator('.cloud-row-actions button').all()){
   const rect=await button.boundingBox();expect(rect.x+rect.width).toBeLessThanOrEqual(right)
   await expect(button).toBeInViewport()
  }
  await page.screenshot({path:info.outputPath(`cloud-actions-${width}.png`)})
 }
})
