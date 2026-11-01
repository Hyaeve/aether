import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function setup(page) {
 const cards=[{id:'old',url:'https://pan.quark.cn/s/old',name:'旧分享',directory:'/media',directoryName:'旧分享',enabled:false,result:{status:'idle'}}],writes=[],unknown=[]
 const settings={storageIds:['q'],directory:'/media',invalidDays:15,retentionHours:12,mediaExtensions:'mkv;mp4',metadataExtensions:'nfo;jpg'}
 await page.route('**/api/**',route=>{
  const request=route.request(),url=new URL(request.url()),p=url.pathname,method=request.method()
  let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'share-options',storages:[{id:'q',name:'夸克',type:'quark',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{},scrapeLibraries:cards.filter(c=>c.enabled&&c.scrapeEnabled).map(c=>({id:`quark-share:${c.id}`,name:c.name,status:c.result?.status||'completed'}))}
  else if(p==='/api/traffic')json={}
  else if(p==='/api/strm-replace')json={running:false}
  else if(p==='/api/generation/jobs')json=[]
  else if(p==='/api/virtual-library/quark'&&method==='GET')json={cards,settings,stats:{}}
  else if(p==='/api/virtual-library/quark/resolve')json={name:'电影合集'}
  else if((p==='/api/virtual-library/quark'&&method==='POST')||(p.startsWith('/api/virtual-library/quark/')&&method==='PUT')){
   const body=request.postDataJSON()
   writes.push(body)
   const id=method==='POST'?'new':p.split('/').at(-1),old=cards.find(c=>c.id===id)
   if(old)Object.assign(old,body)
   else cards.push({...body,id,result:{status:'completed'}})
   json={id}
  }
  else if(p==='/api/strm-scrape/status')json={running:false}
  else if(p==='/api/strm-scrape/settings')json={writeMode:'missing',episodes:true,fanart:false,actors:false,excluded:''}
  else if(p==='/api/strm-scrape/items')json=[]
  else if(p==='/api/strm-scrape/directories')json={root:'/media/电影合集',directories:[]}
  else {unknown.push(`${method} ${p}`);return route.fulfill({status:500,json:{error:`Unexpected ${method} ${p}`}})}
  return route.fulfill({json})
 })
 return {cards,writes,unknown}
}

const layouts=[{width:1440,height:1000,theme:'light'},{width:390,height:844,theme:'dark'},{width:375,height:812,theme:'light'},{width:844,height:390,theme:'dark'}]
for(const layout of layouts)test(`share card more options roundtrip and scrape opt-in ${layout.width} ${layout.theme}`,async({page},info)=>{
 const fixture=await setup(page)
 await page.setViewportSize({width:layout.width,height:layout.height})
 await page.goto('/virtual/quark')
 await page.evaluate(theme=>document.documentElement.dataset.theme=theme,layout.theme)
 await page.getByRole('button',{name:'添加链接',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加夸克链接',exact:true}),more=dialog.getByRole('button',{name:'更多选项',exact:true})
 await expect(more).toHaveAttribute('aria-expanded','false')
 await expect(dialog.getByRole('textbox',{name:'排除目录',exact:true})).toHaveCount(0)
 await more.click()
 await expect(more).toHaveAttribute('aria-expanded','true')
 await expect(more.locator('svg')).toHaveClass(/lucide-chevron-down/)
 const scrape=dialog.getByRole('button',{name:'STRM刮削',exact:true})
 await expect(scrape).toHaveText('不参与')
 await scrape.click();await page.getByRole('option',{name:'参与',exact:true}).click()
 await dialog.getByRole('textbox',{name:'排除目录',exact:true}).fill('花絮;预告')
 await dialog.getByRole('textbox',{name:'排除文件',exact:true}).fill('sample')
 await dialog.getByRole('textbox',{name:'排除类型',exact:true}).fill('mkv;错误类型')
 await dialog.getByRole('textbox',{name:'分享链接',exact:true}).fill('https://pan.quark.cn/s/new')
 await dialog.getByRole('textbox',{name:'分享名称',exact:true}).fill('电影合集')
 await dialog.getByRole('textbox',{name:'目录名称',exact:true}).fill('电影合集')
 await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
 await expect(page.locator('.toast-stack')).toContainText('排除类型不合规范')
 expect(fixture.writes).toHaveLength(0)
 await page.locator('.toast').last().getByRole('button',{name:'关闭通知',exact:true}).click()
 await dialog.getByRole('textbox',{name:'排除类型',exact:true}).fill('iso;mp4')
 await expect(dialog.getByRole('button',{name:/漫画扩展名/})).toHaveCount(0)
 for(const names of [['分享链接','分享名称'],['生成位置','目录名称'],['排除文件','排除类型']]){
  const [a,b]=await Promise.all(names.map(name=>dialog.getByRole('textbox',{name,exact:true}).boundingBox()))
  expect(Math.abs(a.y-b.y)).toBeLessThan(1)
  expect(a.x+a.width).toBeLessThanOrEqual(b.x)
 }
 for(const field of [scrape,dialog.getByRole('textbox',{name:'排除目录',exact:true}),dialog.getByRole('textbox',{name:'排除文件',exact:true}),dialog.getByRole('textbox',{name:'排除类型',exact:true})]){
  await field.scrollIntoViewIfNeeded()
  await expect(field).toBeInViewport()
  await expect(field).toHaveCSS('height','40px')
  await expect.poll(()=>field.evaluate(el=>{
   const r=el.getBoundingClientRect(),dialog=el.closest('.modal')
   return r.top>=dialog.querySelector('.modal-header').getBoundingClientRect().bottom&&r.bottom<=dialog.querySelector('.modal-footer').getBoundingClientRect().top
  })).toBe(true)
 }
 await expect(dialog.locator('.quark-library-editor')).toHaveCSS('scrollbar-width','none')
 await expect.poll(()=>dialog.evaluate(el=>el.scrollWidth<=el.clientWidth&&document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.mouse.move(1,1)
 await captureScreenshot(page,{path:info.outputPath(`share-options-${layout.width}-${layout.theme}.png`)})
 await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
 await expect(dialog).toHaveCount(0)
 expect(fixture.writes[0]).toMatchObject({scrapeEnabled:true,excludeDirs:'花絮;预告',excludeFiles:'sample',excludeTypes:'iso;mp4',enabled:true})
 await page.getByRole('button',{name:'编辑 电影合集',exact:true}).click()
 const edit=page.getByRole('dialog',{name:'编辑夸克链接',exact:true})
 await expect(edit.getByRole('button',{name:'更多选项',exact:true})).toHaveAttribute('aria-expanded','false')
 await edit.getByRole('button',{name:'更多选项',exact:true}).click()
 await expect(edit.getByRole('button',{name:'STRM刮削',exact:true})).toHaveText('参与')
 for(const [name,value] of [['排除目录','花絮;预告'],['排除文件','sample'],['排除类型','iso;mp4']])await expect(edit.getByRole('textbox',{name,exact:true})).toHaveValue(value)
 await edit.getByRole('button',{name:'取消',exact:true}).click()
 await page.getByRole('button',{name:'编辑 旧分享',exact:true}).click()
 await edit.getByRole('button',{name:'更多选项',exact:true}).click()
 await expect(edit.getByRole('button',{name:'STRM刮削',exact:true})).toHaveText('不参与')
 await edit.getByRole('button',{name:'取消',exact:true}).click()
 await page.goto('/tasks/scrape')
 await expect(page.getByRole('button',{name:'STRM 任务',exact:true})).toHaveText('电影合集')
 await page.getByRole('button',{name:'STRM 任务',exact:true}).click()
 await expect(page.getByRole('option',{name:'旧分享',exact:true})).toHaveCount(0)
 await expect(page.getByRole('option',{name:'电影合集',exact:true})).toBeVisible()
 expect(fixture.unknown).toEqual([])
})
