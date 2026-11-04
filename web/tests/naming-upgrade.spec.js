import {test,expect} from '@playwright/test'
import {ensureWorkspaceAccount} from './helpers/workspace.js'
import {captureScreenshot} from './helpers/screenshot.js'
const browserName=process.env.AETHER_NAMING_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})
let authState
test.beforeAll(async({request})=>{await ensureWorkspaceAccount(request);expect((await request.post('/api/auth/login',{data:{username:'my-aether-owner',password:'a1'}})).ok()).toBe(true);authState=await request.storageState()})
test.beforeEach(async({context})=>{await context.addCookies(authState.cookies)})
async function create(page,kind,name,config){const field=kind==='naming'?'naming':'upgrade';const r=await page.request.post('/api/rule-presets',{data:{kind,name,description:'',...(config?{[field]:config}:{})}});expect(r.status()).toBe(201);return r.json()}
async function open(page,kind,name){await page.goto(`/rules/${kind}`);await page.getByRole('button',{name:`编辑预设 ${name}`,exact:true}).click();return page.getByRole('dialog',{name:`编辑${kind==='naming'?'命名规则':'洗版策略'}预设`,exact:true})}
async function source(page,kind,value){await page.getByRole('tab',{name:kind==='tv'?'剧集命名':'电影命名',exact:true}).click();await page.getByRole('button',{name:'源码',exact:true}).click();await page.getByRole('textbox',{name:`${kind==='tv'?'剧集':'电影'}命名源码`,exact:true}).fill(value)}

for(const [width,height,theme]of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`naming and upgrade editors fit theme and viewport ${width}`,async({page},info)=>{
 await page.setViewportSize({width,height});await create(page,'naming',`命名布局${width}`);const d=await open(page,'naming',`命名布局${width}`);await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(d.getByRole('tab')).toHaveText(['剧集命名','电影命名','多版命名']);await expect(d.locator('.naming-token.variable')).not.toHaveCount(0);await expect(d.getByRole('button',{name:'插入季集组合',exact:true})).toBeDisabled();await expect(d.getByRole('button',{name:'插入源文件名',exact:true})).toHaveCount(0)
 if(width===1440)expect((await d.boundingBox()).width).toBe(960)
 await expect(d.getByRole('textbox',{name:'规则名称',exact:true})).toHaveCSS('height','40px');await expect(d.getByRole('textbox',{name:'预设说明',exact:true})).toHaveCount(0)
 const widths=await d.getByRole('tab').evaluateAll(nodes=>nodes.map(n=>n.getBoundingClientRect().width));expect(Math.max(...widths)-Math.min(...widths)).toBeLessThan(1)
 await expect(d.getByRole('tab',{name:'剧集命名',exact:true}).locator('svg')).toHaveClass(/lucide-tv/)
 await expect(d.locator('.naming-token.variable').first()).toHaveCSS('background-color',theme==='dark'?'rgb(36, 60, 51)':'rgb(234, 244, 239)');await expect(d.locator('.naming-token.text').first()).toHaveCSS('background-color',theme==='dark'?'rgb(60, 52, 38)':'rgb(250, 243, 229)');await expect(d.locator('.naming-token.path').first()).toHaveCSS('background-color',theme==='dark'?'rgb(72, 51, 36)':'rgb(255, 240, 222)')
 await expect(d.locator('.modal-body')).toHaveCSS('scrollbar-width','none');await expect(d.getByRole('region',{name:'命名示例'})).toContainText('S01E01');await expect(d.getByRole('region',{name:'命名示例'})).toContainText('.mkv')
 await captureScreenshot(page,{path:info.outputPath(`naming-${width}-${theme}.png`)})
 const bounds=await d.locator('.naming-token').evaluateAll(nodes=>nodes.map(n=>{const r=n.getBoundingClientRect(),p=n.closest('.naming-structure').getBoundingClientRect();return{left:r.left,right:r.right,pl:p.left,pr:p.right}}));for(const b of bounds){expect(b.left).toBeGreaterThanOrEqual(b.pl);expect(b.right).toBeLessThanOrEqual(b.pr)}
 await d.getByRole('button',{name:'取消',exact:true}).click();await create(page,'upgrade',`洗版布局${width}`);const u=await open(page,'upgrade',`洗版布局${width}`);await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(u.locator('.upgrade-group')).toHaveCount(10);await expect(u.getByRole('switch')).toHaveCount(10);for(const toggle of await u.getByRole('switch').all())await expect(toggle).toBeChecked();await expect(u.locator('.upgrade-weight').first()).toHaveText('权重 10');expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`upgrade-${width}-${theme}.png`)})
 await u.getByRole('button',{name:'取消',exact:true}).click()
})

