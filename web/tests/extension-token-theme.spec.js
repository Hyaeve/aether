import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

for(const width of [1440,390])for(const theme of ['light','dark'])test(`extension tokens share the pale starfield palette ${width} ${theme}`,async({page},info)=>{
  await page.setViewportSize({width,height:900})
  let settings={version:1,ignoredExtensions:['tmp','parts']}
  await page.route('**/api/**',route=>{
    const req=route.request(),path=new URL(req.url()).pathname
    let json={}
    if(path==='/api/auth/status')json={initialized:true,authenticated:true}
    if(path==='/api/state')json={username:'token-theme',storages:[{id:'pan',type:'115',name:'115影视',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{}}
    if(['/api/transfers','/api/backup-rules'].includes(path))json=[]
    if(path==='/api/115-rapid')json={manual:{source:'',storageId:'',target:'',extensions:'',successAction:'delete',failureAction:'skip'},watches:[]}
    if(path==='/api/115-rapid/records')json={items:[],running:false}
    if(path==='/api/transfer/settings'){if(req.method()==='PUT')settings=req.postDataJSON();json=settings}
    return route.fulfill({json})
  })
  await page.goto('/transfer/backup')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await page.locator('.task-heading').getByRole('button',{name:'添加规则',exact:true}).click()
  let modal=page.getByRole('dialog',{name:'添加备份规则',exact:true})
  await modal.getByRole('button',{name:'筛选规则',exact:true}).click()
  const expected=theme==='light'?'linear-gradient(120deg, rgb(230, 235, 250), rgb(237, 231, 248))':'linear-gradient(120deg, rgb(48, 61, 97), rgb(64, 54, 92))'
  async function addTokensAndCheck(scope,name){
    await scope.getByRole('button',{name:'添加规则',exact:true}).click()
    await page.getByRole('dialog',{name:'选择规则类型',exact:true}).getByRole('button',{name:'扩展名',exact:true}).click()
    const input=scope.getByRole('textbox',{name:'匹配内容 1',exact:true})
    await input.fill('MKV;mp4');await input.press('Enter')
    await expect(scope.locator('.backup-token > span')).toHaveText(['mkv','mp4'])
    for(const token of await scope.locator('.backup-token').all())await expect(token).toHaveCSS('background-image',expected)
    await expect(input).toHaveCSS('background-color',theme==='light'?'rgb(255, 255, 255)':'rgb(41, 44, 52)')
    await captureScreenshot(page,{path:info.outputPath(`${name}-${width}-${theme}.png`)})
    expect(await scope.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    await scope.getByRole('button',{name:'删除 mkv 规则 1',exact:true}).click()
    await expect(scope.locator('.backup-token > span')).toHaveText(['mp4'])
  }
  await addTokensAndCheck(modal,'backup-tokens')
  await modal.getByRole('button',{name:'关闭',exact:true}).click()
  await page.goto('/tools/115-rapid')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await page.getByRole('button',{name:'添加监控',exact:true}).click()
  modal=page.getByRole('dialog',{name:'添加监控',exact:true})
  await addTokensAndCheck(modal,'monitor-tokens')
  await modal.getByRole('button',{name:'关闭',exact:true}).click()
  await page.goto('/settings/transfer')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(page.locator('.extension-chip').first()).toHaveCSS('background-image',expected)
  await page.getByRole('textbox',{name:'添加忽略扩展名',exact:true}).fill('mkv')
  await page.getByRole('textbox',{name:'添加忽略扩展名',exact:true}).press('Enter')
  await expect(page.getByRole('button',{name:'移除 mkv',exact:true})).toHaveCSS('background-image',expected)
  await captureScreenshot(page,{path:info.outputPath(`global-tokens-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'移除 mkv',exact:true}).click()
  await expect(page.getByRole('button',{name:'移除 mkv',exact:true})).toHaveCount(0)
})
