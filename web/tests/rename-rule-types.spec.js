import { test, expect } from '@playwright/test'

async function workbench(page, storageType = 'local') {
  let previews = [], sets = [], executions = []
  let file = { id: storageType === '115' ? '123' : '/原名.mkv', name: '原名.mkv', isDir: false }
  const unexpected = []
  await page.route('**/api/**', async route => {
    const req = route.request(), url = new URL(req.url())
    let json
    if (url.pathname === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (url.pathname === '/api/state') json = { username: 'rename-types', storages: [{ id: 'pool', name: '影视资料', type: storageType, enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {}, logs: [] }
    else if (url.pathname === '/api/files') json = [file]
    else if (url.pathname === '/api/files/visits') json = []
    else if (url.pathname === '/api/files/rename-rules') {
      if (req.method() === 'POST') sets.push({ ...req.postDataJSON(), id: 'set' })
      json = sets
    } else if (url.pathname === '/api/files/rename-preview') {
      const input = req.postDataJSON()
      previews.push(input)
      json = [{ ...file, newName: input.rules.at(-1)?.kind === 'replace' ? file.name.replace('.mkv', '.mp4') : '预览结果.mkv' }]
    } else if (url.pathname === '/api/files/rename') {
      const input = req.postDataJSON()
      executions.push(input)
      file = { ...file, name: input.expected[0].newName, id: storageType === '115' ? '456' : '/预览结果.mkv' }
      json = { processed: 1, renamed: [{ id: input.ids[0], newId: file.id, name: file.name, pickCode: 'pick456' }] }
    } else if (url.pathname === '/api/strm-replace') json = []
    else if (['/api/traffic', '/api/notifications', '/api/transfers'].includes(url.pathname)) json = {}
    else { unexpected.push(`${req.method()} ${url.pathname}`); return route.fulfill({ status: 501, json: { error: 'Unexpected test API' } }) }
    return route.fulfill({ json })
  })
  await page.goto('/files')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '命名工作台', exact: true }).click()
  await expect(page.locator('.rename-preview-row')).toHaveCount(1)
  return { previews, sets, executions, unexpected }
}

async function add(page, label) {
  await page.getByRole('button', { name: '添加规则', exact: true }).click()
  await page.getByRole('button', { name: label, exact: true }).click()
}
async function select(page, label, option) {
  await page.getByRole('button', { name: label, exact: true }).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

test('add rule discloses six choices before creating a rule and supports keyboard', async ({ page }) => {
  await workbench(page)
  const addButton = page.getByRole('button', { name: '添加规则', exact: true })
  await addButton.click()
  await expect(addButton).toHaveAttribute('aria-expanded', 'true')
  await expect(page.locator('.rename-rule')).toHaveCount(1)
  const choices = page.getByRole('group', { name: '可选规则' })
  await expect(choices.getByRole('button')).toHaveText(['查找替换', '修改名称/添加序号', '添加字符', '删除字符', '移动字符', '字符转换'])
  await choices.getByRole('button', { name: '添加字符', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('.rename-rule')).toHaveCount(2)
  await expect(addButton).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByLabel('添加到第几位', { exact: true })).toHaveValue('1')
  await expect(page.getByLabel('添加的字符', { exact: true })).toHaveAttribute('maxlength', '255')
})

test('all rule controls serialize, save and restore without losing independent options', async ({ page }, info) => {
  const state = await workbench(page)
  await page.getByLabel('查找内容', { exact: true }).fill('.mkv')
  await page.getByLabel('替换为', { exact: true }).fill('.mp4')
  await add(page, '修改名称/添加序号')
  await page.getByLabel('名称统一修改为', { exact: true }).fill('剧集')
  await page.getByLabel('开始序号', { exact: true }).fill('8')
  await page.getByLabel('序号最少位数', { exact: true }).fill('3')
  await select(page, '序号位置', '名称之后')
  await select(page, '分隔符', '连字符（-）')
  await page.screenshot({ path: info.outputPath('rename-number.png') })
  await add(page, '添加字符')
  await page.getByLabel('添加到第几位', { exact: true }).fill('2')
  await page.getByLabel('添加的字符', { exact: true }).fill('中')
  await page.screenshot({ path: info.outputPath('rename-insert.png') })
  await add(page, '删除字符')
  await page.getByLabel('删除英文字母', { exact: true }).check()
  await page.getByLabel('删除序号编号', { exact: true }).check()
  await page.getByLabel('按位置删除', { exact: true }).check()
  await page.getByLabel('从位置', { exact: true }).fill('2')
  await page.getByLabel('到位置', { exact: true }).fill('4')
  await page.getByRole('group', { name: '结束位置方向' }).getByLabel('从右到左').check()
  await page.getByLabel('自定义删除', { exact: true }).fill('旧')
  await page.getByLabel('仅匹配第一个', { exact: true }).check()
  await add(page, '移动字符')
  await page.getByLabel('将第几位', { exact: true }).fill('2')
  await page.getByLabel('移动到第几位', { exact: true }).fill('5')
  await page.screenshot({ path: info.outputPath('rename-move.png') })
  await add(page, '字符转换')
  for (const label of ['大小写', '繁转简', '全半角']) {
    await expect(page.getByRole('button', { name: label, exact: true })).toHaveText('不转换')
  }
  await select(page, '大小写', '每个单词首字母大写')
  await select(page, '繁转简', '简体转繁体(香港)')
  await select(page, '全半角', '将半角转换为中文全角')
  await expect.poll(() => state.previews.at(-1)?.rules).toEqual([
    { kind: 'replace', find: '.mkv', replace: '.mp4', findType: 'literal', caseSensitive: false, firstOnly: false },
    { kind: 'number', name: '剧集', start: 8, digits: 3, numberPosition: 'after', separator: '-' },
    { kind: 'insert', position: 2, text: '中' },
    { kind: 'delete', deleteTypes: ['english', 'serial'], deleteRange: true, from: 2, to: 4, fromEnd: false, toEnd: true, text: '旧', firstOnly: true },
    { kind: 'move', from: 2, to: 5 },
    { kind: 'convert', caseMode: 'title', chineseMode: 's2hk', widthMode: 'wide_cn' }
  ])
  await page.getByRole('button', { name: '保存为规则集' }).click()
  await page.getByLabel('规则集名称').fill('完整规则')
  await page.getByRole('dialog', { name: '保存规则集' }).getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => state.sets.length).toBe(1)
  await page.getByRole('button', { name: '删除规则', exact: true }).last().click()
  await select(page, '常用规则集', '完整规则')
  await expect(page.locator('.rename-rule')).toHaveCount(6)
  await page.getByRole('button', { name: '规则 6', exact: true }).click()
  await expect(page.getByRole('button', { name: '全半角', exact: true })).toHaveText('将半角转换为中文全角')
  await page.screenshot({ path: info.outputPath('rename-convert.png') })
  expect(state.unexpected).toEqual([])
})

test('115 individual extension rename keeps replacement identity for subsequent rule previews', async ({ page }) => {
  const state = await workbench(page, '115')
  await page.locator('.rename-preview-row').hover()
  await page.getByRole('button', { name: '修改', exact: true }).click()
  await page.getByLabel('新名称', { exact: true }).fill('原名.mp4')
  await page.getByRole('button', { name: '确认修改', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '重命名', exact: true })).toHaveCount(0)
  await expect(page.locator('.rename-preview-row')).toContainText('原名.mp4')
  await page.getByLabel('查找内容', { exact: true }).fill('原名')
  await page.getByLabel('替换为', { exact: true }).fill('新名')
  await expect.poll(() => state.previews.at(-1)?.ids).toEqual(['456'])
  expect(state.executions[0].ids).toEqual(['123'])
  expect(state.unexpected).toEqual([])
})

