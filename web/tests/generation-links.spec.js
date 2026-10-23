import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
  test(`CAS mobile links are one-shot with separate multiline input ${width} ${theme}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 })
    const previews = [], jobs = []
    let taskWrites = 0
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname
      let json = []
      if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
      if (path === '/api/state') json = { username: 'link-generation', storages: [], tasks: [], settings: {}, cache: {}, traffic: {}, strmRoot: '/strm' }
      if (path === '/api/generation/preview') {
        const input = route.request().postDataJSON(); previews.push(input)
        const invalid = input.text.includes('invalid-hash')
        const temporary = input.text.includes('missing-hash') ? { storageId:'mobile-pool', storageName:'移动临时池',directory:'/Aether/cas-import-preview/batch',count:2 } : null
        json = { preview: invalid ? '' : 'one-shot', valid: !invalid,temporary, items: [{ path: 'Season 1/Episode.mkv.cas.strm', size: 12, error: invalid ? '移动分享 SHA256 格式无效' : '' }] }
      }
      if (path === '/api/generation/jobs' && route.request().method() === 'POST') { jobs.push(route.request().postDataJSON()); json = { id: 'job', status: 'queued' } }
      if (path === '/api/tasks' && route.request().method() === 'POST') taskWrites++
      return route.fulfill({ json })
    })
    await page.goto('/tasks/cas')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await page.getByRole('button', { name: '添加任务', exact: true }).click()
    let modal = page.getByRole('dialog', { name: '添加 CAS 任务', exact: true })
    await modal.getByRole('textbox', { name: 'CAS 信息', exact: true }).fill('{"name":"one.mkv"}\nencoded-second-line')
    await modal.getByRole('tab', { name: '链接解析', exact: true }).click()
    const links = 'https://yun.139.com/shareweb/#/w/i/first?pwd=Ab12\nhttps://yun.139.com/shareweb/#/w/i/second'
    await modal.getByRole('textbox', { name: '移动分享链接', exact: true }).fill(links)
    await expect(modal.getByRole('tab')).toHaveText(['文本解析', '文件解析', '链接解析'])
    await captureScreenshot(page, { path: info.outputPath(`generation-links-${width}-${theme}.png`) })
    expect(await modal.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await modal.getByRole('button', { name: '解析', exact: true }).click()
    expect(previews[0]).toEqual({ kind: 'cas', mode: 'links', text: links })
    await modal.getByRole('button', { name: '上一步', exact: true }).click()
    await expect(modal.getByRole('textbox', { name: '移动分享链接', exact: true })).toHaveValue(links)
    await modal.getByRole('tab', { name: '文本解析', exact: true }).click()
    await expect(modal.getByRole('textbox', { name: 'CAS 信息', exact: true })).toHaveValue('{"name":"one.mkv"}\nencoded-second-line')
    await modal.getByRole('tab', { name: '链接解析', exact: true }).click()
    await modal.getByRole('textbox', { name: '移动分享链接', exact: true }).fill('https://yun.139.com/shareweb/#/w/i/invalid-hash')
    await modal.getByRole('button', { name: '解析', exact: true }).click()
    await expect(modal.getByRole('button', { name: '加入生成队列', exact: true })).toBeDisabled()
    await expect(page.locator('.toast-stack')).toContainText('移动分享 SHA256 格式无效')
    await modal.getByRole('button', { name: '上一步', exact: true }).click()
    await modal.getByRole('textbox', { name: '移动分享链接', exact: true }).fill(links)
    await modal.getByRole('button', { name: '解析', exact: true }).click()
    await modal.getByRole('button', { name: '加入生成队列', exact: true }).click()
    await expect(modal).toHaveCount(0)
    expect(jobs).toEqual([{ preview: 'one-shot', target: '/strm' }])
    expect(taskWrites).toBe(0)
    await page.getByRole('button', { name:'添加任务',exact:true }).click()
    modal=page.getByRole('dialog',{name:'添加 CAS 任务',exact:true})
    await modal.getByRole('tab',{name:'链接解析',exact:true}).click()
    await modal.getByRole('textbox',{name:'移动分享链接',exact:true}).fill('https://yun.139.com/shareweb/#/w/i/missing-hash')
    await modal.getByRole('button',{name:'解析',exact:true}).click()
    await modal.getByRole('button',{name:'加入生成队列',exact:true}).click()
    let consent=page.getByRole('dialog',{name:'确认临时转存生成',exact:true})
    await expect(consent).toContainText('移动临时池')
    await expect(consent).toContainText('/Aether/cas-import-preview/batch')
    await expect(consent).toContainText('2 个文件')
    expect(jobs).toHaveLength(1)
    await captureScreenshot(page,{path:info.outputPath(`generation-consent-${width}-${theme}.png`)})
    expect(await consent.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    await consent.getByRole('button',{name:'取消',exact:true}).click()
    await expect(consent).toHaveCount(0)
    await expect(modal).toBeVisible()
    expect(jobs).toHaveLength(1)
    await modal.getByRole('button',{name:'加入生成队列',exact:true}).click()
    consent=page.getByRole('dialog',{name:'确认临时转存生成',exact:true})
    await consent.getByRole('button',{name:'确认生成',exact:true}).click()
    await expect(modal).toHaveCount(0)
    expect(jobs[1]).toEqual({preview:'one-shot',target:'/strm',confirm:true})
    await page.goto('/tasks/ed2k')
    await page.getByRole('button', { name: '添加任务', exact: true }).click()
    modal = page.getByRole('dialog', { name: '添加 ED2K 任务', exact: true })
    await expect(modal.getByRole('tab')).toHaveText(['文本解析', '文件解析'])
  })
}
