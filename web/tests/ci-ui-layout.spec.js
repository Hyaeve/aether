import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const layouts = [[1440, 1000, 'light'], [390, 844, 'dark'], [375, 812, 'light'], [844, 390, 'dark']]
async function fixture(page, theme) {
  const unexpected = [], writes = []
  let link = { id: 'book', name: '有声书', type: 'audiobookshelf', mode: 'always', port: 18080, enabled: true }
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = {
      username: 'ci-layout', storages: [{ id: 'local', name: '本地', type: 'local', enabled: true, config: {} }],
      links: [link], tasks: [], settings: {}, traffic: {}, cache: {}
    }
    else if (path === '/api/links/book' && request.method() === 'PUT') {
      link = request.postDataJSON(); writes.push(link); json = link
    }
    else if (path === '/api/links') json = [link]
    else if (path === '/api/generation/jobs') json = []
    else if (path === '/api/115-rapid') json = { manual: {}, watches: [] }
    else if (path === '/api/115-rapid/records') json = { items: [], running: false }
    else if (['/api/traffic', '/api/strm-replace'].includes(path)) json = {}
    else {
      unexpected.push(`${request.method()} ${path}`)
      return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
    }
    return route.fulfill({ json })
  })
  return { unexpected, writes }
}

async function requiredTitle(control, text) {
  const geometry = await control.evaluate(el => {
    const label = el.labels[0], star = label.querySelector('.required'), title = star.parentElement
    const range = document.createRange()
    range.selectNodeContents(star)
    const marker = range.getBoundingClientRect(), bounds = title.getBoundingClientRect()
    return {
      text: title.textContent, grouped: title !== label,
      sameLine: marker.y >= bounds.y && marker.bottom <= bounds.bottom,
      height: bounds.height, han: title.querySelectorAll('.han-gap').length
    }
  })
  expect(geometry.text).toBe(`${text} *`)
  expect(geometry.grouped).toBe(true)
  expect(geometry.sameLine).toBe(true)
  expect(geometry.height).toBeLessThan(25)
  expect(geometry.han).toBe(text.length - 1)
}

async function aligned(left, right) {
  const bounds = locator => locator.evaluate(el => {
    const rect = (el.closest('.number-control') || el).getBoundingClientRect()
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height }
  })
  await expect.poll(async () => {
    const a = await bounds(left), b = await bounds(right)
    return { sameRow: a.y === b.y, height: a.height, rightHeight: b.height, separated: a.x + a.width < b.x }
  }).toEqual({ sameRow: true, height: 40, rightHeight: 40, separated: true })
}

