import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const storageName = '中文ABC汉字115名称'
async function fixture(page, theme) {
  const unexpected = []
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = {
      username: 'spacing', storages: [{ id: 'pool', name: storageName, type: 'local', enabled: true, config: {} }],
      tasks: [], settings: { sessionDays: 15 }, cache: {}, traffic: {}, logs: []
    }
    else if (path === '/api/traffic') json = {}
    else if (path === '/api/strm-replace') json = { jobs: [] }
    else if (path === '/api/plugins/proxy') json = { enabled: false, address: '', username: '', password: '' }
    else if (['/api/plugins/tmdb', '/api/plugins/ai', '/api/plugins/emby'].includes(path)) json = { enabled: false }
    else if (['/api/115-signin', '/api/115-folder-cleanup', '/api/115-recycle-cleanup'].includes(path)) json = { enabled: false, rules: [] }
    else if (path === '/api/115-simulcast/enabled') json = { enabled: true }
    else if (path === '/api/quark-takeover') json = { enabled: false, bindings: [] }
    else {
      unexpected.push(path)
      return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
    }
    return route.fulfill({ json })
  })
  return unexpected
}


const browserName=process.env.AETHER_SELECTION_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})
for (const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[844,390,'light']]) {
 test(`native Chinese text and continuous selection ${width} ${theme}`,async({page},info)=>{
  const unexpected=await fixture(page,theme);await page.setViewportSize({width,height});await page.goto('/storage')
  const heading=page.getByRole('heading',{name:storageName,exact:true})
  await expect(heading).toBeVisible();await expect(heading).toHaveText(storageName)
  await expect(page.locator('aether-char,aether-text,.han-gap')).toHaveCount(0)
  await page.goto('/settings/account')
  const selectable=page.getByRole('heading',{name:'账户配置',exact:true})
  const result=await selectable.evaluate(el=>{const range=document.createRange();range.selectNodeContents(el);const selection=getSelection();selection.removeAllRanges();selection.addRange(range);const c=getComputedStyle(el,'::selection'),spacing=getComputedStyle(el).letterSpacing;return{text:selection.toString(),rects:[...range.getClientRects()].filter(r=>r.width>0).length,spacing:spacing==='normal'?0:parseFloat(spacing),bg:c.backgroundColor,color:c.color}})
  expect(result).toEqual({text:'账户配置',rects:1,spacing:0,bg:'rgb(58, 73, 128)',color:'rgb(255, 255, 255)'})
  await captureScreenshot(page,{path:info.outputPath(`text-selection-${width}-${theme}.png`)})
  const username=page.getByRole('textbox',{name:'账号',exact:true});await username.fill(storageName);await username.evaluate(el=>el.select());await expect(username).toHaveValue(storageName)
  expect(await username.evaluate(el=>el.selectionEnd-el.selectionStart)).toBe(storageName.length)
  expect(unexpected).toEqual([])
 })
}
test('native plugin text retains search and button actions',async({page})=>{
 const unexpected=await fixture(page,'light');await page.goto('/tools')
 const tool=page.getByRole('button',{name:'115 自动签到',exact:true});await expect(tool.locator('strong')).toHaveText('115 自动签到');await expect(tool.locator('.han-gap')).toHaveCount(0)
 await tool.click();await expect(page.getByRole('dialog',{name:'115 自动签到',exact:true})).toBeVisible();await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'搜索辅助工具',exact:true}).click();const search=page.getByRole('textbox',{name:'插件搜索关键词',exact:true});await search.fill('秒传');await expect(page.locator('.plugin-search-result')).toHaveCount(2)
 await expect(page.locator('.plugin-search-title strong')).toHaveText(['115 秒传中心','阿里云盘秒传']);expect(unexpected).toEqual([])
})
