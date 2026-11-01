import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page,provider='mobile',count=45){
  const cards=Array.from({length:count},(_,i)=>({id:`c${i+1}`,name:`分享 ${i+1}`,url:provider==='mobile'?`https://yun.139.com/shareweb/Code${i+1}?pwd=Ab12`:`https://pan.quark.cn/s/code${i+1}`,directory:'/media/library',directoryName:`目录${i+1}`,cron:'0 */6 * * *',enabled:true,cascadeCleanup:provider==='mobile'?'off':'on',result:{status:'completed',message:'新增STRM 3 条，跳过 2 个',lastRun:'2026-10-31T08:00:00Z'}}))
  const cfg={storageIds:['main'],directory:'/media/library',mediaExtensions:'mkv;mp4',metadataExtensions:'nfo;jpg',...(provider==='mobile'?{playbackMode:'redirect'}:{invalidDays:15,retentionHours:12})}
  const writes=[],unexpected=[]
  await page.route('**/api/**',route=>{
    const request=route.request(),path=new URL(request.url()).pathname,method=request.method(),base=`/api/virtual-library/${provider}`
    let json
    if(path==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(path==='/api/state')json={username:'mobile-library',storages:[{id:'main',type:provider,name:'主账号',enabled:true,config:{mode:'native'}},{id:'second',type:provider,name:'备用账号',enabled:true,config:{mode:'native'}},{id:'gateway',type:'mobile',name:'网关存储',enabled:true,config:{mode:'gateway'}}],tasks:[],settings:{},traffic:{},cache:{}}
    else if(path===base&&method==='GET')json={cards,settings:cfg,stats:{strm:count*3,metadata:0}}
    else if(path===base+'/resolve')json={name:'移动分享名称'}
    else if(path===base+'/settings'&&method==='PUT'){writes.push({method,path,body:request.postDataJSON()});Object.assign(cfg,request.postDataJSON());json=cfg}
    else if(path.startsWith(base+'/')&&method==='DELETE'){const index=cards.findIndex(c=>c.id===path.split('/').at(-1));cards.splice(index,1);json={ok:true}}
    else if(path.endsWith('/run')&&path.startsWith(base+'/')&&method==='POST'){writes.push({method,path});json={message:'已在后台开始生成'}}
    else if((path===base&&method==='POST')||(path.startsWith(base+'/')&&method==='PUT')){
      const body=request.postDataJSON();writes.push({method,path,body})
      const id=method==='POST'?'new':path.split('/').at(-1),card=cards.find(c=>c.id===id)
      if(card)Object.assign(card,body)
      else cards.push({...body,id,result:{status:'idle'}})
      json={id,message:'已在后台开始生成'}
    }else if(['/api/traffic','/api/plugins/proxy'].includes(path))json={}
    else if(path==='/api/strm-replace')json={tasks:[]}
    else if(path==='/api/cron/validate')json={valid:true}
    else{unexpected.push(method+' '+path);return route.fulfill({status:500,json:{error:'Unexpected API '+path}})}
    return route.fulfill({json})
  })
  return{cards,writes,unexpected,cfg}
}

for(const provider of ['mobile','quark'])for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'light']]){
  test(`share library virtual rows and twenty-item page height ${provider} ${width}`,async({page},info)=>{
    const f=await fixture(page,provider)
    await page.setViewportSize({width,height});await page.goto('/virtual/'+provider)
    await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
    const window=page.getByRole('region',{name:provider==='mobile'?'移动分享链接':'夸克分享链接',exact:true})
    await expect(window).toBeVisible()
    const rows=window.locator('.share-library-rows')
    const expected=await rows.evaluate(el=>el.clientWidth<700?152:112)
    await expect(rows).toHaveCSS('height',`${expected*20}px`)
    const rowSize=expected
    await expect(window.getByRole('button',{name:'编辑 分享 1',exact:true})).toBeVisible()
    expect(await window.locator('.share-library-row').count()).toBeLessThan(20)
    const first=window.locator('.share-library-row').first(),second=window.locator('.share-library-row').nth(1)
    expect((await second.boundingBox()).y-(await first.boundingBox()).y).toBe(rowSize)
    await captureScreenshot(page,{path:info.outputPath(`rows-${provider}-${width}.png`)})
    const jump=window.getByRole('spinbutton',{name:'跳转页码',exact:true})
    await jump.scrollIntoViewIfNeeded()
    await jump.fill('999');await expect(jump).toHaveValue('3')
    await expect(window.getByRole('status',{name:'当前页'})).toHaveText('1 / 3')
    await jump.press('Enter')
    await expect(window.getByRole('button',{name:'编辑 分享 41',exact:true})).toBeVisible()
    await expect(window.locator('.share-library-row')).toHaveCount(5)
    await expect.poll(()=>rows.evaluate(el=>el.clientHeight)).toBe(rowSize*5)
    await captureScreenshot(page,{path:info.outputPath(`last-page-${provider}-${width}.png`)})
    await jump.scrollIntoViewIfNeeded();await jump.fill('0');await expect(jump).toHaveValue('1');await jump.press('Enter')
    await expect(window.getByRole('button',{name:'编辑 分享 1',exact:true})).toBeVisible()
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    expect(f.unexpected).toEqual([])
  })
}

