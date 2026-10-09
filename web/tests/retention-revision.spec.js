import { test, expect } from '@playwright/test'
import { recentNotices, noticeResult } from '../src/task-notices'

test('notifications prefer one day or fall back to five with result counts', () => {
  const now=Date.now(), row=(days,id)=>({id,lastRun:new Date(now-days*86400000).toISOString()})
  expect(recentNotices([row(.5,'new'),row(2,'old')],now).map(i=>i.id)).toEqual(['new'])
  expect(recentNotices(Array.from({length:8},(_,i)=>row(i+4,i)),now)).toHaveLength(5)
  expect(noticeResult({status:'success',processed:12})).toContain('12')
})

test('double-click non-media file opens menu without playback-copy and shift retains selection', async ({page}) => {
  await page.route('**/api/auth/status', r=>r.fulfill({json:{initialized:true,authenticated:true}}))
  await page.route('**/api/state', r=>r.fulfill({json:{storages:[{id:'s',type:'local',name:'Files',enabled:true,config:{root:'/'}}],tasks:[],settings:{},cache:{},traffic:{}}}))
  await page.route('**/api/files?**', r=>r.fulfill({json:[{id:'/a',name:'a.epub',url:'https://example.test/a',isDir:false},{id:'/b',name:'b.epub',isDir:false}]}))
  await page.goto('/files')
  const rows=page.locator('.file-row')
  await rows.first().dblclick()
  await expect(page.getByRole('button',{name:'下载',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'复制播放链接',exact:true})).toHaveCount(0)
  await page.keyboard.press('Escape')
  await rows.last().click({modifiers:['Shift']})
  await expect(page.locator('.file-row.selected')).toHaveCount(2)
  expect(await rows.last().evaluate(el=>getComputedStyle(el).outlineStyle)).toBe('none')
})
