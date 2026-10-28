import { test, expect } from '@playwright/test'
import { clipboardShares } from '../src/share-clipboard.js'

const links = {
  '115': 'https://115cdn.com/s/swsj1ir3zr4?password=q526',
  quark: 'https://pan.quark.cn/s/33702e8b82b9',
  mobile: 'https://yun.139.com/shareweb/#/w/i/2xG3rxaJrnv2o'
}
const storages = ['115', 'quark', 'mobile'].map(type => ({ id:type, type, name:`${type}资料`, enabled:true, config:{mode:'native'} }))
async function base(page, extra = {}) {
  await page.route('**/api/files/share/info',r=>r.fulfill({json:{directory:''}}))
  await page.route('**/api/files/share-preferences', r => r.fulfill({ json: { last: {}, directories: {} } }))
  await page.addInitScript(()=>{localStorage.setItem('aether-clipboard-enabled:clipboard-ui','true');localStorage.setItem('aether-clipboard-enabled:clipboard-ui:consent','1')})
  await page.route('**/api/auth/status', r => r.fulfill({json:{initialized:true,authenticated:true}}))
  await page.route('**/api/state', r => r.fulfill({json:{username:'clipboard-ui',storages,tasks:[],settings:{},cache:{},...extra}}))
  await page.route('**/api/plugins/**', r => r.fulfill({json:{}}))
  await page.route('**/api/quark-takeover', r => r.fulfill({json:{bindings:[]}}))
}

test('clipboard parser accepts exact share hosts and preserves passwords', () => {
  for (const [provider, url] of Object.entries(links)) expect(clipboardShares(`[${url}](${url})`)).toEqual({provider,links:url,batches:[{provider,links:url}]})
  for (const url of ['https://115cdn.com.attacker.test/s/abc', 'https://user:secret@115cdn.com/s/abc', 'https://pan.quark.cn/file/123', 'https://yun.139.com/', 'http://115cdn.com/s/abc']) expect(clipboardShares(url)).toBeNull()
  expect(clipboardShares('普通文字')).toBeNull()
  expect(clipboardShares('a'.repeat(200001))).toBeNull()
})

test('clipboard polls while focused and detects all providers without another click', async ({page}) => {
  await page.addInitScript(() => {
    window.shareText = ''
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>window.shareText}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
  })
  await base(page); await page.goto('/storage')
  await expect(page.getByRole('button',{name:'添加存储池',exact:true})).toBeVisible()
  for (const [provider,url] of Object.entries(links)) {
    await page.evaluate(text=>{window.shareText=text},url)
    const modal=page.getByRole('dialog',{name:'分享转存',exact:true})
    await expect(modal).toBeVisible()
    await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(url)
    await expect(modal.getByRole('button',{name:'转存存储池'})).toHaveText(`${provider}资料`)
    await modal.getByRole('button',{name:'取消',exact:true}).click()
  }
})

test('mixed clipboard batches preserve passwords and advance on cancel, submit and close', async ({page},info) => {
  await page.addInitScript(() => {
    window.shareText = ''
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>window.shareText}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
  })
  await base(page)
  const submitted=[]
  await page.route('**/api/files/share/*',r=>{
    const body=r.request().postDataJSON()
    if(r.request().url().endsWith('/preview')) { submitted.push(body);return r.fulfill({json:{preview:'batch',batches:1}}) }
    return r.fulfill({json:{status:'submitted'}})
  })
  await page.goto('/storage')
  const shares={
    '115':Array.from({length:2},(_,i)=>`https://115cdn.com/s/test${i} 提取码: a00${i}`),
    mobile:Array.from({length:3},(_,i)=>`https://yun.139.com/shareweb/#/w/i/test${i} 提取码: b00${i}`),
    quark:Array.from({length:5},(_,i)=>`https://pan.quark.cn/s/test${i} 提取码: c00${i}`)
  }
  const mixed=[shares.quark[0],shares.mobile[0],shares['115'][0],shares.quark[1],shares.mobile[1],shares.quark[2],shares['115'][1],shares.quark[3],shares.mobile[2],shares.quark[4]].join('\n').replaceAll(' 提取码:', '\n提取码:')
  const mobileLinks=shares.mobile.map(link=>link.replace(' 提取码: ','?pwd='))
  await page.evaluate(text=>{window.shareText=text;window.dispatchEvent(new Event('focus'))},mixed)
  const modal=page.getByRole('dialog',{name:'分享转存',exact:true})
  await expect(modal.getByRole('button',{name:'转存存储池'})).toHaveText('115资料')
  await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(shares['115'].join('\n'))
  await expect(modal.locator('.share-link-editor mark')).toHaveCount(2)
  await modal.getByRole('button',{name:'取消',exact:true}).click()
  await expect(modal.getByRole('button',{name:'转存存储池'})).toHaveText('mobile资料')
  await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(mobileLinks.join('\n'))
  for(const [theme,width] of [['light',1440],['dark',390]]){
    await page.setViewportSize({width,height:844});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await expect(modal.locator('.share-link-editor mark')).toHaveCount(3)
    expect(await modal.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    await page.screenshot({path:info.outputPath('passwords-'+theme+'.png')})
  }
  await modal.getByRole('button',{name:'转存',exact:true}).click()
  await expect(modal.getByRole('button',{name:'转存存储池'})).toHaveText('quark资料')
  await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(shares.quark.join('\n'))
  await expect.poll(()=>submitted.length).toBe(3)
  expect(submitted.map(body=>body.url)).toEqual(mobileLinks)
  expect(submitted.every(body=>body.storageId==='mobile')).toBe(true)
  await modal.getByRole('button',{name:'关闭',exact:true}).click()
  await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
  await expect(modal).toHaveCount(0)
  await page.waitForTimeout(1700)
  await expect(modal).toHaveCount(0)
})