test('mobile settings bind native accounts and save only mobile playback fields',async({page},info)=>{
  const f=await fixture(page,'mobile',2)
  await page.goto('/virtual/mobile')
  await page.getByRole('button',{name:'移动链库设置',exact:true}).click()
  await expect(page.getByRole('heading',{name:'移动链库设置',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'绑定 主账号',exact:true})).toBeDisabled()
  await expect(page.getByRole('button',{name:'绑定 网关存储',exact:true})).toHaveCount(0)
  await page.getByRole('button',{name:'绑定 备用账号',exact:true}).click()
  await expect(page.getByRole('spinbutton',{name:'临时转存保活期',exact:true})).toHaveCount(0)
  await page.getByRole('button',{name:'播放模式',exact:true}).click()
  await page.getByRole('option',{name:'原文件中继',exact:true}).click()
  await page.getByRole('button',{name:'保存设置',exact:true}).click()
  await expect.poll(()=>f.writes.length).toBe(1)
  expect(f.writes[0].body).toEqual({storageIds:['main','second'],directory:'/media/library',mediaExtensions:'mkv;mp4',metadataExtensions:'nfo;jpg',playbackMode:'relay'})
  await captureScreenshot(page,{path:info.outputPath('mobile-settings.png')})
  await page.getByRole('button',{name:'返回页面',exact:true}).click()
  await expect(page).toHaveURL(/\/virtual\/mobile$/)
  expect(f.unexpected).toEqual([])
})

test('mobile share editor keeps more options and creates a readable share entry',async({page},info)=>{
  const f=await fixture(page,'mobile',0)
  await page.goto('/virtual/mobile')
  await page.getByRole('button',{name:'添加链接',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'添加移动链接',exact:true})
  await expect(dialog.getByRole('button',{name:'级联清理',exact:true})).toHaveCount(0)
  await dialog.getByRole('textbox',{name:'分享链接',exact:true}).fill('https://yun.139.com/shareweb/#/w/i/code?pwd=Ab12')
  await dialog.getByRole('textbox',{name:'分享名称',exact:true}).focus()
  await expect(dialog.getByRole('textbox',{name:'分享名称',exact:true})).toHaveValue('移动分享名称')
  await expect(dialog.getByRole('textbox',{name:'目录名称',exact:true})).toHaveValue('移动分享名称')
  await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
  await expect(dialog.getByRole('button',{name:'STRM刮削',exact:true})).toHaveText('不参与')
  await dialog.getByRole('textbox',{name:'排除类型',exact:true}).fill('iso')
  await captureScreenshot(page,{path:info.outputPath('mobile-editor.png')})
  await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  await expect.poll(()=>f.writes.length).toBe(1)
  expect(f.writes[0].body).toMatchObject({name:'移动分享名称',directoryName:'移动分享名称',cascadeCleanup:'off',scrapeEnabled:false,excludeTypes:'iso'})
  expect(f.unexpected).toEqual([])
})

test('removing the only last-page item clamps the current page and jump input',async({page})=>{
  const f=await fixture(page,'mobile',21)
  await page.goto('/virtual/mobile')
  const jump=page.getByRole('spinbutton',{name:'跳转页码',exact:true})
  await jump.scrollIntoViewIfNeeded();await jump.fill('2');await jump.press('Enter')
  await page.getByRole('button',{name:'移除 分享 21',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'移除移动链接',exact:true})
  await expect(dialog).toContainText('本地文件保留')
  await dialog.getByRole('button',{name:'确认移除',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  await expect(page.getByRole('status',{name:'当前页'})).toHaveText('1 / 1')
  await expect(jump).toHaveValue('1')
  await expect(page.getByRole('button',{name:'下一页',exact:true})).toBeDisabled()
  expect(f.cards).toHaveLength(20);expect(f.unexpected).toEqual([])
})