test('naming source visual editing separators and per-directory variable scope persist',async({page})=>{
 const p=await create(page,'naming','模板编辑');const d=await open(page,'naming','模板编辑')
 await source(page,'tv','{title}/{title}');await d.getByRole('button',{name:'可视化',exact:true}).click();await expect(d.getByRole('button',{name:'插入媒体标题',exact:true})).toBeDisabled();await expect(d.getByRole('button',{name:'插入发行年份',exact:true})).toBeEnabled()
 await d.getByRole('button',{name:'文本标签 .',exact:true}).click();await expect(d.locator('.naming-token.text')).toHaveText(/文本\s*\./);await d.getByRole('button',{name:'插入季集组合',exact:true}).click()
 await d.getByRole('button',{name:'选择标签 4',exact:true}).dblclick();const connector=d.getByRole('textbox',{name:'连接符文本',exact:true});await connector.fill('/');await d.getByRole('button',{name:'应用文本',exact:true}).click();await expect(d.locator('.naming-text-editor [role=alert]')).toContainText('路径符号');await connector.fill('');await d.getByRole('button',{name:'应用文本',exact:true}).click()
 await expect(d.getByRole('region',{name:'命名示例'})).toContainText('三体 S01E01.mkv');await d.getByRole('button',{name:'源码',exact:true}).click();await expect(d.getByRole('textbox',{name:'剧集命名源码',exact:true})).toHaveValue('{title}/{title} {season_episode}')
 await source(page,'movie','{title}.{year}');await d.getByRole('tab',{name:'多版命名',exact:true}).click();await expect(d.getByRole('tabpanel',{name:'多版命名',exact:true})).toContainText('暂未开放');await d.getByRole('button',{name:'保存',exact:true}).click();await expect(d).toHaveCount(0)
 const saved=await(await page.request.get('/api/rule-presets?kind=naming')).json();expect(saved.find(x=>x.id===p.id).naming).toEqual({tv:'{title}/{title} {season_episode}',movie:'{title}.{year}'})
 await open(page,'naming','模板编辑');await page.getByRole('button',{name:'源码',exact:true}).click();await expect(page.getByRole('textbox',{name:'剧集命名源码',exact:true})).toHaveValue('{title}/{title} {season_episode}')
})

test('invalid source blocks both tabs save and path helpers recover an incomplete draft',async({page})=>{
 await create(page,'naming','模板校验');const d=await open(page,'naming','模板校验');await source(page,'tv','{title}.{title}');await expect(d.getByRole('button',{name:'保存',exact:true})).toBeDisabled();await expect(d.getByRole('button',{name:'可视化',exact:true})).toBeDisabled();await d.getByRole('tab',{name:'电影命名',exact:true}).click();await expect(d.getByRole('button',{name:'保存',exact:true})).toBeDisabled();await source(page,'tv','{title}');await d.getByRole('button',{name:'可视化',exact:true}).click()
 await d.getByRole('button',{name:'路径 /',exact:true}).click();await expect(d.getByRole('button',{name:'保存',exact:true})).toBeDisabled();await d.getByRole('button',{name:'插入媒体标题',exact:true}).click();await expect(d.getByRole('button',{name:'保存',exact:true})).toBeEnabled();await d.getByRole('button',{name:'源码',exact:true}).click();await expect(d.getByRole('textbox',{name:'剧集命名源码',exact:true})).toHaveValue('{title}/{title}')
})

test('template dragging and keyboard alternatives reorder actual persisted source',async({page})=>{
 await create(page,'naming','标签调序');const d=await open(page,'naming','标签调序');await source(page,'tv','{title}.{year}');await d.getByRole('button',{name:'可视化',exact:true}).click()
 const from=d.getByRole('button',{name:'拖动标签 3',exact:true}),to=d.locator('.naming-token').first();await from.scrollIntoViewIfNeeded();const a=await from.boundingBox(),b=await to.boundingBox();await page.mouse.move(a.x+a.width/2,a.y+a.height/2);await page.mouse.down();await page.mouse.move(b.x+b.width/2,b.y+b.height/2,{steps:8});await page.mouse.up()
 await expect(page.locator('.rule-drag-ghost')).toHaveCount(0)
 await expect(d.locator('.naming-token-label').first()).toContainText('发行年份');await d.getByRole('button',{name:'拖动标签 1',exact:true}).focus();await page.keyboard.press('Alt+ArrowRight');await expect(d.locator('.naming-token-label').nth(1)).toContainText('发行年份');await d.getByRole('button',{name:'选择标签 2',exact:true}).click();await d.getByRole('button',{name:'前移',exact:true}).click();await expect(d.locator('.naming-token-label').first()).toContainText('发行年份')
 await d.getByRole('button',{name:'保存',exact:true}).click();await expect(d).toHaveCount(0)
})