test('returning to focus reads new clipboard even when an input retained focus',async({page})=>{
  await page.addInitScript(()=>{
    window.shareText='';window.clipboardReads=0
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>{window.clipboardReads++;return window.shareText}}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
    window.pageFocused=true
    Object.defineProperty(document,'hasFocus',{value:()=>window.pageFocused})
  })
  await base(page)
  await page.route('**/api/files?**',r=>r.fulfill({json:[]}))
  await page.goto('/files?storage=mobile')
  await expect.poll(()=>page.evaluate(()=>window.clipboardReads)).toBeGreaterThan(0)
  await page.locator('.files-heading-actions input').focus()
  // Headless Chromium keeps every tab focused; model the OS focus boundary explicitly.
  await page.evaluate(()=>{window.pageFocused=false;window.dispatchEvent(new Event('blur'))})
  await expect.poll(()=>page.evaluate(()=>document.hasFocus())).toBe(false)
  const reads=await page.evaluate(()=>window.clipboardReads)
  await page.evaluate(()=>{window.shareText='链接: https://yun.139.com/shareweb/#/w/i/2ygb7gk5b1gfy\n提取码: x2ol'})
  await page.waitForTimeout(1700)
  expect(await page.evaluate(()=>window.clipboardReads)).toBe(reads)
  await page.evaluate(()=>{window.pageFocused=true;window.dispatchEvent(new Event('focus'))})
  const modal=page.getByRole('dialog',{name:'分享转存',exact:true})
  await expect(modal).toBeVisible()
  await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue('https://yun.139.com/shareweb/#/w/i/2ygb7gk5b1gfy?pwd=x2ol')
  await expect(modal.locator('mark')).toHaveText('pwd=x2ol')
})

test('focus resume can read after a previous quiet permission denial',async({page})=>{
  await page.addInitScript(()=>{
    window.allowClipboard=false;window.shareText='';window.reads=0
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'prompt'})}})
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>{window.reads++;if(!window.allowClipboard)throw new DOMException('Denied','NotAllowedError');return window.shareText}}})
  })
  await base(page);await page.goto('/storage')
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await expect.poll(()=>page.evaluate(()=>window.reads)).toBe(1)
  await page.evaluate(()=>{window.allowClipboard=true;window.shareText='https://pan.quark.cn/s/new123\n提取码: Ab12';window.dispatchEvent(new Event('focus'))})
  await expect(page.getByRole('dialog',{name:'分享转存'}).getByRole('textbox',{name:'分享链接'})).toHaveValue('https://pan.quark.cn/s/new123 提取码: Ab12')
})

test('changes in later provider batches are not hidden by deduplication',async({page})=>{
  await page.addInitScript(()=>{
    window.shareText=''
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>window.shareText}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
  })
  await base(page);await page.goto('/storage')
  const modal=page.getByRole('dialog',{name:'分享转存'})
  for(const password of ['Ab12','Cd34']){
    await page.evaluate(code=>{window.shareText=`https://115cdn.com/s/one\nhttps://pan.quark.cn/s/two 提取码: ${code}`;window.dispatchEvent(new Event('focus'))},password)
    await expect(modal.getByRole('button',{name:'转存存储池'})).toHaveText('115资料')
    await modal.getByRole('button',{name:'取消',exact:true}).click()
    await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(`https://pan.quark.cn/s/two 提取码: ${password}`)
    await modal.getByRole('button',{name:'取消',exact:true}).click()
    await expect(modal).toHaveCount(0)
  }
})

