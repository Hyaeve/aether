import { test, expect } from '@playwright/test'

async function accountMock(page) {
  await page.route('**/api/**',r=>{
    const path=new URL(r.request().url()).pathname
    return r.fulfill({json:path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'clipboard-account',storages:[{id:'quark',type:'quark',name:'夸克',enabled:true,config:{}}],tasks:[],settings:{sessionDays:15},cache:{},traffic:{}}:path==='/api/files/share/info'?{directory:''}:[]})
  })
}

test('account credentials and security occupy separate themed panels with an input-width save',async({page},info)=>{
  await page.addInitScript(()=>{
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>''}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
  })
  await accountMock(page);await page.goto('/settings/account')
  await expect(page.locator('.account-panel')).toHaveCount(2)
  await expect(page.locator('.account-credentials').getByRole('textbox',{name:'账号',exact:true})).toBeVisible()
  await expect(page.locator('.account-security').getByRole('spinbutton',{name:'会话有效期',exact:true})).toBeVisible()
  await expect(page.locator('.account-security').getByRole('switch',{name:'剪贴板监听',exact:true})).not.toBeChecked()
  for(const [theme,width] of [['light',1440],['dark',1440],['light',390],['dark',390]]){
    await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    const rects=await page.locator('.account-credentials').evaluate(el=>[...el.querySelectorAll('.credential-input,.account-save')].map(n=>{const r=n.getBoundingClientRect();return{x:r.x,width:r.width}}))
    for(const rect of rects){expect(Math.abs(rect.width-rects[0].width)).toBeLessThan(1);expect(Math.abs(rect.x-rects[0].x)).toBeLessThan(1)}
    await expect(page.locator('.account-panel').first()).toHaveCSS('background-color',theme==='light'?'rgb(250, 251, 252)':'rgb(27, 29, 35)')
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`account-${theme}-${width}.png`)})
  }
  let saved
  await page.route('**/api/account',r=>{saved=r.request().postDataJSON();return r.fulfill({json:{ok:true}})})
  await page.getByLabel('密码',{exact:true}).fill('new-password')
  await page.getByLabel('会话有效期',{exact:true}).fill('14')
  await page.getByRole('button',{name:'保存修改',exact:true}).click()
  await expect.poll(()=>saved).toEqual({username:'clipboard-account',password:'new-password',sessionDays:14})
  await expect(page.getByLabel('密码',{exact:true})).toHaveValue('')
})

test('listener switch stops reads, persists per browser account, and re-enables with authorization',async({page})=>{
  await page.addInitScript(()=>{
    window.clipboardReads=0;window.shareText=''
    if(localStorage.getItem('aether-clipboard-enabled:clipboard-account')===null){localStorage.setItem('aether-clipboard-enabled:clipboard-account','true');localStorage.setItem('aether-clipboard-enabled:clipboard-account:consent','1')}
    Object.defineProperty(navigator,'clipboard',{value:{readText:async()=>{window.clipboardReads++;return window.shareText}}})
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
  })
  await accountMock(page);await page.goto('/settings/account')
  await expect.poll(()=>page.evaluate(()=>window.clipboardReads)).toBeGreaterThan(0)
  const toggle=page.getByRole('switch',{name:'剪贴板监听',exact:true})
  await toggle.uncheck()
  const reads=await page.evaluate(()=>window.clipboardReads)
  await page.evaluate(()=>{window.shareText='https://pan.quark.cn/s/abc 提取码: Ab12';window.dispatchEvent(new Event('focus'))})
  await page.waitForTimeout(1700)
  expect(await page.evaluate(()=>window.clipboardReads)).toBe(reads)
  await expect(page.getByRole('dialog',{name:'分享转存'})).toHaveCount(0)
  await page.reload()
  await expect(toggle).not.toBeChecked()
  expect(await page.evaluate(()=>window.clipboardReads)).toBe(0)
  await page.evaluate(()=>{window.shareText='https://pan.quark.cn/s/abc 提取码: Ab12'})
  await toggle.click()
  await expect(toggle).not.toBeChecked()
  await page.getByRole('button',{name:'我已阅读并同意',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'分享转存'}).getByRole('textbox',{name:'分享链接'})).toHaveValue('https://pan.quark.cn/s/abc 提取码: Ab12')
})

test('plain LAN HTTP exposes the real clipboard restriction and keeps switch usable',async({page},info)=>{
  const origin='http://192.168.50.2:15159'
  await page.route(origin+'/**',async r=>r.fulfill({response:await r.fetch({url:r.request().url().replace(origin,'http://127.0.0.1:15159')})}))
  await accountMock(page)
  await page.goto(origin+'/settings/account')
  expect(await page.evaluate(()=>window.isSecureContext)).toBe(false)
  expect(await page.evaluate(()=>typeof navigator.clipboard)).toBe('undefined')
  await expect(page.locator('.clipboard-setting [role=status]')).toHaveText('已关闭')
  await expect(page.getByRole('button',{name:'授权剪贴板',exact:true})).toHaveCount(0)
  await expect(page.locator('.clipboard-security-mark')).toHaveCSS('pointer-events','none')
  const toggle=page.getByRole('switch',{name:'剪贴板监听',exact:true})
  await toggle.uncheck();await expect(toggle).not.toBeChecked()
  await toggle.click();await page.getByRole('button',{name:'我已阅读并同意',exact:true}).click();await expect(toggle).toBeChecked()
  await expect(page.locator('.clipboard-setting [role=status]')).toHaveText('当前 HTTP 连接仅支持主动粘贴')
  await page.screenshot({path:info.outputPath('account-lan-http.png')})
})

test('turning listener off discards pending mixed batches and late clipboard reads',async({page})=>{
  await page.addInitScript(()=>{
    window.shareText='';window.holdClipboard=false
    localStorage.setItem('aether-clipboard-enabled:clipboard-account','true');localStorage.setItem('aether-clipboard-enabled:clipboard-account:consent','1')
    Object.defineProperty(navigator,'permissions',{value:{query:async()=>({state:'granted'})}})
    Object.defineProperty(navigator,'clipboard',{value:{readText:()=>window.holdClipboard?new Promise(resolve=>window.releaseClipboard=resolve):Promise.resolve(window.shareText)}})
  })
  await accountMock(page);await page.goto('/settings/account')
  await page.evaluate(()=>{window.holdClipboard=true;window.dispatchEvent(new Event('focus'))})
  await expect.poll(()=>page.evaluate(()=>typeof window.releaseClipboard)).toBe('function')
  await page.getByRole('switch',{name:'剪贴板监听',exact:true}).uncheck()
  await page.evaluate(()=>window.releaseClipboard('https://115cdn.com/s/one\nhttps://pan.quark.cn/s/two'))
  await expect(page.getByRole('dialog',{name:'分享转存'})).toHaveCount(0)
})
