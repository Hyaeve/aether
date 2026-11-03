import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page){
 const configs={tmdb:{enabled:true,apiURL:'https://api.tmdb.test',imageURL:'https://image.tmdb.test',apiKey:'********',language:'zh-CN',requestInterval:250},ai:{enabled:false,apiURL:'https://model.test/v1',apiKey:'********',model:'test-model'}}
 const writes=[],unexpected=[];let failRead='',failWrite=false
 await page.route('**/api/**',async route=>{
  const r=route.request(),p=new URL(r.url()).pathname;let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'rules-workspace',storages:[],tasks:[],links:[],settings:{},traffic:{},cache:{},logs:[]}
  else if(/^\/api\/plugins\/(tmdb|ai)$/.test(p)){
   const kind=p.split('/').at(-1)
   if(r.method()==='PUT'){writes.push({kind,...r.postDataJSON()});if(failWrite)return route.fulfill({status:409,json:{error:'配置保存失败，请重试'}});configs[kind]=r.postDataJSON()}
   else if(failRead===kind)return route.fulfill({status:503,json:{error:'读取配置失败'}})
   json=configs[kind]
  }
  else if(/^\/api\/plugins\/(tmdb|ai)\/secret$/.test(p))json=r.postDataJSON().metadataOnly?{length:12}:{value:'test-private'}
  else if(/^\/api\/plugins\/(tmdb|ai)\/test$/.test(p))json={ok:true}
  else if(p==='/api/plugins/emby'||p==='/api/plugins/proxy')json={enabled:false,token:'token'}
  else if(['/api/115-folder-cleanup','/api/115-signin','/api/115-recycle-cleanup'].includes(p))json={enabled:false,rules:[]}
  else if(p==='/api/115-simulcast/enabled')json={enabled:false}
  else if(p==='/api/quark-takeover')json={enabled:false,bindings:[]}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(p==='/api/traffic')json={}
  else if(p==='/api/rule-presets')json=[]
  else if(p==='/api/recognition-rules')json={matchRules:[],namingRules:[],blacklist:[],releaseGroups:[],minSizeMB:0}
  else if(p==='/api/recognition-rules/preview')json={result:r.postDataJSON().name}
  else{unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  await route.fulfill({json})
 })
 return{configs,writes,unexpected,failRead(value){failRead=value},failWrite(value){failWrite=value}}
}
async function navigate(page,name){const menu=page.getByRole('button',{name:'打开导航',exact:true});if(await menu.isVisible())await menu.click();await page.getByRole('navigation',{name:'主导航',exact:true}).getByRole('link',{name,exact:true}).click()}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`rules workshop tabs and migrated configuration ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/rules');await page.evaluate(value=>document.documentElement.dataset.theme=value,theme)
 await expect(page).toHaveURL(/\/rules\/recognition$/)
 const group=page.getByRole('group',{name:'以太工坊',exact:true,includeHidden:true});await expect(group.getByRole('link',{includeHidden:true}).first()).toHaveText('以太识律')
 const tabs=page.getByRole('navigation',{name:'以太识律栏目',exact:true});await expect(tabs.getByRole('link')).toHaveText(['识别规则','命名规则','媒体分类','洗版策略','辅助配置'])
 await expect(page.getByRole('form',{name:'识别规则',exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'添加匹配替换',exact:true})).toBeEnabled();await expect(page.locator('.rule-placeholder,.rule-editor-placeholder')).toHaveCount(0)
 await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('识别规则')
 await tabs.getByRole('link',{name:'命名规则',exact:true}).click();await expect(page.getByRole('button',{name:'添加命名规则预设',exact:true})).toBeEnabled();await expect(page.getByText('整理规则',{exact:true})).toHaveCount(0);await expect(page.locator('.rule-placeholder')).toHaveCount(0)
 await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('命名规则')
 await tabs.getByRole('link',{name:'媒体分类',exact:true}).click();await expect(page.getByRole('button',{name:'添加媒体分类预设',exact:true})).toBeEnabled();await expect(page.locator('.rule-placeholder')).toHaveCount(0)
 await tabs.getByRole('link',{name:'洗版策略',exact:true}).click();await expect(page.getByRole('button',{name:'添加洗版策略预设',exact:true})).toBeEnabled()
 await tabs.getByRole('link',{name:'辅助配置',exact:true}).click();await expect(page.locator('.plugin-settings-panel')).toHaveCount(2)
 await expect(page.getByRole('button',{name:'停用TMDB 配置',exact:true})).toBeEnabled()
 await expect(page.getByRole('dialog')).toHaveCount(0);await expect(page.getByLabel('API 域名',{exact:true})).toHaveValue('https://api.tmdb.test')
 const tmdb=page.getByRole('region',{name:'TMDB 配置',exact:true}),ai=page.getByRole('region',{name:'AI 辅助识别',exact:true})
 await expect(ai.getByLabel('模型名称',{exact:true})).toHaveValue('test-model')
 const panels=await page.locator('.plugin-settings-panel').evaluateAll(nodes=>nodes.map(el=>{const r=el.getBoundingClientRect();return{x:r.x,y:r.y,right:r.right,bottom:r.bottom}}))
 if(width>760){expect(panels[0].y).toBe(panels[1].y);expect(panels[0].right).toBeLessThan(panels[1].x);expect(panels[1].bottom).toBeLessThanOrEqual(height)}
 else expect(panels[1].y).toBeGreaterThan(panels[0].bottom)
 await expect(tmdb.getByRole('button',{name:'保存配置',exact:true})).toBeVisible();await expect(ai.getByRole('button',{name:'保存配置',exact:true})).toBeVisible()
 if(width>760&&height<600){
  for(const panel of [tmdb,ai]){
   for(const input of await panel.locator('input').all()){
    await input.scrollIntoViewIfNeeded()
    await expect.poll(()=>input.evaluate(el=>{const r=el.getBoundingClientRect(),body=el.closest('.plugin-settings').getBoundingClientRect();return r.top>=body.top&&r.bottom<=body.bottom})).toBe(true)
   }
   await panel.locator('.plugin-settings').evaluate(el=>el.scrollTop=0)
  }
 }
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`rules-assist-${width}-${theme}.png`)})
 await navigate(page,'辅助工具');await expect(page.locator('.plugin-card')).toHaveCount(11)
 for(const name of ['整理规则','识别规则','媒体分类','洗版策略','AI 辅助识别','TMDB 配置'])await expect(page.locator('.plugin-card').filter({hasText:name})).toHaveCount(0)
 await navigate(page,'以太识律');await expect(page).toHaveURL(/\/rules\/assist$/);await expect(tabs.getByRole('link',{name:'辅助配置',exact:true})).toHaveAttribute('aria-current','page')
 await page.reload();await expect(page.locator('.plugin-settings-panel')).toHaveCount(2)
 expect(f.writes).toEqual([]);expect(f.unexpected).toEqual([])
})

test('migrated TMDB and AI preserve save test and enable behavior',async({page})=>{
 const f=await fixture(page);await page.goto('/rules/assist')
 const tmdb=page.getByRole('region',{name:'TMDB 配置',exact:true}),ai=page.getByRole('region',{name:'AI 辅助识别',exact:true})
 await tmdb.getByLabel('API 域名',{exact:true}).fill('https://new.tmdb.test')
 await page.getByRole('button',{name:'停用TMDB 配置',exact:true}).click();await expect.poll(()=>f.configs.tmdb.enabled).toBe(false);expect(f.configs.tmdb.apiKey).toBe('********')
 expect(f.configs.tmdb.apiURL).toBe('https://api.tmdb.test');await expect(tmdb.getByLabel('API 域名',{exact:true})).toHaveValue('https://new.tmdb.test')
 await tmdb.getByRole('button',{name:'保存配置',exact:true}).click();await expect.poll(()=>f.configs.tmdb.apiURL).toBe('https://new.tmdb.test');await tmdb.getByRole('button',{name:'测试连接',exact:true}).click();await expect(page.locator('.toast-stack')).toContainText('连接成功')
 await page.getByRole('button',{name:'启用AI 辅助识别',exact:true}).click();await expect.poll(()=>f.configs.ai.enabled).toBe(true)
 await ai.getByLabel('模型名称',{exact:true}).fill('another-model');await ai.getByRole('button',{name:'保存配置',exact:true}).click();await expect.poll(()=>f.configs.ai.model).toBe('another-model')
 expect(f.unexpected).toEqual([])
})

test('failed rule configuration read and toggle do not claim saved enabled state',async({page})=>{
 const f=await fixture(page);f.failRead('tmdb');await page.goto('/rules/assist');await expect(page.getByRole('alert')).toHaveText('读取配置失败');await expect(page.getByRole('alert')).toHaveCSS('color','rgb(173, 63, 74)');await expect(page.getByRole('button',{name:'启用TMDB 配置',exact:true})).toBeDisabled()
 f.failRead('');await page.reload();f.failWrite(true);await page.getByRole('button',{name:'停用TMDB 配置',exact:true}).click();await expect(page.locator('.toast-stack')).toContainText('配置保存失败，请重试');await expect(page.getByRole('button',{name:'停用TMDB 配置',exact:true})).toHaveAttribute('aria-pressed','true');expect(f.configs.tmdb.enabled).toBe(true);expect(f.unexpected).toEqual([])
})