test('upgrade ordering disabled weights reset and failed-save drafts persist',async({page})=>{
 const p=await create(page,'upgrade','优先级策略');const d=await open(page,'upgrade','优先级策略');await expect(d.locator('.upgrade-weight')).toHaveText([10,8,6,5,4,3,2,2,1,.6].map(w=>`权重 ${w}`));await d.getByRole('button',{name:'上移分组 片源类型',exact:true}).click();await expect(d.locator('.upgrade-group').first()).toHaveAttribute('data-sort-id','source');await d.getByRole('switch',{name:'启用评分 片源类型',exact:true}).uncheck();await expect(d.locator('.upgrade-weight').first()).toHaveText('不参与评分');await expect(d.locator('.upgrade-weight').nth(1)).toHaveText('权重 10')
 await d.getByRole('button',{name:'选择分辨率标签 4K',exact:true}).click();await d.getByRole('button',{name:'前移标签',exact:true}).click();await expect(d.locator('[data-sort-id=resolution] .upgrade-value-label').first()).toHaveText('4K');await d.getByRole('button',{name:'添加分辨率标签 8K',exact:true}).click();await expect(d.locator('[data-sort-id=resolution] .upgrade-value-label').last()).toHaveText('8K')
 await page.route(`**/api/rule-presets/${p.id}`,route=>route.fulfill({status:409,json:{error:'模拟保存失败'}}));await d.getByRole('button',{name:'保存',exact:true}).click();await expect(d.getByRole('alert')).toHaveText('模拟保存失败');await expect(d.locator('[data-sort-id=resolution] .upgrade-value-label').first()).toHaveText('4K');await page.unroute(`**/api/rule-presets/${p.id}`)
 await d.getByRole('button',{name:'保存',exact:true}).click();await expect(d).toHaveCount(0);await open(page,'upgrade','优先级策略');await expect(page.getByRole('switch',{name:'启用评分 片源类型',exact:true})).not.toBeChecked();await expect(page.locator('[data-sort-id=resolution] .upgrade-value-label').first()).toHaveText('4K');await page.getByRole('button',{name:'恢复默认洗版配置',exact:true}).click();await expect(page.getByRole('switch',{name:'启用评分 片源类型',exact:true})).toBeChecked();await expect(page.locator('.upgrade-group').first()).toHaveAttribute('data-sort-id','resolution')
})

test('upgrade group arrows and tag dragging persist their independent priority orders',async({page})=>{
 const p=await create(page,'upgrade','拖拽洗版');const d=await open(page,'upgrade','拖拽洗版')
 await expect(d.locator('.upgrade-code').first()).toHaveText('（resolution）')
 async function drag(handle,target){await handle.scrollIntoViewIfNeeded();const a=await handle.boundingBox(),b=await target.boundingBox();await page.mouse.move(a.x+a.width/2,a.y+a.height/2);await page.mouse.down();await page.mouse.move(b.x+b.width/2,b.y+15,{steps:10});await page.mouse.up()}
 await expect(d.getByRole('button',{name:'拖动分组 片源类型',exact:true})).toHaveCount(0);await d.getByRole('button',{name:'上移分组 片源类型',exact:true}).click();await expect(d.locator('.upgrade-group').first()).toHaveAttribute('data-sort-id','source')
 const row=d.locator('[data-sort-id=resolution]');await drag(d.getByRole('button',{name:'拖动分辨率标签 4K',exact:true}),row.locator('.upgrade-value').first());await expect(row.locator('.upgrade-value-label').first()).toHaveText('4K');await expect(d.locator('[data-sort-id=source] .upgrade-value-label').first()).toHaveText('REMUX')
 await d.getByRole('button',{name:'保存',exact:true}).click();await expect(d).toHaveCount(0);const saved=(await(await page.request.get('/api/rule-presets?kind=upgrade')).json()).find(v=>v.id===p.id);expect(saved.upgrade.groups[0].id).toBe('source');expect(saved.upgrade.groups[1].values[0]).toBe('4K')
})

test('tag drag ghost follows the pointer and Escape cancels without editing',async({page})=>{
 await create(page,'naming','拖动浮层');const d=await open(page,'naming','拖动浮层');await source(page,'tv','{title}.{year}');await d.getByRole('button',{name:'可视化',exact:true}).click()
 const handle=d.getByRole('button',{name:'拖动标签 1',exact:true});await handle.scrollIntoViewIfNeeded();const box=await handle.boundingBox(),row=await d.locator('.naming-token').first().boundingBox()
 await page.mouse.move(box.x+8,box.y+8);await page.mouse.down();await page.mouse.move(box.x+53,box.y+33)
 const ghost=page.locator('.rule-drag-ghost');await expect(ghost).toHaveAttribute('aria-hidden','true');const floating=await ghost.boundingBox();expect(floating.x-row.x).toBeCloseTo(45,0);expect(floating.y-row.y).toBeCloseTo(25,0);await expect(ghost).toHaveCSS('pointer-events','none')
 await page.keyboard.press('Escape');await page.mouse.up();await expect(ghost).toHaveCount(0);await expect(d.locator('.naming-token-label').first()).toContainText('媒体标题')
 await d.getByRole('button',{name:'源码',exact:true}).click();await expect(d.getByRole('textbox',{name:'剧集命名源码',exact:true})).toHaveValue('{title}.{year}')
})
