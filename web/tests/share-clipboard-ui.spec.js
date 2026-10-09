import { test, expect } from '@playwright/test'
import { clipboardShares } from '../src/share-clipboard.js'

const links = {
  '115': 'https://115cdn.com/s/swsj1ir3zr4?password=q526',
  quark: 'https://pan.quark.cn/s/33702e8b82b9',
  mobile: 'https://yun.139.com/shareweb/#/w/i/2xG3rxaJrnv2o'
}
const storages = ['115', 'quark', 'mobile'].map(type => ({ id:type, type, name:`${type}资料`, enabled:true, config:{mode:'native'} }))
async function base(page, extra = {}) {
  await page.route('**/api/auth/status', r => r.fulfill({json:{initialized:true,authenticated:true}}))
  await page.route('**/api/state', r => r.fulfill({json:{username:'clipboard-ui',storages,tasks:[],settings:{},cache:{},...extra}}))
  await page.route('**/api/plugins/**', r => r.fulfill({json:{}}))
  await page.route('**/api/quark-takeover', r => r.fulfill({json:{bindings:[]}}))
}

test('clipboard parser accepts exact share hosts and preserves passwords', () => {
  for (const [provider, url] of Object.entries(links)) expect(clipboardShares(`[${url}](${url})`)).toEqual({provider,links:url})
  for (const url of ['https://115cdn.com.attacker.test/s/abc', 'https://user:secret@115cdn.com/s/abc', 'https://pan.quark.cn/file/123', 'https://yun.139.com/', 'http://115cdn.com/s/abc']) expect(clipboardShares(url)).toBeNull()
  expect(clipboardShares('普通文字')).toBeNull()
  expect(clipboardShares('a'.repeat(200001))).toBeNull()
})

test('authorized clipboard opens matching share modal once and never submits automatically', async ({page},info) => {
  await page.addInitScript(() => {
    window.shareText = ''; window.clipboardReads = 0
    Object.defineProperty(navigator, 'clipboard', {value:{readText:async()=>{window.clipboardReads++;return window.shareText}}})
    Object.defineProperty(navigator, 'permissions', {value:{query:async()=>({state:'granted'})}})
  })
  await base(page)
  let submitted = 0
  await page.route('**/api/shares/**', r => { submitted++; return r.fulfill({json:{}}) })
  await page.goto('/storage')
  for (const [provider,url] of Object.entries(links)) {
    await page.evaluate(value => { window.shareText=value;window.dispatchEvent(new Event('focus')) },url)
    const modal=page.getByRole('dialog',{name:'分享转存',exact:true})
    await expect(modal).toBeVisible()
    await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(url)
    await expect(modal.getByRole('button',{name:'转存存储池'})).toHaveText(`${provider}资料`)
    await modal.getByRole('button',{name:'取消',exact:true}).click()
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
    await expect.poll(()=>page.evaluate(()=>window.clipboardReads)).toBeGreaterThan(1)
    await expect(modal).toHaveCount(0)
  }
  expect(submitted).toBe(0)
  await page.evaluate(value=>{window.shareText=value;window.dispatchEvent(new Event('focus'))},links['115'])
  await expect(page.getByRole('dialog',{name:'分享转存'})).toHaveCount(0)
  await page.evaluate(()=>{window.shareText='https://pan.quark.cn/s/new123';window.dispatchEvent(new Event('focus'))})
  await expect(page.getByRole('dialog',{name:'分享转存'})).toBeVisible()
  await page.screenshot({path:info.outputPath('clipboard-share.png')})
})

test('clipboard results arriving after logout cannot reopen the share modal',async({page})=>{
  await page.addInitScript(()=>{
    window.clipboardStarted=false
    Object.defineProperty(navigator,'clipboard',{value:{readText:()=>new Promise(resolve=>{window.clipboardStarted=true;window.releaseClipboard=resolve})}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
  })
  await base(page)
  await page.route('**/api/auth/logout',r=>r.fulfill({json:{ok:true}}))
  await page.goto('/storage')
  await expect.poll(()=>page.evaluate(()=>window.clipboardStarted)).toBe(true)
  await page.getByRole('button',{name:'账号菜单'}).click()
  await page.getByRole('button',{name:'退出登录',exact:true}).click()
  await expect(page.locator('.login-page')).toBeVisible()
  await page.evaluate(url=>window.releaseClipboard(url),links['115'])
  await expect(page.getByRole('dialog',{name:'分享转存'})).toHaveCount(0)
})

test('denied clipboard is quiet and attempted only once per authenticated session', async ({page}) => {
  await page.addInitScript(() => {
    window.clipboardReads=0
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>{window.clipboardReads++;throw new DOMException('Denied','NotAllowedError')}}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'denied'})}})
  })
  await base(page);await page.goto('/storage')
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await expect.poll(()=>page.evaluate(()=>window.clipboardReads)).toBe(1)
  await expect(page.getByRole('dialog',{name:'分享转存'})).toHaveCount(0)
  await expect(page.locator('.toast.error')).toHaveCount(0)
})