for (const [width, height, theme] of [[1440, 1000, 'light'], [390, 844, 'dark'], [375, 812, 'light'], [844, 390, 'dark']]) {
  test(`six rule layout at ${width}x${height} ${theme}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height })
    await page.addInitScript(theme => localStorage.setItem('aether-theme', theme), theme)
    await workbench(page)
    await select(page, '规则类型', '删除字符')
    await expect(page.getByLabel('删除绘表符号', { exact: true })).toBeAttached()
    for (const control of [page.getByLabel('删除英文字母', { exact: true }), page.getByLabel('自定义删除', { exact: true })]) {
      await control.scrollIntoViewIfNeeded()
      await expect(control).toBeVisible()
      const box = await control.boundingBox()
      expect(box.x).toBeGreaterThanOrEqual(0)
      expect(box.x + box.width).toBeLessThanOrEqual(width)
    }
    await page.screenshot({ path: info.outputPath('rename-delete.png') })
    await select(page, '规则类型', '字符转换')
    for (const [label, option] of [['大小写', '仅第一个首字母大写'], ['繁转简', '简体转繁体(台湾)'], ['全半角', '将全角转换为英文半角']]) {
      await select(page, label, option)
      await expect(page.getByRole('button', { name: label, exact: true })).toContainText(option)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath('rename-convert-layout.png') })
  })
}
