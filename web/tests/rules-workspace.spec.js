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
  else{unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  await route.fulfill({json})
 })
 return{configs,writes,unexpected,failRead(value){failRead=value},failWrite(value){failWrite=value}}
}
async function navigate(page,name){const menu=page.getByRole('button',{name:'打开导航',exact:true});if(await menu.isVisible())await menu.click();await page.getByRole('navigation',{name:'主导航',exact:true}).getByRole('link',{name,exact:true}).click()}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`rules workshop tabs and migrated configuration ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/rules');await page.evaluate(value=>document.documentElement.dataset.theme=value,theme)
 await expect(page).toHaveURL(/\/rules\/naming$/)
 const group=page.getByRole('group',{name:'以太工坊',exact:true,includeHidden:true});await expect(group.getByRole('link',{includeHidden:true}).first()).toHaveText('以太识律')
 const tabs=page.getByRole('navigation',{name:'以太识律栏目',exact:true});await expect(tabs.getByRole('link')).toHaveText(['命名规则','二级分类','洗版策略','辅助配置'])
 await expect(page.locator('.rule-placeholder')).toHaveCount(2);await expect(page.locator('.rule-placeholder').filter({hasText:'识别规则'})).toContainText('待实装')
 await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('命名规则')
 await tabs.getByRole('link',{name:'二级分类',exact:true}).click();await expect(page.locator('.rule-placeholder')).toHaveCount(1);await expect(page.locator('.rule-placeholder')).toContainText('二级分类')
 await tabs.getByRole('link',{name:'洗版策略',exact:true}).click();await expect(page.locator('.rule-placeholder')).toContainText('待实装')
 await tabs.getByRole('link',{name:'辅助配置',exact:true}).click();await expect(page.locator('.rule-config-card')).toHaveCount(2)
 await expect(page.getByRole('button',{name:'停用TMDB 配置',exact:true})).toBeEnabled()
 await page.getByRole('button',{name:'TMDB 配置',exact:true}).click();await expect(page.getByRole('dialog',{name:'TMDB 配置',exact:true})).toBeVisible();await expect(page.getByLabel('API 域名',{exact:true})).toHaveValue('https://api.tmdb.test');await page.keyboard.press('Escape')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`rules-assist-${width}-${theme}.png`)})
 await navigate(page,'辅助工具');await expect(page.locator('.plugin-card')).toHaveCount(11)
 for(const name of ['整理规则','识别规则','二级分类','洗版策略','AI 辅助识别','TMDB 配置'])await expect(page.locator('.plugin-card').filter({hasText:name})).toHaveCount(0)
 await navigate(page,'以太识律');await expect(page).toHaveURL(/\/rules\/assist$/);await expect(tabs.getByRole('link',{name:'辅助配置',exact:true})).toHaveAttribute('aria-current','page')
 await page.reload();await expect(page.locator('.rule-config-card')).toHaveCount(2)
 expect(f.writes).toEqual([]);expect(f.unexpected).toEqual([])
})

test('migrated TMDB and AI preserve save test and enable behavior',async({page})=>{
 const f=await fixture(page);await page.goto('/rules/assist')
 await page.getByRole('button',{name:'停用TMDB 配置',exact:true}).click();await expect.poll(()=>f.configs.tmdb.enabled).toBe(false);expect(f.configs.tmdb.apiKey).toBe('********')
 await page.getByRole('button',{name:'TMDB 配置',exact:true}).click();await page.getByLabel('API 域名',{exact:true}).fill('https://new.tmdb.test');await page.getByRole('button',{name:'保存配置',exact:true}).click();await expect.poll(()=>f.configs.tmdb.apiURL).toBe('https://new.tmdb.test');await page.getByRole('button',{name:'测试连接',exact:true}).click();await expect(page.locator('.toast-stack')).toContainText('连接成功');await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'启用AI 辅助识别',exact:true}).click();await expect.poll(()=>f.configs.ai.enabled).toBe(true)
 await page.getByRole('button',{name:'AI 辅助识别',exact:true}).click();await page.getByLabel('模型名称',{exact:true}).fill('another-model');await page.getByRole('button',{name:'保存配置',exact:true}).click();await expect.poll(()=>f.configs.ai.model).toBe('another-model')
 expect(f.unexpected).toEqual([])
})

test('failed rule configuration read and toggle do not claim saved enabled state',async({page})=>{
 const f=await fixture(page);f.failRead('tmdb');await page.goto('/rules/assist');await expect(page.getByRole('alert')).toHaveText('读取配置失败');await expect(page.getByRole('alert')).toHaveCSS('color','rgb(173, 63, 74)');await expect(page.getByRole('button',{name:'启用TMDB 配置',exact:true})).toBeDisabled()
 f.failRead('');await page.reload();f.failWrite(true);await page.getByRole('button',{name:'停用TMDB 配置',exact:true}).click();await expect(page.locator('.toast-stack')).toContainText('配置保存失败，请重试');await expect(page.getByRole('button',{name:'停用TMDB 配置',exact:true})).toHaveAttribute('aria-pressed','true');expect(f.configs.tmdb.enabled).toBe(true);expect(f.unexpected).toEqual([])
})