test('over fifty shares advance through same-provider windows without dropping entries',async({page})=>{
  await page.addInitScript(()=>{
    window.shareText=''
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>window.shareText}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
  })
  await base(page);await page.goto('/storage')
  const lines=Array.from({length:51},(_,i)=>`https://pan.quark.cn/s/test${i} 提取码: Ab12`)
  await page.evaluate(text=>{window.shareText=text;window.dispatchEvent(new Event('focus'))},lines.join('\n'))
  const modal=page.getByRole('dialog',{name:'分享转存'})
  await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(lines.slice(0,50).join('\n'))
  await page.keyboard.press('Escape')
  await expect(modal.getByRole('textbox',{name:'分享链接'})).toHaveValue(lines[50])
  await page.keyboard.press('Escape')
  await expect(modal).toHaveCount(0)
})

test('password highlighting follows editing and scrolling, while conflicting codes block submit',async({page},info)=>{
  await base(page)
  await page.route('**/api/files?**',r=>r.fulfill({json:[]}))
  await page.goto('/files?storage=mobile')
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'分享转存',exact:true}).click()
  const modal=page.getByRole('dialog',{name:'分享转存',exact:true}), input=modal.getByRole('textbox',{name:'分享链接'})
  const lines=Array.from({length:20},(_,i)=>`https://yun.139.com/shareweb/#/w/i/test${i}\n提取码: x2ol`).join('\n')
  await input.fill(lines)
  await expect(modal.locator('mark')).toHaveCount(20)
  await input.evaluate(el=>{el.scrollTop=el.scrollHeight;el.dispatchEvent(new Event('scroll'))})
  await expect.poll(()=>modal.locator('.share-link-editor').evaluate(el=>Math.abs(el.querySelector('pre').scrollTop-el.querySelector('textarea').scrollTop))).toBeLessThan(2)
  await page.screenshot({path:info.outputPath('password-editor-scrolled.png')})
  await input.fill('https://yun.139.com/shareweb/?password=abcd#/w/i/test\n提取码: efgh')
  await expect(modal.getByRole('button',{name:'转存',exact:true})).toBeDisabled()
  await expect(modal.getByRole('alert')).toContainText('不同提取码')
  await input.fill('https://yun.139.com/shareweb/#/w/i/test\n提取码: Ab12')
  await expect(modal.getByRole('button',{name:'转存',exact:true})).toBeEnabled()
  await expect(modal.locator('mark')).toHaveText('提取码: Ab12')
})

test('clipboard authorization can be retried explicitly', async ({page},info) => {
  await page.addInitScript(() => {
    window.allowClipboard=false; window.shareText='https://pan.quark.cn/s/33702e8b82b9'
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'denied'})}})
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>{
      if(!window.allowClipboard) throw new DOMException('Denied','NotAllowedError')
      return window.shareText
    }}})
  })
  await base(page); await page.goto('/storage')
  await page.getByRole('button',{name:'账号菜单'}).click()
  await page.getByRole('button',{name:'账号设置',exact:true}).click()
  await page.getByRole('button',{name:'授权剪贴板',exact:true}).click()
  await expect(page.locator('.toast').filter({hasText:'请允许浏览器读取剪贴板'})).toBeVisible()
  await page.evaluate(()=>{window.allowClipboard=true})
  await page.getByRole('button',{name:'授权剪贴板',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'分享转存',exact:true})).toBeVisible()
  await page.getByRole('dialog',{name:'分享转存',exact:true}).getByRole('button',{name:'取消',exact:true}).click()
  await page.screenshot({path:info.outputPath('share-listener-settings.png')})
})

test('unavailable clipboard explains secure-context restriction', async ({page}) => {
  await page.addInitScript(()=>Object.defineProperty(navigator,'clipboard',{value:undefined}))
  await base(page);await page.goto('/storage')
  await page.getByRole('button',{name:'账号菜单'}).click()
  await page.getByRole('button',{name:'账号设置',exact:true}).click()
  await expect(page.getByRole('button',{name:'授权剪贴板',exact:true})).toHaveCount(0)
  await expect(page.locator('.clipboard-setting [role=status]')).toHaveText('当前浏览器仅支持主动粘贴')
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
  await expect(page.getByRole('dialog',{name:'分享转存'})).toBeVisible()
  await page.getByRole('dialog',{name:'分享转存'}).getByRole('button',{name:'取消',exact:true}).click()
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
  await expect(page.locator('.file-toolbar > .rounded-select')).toHaveCSS('width','160px')
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await expect(page.locator('.notification-list time').first()).toHaveText(/^\d{2}\/\d{2}\/\d{2} \d{2}:\d{2}$/)
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