test('clipboard modal keeps per-storage directories isolated and waits for other dialogs', async ({page}) => {
  await page.addInitScript(() => {
    window.shareText=''
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>window.shareText}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
    localStorage.setItem('aether-share-directory:clipboard-ui:115',JSON.stringify({path:'123',label:'目录甲',history:[{id:'/',name:'目录甲'}]}))
    localStorage.setItem('aether-share-directory:clipboard-ui:second',JSON.stringify({path:'456',label:'目录乙',history:[{id:'/',name:'目录乙'}]}))
  })
  await base(page,{storages:[...storages,{id:'second',type:'115',name:'第二账号',enabled:true,config:{}}]})
  await page.goto('/storage')
  await page.getByRole('button',{name:'添加存储池',exact:true}).click()
  await page.evaluate(url=>{window.shareText=url;window.dispatchEvent(new Event('focus'))},links['115'])
  await expect(page.getByRole('dialog',{name:'分享转存'})).toHaveCount(0)
  await page.keyboard.press('Escape')
  await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
  const modal=page.getByRole('dialog',{name:'分享转存'})
  await expect(modal).toBeVisible()
  await expect(modal.getByRole('button',{name:'选择转存目录'})).toHaveText('目录甲')
  await modal.getByRole('button',{name:'转存存储池'}).click()
  await page.getByRole('option',{name:'第二账号',exact:true}).click()
  await expect(modal.getByRole('button',{name:'选择转存目录'})).toHaveText('目录乙')
  await modal.getByRole('button',{name:'转存存储池'}).click()
  await page.getByRole('option',{name:'115资料',exact:true}).click()
  await expect(modal.getByRole('button',{name:'选择转存目录'})).toHaveText('目录甲')
  expect(await page.evaluate(()=>JSON.parse(localStorage.getItem('aether-share-directory:clipboard-ui:second')).path)).toBe('456')
})

test('notice time, log colors, transfer icon and favorites alignment are consistent',async({page},info)=>{
  await base(page,{libraryNotices:Array.from({length:7},(_,i)=>({id:String(i),name:`通知${i}`,event:'library.new',time:new Date().toISOString()}))})
  await page.route('**/api/files?**',r=>r.fulfill({json:[]}))
  await page.route('**/api/transfers',r=>r.fulfill({json:[]}))
  await page.route('**/api/logs',r=>r.fulfill({json:[{time:new Date().toISOString(),module:'system',level:'info',message:'日志文本'}]}))
  await page.goto('/files')
  await page.getByRole('button',{name:'展开收藏栏'}).click()
  await expect.poll(async()=>{
    const a=await page.locator('.file-toolbar > .rounded-select').boundingBox(),b=await page.locator('.file-favorites').boundingBox()
    return Math.abs(a.x+a.width-b.x-b.width)
  }).toBeLessThan(2)
  await expect(page.locator('.file-toolbar > .rounded-select')).toHaveCSS('width','128px')
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await expect(page.locator('.notification-list time').first()).toHaveText(/^\d{2}-\d{2} \d{2}:\d{2}$/)
  await expect.poll(()=>page.locator('.notice-scroll .thin-scroll-thumb').evaluate(el=>parseFloat(getComputedStyle(el,'::after').width)*devicePixelRatio)).toBeGreaterThan(1.8)
  await page.screenshot({path:info.outputPath('notice-files.png')})
  await page.goto('/transfer')
  await expect(page.locator('.transfer-modes .upload svg')).toHaveClass(/lucide-upload/)
  await page.goto('/logs')
  await expect(page.locator('.log-entry time')).toBeVisible()
  const colors=await page.locator('.log-entry').evaluate(el=>[getComputedStyle(el.querySelector('time')).color,getComputedStyle(el.querySelector('.log-message')).color])
  expect(colors[0]).not.toBe(colors[1])
  await page.getByRole('button',{name:'当前结构化列表，切换原始列表'}).click()
  await expect(page.locator('.raw-time')).toBeVisible()
  await page.screenshot({path:info.outputPath('raw-time.png')})
})
