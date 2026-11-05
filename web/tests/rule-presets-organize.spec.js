import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
const browserName=process.env.AETHER_ORGANIZE_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})
async function fixture(page){
 const presets=['naming','categories','upgrade'].flatMap(kind=>Array.from({length:3},(_,i)=>({id:`${kind}-${i}`,kind,name:`预设 ${i+1}`,description:'自定义预设说明，具体规则字段后续补充。'})))
 const tasks=[],writes=[],unexpected=[];let reject=false,aiAvailable=false
 const storages=[{id:'l1',name:'本地一',type:'local'},{id:'l2',name:'本地二',type:'local'},{id:'q1',name:'夸克一',type:'quark'},{id:'q2',name:'夸克二',type:'quark'},{id:'m',name:'移动',type:'mobile'}].map(s=>({...s,enabled:true,config:{mode:'native'}}))
 await page.route('**/api/**',async route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname,m=r.method();let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'preset-organize',storages,tasks:[],links:[],settings:{},cache:{},traffic:{},logs:[]}
  else if(p==='/api/naming-rules/catalog')json={variables:[{id:'title',label:'媒体标题',group:'基础信息'}],defaults:{tv:'{title}',movie:'{title}'}}
  else if(p==='/api/naming-rules/preview')json={directory:'',filename:'示例.mkv'}
  else if(p==='/api/rule-presets'||p.startsWith('/api/rule-presets/')){
   const id=p.split('/')[3];if(m==='GET')json=presets.filter(x=>x.kind===u.searchParams.get('kind'))
   else {writes.push({p,m,body:m==='DELETE'?null:r.postDataJSON()});if(reject)return route.fulfill({status:409,json:{error:'保存失败，请重试'}});if(m==='DELETE'){presets.splice(presets.findIndex(x=>x.id===id),1);json={ok:true}}else if(m==='POST'){json={...r.postDataJSON(),id:'new-preset'};presets.push(json)}else{json={...r.postDataJSON(),id};presets.splice(presets.findIndex(x=>x.id===id),1,json)}}
  }else if(p==='/api/organize-options')json={rules:Object.fromEntries(['naming','categories','upgrade'].map(kind=>[kind,presets.filter(x=>x.kind===kind)])),aiAvailable}
  else if(p==='/api/organize-tasks'||p.startsWith('/api/organize-tasks/')){
   const id=p.split('/')[3];if(m==='GET')json=tasks
   else {writes.push({p,m,body:m==='DELETE'?null:r.postDataJSON()});if(reject)return route.fulfill({status:409,json:{error:'保存失败，请重试'}});if(m==='DELETE'){tasks.splice(tasks.findIndex(x=>x.id===id),1);json={ok:true}}else if(m==='POST'){json={...r.postDataJSON(),id:'organize-1'};tasks.push(json)}else{json={...r.postDataJSON(),id};tasks.splice(tasks.findIndex(x=>x.id===id),1,json)}}
  }else if(p==='/api/files'){
   const local=u.searchParams.get('storage')?.startsWith('l');json=u.searchParams.get('path')==='/'?[{id:local?'/Source':'source',name:'Source',isDir:true},{id:local?'/Archive':'archive',name:'Archive',isDir:true}]:[]
  }else if(p==='/api/traffic')json={}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else{unexpected.push(m+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  await route.fulfill({json})
 })
 return{presets,tasks,writes,unexpected,reject(value){reject=value},setAI(value){aiAvailable=value}}
}
for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`full width preset rows and organize form layout ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height})
 for(const [kind,name] of [['naming','命名规则'],['categories','媒体分类'],['upgrade','洗版策略']]){
  await page.goto(`/rules/${kind}`);await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const region=page.getByRole('region',{name,exact:true});await expect(region.locator('.rule-preset-card')).toHaveCount(3)
  const rects=await region.evaluate(el=>{const list=el.getBoundingClientRect();return {width:list.width,rows:[...el.querySelectorAll('.rule-preset-card')].map(row=>{const r=row.getBoundingClientRect();return {x:r.x,width:r.width,height:r.height,bottom:r.bottom}})}})
  for(const r of rects.rows){expect(Math.abs(r.width-rects.width)).toBeLessThan(1);expect(r.height).toBeGreaterThanOrEqual(180);expect(r.x).toBe(rects.rows[0].x)}
  if(height>=800)expect(rects.rows[2].bottom).toBeLessThanOrEqual(height)
  await captureScreenshot(page,{path:info.outputPath(`presets-${kind}-${width}-${theme}.png`)})
 }
 await page.goto('/tasks/organize');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.getByRole('navigation',{name:'任务栏目'}).getByRole('link',{name:'整理归档',exact:true})).toBeVisible();await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('整理归档')
 await page.getByRole('button',{name:'添加整理归档任务',exact:true}).click();const dialog=page.getByRole('dialog',{name:'添加整理归档',exact:true})
 const fields=await dialog.locator('.organize-task-form>label,.organize-task-form>.field').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y}}))
 expect(fields[0].y).toBe(fields[1].y);expect(fields[2].y).toBe(fields[3].y);expect(fields[2].y).toBeGreaterThan(fields[0].y);expect(fields[0].x).toBe(fields[2].x)
 expect(fields).toHaveLength(8);expect(fields[4].y).toBe(fields[5].y);expect(fields[4].y).toBeGreaterThan(fields[2].y);expect(fields[4].x).toBe(fields[0].x)
 await expect(dialog.getByRole('textbox',{name:'冗余归类',exact:true})).toHaveValue('');await expect(dialog.getByRole('button',{name:'选择冗余目录',exact:true})).toBeDisabled()
 const ruleRects=await dialog.locator('.organize-rules-row>.field').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width}}))
 expect(ruleRects).toHaveLength(4);expect(ruleRects[0].y).toBe(ruleRects[1].y);expect(ruleRects[2].y).toBe(ruleRects[3].y);expect(ruleRects[2].y).toBeGreaterThan(ruleRects[0].y);expect(ruleRects[0].y).toBeGreaterThan(fields[2].y);expect(ruleRects[3].y).toBeLessThan(fields[4].y)
 const ruleBoxes=await dialog.locator('.organize-rules-row').evaluate(el=>{const r=el.getBoundingClientRect();return {left:r.left,right:r.right,boxes:[...el.querySelectorAll('.rounded-select')].map(c=>{const r=c.getBoundingClientRect();return{left:r.left,right:r.right}})}})
 for(let i=0;i<ruleBoxes.boxes.length;i++){const b=ruleBoxes.boxes[i];expect(b.left).toBeGreaterThanOrEqual(ruleBoxes.left-1);expect(b.right).toBeLessThanOrEqual(ruleBoxes.right+1);if(i===1)expect(b.left).toBeGreaterThan(ruleBoxes.boxes[i-1].right)}
 for(const label of ['命名规则','媒体分类'])await expect(dialog.getByRole('button',{name:label,exact:true})).toHaveText('预设 1')
 await expect(dialog.getByRole('button',{name:'洗版策略',exact:true})).toHaveText('不洗版')
 await expect(dialog.getByRole('textbox',{name:'元数据归档',exact:true})).toHaveValue('srt;ass;ssa')
 const toggleRects=await dialog.getByRole('switch').evaluateAll(els=>els.map(el=>el.getBoundingClientRect().y));expect(toggleRects).toHaveLength(4);expect(new Set(toggleRects).size).toBe(1)
 await expect(dialog.getByRole('switch',{name:'AI增强识别',exact:true})).toBeDisabled();await expect(dialog.getByRole('switch',{name:'AI增强识别',exact:true})).not.toBeChecked();await expect(dialog.getByRole('switch',{name:'TMDB刮削',exact:true})).toBeChecked();await expect(dialog.getByRole('switch',{name:'FFprobe解析',exact:true})).toBeChecked();await expect(dialog.getByRole('switch',{name:'智能清理',exact:true})).not.toBeChecked()
 const scroll=await dialog.evaluate(el=>({outer:getComputedStyle(el).overflowY,body:getComputedStyle(el.querySelector('.organize-task-form')).scrollbarWidth}));expect(scroll).toEqual({outer:'hidden',body:'none'})
 await expect(dialog.getByRole('spinbutton',{name:'文件过滤',exact:true})).toHaveValue('0');const filterUnit=dialog.locator('.organize-filter-field .number-unit');await expect(filterUnit).toHaveText('MB');await expect(dialog.getByRole('spinbutton',{name:'处理并发',exact:true})).toHaveValue('1')
 const concurrency=dialog.getByRole('spinbutton',{name:'处理并发',exact:true});await concurrency.fill('0');await concurrency.press('Tab');await expect(concurrency).toHaveValue('1');await concurrency.fill('8');await concurrency.press('Tab');await expect(concurrency).toHaveValue('3')
 for(const control of await dialog.locator('input:not([role=switch]),.source-trigger,.rounded-select-trigger').all())await expect(control).toHaveCSS('height','40px')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`organize-form-${width}-${theme}.png`)})
 expect(f.unexpected).toEqual([])
})
test('preset create edit failed save and confirmed removal persist independently',async({page})=>{
 const f=await fixture(page);await page.goto('/rules/naming');await page.getByRole('button',{name:'添加命名规则预设',exact:true}).click()
 await page.getByRole('textbox',{name:'规则名称',exact:true}).fill('自定义电影');await expect(page.getByRole('textbox',{name:'预设说明',exact:true})).toHaveCount(0)
 f.reject(true);await page.getByRole('button',{name:'保存',exact:true}).click();await expect(page.getByRole('alert')).toContainText('保存失败');await expect(page.getByRole('dialog')).toBeVisible()
 f.reject(false);await page.getByRole('button',{name:'保存',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);await page.reload();await expect(page.getByRole('article',{name:'命名规则预设 自定义电影',exact:true})).toBeVisible()
 await page.getByRole('button',{name:'编辑预设 自定义电影',exact:true}).click();await page.getByRole('textbox',{name:'规则名称',exact:true}).fill('电影备用');await page.getByRole('button',{name:'保存',exact:true}).click()
 await page.getByRole('button',{name:'删除预设 电影备用',exact:true}).click();await page.getByRole('button',{name:'取消',exact:true}).click();expect(f.presets).toHaveLength(10)
 await page.getByRole('button',{name:'删除预设 电影备用',exact:true}).click();await page.getByRole('button',{name:'确认删除',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);expect(f.presets).toHaveLength(9)
 expect(f.presets.filter(x=>x.kind==='categories')).toHaveLength(3);expect(f.unexpected).toEqual([])
})
async function pick(page,side,pool,folder){
 await page.getByRole('button',{name:side==='failure'?'选择冗余目录':side==='source'?'选择待整理目录':'选择归档目录',exact:true}).click()
 const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true});await picker.locator('.source-accounts button').filter({hasText:pool}).click();await picker.getByRole('button',{name:folder,exact:true}).click();await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
}
test('organize directory selection filters both directions and preserves method choices',async({page})=>{
 const f=await fixture(page);await page.goto('/tasks/organize');await page.getByRole('button',{name:'添加整理归档任务',exact:true}).click();await page.getByRole('textbox',{name:'任务名称',exact:true}).fill('电影归档')
 await pick(page,'target','夸克二','Archive');await page.getByRole('button',{name:'选择待整理目录',exact:true}).click();await expect(page.locator('.source-accounts button strong')).toHaveText(['夸克一','夸克二']);await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'整理方式',exact:true}).click();await expect(page.getByRole('option')).toHaveText(['移动','复制']);await page.keyboard.press('Escape');await page.getByRole('button',{name:'清除归档目录',exact:true}).click()
 await pick(page,'source','本地一','Source');await page.getByRole('button',{name:'整理方式',exact:true}).click();await expect(page.getByRole('option')).toHaveText(['移动','复制','硬链接','软链接']);await page.getByRole('option',{name:'硬链接',exact:true}).click()
 await page.getByRole('button',{name:'选择归档目录',exact:true}).click();await expect(page.locator('.source-accounts button strong')).toHaveText(['本地一','本地二']);await page.keyboard.press('Escape');await pick(page,'target','本地二','Archive')
 f.reject(true);await page.getByRole('button',{name:'保存',exact:true}).click();await expect(page.getByRole('alert')).toContainText('保存失败');f.reject(false);await page.getByRole('button',{name:'保存',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0)
 expect(f.tasks[0]).toMatchObject({name:'电影归档',storageId:'l1',source:'/Source',targetStorageId:'l2',target:'/Archive',method:'hardlink'})
 await page.reload();await page.getByRole('button',{name:'编辑整理归档 电影归档',exact:true}).click();await expect(page.getByRole('button',{name:'整理方式',exact:true})).toContainText('硬链接');await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'删除整理归档 电影归档',exact:true}).click();await page.getByRole('button',{name:'确认删除',exact:true}).click();await expect(page.locator('.organize-task-card')).toHaveCount(0);expect(f.unexpected).toEqual([])
})

test('organize redundancy directory stays in source pool and integer filter survives reopen',async({page})=>{
 const f=await fixture(page);await page.goto('/tasks/organize');await page.getByRole('button',{name:'添加整理归档任务',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加整理归档',exact:true}),filter=dialog.getByRole('spinbutton',{name:'文件过滤',exact:true}),save=dialog.getByRole('button',{name:'保存',exact:true})
 await dialog.getByRole('textbox',{name:'任务名称',exact:true}).fill('失败目录选项');await pick(page,'source','本地一','Source');await pick(page,'target','本地二','Archive')
 await dialog.getByRole('button',{name:'选择冗余目录',exact:true}).click();await expect(page.locator('.source-accounts button strong')).toHaveText(['本地一'])
 const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true});await picker.getByRole('button',{name:'Archive',exact:true}).click();await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
 await expect(dialog.getByRole('textbox',{name:'冗余归类',exact:true})).toHaveValue('/Archive')
 for(const value of ['-1','1.5']){await filter.fill(value);await expect(save).toBeDisabled();await expect(filter).toHaveAttribute('aria-invalid','true');await expect(dialog.locator('#organize-filter-help')).toHaveText('仅可输入非负整数')}
 await filter.fill('128');await save.click();await expect(dialog).toHaveCount(0)
 expect(f.tasks[0]).toMatchObject({storageId:'l1',failureDirectory:'/Archive',failurePath:'/Archive',fileFilterMB:128})
 await page.reload();await page.getByRole('button',{name:'编辑整理归档 失败目录选项',exact:true}).click()
 const edit=page.getByRole('dialog',{name:'编辑整理归档',exact:true});await expect(edit.getByRole('spinbutton',{name:'文件过滤',exact:true})).toHaveValue('128');await expect(edit.getByRole('textbox',{name:'冗余归类',exact:true})).toHaveValue('/Archive')
 await edit.getByRole('button',{name:'清除待整理目录',exact:true}).click();await expect(edit.getByRole('textbox',{name:'冗余归类',exact:true})).toHaveValue('');await expect(edit.getByRole('button',{name:'选择冗余目录',exact:true})).toBeDisabled()
 await pick(page,'source','本地二','Source');await edit.getByRole('spinbutton',{name:'文件过滤',exact:true}).fill('0');await edit.getByRole('button',{name:'保存',exact:true}).click()
 expect(f.tasks[0]).toMatchObject({storageId:'l2',failureDirectory:'',failurePath:'',fileFilterMB:0});expect(f.unexpected).toEqual([])
})

test('organize rules use bounded keyboard menus and saved switches preserve explicit values',async({page},info)=>{
 const f=await fixture(page);f.setAI(true)
 for(let i=0;i<32;i++)f.presets.push({id:`long-${i}`,kind:'naming',name:`长名称电影命名预设 ${i}`,description:''})
 await page.goto('/tasks/organize');await page.getByRole('button',{name:'添加整理归档任务',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加整理归档',exact:true})
 await dialog.getByRole('textbox',{name:'任务名称',exact:true}).fill('自定义规则')
 await dialog.getByRole('button',{name:'命名规则',exact:true}).click()
 const menu=page.getByRole('listbox',{name:'命名规则选项',exact:true});await expect(menu).toHaveCSS('position','fixed');await expect(menu).toHaveCSS('height','160px');await expect(menu).toHaveCSS('scrollbar-width','none')
 expect(await menu.evaluate(el=>el.scrollHeight>el.clientHeight)).toBe(true)
 await menu.press('Tab');await expect(dialog.getByRole('button',{name:'媒体分类',exact:true})).toBeFocused();await dialog.getByRole('button',{name:'命名规则',exact:true}).click()
 await menu.press('End');await expect.poll(()=>menu.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
 await captureScreenshot(page,{path:info.outputPath('rule-menu.png')})
 await menu.press('Enter');await expect(menu).toHaveCount(0);await expect(dialog.getByRole('button',{name:'命名规则',exact:true})).toContainText('长名称电影命名预设 31')
 await expect(dialog.getByRole('switch',{name:'AI增强识别',exact:true})).toBeEnabled()
 await dialog.getByRole('switch',{name:'AI增强识别',exact:true}).check();await dialog.getByRole('switch',{name:'TMDB刮削',exact:true}).uncheck();await dialog.getByRole('switch',{name:'FFprobe解析',exact:true}).uncheck();await dialog.getByRole('switch',{name:'智能清理',exact:true}).check()
 await dialog.getByRole('textbox',{name:'元数据归档',exact:true}).fill('')
 const excludes=dialog.getByRole('textbox',{name:'排除名单',exact:true});await expect(excludes).toHaveAttribute('placeholder','预告;.strm;/花絮');await expect(dialog).toContainText('文件关键词；.扩展名；/目录关键词');await excludes.fill('预告;.cas.strm;/花絮')
 await pick(page,'source','本地一','Source');await pick(page,'target','本地二','Archive');await dialog.getByRole('button',{name:'保存',exact:true}).click()
 expect(f.tasks[0]).toMatchObject({optionsVersion:1,namingRuleId:'long-31',mediaCategoryRuleId:'categories-0',upgradeRuleId:'',metadataExtensions:'',excludeList:'预告;.cas.strm;/花絮',aiEnhance:true,tmdbScrape:false,ffprobe:false,smartCleanup:true})
 await page.reload();await page.getByRole('button',{name:'编辑整理归档 自定义规则',exact:true}).click()
 const edit=page.getByRole('dialog',{name:'编辑整理归档',exact:true});await expect(edit.getByRole('textbox',{name:'排除名单',exact:true})).toHaveValue('预告;.cas.strm;/花絮');await expect(edit.getByRole('textbox',{name:'元数据归档',exact:true})).toHaveValue('');await expect(edit.getByRole('button',{name:'命名规则',exact:true})).toContainText('31');await expect(edit.getByRole('switch',{name:'AI增强识别',exact:true})).toBeChecked();await expect(edit.getByRole('switch',{name:'TMDB刮削',exact:true})).not.toBeChecked()
 await expect(edit.getByRole('button',{name:'洗版策略',exact:true})).toHaveText('不洗版')
 await edit.getByRole('button',{name:'洗版策略',exact:true}).click();await page.getByRole('option',{name:'预设 1',exact:true}).click();await edit.getByRole('button',{name:'保存',exact:true}).click()
 await page.getByRole('button',{name:'编辑整理归档 自定义规则',exact:true}).click();await expect(edit.getByRole('button',{name:'洗版策略',exact:true})).toHaveText('预设 1');expect(f.tasks[0].upgradeRuleId).toBe('upgrade-0')
 await edit.getByRole('button',{name:'洗版策略',exact:true}).click();await page.getByRole('option',{name:'不洗版',exact:true}).click();await edit.getByRole('button',{name:'保存',exact:true}).click();await page.reload()
 await page.getByRole('button',{name:'编辑整理归档 自定义规则',exact:true}).click();await expect(edit.getByRole('button',{name:'洗版策略',exact:true})).toHaveText('不洗版');expect(f.tasks[0].upgradeRuleId).toBe('')
 expect(f.unexpected).toEqual([])
})

test('organize missing rule lists stay empty and removed selected rules require a new choice',async({page})=>{
 const f=await fixture(page);f.presets.splice(0)
 await page.goto('/tasks/organize');await page.getByRole('button',{name:'添加整理归档任务',exact:true}).click()
 for(const label of ['命名规则','媒体分类']){await expect(page.getByRole('button',{name:label,exact:true})).toBeDisabled();await expect(page.getByRole('button',{name:label,exact:true})).toContainText('尚未添加规则')}
 await expect(page.getByRole('button',{name:'洗版策略',exact:true})).toBeEnabled();await expect(page.getByRole('button',{name:'洗版策略',exact:true})).toHaveText('不洗版')
 await page.keyboard.press('Escape')
 f.presets.push({id:'replacement',kind:'naming',name:'可用命名',description:''});f.tasks.push({id:'stale',name:'失效引用',storageId:'l1',source:'/Source',targetStorageId:'l2',target:'/Archive',optionsVersion:1,namingRuleId:'removed',metadataExtensions:'srt',method:'move'})
 await page.reload();await page.getByRole('button',{name:'编辑整理归档 失效引用',exact:true}).click()
 const edit=page.getByRole('dialog',{name:'编辑整理归档',exact:true});await expect(edit.getByRole('button',{name:'保存',exact:true})).toBeDisabled();await expect(edit).toContainText('所选规则已移除，请重新选择')
 await edit.getByRole('button',{name:'命名规则',exact:true}).click();await page.getByRole('option',{name:'可用命名',exact:true}).click();await expect(edit.getByRole('button',{name:'保存',exact:true})).toBeEnabled()
})
