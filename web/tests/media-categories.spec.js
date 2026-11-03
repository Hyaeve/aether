import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
const browserName=process.env.AETHER_MEDIA_CATEGORY_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})
const defaults=()=>({directories:[{id:'tv-root',mediaType:'tv',name:'剧集',path:[],enabled:true},{id:'movie-root',mediaType:'movie',name:'电影',path:[],enabled:true}],rules:[],fallbacks:{tv:'tv-root',movie:'movie-root'}})
const textOps=['equals','not_equals','contains','starts_with','ends_with','in','not_in','exists','not_exists'],listOps=['contains_any','contains_all','contains_none','exists','not_exists']
const fields=[{id:'title',label:'名称',type:'string',operators:textOps,example:'蜘蛛侠'},{id:'year',label:'年份',type:'integer',operators:['equals','not_equals','gt','gte','lt','lte','between','in','not_in','exists','not_exists'],example:'2021'},{id:'genreIds',label:'类型ID',type:'integer_list',operators:listOps,example:'16;99'},{id:'genres',label:'类型／题材',type:'list',operators:listOps,example:'动画;纪录片'},{id:'countries',label:'国家／地区',type:'list',operators:listOps,example:'CN;JP'}]
async function fixture(page){
 const presets=[{id:'name-1',kind:'naming',name:'默认命名',description:''},{id:'cat-1',kind:'categories',name:'分类方案 1',description:'我的媒体分类配置'},{id:'cat-2',kind:'categories',name:'备用分类',description:''},{id:'cat-3',kind:'categories',name:'第三套分类',description:''},{id:'upgrade-1',kind:'upgrade',name:'默认洗版',description:''}]
 const tasks=[{id:'task-1',name:'待整理',storageId:'local',source:'/in',sourcePath:'/in',targetStorageId:'local',target:'/out',targetPath:'/out',method:'move',categoryRuleId:'cat-2'}]
 const writes=[],unexpected=[];let reject=false
 await page.route('**/api/**',async route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname;let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'media-category',storages:[{id:'local',name:'本地',type:'local',enabled:true,config:{}}],tasks:[],links:[],settings:{},traffic:{},cache:{},logs:[]}
  else if(p==='/api/media-categories/catalog')json={fields,defaults:defaults()}
  else if(p==='/api/rule-presets'||p.startsWith('/api/rule-presets/')){
   const id=p.split('/')[3]
   if(r.method()==='GET')json=presets.filter(v=>!u.searchParams.get('kind')||v.kind===u.searchParams.get('kind'))
   else{const value=r.method()==='DELETE'?null:r.postDataJSON();writes.push({p,method:r.method(),value});if(reject)return route.fulfill({status:409,json:{error:'媒体分类保存失败'}});if(r.method()==='DELETE'){presets.splice(presets.findIndex(x=>x.id===id),1);json={ok:true}}else if(r.method()==='POST'){json={...value,id:'cat-new'};presets.push(json)}else{json={...value,id};presets.splice(presets.findIndex(x=>x.id===id),1,json)}}
  }else if(p==='/api/organize-options')json={rules:Object.fromEntries(['naming','categories','upgrade'].map(k=>[k,presets.filter(p=>p.kind===k)])),aiAvailable:false}
  else if(p==='/api/organize-tasks')json=tasks
  else if(p==='/api/organize-tasks/task-1'){const value=r.postDataJSON();writes.push({p,value});tasks[0]=value;json=value}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(p==='/api/traffic')json={}
  else {unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  await route.fulfill({json})
 })
 return{presets,tasks,writes,unexpected,reject(value){reject=value}}
}
async function choose(page,label,option){await page.getByRole('button',{name:label,exact:true}).click();await page.getByRole('option',{name:option,exact:true}).click()}
async function directory(page,name,path){await page.getByRole('button',{name:'添加剧集分类目录',exact:true}).click();await page.getByRole('textbox',{name:'分类目录名称 1',exact:true}).fill(name);await page.getByRole('textbox',{name:'分类目录路径 1',exact:true}).fill(path)}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`media category editor tabs and two media roots ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/rules/categories');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('媒体分类');await expect(page.getByRole('region',{name:'媒体分类',exact:true}).locator('.rule-preset-card')).toHaveCount(3)
 await page.getByRole('button',{name:'编辑预设 分类方案 1',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'编辑媒体分类预设',exact:true}),tabs=dialog.getByRole('tablist',{name:'媒体分类编辑栏目',exact:true})
 await expect(tabs.getByRole('tab')).toHaveText(['分类目录','分类规则']);await expect(dialog.getByRole('group',{name:'媒体大类',exact:true}).getByRole('button')).toHaveText(['剧集','电影']);await expect(dialog.getByText('音乐',{exact:true})).toHaveCount(0)
 await directory(page,'国漫','动画/国漫')
 for(const box of await dialog.getByRole('textbox').all())await expect(box).toHaveCSS('height',await box.getAttribute('aria-label')==='预设说明'?'66px':'40px')
 await expect(dialog.locator('.modal-body')).toHaveCSS('scrollbar-width','none');expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`directories-${width}-${theme}.png`)})
 await tabs.getByRole('tab',{name:'分类目录',exact:true}).focus();await page.keyboard.press('ArrowRight');await expect(tabs.getByRole('tab',{name:'分类规则',exact:true})).toBeFocused();await page.keyboard.press('ArrowLeft');await expect(tabs.getByRole('tab',{name:'分类目录',exact:true})).toBeFocused();await page.keyboard.press('ArrowRight')
 await page.getByRole('button',{name:'添加媒体分类规则',exact:true}).click();await choose(page,'分类规则归入目录 1','国漫 · 动画/国漫')
 await choose(page,'规则1条件字段','类型ID');await page.getByRole('textbox',{name:'规则1条件匹配值',exact:true}).fill('16')
 const inputs=await dialog.locator('.media-category-rule input:not(.switch)').evaluateAll(nodes=>nodes.map(n=>{const r=n.getBoundingClientRect(),p=n.closest('.media-category-rule').getBoundingClientRect();return{left:r.left,right:r.right,pLeft:p.left,pRight:p.right,height:r.height}}))
 for(const input of inputs){expect(input.left).toBeGreaterThanOrEqual(input.pLeft);expect(input.right).toBeLessThanOrEqual(input.pRight);expect(input.height).toBe(40)}
 await captureScreenshot(page,{path:info.outputPath(`rules-${width}-${theme}.png`)})
 await dialog.getByRole('button',{name:'取消',exact:true}).click();expect(f.presets[1].mediaCategory).toBeUndefined();expect(f.writes).toEqual([]);expect(f.unexpected).toEqual([])
})

test('media category presets persist nested directories and ordered condition groups',async({page})=>{
 const f=await fixture(page);await page.goto('/rules/categories');await page.getByRole('button',{name:'添加媒体分类预设',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加媒体分类预设',exact:true});await dialog.getByRole('textbox',{name:'预设名称',exact:true}).fill('动漫专用')
 await directory(page,'国漫','动画/国漫');await dialog.getByRole('group',{name:'媒体大类',exact:true}).getByRole('button',{name:'电影',exact:true}).click();await page.getByRole('button',{name:'添加电影分类目录',exact:true}).click();await page.getByRole('textbox',{name:'分类目录名称 1',exact:true}).fill('动画电影');await page.getByRole('textbox',{name:'分类目录路径 1',exact:true}).fill('动画')
 await dialog.getByRole('tab',{name:'分类规则',exact:true}).click();await page.getByRole('button',{name:'添加媒体分类规则',exact:true}).click();await page.getByRole('textbox',{name:'分类规则名称 1',exact:true}).fill('国漫优先');await choose(page,'分类规则归入目录 1','国漫 · 动画/国漫')
 await choose(page,'规则1条件组合','全部满足');await choose(page,'规则1条件.1字段','类型ID');await page.getByRole('textbox',{name:'规则1条件.1匹配值',exact:true}).fill('16')
 await page.getByRole('button',{name:'添加规则1条件子条件',exact:true}).click();await choose(page,'规则1条件.2字段','国家／地区');await page.getByRole('textbox',{name:'规则1条件.2匹配值',exact:true}).fill('CN')
 await page.getByRole('button',{name:'复制分类规则 1',exact:true}).click();await page.getByRole('textbox',{name:'分类规则名称 2',exact:true}).fill('备用动画');await page.getByRole('button',{name:'上移分类规则 2',exact:true}).click();await expect(page.getByRole('textbox',{name:'分类规则名称 1',exact:true})).toHaveValue('备用动画');await page.getByRole('button',{name:'下移分类规则 1',exact:true}).click();await page.getByRole('switch',{name:'启用分类规则 2',exact:true}).uncheck()
 f.reject(true);await dialog.getByRole('button',{name:'保存',exact:true}).click();await expect(dialog.getByRole('alert')).toHaveText('媒体分类保存失败');await expect(page.getByRole('textbox',{name:'分类规则名称 1',exact:true})).toHaveValue('国漫优先')
 f.reject(false);await dialog.getByRole('button',{name:'保存',exact:true}).click();await expect(dialog).toHaveCount(0)
 const saved=f.presets.find(p=>p.id==='cat-new');expect(saved.mediaCategory.rules).toHaveLength(2);expect(saved.mediaCategory.rules[0].when).toEqual({kind:'all',children:[{kind:'condition',field:'genreIds',operator:'contains_any',value:[16]},{kind:'condition',field:'countries',operator:'contains_any',value:['CN']}]});expect(saved.mediaCategory.rules[1].enabled).toBe(false)
 await page.reload();await page.getByRole('button',{name:'编辑预设 动漫专用',exact:true}).click();await page.getByRole('tab',{name:'分类规则',exact:true}).click();await expect(page.getByRole('textbox',{name:'规则1条件.1匹配值',exact:true})).toHaveValue('16');await page.getByRole('button',{name:'取消',exact:true}).click()
 await page.goto('/tasks/organize');await page.getByRole('button',{name:'编辑整理归档 待整理',exact:true}).click();await expect(page.getByRole('button',{name:'媒体分类',exact:true})).toContainText('备用分类');await choose(page,'媒体分类','动漫专用');await page.getByRole('button',{name:'保存',exact:true}).click();expect(f.tasks[0].mediaCategoryRuleId).toBe('cat-new');expect(f.tasks[0]).not.toHaveProperty('categoryRuleId');expect(f.unexpected).toEqual([])
})

test('media category reference guards and numeric condition controls stay editable',async({page})=>{
 const f=await fixture(page);await page.goto('/rules/categories');await page.getByRole('button',{name:'编辑预设 分类方案 1',exact:true}).click();const dialog=page.getByRole('dialog',{name:'编辑媒体分类预设',exact:true})
 await directory(page,'年份分类','按年');await choose(page,'剧集兜底目录','年份分类 · 按年');await expect(page.getByRole('button',{name:'删除分类目录 1',exact:true})).toBeDisabled();await expect(page.getByRole('switch',{name:'启用分类目录 1',exact:true})).toBeDisabled()
 await choose(page,'剧集兜底目录','剧集根目录');await expect(page.getByRole('button',{name:'删除分类目录 1',exact:true})).toBeEnabled()
 await dialog.getByRole('tab',{name:'分类规则',exact:true}).click();await page.getByRole('button',{name:'添加媒体分类规则',exact:true}).click();await choose(page,'规则1条件字段','年份');await page.getByRole('spinbutton',{name:'规则1条件匹配值',exact:true}).fill('2021');await dialog.locator('.media-condition-value .number-accessory').hover();await expect(dialog.locator('.media-condition-value .number-steppers')).toHaveCSS('pointer-events','auto');await page.getByRole('button',{name:'增加规则1条件匹配值',exact:true}).click();await expect(page.getByRole('spinbutton',{name:'规则1条件匹配值',exact:true})).toHaveValue('2022')
 await choose(page,'规则1条件组合','取反');await page.getByRole('button',{name:'删除规则1条件.1',exact:true}).click();await page.getByRole('button',{name:'添加规则1条件子条件',exact:true}).click();await expect(page.getByRole('button',{name:'规则1条件.1字段',exact:true})).toContainText('名称')
 await page.getByRole('button',{name:'删除分类规则 1',exact:true}).click();await expect(page.getByRole('article',{name:'媒体分类规则 1',exact:true})).toHaveCount(0);await dialog.getByRole('button',{name:'取消',exact:true}).click();expect(f.writes).toEqual([]);expect(f.unexpected).toEqual([])
})