for (const [width, height, theme] of layouts) test.describe(`CI layout ${width} ${theme}`, () => {
  test.use({ viewport: { width, height } })

  test('required storage titles stay inline and paired controls align', async ({ page }, info) => {
    const f = await fixture(page, theme)
    await page.goto('/storage')
    for (const kind of ['115', 'mobile', 'tianyi', 'openlist', 'webdav']) {
      await page.getByRole('button', { name: '添加存储池', exact: true }).click()
      await page.locator(`.driver-option[data-provider="${kind}"]`).click()
      const dialog = page.getByRole('dialog'), name = dialog.getByRole('textbox', { name: '存储池名称', exact: false })
      await requiredTitle(name, '存储池名称')
      await expect(name).toHaveAttribute('required', '')
      if (['115', 'mobile', 'tianyi'].includes(kind)) {
        const other = kind === 'mobile' ? dialog.getByRole('textbox', { name: '账号域 ID', exact: true })
          : dialog.getByRole('button', { name: kind === '115' ? '设备类型' : '天翼接入模式', exact: true })
        await aligned(name, other)
      } else {
        const address = dialog.getByRole('textbox', { name: '服务地址', exact: false })
        await requiredTitle(address, '服务地址')
        await expect(address).toHaveAttribute('required', '')
        if (kind === 'openlist') await aligned(address, dialog.getByRole('button', { name: '接入模式', exact: true }))
      }
      expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      if (kind === 'mobile') await captureScreenshot(page, { path: info.outputPath('storage-aligned.png') })
      await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    }
    expect(f.unexpected).toEqual([])
  })

  test('required task titles align across all task kinds without changing text spacing', async ({ page }, info) => {
    const f = await fixture(page, theme)
    for (const [kind, title, mode] of [['strm', 'STRM', '生成方式'], ['cas', 'CAS', 'CAS 信息'], ['ed2k', 'ED2K', '生成方式'], ['cache', '缓存', '执行间隔']]) {
      await page.goto(`/tasks/${kind}`)
      await page.getByRole('button', { name: `添加 ${title} 任务卡片`, exact: true }).click()
      const dialog = page.getByRole('dialog'), name = dialog.getByRole('textbox', { name: '任务名称', exact: false })
      await requiredTitle(name, '任务名称')
      const other = kind === 'cache' ? dialog.getByRole('spinbutton', { name: mode, exact: true }) : dialog.getByRole('button', { name: mode, exact: true })
      await aligned(name, other)
      const source = dialog.getByRole('button', { name: '选择目录', exact: true })
      await requiredTitle(source, '源目录')
      await aligned(source, dialog.locator(kind === 'cache' ? '#task-depth' : '#task-target'))
      expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      if (kind === 'cas') {
        await name.scrollIntoViewIfNeeded()
        await captureScreenshot(page, { path: info.outputPath('task-aligned.png') })
      }
      await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    }
    expect(f.unexpected).toEqual([])
  })

  test('every link mode keeps its complete label and both icons inside the trigger', async ({ page }, info) => {
    const f = await fixture(page, theme)
    await page.goto('/links/manage')
    const trigger = page.getByRole('button', { name: '有声书跳转模式', exact: true })
    for (const [index, label] of ['始终跳转', '公网跳转', '内网跳转', '始终中继'].entries()) {
      await trigger.click()
      await page.getByRole('option', { name: label, exact: true }).click()
      await expect.poll(() => f.writes.length).toBe(index + 1)
      await expect(trigger).toBeEnabled()
      await expect(trigger).toHaveText(label)
      await expect(trigger.locator('svg')).toHaveCount(2)
      const geometry = await trigger.evaluate(el => {
        const bounds = el.getBoundingClientRect(), content = el.querySelector('.select-label'), text = content.querySelector('.han-text')
        const icon = content.querySelector('svg').getBoundingClientRect(), words = text.getBoundingClientRect(), arrow = el.lastElementChild.getBoundingClientRect()
        return {
          fits: content.scrollWidth <= content.clientWidth,
          inside: icon.x >= bounds.x && words.right <= arrow.x && arrow.right <= bounds.right,
          separated: icon.right < words.x, oneLine: words.height < 25
        }
      })
      expect(geometry).toEqual({ fits: true, inside: true, separated: true, oneLine: true })
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const notices = page.getByRole('button', { name: '关闭通知', exact: true })
    await notices.evaluateAll(buttons => buttons.forEach(button => button.click()))
    await expect(notices).toHaveCount(0)
    await captureScreenshot(page, { path: info.outputPath('link-mode-complete.png') })
    expect(f.unexpected).toEqual([])
  })

  test('monitor filter layers brighten inward and preserve pure input surfaces', async ({ page }, info) => {
    const f = await fixture(page, theme)
    await page.goto('/tools/115-rapid')
    await page.getByRole('button', { name: '添加监控', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '添加监控', exact: true })
    await dialog.getByRole('button', { name: '添加规则', exact: true }).click()
    await page.getByRole('dialog', { name: '选择规则类型', exact: true }).getByRole('button', { name: '扩展名', exact: true }).click()
    await dialog.getByRole('textbox', { name: '匹配内容 1', exact: true }).fill('mkv')
    await dialog.getByRole('textbox', { name: '匹配内容 1', exact: true }).press('Enter')
    const colors = await dialog.evaluate(el => [el, el.querySelector('.backup-filter-frame'), el.querySelector('.backup-filter'), el.querySelector('.backup-token-input input')].map(node => getComputedStyle(node).backgroundColor))
    expect(colors).toEqual(theme === 'light'
      ? ['rgb(237, 240, 247)', 'rgb(250, 251, 254)', 'rgb(253, 253, 255)', 'rgb(255, 255, 255)']
      : ['rgb(19, 20, 25)', 'rgb(34, 37, 45)', 'rgb(38, 40, 49)', 'rgb(41, 44, 52)'])
    const brightness = colors.map(value => value.match(/\d+/g).map(Number).reduce((sum, n) => sum + n, 0))
    expect(brightness.every((value, i) => i === 0 || value > brightness[i - 1])).toBe(true)
    await expect(dialog.locator('.backup-token')).toHaveText('mkv')
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await dialog.getByRole('textbox', { name: '匹配内容 1', exact: true }).scrollIntoViewIfNeeded()
    await captureScreenshot(page, { path: info.outputPath('filter-layers.png') })
    expect(f.unexpected).toEqual([])
  })
})
