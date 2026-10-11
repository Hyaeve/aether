import { test, expect } from '@playwright/test'

async function setup(page) {
  const stores = [{id:'local',name:'本地文件',type:'local',enabled:true,config:{}},{id:'pan',name:'云盘',type:'115',enabled:true,config:{}}]
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    const json = path === '/api/auth/status' ? {initialized:true,authenticated:true}
      : path === '/api/state' ? {username:'workspace-revision',storages:stores,tasks:[],settings:{},cache:{},logs:[]}
      : path === '/api/files' ? [{id:'folder',name:'文件夹',isDir:true}]
      : path === '/api/link-playback' ? {recentEvents:[{time:new Date().toISOString(),upstream:'upstream',outcome:'redirect',userAgent:'Browser '+ 'long agent '.repeat(30),target:'https://cdn.example.test/'+'very-long-path/'.repeat(45),client:'192.168.1.1'}]}
      : ['/api/backup-rules','/api/transfers','/api/links'].includes(path) ? [] : {}
    return route.fulfill({json})
  })
}

test('backup filter header is unique and token removal overlays compact text', async ({page}, info) => {
  await setup(page); await page.goto('/transfer/backup')
  await expect(page.locator('.empty-state .btn')).not.toHaveClass(/primary/)
  await page.locator('.task-heading').getByRole('button',{name:'添加规则',exact:true}).click()
  const modal = page.getByRole('dialog',{name:'添加备份规则',exact:true})
  await modal.getByRole('button',{name:'筛选规则',exact:true}).click()
  await modal.getByRole('button',{name:'添加规则',exact:true}).click()
  await page.getByRole('dialog',{name:'选择规则类型'}).getByRole('button',{name:'扩展名',exact:true}).click()
  const card = modal.locator('.backup-filter')
  await expect(card.locator('label').filter({hasText:'扩展名'})).toHaveCount(0)
  await expect(card.locator('header')).toContainText('1. 扩展名')
  await card.getByLabel('匹配内容 1').fill('.MKV'); await card.getByLabel('匹配内容 1').press('Enter')
  const token = card.locator('.backup-token')
  await expect(token).toHaveText('mkv')
  const dimensions = await token.evaluate(el => ({chip:el.getBoundingClientRect().width,text:el.querySelector('span').getBoundingClientRect().width}))
  expect(dimensions.chip-dimensions.text).toBeLessThan(16)
  await token.hover()
  await expect(token.getByRole('button',{name:'删除 mkv 规则 1'})).toHaveCSS('opacity','1')
  await page.screenshot({path:info.outputPath('compact-filter-token.png')})
  await token.getByRole('button').click(); await expect(token).toHaveCount(0)
})

test('storage selector uses provider icons and only selected highlighting', async ({page}, info) => {
  await setup(page); await page.goto('/files')
  const selector = page.getByRole('button',{name:'选择存储池',exact:true})
  await expect(selector.locator('.provider-icon')).toHaveCount(1)
  await selector.click()
  const option = page.getByRole('option',{name:'本地文件',exact:true})
  await expect(option).toHaveAttribute('aria-selected','true')
  await expect(option.locator('.provider-icon')).toHaveCount(1)
  await expect(option.locator(':scope > svg')).toHaveCount(0)
  await page.getByRole('option',{name:'云盘',exact:true}).click()
  await expect(selector.locator('.provider-icon')).toHaveAttribute('data-provider','115')
  await page.screenshot({path:info.outputPath('provider-selector.png')})
})

test('cache text tooltip waits for stationary pointer and shows full text without scrollbar', async ({page}, info) => {
  await setup(page); await page.goto('/links/cache')
  const cell = page.locator('.playback-copy').filter({hasText:'very-long-path'}), tooltip = page.getByRole('tooltip')
  const rect = await cell.boundingBox()
  await page.mouse.move(rect.x+8,rect.y+10)
  await page.waitForTimeout(400); await expect(tooltip).toHaveCount(0)
  await page.mouse.move(rect.x+9,rect.y+10)
  await page.waitForTimeout(400); await expect(tooltip).toHaveCount(0)
  await expect(tooltip).toBeVisible()
  await expect(tooltip).toHaveCSS('max-height','none')
  await expect(tooltip).toHaveCSS('overflow','visible')
  expect(await tooltip.evaluate(el=>el.scrollHeight<=el.clientHeight)).toBe(true)
  await page.screenshot({path:info.outputPath('stationary-cache-tooltip.png')})
  await page.mouse.move(rect.x+12,rect.y+10); await expect(tooltip).toHaveCount(0)
  await expect(tooltip).toBeVisible()
})
