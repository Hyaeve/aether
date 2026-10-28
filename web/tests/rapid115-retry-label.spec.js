import {test,expect} from '@playwright/test'

for(const [width,theme] of [[1440,'light'],[390,'dark']])test(`rapid retry success labels stay concise in both record views ${width} ${theme}`,async({page},info)=>{
  const job={source:'/media/incoming',storageId:'p',target:'10',targetLabel:'115 / 目标',extensions:'mkv',successAction:'keep',failureAction:'skip'}
  const items=Array.from({length:4},(_,retries)=>({
    id:`retry-${retries}`,name:`影片-${retries}.mkv`,status:'success',retries,
    message:retries?`重试${retries}次秒传成功`:'秒传成功',
    source:'监控 · 影视',sourceKind:'monitor',size:1024,pickcode:'pickabc',
    time:'2026-10-28T10:00:00Z',done:1024,total:1024,
  }))
  await page.route('**/api/**',route=>{
    const path=new URL(route.request().url()).pathname
    const json=path==='/api/auth/status'?{initialized:true,authenticated:true}
      :path==='/api/state'?{username:'rapid-label',storages:[{id:'p',name:'115',type:'115',enabled:true,config:{}}],tasks:[],settings:{},traffic:{},cache:{}}
      :path==='/api/115-rapid'?{manual:job,watches:[{...job,id:'watch',name:'影视监控',mode:'watch',enabled:true}]}
      :path==='/api/115-rapid/records'?{items,page:1,pages:1,total:4,manualRunning:false,monitorRunning:false}:{}
    return route.fulfill({json})
  })
  await page.setViewportSize({width,height:900})
  await page.goto('/tools/115-rapid')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  const labels=['秒传成功','重试1次成功','重试2次成功','重试3次成功']
  const verify=async root=>{
    await expect(root.locator('.rapid115-status-label>span')).toHaveText(labels)
    for(let i=0;i<labels.length;i++){
      await expect(root.locator('.rapid115-status-label').nth(i)).toHaveAttribute('data-tooltip',labels[i])
      await expect(root.locator('progress').nth(i)).toHaveAttribute('aria-label',`影片-${i}.mkv：${labels[i]}`)
    }
  }
  const records=page.locator('.rapid115-records')
  await verify(records)
  await records.locator('.rapid115-record-table').screenshot({path:info.outputPath(`retry-labels-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'查看 影视监控 秒传记录',exact:true}).click()
  await verify(page.getByRole('dialog',{name:'影视监控 · 秒传记录',exact:true}))
})
