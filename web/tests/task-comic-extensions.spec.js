import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const comics = 'cbz;cbr;zip;rar;epub;pdf'

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) test(`task media presets omit comics but custom extensions save and reopen ${width} ${theme}`, async ({ page }, info) => {
  let tasks = []
  const saved = []
  await page.setViewportSize({ width, height: 1000 })
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'comic-task', storages: [{ id: 'local', name: '本地', type: 'local', enabled: true, config: {} }], tasks, settings: {}, traffic: {}, cache: {} }
    else if (path === '/api/files' || path === '/api/generation/jobs') json = []
    else if (path.startsWith('/api/tasks') && ['POST', 'PUT'].includes(request.method())) {
      json = { ...request.postDataJSON(), id: request.method() === 'POST' ? `comic-${saved.length}` : path.split('/').at(-1) }
      saved.push(json)
      tasks = [...tasks.filter(task => task.id !== json.id), json]
    }
    return route.fulfill({ json })
  })
  for (const kind of ['strm', 'cas', 'ed2k']) {
    await page.goto(`/tasks/${kind}`)
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await page.getByRole('button', { name: `添加 ${kind.toUpperCase()} 任务卡片`, exact: true }).click()
    const modal = page.getByRole('dialog', { name: `添加 ${kind.toUpperCase()} 任务`, exact: true })
    const media = modal.getByRole('textbox', { name: '媒体扩展名', exact: true })
    const metadata = modal.getByRole('textbox', { name: '元数据扩展名', exact: true })
    await expect(modal.getByRole('button', { name: '漫画扩展名', exact: true })).toHaveCount(0)
    const presets = modal.locator('.extension-field').first().locator('.extension-preset')
    await expect(presets).toHaveCount(2)
    const videoButton = modal.getByRole('button', { name: '视频扩展名', exact: true })
    const audioButton = modal.getByRole('button', { name: '音频扩展名', exact: true })
    await expect(videoButton).toBeVisible()
    await expect(audioButton).toBeVisible()
    await media.fill('my-custom')
    await metadata.fill('nfo')
    await videoButton.click()
    await expect(videoButton).toHaveAttribute('aria-pressed', 'true')
    await expect(media).toHaveValue(/^my-custom;mp4;mkv;/)
    await videoButton.click()
    await expect(media).toHaveValue('my-custom')
    await audioButton.click()
    const audio = await media.inputValue()
    await expect(audioButton).toHaveAttribute('aria-pressed', 'true')
    await audioButton.click()
    await expect(media).toHaveValue('my-custom')
    await media.fill(`${audio};${comics}`)
    await expect(modal.getByRole('button', { name: '漫画扩展名', exact: true })).toHaveCount(0)
    await expect(metadata).toHaveValue('nfo')
    await page.mouse.move(1, 1)
    expect(await modal.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    const iconBox = await videoButton.boundingBox(), audioBox = await audioButton.boundingBox()
    expect(iconBox.width).toBe(audioBox.width)
    expect(iconBox.height).toBe(audioBox.height)
    expect(iconBox.x).toBeLessThan(audioBox.x)
    expect(Math.abs(iconBox.y - audioBox.y)).toBeLessThan(1)
    if (kind === 'strm') await captureScreenshot(page, { path: info.outputPath(`task-comic-${width}-${theme}.png`) })
    await modal.getByRole('textbox', { name: '任务名称' }).fill(`${kind}漫画`)
    await modal.getByRole('button', { name: '选择目录', exact: true }).click()
    await page.locator('.source-accounts button').filter({ hasText: '本地' }).click()
    await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
    const count = saved.length
    await modal.getByRole('button', { name: '保存任务', exact: true }).click()
    await expect(modal).toHaveCount(0)
    expect(saved).toHaveLength(count + 1)
    expect(saved.at(-1)).toMatchObject({ kind, mediaExtensions: `${audio};${comics}`, metadataExtensions: 'nfo' })
    await page.getByRole('button', { name: `任务操作 ${kind}漫画`, exact: true }).click()
    await page.getByRole('button', { name: '编辑任务', exact: true }).click()
    const edit = page.getByRole('dialog', { name: `编辑 ${kind.toUpperCase()} 任务`, exact: true })
    await expect(edit.getByRole('button', { name: '漫画扩展名', exact: true })).toHaveCount(0)
    await expect(edit.getByRole('textbox', { name: '媒体扩展名', exact: true })).toHaveValue(`${audio};${comics}`)
    await edit.getByRole('textbox', { name: '媒体扩展名', exact: true }).fill(audio)
    await edit.getByRole('button', { name: '保存任务', exact: true }).click()
    await expect(edit).toHaveCount(0)
    expect(saved.at(-1).mediaExtensions).toBe(audio)
  }
})
