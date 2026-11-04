import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
const browserName=process.env.AETHER_RECOGNITION_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})
const defaults=()=>({matchRules:[],namingRules:[],blacklist:[],releaseGroups:[],minSizeMB:0})
async function fixture(page){
 let config=defaults(),failRead=false,failSave=false,releaseLate
 const writes=[],previews=[],unexpected=[]
 await page.route('**/api/**',async route=>{
  const req=route.request(),p=new URL(req.url()).pathname;let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'recognition-rules',storages:[],tasks:[],links:[],settings:{},traffic:{},cache:{},logs:[]}
  else if(p==='/api/traffic')json={}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(p==='/api/recognition-rules'){
   if(req.method()==='GET'){if(failRead)return route.fulfill({status:503,json:{error:'读取识别规则失败'}});json=config}
   else{
    const value=req.postDataJSON();writes.push(value)
    if(failSave)return route.fulfill({status:500,json:{error:'写入识别规则失败'}})
    const index=value.namingRules.findIndex(r=>r.regex&&r.from==='[')
    if(index>=0)return route.fulfill({status:400,json:{error:`命名替换第${index+1}条：正则表达式无效`,field:`namingRules.${index}.from`}})
    config=structuredClone(value);json=config
   }
  }else if(p==='/api/recognition-rules/preview'){
   const value=req.postDataJSON();previews.push(value)
   if(value.name==='slow.mkv')await new Promise(resolve=>releaseLate=resolve)
   let result=value.name
   for(let i=0;i<value.rules.length;i++){
    const r=value.rules[i]
    if(r.regex&&r.from==='[')return route.fulfill({status:400,json:{error:`匹配替换第${i+1}条：正则表达式无效`,field:`matchRules.${i}.from`}})
    result=r.regex?result.replace(new RegExp(r.from,'g'),r.to):result.split(r.from).join(r.to)
   }
   json={result}
  }else {unexpected.push(req.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  await route.fulfill({json})
 })
 return{writes,previews,unexpected,get config(){return config},setConfig(value){config=structuredClone(value)},failRead(v){failRead=v},failSave(v){failSave=v},releaseLate(){releaseLate?.()}}
}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`recognition windows and responsive fields ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/rules/recognition');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 for(const name of ['匹配替换','归档黑名单','命名替换','发布组扩展','文件体积过滤'])await expect(page.getByRole('region',{name,exact:true})).toBeVisible()
 await expect(page.getByRole('textbox',{name:'示例名称',exact:true})).toHaveValue('【高清影视之家发布】www\\.abc.com@蜘蛛侠_2021_国语中字_1080p.WEB-DL.H265-TnT.mkv')
 await expect(page.getByRole('spinbutton',{name:'文件体积过滤',exact:true})).toHaveValue('0')
 await expect(page.locator('.recognition-size .number-unit')).toHaveText('MB');await expect(page.locator('.recognition-size .number-unit')).toHaveCSS('opacity','1')
 await expect(page.getByRole('textbox',{name:'发布组词条',exact:true})).toHaveAttribute('rows','5')
 await page.getByRole('button',{name:'添加匹配替换',exact:true}).click();await page.getByRole('button',{name:'添加命名替换',exact:true}).click()
 for(const label of await page.locator('.regex-option').all())await expect(label).toHaveCSS('flex-direction','row')
 const cols=await page.locator('.replacement-fields').evaluateAll(nodes=>nodes.map(el=>{const r=el.getBoundingClientRect();return{left:r.left,right:r.right,fields:[...el.querySelectorAll('input')].map(n=>{const r=n.getBoundingClientRect();return{x:r.x,y:r.y,right:r.right,height:r.height}})}}))
 for(const row of cols){expect(row.fields).toHaveLength(2);expect(row.fields[0].y).toBe(row.fields[1].y);expect(row.fields[0].right).toBeLessThan(row.fields[1].x);for(const field of row.fields){expect(field.x).toBeGreaterThanOrEqual(row.left);expect(field.right).toBeLessThanOrEqual(row.right+1);expect(field.height).toBe(40)}}
 const panels=await page.locator('.recognition-panel').evaluateAll(nodes=>nodes.map(n=>{const r=n.getBoundingClientRect();return{x:r.x,y:r.y,right:r.right,bottom:r.bottom}}))
 for(let i=1;i<panels.length;i++){expect(panels[i].x).toBe(panels[0].x);expect(panels[i].right).toBe(panels[0].right);expect(panels[i].y).toBeGreaterThanOrEqual(panels[i-1].bottom)}
 await expect(page.getByRole('button',{name:'保存识别规则',exact:true})).toHaveCount(0)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.getByRole('spinbutton',{name:'文件体积过滤',exact:true}).scrollIntoViewIfNeeded();await expect(page.locator('.recognition-size .number-unit')).toBeVisible()
 await captureScreenshot(page,{path:info.outputPath(`recognition-size-${width}-${theme}.png`)})
 await page.locator('.page-content').evaluate(el=>{for(let node=el;node;node=node.parentElement)node.scrollTop=0;document.scrollingElement.scrollTop=0})
 await captureScreenshot(page,{path:info.outputPath(`recognition-${width}-${theme}.png`)})
 expect(f.writes).toEqual([]);expect(f.unexpected).toEqual([])
})

test('recognition replacements preview in sequence and editable rules persist',async({page})=>{
 const f=await fixture(page);await page.goto('/rules/recognition')
 await page.getByRole('textbox',{name:'示例名称',exact:true}).fill('广告Spider_2021.mkv')
 for(let i=1;i<=3;i++)await page.getByRole('button',{name:'添加匹配替换',exact:true}).click()
 await page.getByRole('textbox',{name:'匹配替换原文本 1',exact:true}).fill('广告')
 await page.getByRole('textbox',{name:'匹配替换原文本 2',exact:true}).fill('_');await page.getByRole('textbox',{name:'匹配替换替换文本 2',exact:true}).fill('.')
 await page.getByRole('textbox',{name:'匹配替换原文本 3',exact:true}).fill('Spider');await page.getByRole('textbox',{name:'匹配替换替换文本 3',exact:true}).fill('蜘蛛侠')
 await expect(page.getByRole('status')).toHaveText('蜘蛛侠.2021.mkv')
 await page.getByRole('button',{name:'上移匹配替换 3',exact:true}).click();await expect(page.getByRole('textbox',{name:'匹配替换原文本 2',exact:true})).toHaveValue('Spider')
 await page.getByRole('button',{name:'下移匹配替换 2',exact:true}).click();await expect(page.getByRole('textbox',{name:'匹配替换原文本 2',exact:true})).toHaveValue('_')
 await page.getByRole('button',{name:'添加命名替换',exact:true}).click();await page.getByRole('textbox',{name:'命名替换原文本 1',exact:true}).fill('Spider');await page.getByRole('textbox',{name:'命名替换替换文本 1',exact:true}).fill('蜘蛛侠')
 await page.getByRole('button',{name:'归档黑名单',exact:true}).click();await page.getByRole('textbox',{name:'黑名单词条',exact:true}).fill('预告\n.MKV\n.mkv\n/花絮');await page.getByRole('textbox',{name:'黑名单词条',exact:true}).press('Escape');await expect(page.getByRole('textbox',{name:'黑名单词条',exact:true})).toHaveCount(0)
 const groups=page.getByRole('textbox',{name:'发布组词条',exact:true});const before=(await groups.boundingBox()).height;await groups.fill('TnT\nWiKi\nFRDS\nONE\nTWO\nTHREE\nFOUR\nFIVE');expect((await groups.boundingBox()).height).toBeGreaterThan(before)
 const min=page.getByRole('spinbutton',{name:'文件体积过滤',exact:true})
 for(const value of ['-1','1.5']){await min.fill(value);await expect(min).toHaveAttribute('aria-invalid','true')}
 await min.fill('10');await min.press('Tab');await expect.poll(()=>f.config.minSizeMB).toBe(10);await expect(page.locator('.recognition-save-state')).toHaveText('已自动保存')
 expect(f.writes.every(value=>Number.isSafeInteger(value.minSizeMB)&&value.minSizeMB>=0)).toBe(true)
 expect(f.config).toMatchObject({matchRules:[{from:'广告',to:'',regex:false},{from:'_',to:'.',regex:false},{from:'Spider',to:'蜘蛛侠',regex:false}],namingRules:[{from:'Spider',to:'蜘蛛侠',regex:false}],blacklist:['预告','.MKV','.mkv','/花絮'],minSizeMB:10})
 await page.reload();await expect(page.getByRole('textbox',{name:'匹配替换原文本 1',exact:true})).toHaveValue('广告');await expect(min).toHaveValue('10');await expect(groups).toHaveValue('TnT\nWiKi\nFRDS\nONE\nTWO\nTHREE\nFOUR\nFIVE')
 await page.getByRole('button',{name:'归档黑名单',exact:true}).click();await expect(page.getByRole('textbox',{name:'黑名单词条',exact:true})).toHaveValue('预告\n.MKV\n.mkv\n/花絮')
 await page.getByRole('textbox',{name:'示例名称',exact:true}).click();await expect(page.getByRole('button',{name:'归档黑名单',exact:true})).toHaveAttribute('aria-expanded','false')
 await page.getByRole('button',{name:'删除匹配替换 3',exact:true}).click();await expect(page.getByRole('textbox',{name:'匹配替换原文本 3',exact:true})).toHaveCount(0)
 expect(f.unexpected).toEqual([])
})

test('recognition regex errors and failed saves retain the editable draft',async({page})=>{
 const f=await fixture(page);await page.goto('/rules/recognition');await page.getByRole('button',{name:'添加匹配替换',exact:true}).click()
 const from=page.getByRole('textbox',{name:'匹配替换原文本 1',exact:true})
 await from.fill('[');await page.getByRole('checkbox',{name:'匹配替换正则 1',exact:true}).check();await expect(page.getByRole('status')).toContainText('匹配替换第1条');await expect(from).toHaveAttribute('aria-invalid','true')
 f.failSave(true)
 await from.fill('_');await page.getByRole('textbox',{name:'匹配替换替换文本 1',exact:true}).fill('.');await expect(page.getByRole('status')).not.toContainText('正则表达式无效')
 await expect(page.getByRole('form',{name:'识别规则',exact:true}).getByRole('alert')).toHaveText('写入识别规则失败');await expect(from).toHaveValue('_');await expect(from).toBeEnabled();expect(f.config.matchRules).toEqual([])
 f.failSave(false);await page.getByRole('button',{name:'重试',exact:true}).click();await expect.poll(()=>f.config.matchRules[0]?.from).toBe('_')
 await page.getByRole('button',{name:'添加命名替换',exact:true}).click();await page.getByRole('textbox',{name:'命名替换原文本 1',exact:true}).fill('[');await page.getByRole('checkbox',{name:'命名替换正则 1',exact:true}).check();await expect(page.getByRole('form',{name:'识别规则',exact:true}).getByRole('alert')).toContainText('命名替换第1条')
 const naming=page.getByRole('textbox',{name:'命名替换原文本 1',exact:true});await expect(naming).toHaveValue('[');await expect(naming).toBeEnabled();await expect(naming).toHaveAttribute('aria-invalid','true');await naming.fill('Spider');await expect.poll(()=>f.config.namingRules[0]?.from).toBe('Spider')
 expect(f.unexpected).toEqual([])
})

test('recognition preview ignores an older response and failed reads can retry',async({page})=>{
 const f=await fixture(page);f.failRead(true);await page.goto('/rules/recognition');await expect(page.getByRole('form',{name:'识别规则',exact:true}).getByRole('alert')).toHaveText('读取识别规则失败');await expect(page.getByRole('textbox',{name:'示例名称',exact:true})).toBeDisabled()
 f.failRead(false);await page.getByRole('button',{name:'重新读取',exact:true}).click();await expect(page.getByRole('textbox',{name:'示例名称',exact:true})).toBeEnabled()
 await page.getByRole('textbox',{name:'示例名称',exact:true}).fill('slow.mkv');await expect.poll(()=>f.previews.some(v=>v.name==='slow.mkv')).toBe(true)
 await page.getByRole('textbox',{name:'示例名称',exact:true}).fill('fresh.mkv');await expect(page.getByRole('status')).toHaveText('fresh.mkv');f.releaseLate();await expect(page.getByRole('status')).toHaveText('fresh.mkv')
 expect(f.unexpected).toEqual([])
})

test('autosave preserves edits during slow writes and flushes the latest draft before reopening',async({page})=>{
 const f=await fixture(page);let release,started=false
 await page.route('**/api/recognition-rules',async route=>{if(route.request().method()==='PUT'&&!started){started=true;await new Promise(resolve=>release=resolve)}await route.fallback()})
 await page.route('**/api/rule-presets**',route=>route.fulfill({json:[]}))
 await page.goto('/rules/recognition');const groups=page.getByRole('textbox',{name:'发布组词条',exact:true})
 await groups.fill('first');await expect.poll(()=>started).toBe(true);await expect(groups).toBeEnabled();await groups.fill('latest')
 await page.getByRole('navigation',{name:'以太识律栏目'}).getByRole('link',{name:'命名规则',exact:true}).click()
 release();await page.getByRole('navigation',{name:'以太识律栏目'}).getByRole('link',{name:'识别规则',exact:true}).click()
 await expect(groups).toHaveValue('latest');await expect.poll(()=>f.config.releaseGroups).toEqual(['latest'])
 expect(f.writes.map(v=>v.releaseGroups)).toEqual([['first'],['latest']]);expect(f.unexpected).toEqual([])
})
