import { test, expect } from '@playwright/test'

async function mock(page){
  await page.route('**/api/**',r=>{
    const path=new URL(r.request().url()).pathname
    const storages=[{id:'local',type:'local',name:'本地',enabled:true,config:{}},{id:'mobile',type:'mobile',name:'移动',enabled:true,config:{mode:'native'}}]
    const tasks=['strm','cas','ed2k','cache'].map(kind=>({id:kind,kind,name:`${kind} 任务`,storageId:'local',enabled:true,status:'success',processed:1,casGenerated:1,lastRun:'2026-10-13T10:00:00Z'}))
    return r.fulfill({json:path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'style-revision',storages,tasks,settings:{sessionDays:15},traffic:{},cache:{hits:8,misses:2}}:path==='/api/strm/settings'?{address:'http://10.0.0.1:15151',format:'pickcode_name'}:[]})
  })
}

test('consent requires ten seconds and the bottom independently, resets on reopen and hides scrollbars',async({page},info)=>{
  await page.setViewportSize({width:390,height:500})
  await page.clock.install()
  await page.clock.pauseAt(new Date())
  await mock(page);await page.goto('/settings/account')
  const toggle=page.getByRole('switch',{name:'剪贴板监听',exact:true})
  await toggle.click()
  const dialog=page.getByRole('dialog',{name:'剪贴板监听与隐私说明'}),agree=dialog.getByRole('button',{name:'我已阅读并同意',exact:true}),body=dialog.locator('.clipboard-consent')
  await expect(agree).toBeDisabled();await expect(agree.locator('.consent-countdown')).toHaveText('10')
  await expect(dialog).toHaveCSS('overflow','hidden');await expect(body).toHaveCSS('scrollbar-width','none')
  expect(await body.evaluate(el=>el.scrollHeight>el.clientHeight)).toBe(true)
  await page.clock.runFor(10000)
  await expect(agree.locator('svg')).toBeVisible();await expect(agree).toBeDisabled()
  await body.focus();await page.keyboard.press('End');await expect(agree).toBeEnabled()
  await page.screenshot({path:info.outputPath('consent-small-light.png')})
  await dialog.getByRole('button',{name:'取消',exact:true}).click()
  await toggle.click();await expect(agree.locator('.consent-countdown')).toHaveText('10')
  await body.evaluate(el=>{el.scrollTop=el.scrollHeight;el.dispatchEvent(new Event('scroll'))})
  await page.clock.runFor(9900);await expect(agree).toBeDisabled();await expect(agree.locator('.consent-countdown')).toHaveText('1')
  await page.clock.runFor(100);await expect(agree).toBeEnabled()
  await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.clock.runFor(200)
  await page.screenshot({path:info.outputPath('consent-small-dark.png')})
  await agree.click();await expect(toggle).toBeChecked()
  await page.setViewportSize({width:1440,height:1000})
  await toggle.uncheck();await toggle.click();await expect(dialog).toHaveCSS('width','640px')
  expect(await body.evaluate(el=>el.scrollHeight<=el.clientHeight)).toBe(true)
  await page.clock.runFor(10000);await expect(agree).toBeEnabled()
  await page.screenshot({path:info.outputPath('consent-desktop.png')})
})

test('task cards, rings, navigation and settings widths use shared geometry in both themes',async({page},info)=>{
  await mock(page)
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:900})
    for(const kind of ['strm','cas','ed2k','cache']){
      await page.goto(`/tasks/${kind}`)
      const card=page.locator('.task-row-card'),add=page.locator('.add-task-tile')
      await expect(card).toHaveCount(1)
      expect((await add.boundingBox()).height).toBeCloseTo((await card.boundingBox()).height,0)
      await expect(add).toHaveCSS('flex-direction','row');await expect(add.locator('strong')).toHaveCSS('font-size','13px')
      for(const theme of ['light','dark']){
        await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
        const tab=page.locator('.content-tabs a:not(.active)').first()
        await expect.poll(()=>tab.evaluate(el=>{
          const probe=document.createElement('span');probe.style.color=document.documentElement.dataset.theme==='dark'?'#edf0f8':'color-mix(in srgb,var(--text) 82%,var(--muted))';el.append(probe)
          const expected=getComputedStyle(probe).color,actual=getComputedStyle(el).color;probe.remove();return actual===expected
        })).toBe(true)
        if(kind!=='strm'){
          const hit=page.locator('.cache-hit'),track=page.locator('.cache-track')
          expect(await hit.evaluate(el=>el.style.stroke)).toMatch(/^url\(/)
          expect(await track.evaluate(el=>getComputedStyle(el).stroke)).not.toMatch(/red|green/)
          await expect(page.locator('.cache-chart linearGradient stop')).toHaveCount(2)
          const stops=await page.locator('.cache-chart linearGradient stop').evaluateAll(nodes=>nodes.map(el=>getComputedStyle(el).stopColor))
          expect(stops[0]).not.toBe(stops[1])
          expect(await track.evaluate(el=>getComputedStyle(el).stroke)).toBe(await page.locator('.cas-progress-track,.cache-progress-track').first().evaluate(el=>{
            const probe=document.createElement('span');probe.style.color='color-mix(in srgb,var(--text) 9%,transparent)';el.append(probe);const color=getComputedStyle(probe).color;probe.remove();return color
          }))
        }
      }
      if(kind==='cas')await page.screenshot({path:info.outputPath(`tasks-cas-${width}.png`)})
    }
    await page.goto('/settings/strm');await expect(page.locator('.strm-settings')).toHaveCSS('max-width','380px')
    await page.screenshot({path:info.outputPath(`strm-settings-${width}.png`)})
  }
})

test('entry highlights only its tall icon, tooltip needs a stationary pointer and transitions move smoothly',async({page},info)=>{
  await mock(page);await page.goto('/tasks/cas')
  const entry=page.getByRole('button',{name:'打开存储绑定池',exact:true}),tooltip=page.getByRole('tooltip').filter({hasText:'进入绑定池'})
  await expect(entry.locator('svg')).toHaveCSS('height','64px')
  expect(await entry.locator('svg path').evaluate(el=>el.getBoundingClientRect().height)).toBeGreaterThan(31)
  const box=await entry.boundingBox()
  await page.mouse.move(box.x+20,box.y+30)
  await page.waitForTimeout(400);await expect(tooltip).toHaveCount(0)
  await page.mouse.move(box.x+21,box.y+31)
  await page.waitForTimeout(400);await expect(tooltip).toHaveCount(0)
  await expect(tooltip).toBeVisible()
  await expect(entry).toHaveCSS('background-color','rgba(0, 0, 0, 0)')
  await page.mouse.move(box.x+22,box.y+32);await expect(tooltip).toHaveCount(0)
  await entry.click()
  const root=page.locator('.page-slide').first()
  await expect(root).toHaveCSS('animation-duration','0.55s')
  await root.evaluate(async el=>{await Promise.all(el.getAnimations().map(a=>a.finished))})
  await page.screenshot({path:info.outputPath('pool-entered.png')})
  await page.getByRole('button',{name:'返回 CAS 任务',exact:true}).click()
  await expect(root).toHaveCSS('animation-duration','0.32s')
  await expect(page).toHaveURL(/\/tasks\/cas$/)
})
