import { test, expect } from '@playwright/test'
import { rememberVisit, validVisits } from '../src/visit-history.js'

const visit = (storage, id, parents) => ({ storage,id,history:parents.map(([id,name])=>({id,name})) })
test('directory history preserves opaque ids, deepest paths and sibling branches',()=>{
  const c=visit('pan','cid-c',[['/','A'],['cid-a','B'],['cid-b','C']])
  const b=visit('pan','cid-b',[['/','A'],['cid-a','B']])
  const d=visit('pan','cid-d',[['/','A'],['cid-a','B'],['cid-b','D']])
  let entries=rememberVisit([],b)
  entries=rememberVisit(entries,c);expect(entries).toEqual([c])
  entries=rememberVisit(entries,b);expect(entries).toEqual([c])
  entries=rememberVisit(entries,d);expect(entries).toEqual([d,c])
  entries=rememberVisit(entries,b);expect(entries).toEqual([d,c])
  const before = [...entries]
  entries=rememberVisit(entries,visit('other','cid-c',[]));expect(entries).toEqual(before)
  entries=rememberVisit(entries,visit('pan','cid-a',[['/','A']]));expect(entries).toEqual(before)
  for(let i=0;i<8;i++)entries=rememberVisit(entries,visit(`pool-${i}`,'b',[['/','A'],['a','B']]))
  expect(entries).toHaveLength(5)
  expect(validVisits([null,{storage:'bad',id:'/',history:[null]},visit('pan','/',[]),visit('pan','cid-a',[['/','A']]),c])).toEqual([c])
})

function sharedHistoryMock() {
  let entries=[],fail=false
  return {
    set fail(value){fail=value},
    get entries(){return entries},
    async install(context){await context.route('**/api/**',r=>{
      const u=new URL(r.request().url())
      let data={}
      if(u.pathname==='/api/auth/status')data={initialized:true,authenticated:true}
      if(u.pathname==='/api/state')data={username:'history-owner',fileVisits:entries,storages:[{id:'pool',type:'115',name:'115',enabled:true}],tasks:[],settings:{},cache:{},traffic:{}}
      if(u.pathname==='/api/files')data=u.searchParams.get('path')==='/'?[{id:'a',name:'A',isDir:true}]:u.searchParams.get('path')==='a'?[{id:'b',name:'B',isDir:true}]:[{id:'file',name:'命中目录.txt'}]
      if(u.pathname==='/api/files/visits') {
        if(fail)return r.fulfill({status:500,json:{error:'历史保存失败'}})
        const {action,items}=r.request().postDataJSON()
        if(action==='import'){
          let merged=[]
          for(const item of [...items].reverse())merged=rememberVisit(merged,item)
          for(const item of [...entries].reverse())merged=rememberVisit(merged,item)
          entries=merged
        }else entries=rememberVisit(entries,items[0])
        data=entries
      }
      return r.fulfill({json:data})
    })}
  }
}

test('history is shared across independent browsers and legacy entries migrate only after success',async({browser,baseURL})=>{
  const contexts=[await browser.newContext({baseURL}),await browser.newContext({baseURL})]
  const mock=sharedHistoryMock()
  for(const context of contexts)await mock.install(context)
  try {
    const first=await contexts[0].newPage(),second=await contexts[1].newPage()
    await first.addInitScript(()=>{if(!sessionStorage.getItem('seeded')){sessionStorage.setItem('seeded','1');localStorage.setItem('aether-file-history:history-owner',JSON.stringify([{storage:'pool',id:'b',history:[{id:'/',name:'A'},{id:'a',name:'B'}]}]))}})
    mock.fail=true
    await first.goto('/files')
    await expect(first.getByRole('alert')).toContainText('历史保存失败')
    expect(await first.evaluate(()=>JSON.parse(localStorage.getItem('aether-file-history:history-owner')).length)).toBe(1)
    mock.fail=false
    await first.reload()
    const history=first.getByRole('button',{name:'历史访问',exact:true})
    await expect.poll(()=>mock.entries.length).toBe(1)
    await expect.poll(()=>first.evaluate(()=>localStorage.getItem('aether-file-history:history-owner'))).toBeNull()
    await history.click();await first.getByRole('option',{name:'115 / B',exact:true}).click()
    await expect(first.getByRole('button',{name:'命中目录.txt',exact:true})).toBeVisible()
    await expect(first.locator('.path-bar')).toContainText('B')
    await second.goto('/files')
    await second.getByRole('button',{name:'历史访问',exact:true}).click()
    await expect(second.getByRole('option',{name:'115 / B',exact:true})).toBeVisible()
    expect(await second.evaluate(()=>localStorage.getItem('aether-file-history:history-owner'))).toBeNull()
    await second.reload();await second.getByRole('button',{name:'历史访问',exact:true}).click()
    await expect(second.getByRole('option',{name:'115 / B',exact:true})).toBeVisible()
  }finally{for(const context of contexts)await context.close()}
})

test('empty history remains clickable and hover opens immediately',async({page},info)=>{
  const mock=sharedHistoryMock();await mock.install(page)
  await page.goto('/files')
  const history=page.getByRole('button',{name:'历史访问',exact:true}),menu=page.locator('.file-visit-history [role=listbox]')
  await expect(history).toBeEnabled()
  await history.click();await expect(menu).toContainText('暂无访问记录')
  await page.keyboard.press('Escape');await expect(menu).toHaveCount(0)
  await page.mouse.move(500,40)
  await page.clock.install()
  await page.clock.pauseAt(new Date())
  await history.hover()
  await page.clock.runFor(1);await expect(menu).toBeVisible()
  await page.clock.resume()
  await page.screenshot({path:info.outputPath('empty-history.png')})
  await page.mouse.move(500,40);await expect(menu).toHaveCount(0)
  await page.locator('.file-view').getByRole('button',{name:'A',exact:true}).dblclick()
  await page.locator('.file-view').getByRole('button',{name:'B',exact:true}).dblclick()
  await expect.poll(()=>mock.entries.length).toBe(1)
  await history.click();await expect(menu.getByRole('option',{name:'115 / B',exact:true})).toBeVisible()
})
