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

test('history menu jumps to the exact stored cloud directory and persists per browser',async({page})=>{
  await page.route('**/api/**',r=>{
    const u=new URL(r.request().url())
    let data={}
    if(u.pathname==='/api/auth/status')data={initialized:true,authenticated:true}
    if(u.pathname==='/api/state')data={username:'history-test',storages:[{id:'one',type:'quark',name:'夸克',enabled:true},{id:'two',type:'115',name:'115',enabled:true}],tasks:[],settings:{},cache:{},traffic:{}}
    if(u.pathname==='/api/files')data=u.searchParams.get('path')==='cid-c'?[{id:'file',name:'命中目录.txt'}]:[{id:'cid-c',name:'C',isDir:true}]
    return r.fulfill({json:data})
  })
  await page.goto('/files')
  await expect(page.getByRole('button',{name:'C',exact:true})).toBeVisible()
  await page.evaluate(()=>localStorage.setItem('aether-file-history:history-test',JSON.stringify([{storage:'two',id:'cid-c',history:[{id:'/',name:'A'},{id:'cid-a',name:'B'},{id:'cid-b',name:'C'}]}])))
  await page.reload()
  await expect(page.getByRole('button',{name:'选择存储池',exact:true})).toBeEnabled()
  await page.getByRole('button',{name:'历史访问',exact:true}).click()
  await page.getByRole('option',{name:'115 / C',exact:true}).click()
  await expect(page.getByRole('button',{name:'选择存储池',exact:true})).toContainText('115')
  await expect(page.getByRole('button',{name:'命中目录.txt',exact:true})).toBeVisible()
  await expect(page.locator('.path-bar')).toContainText('C')
  await page.reload()
  await page.getByRole('button',{name:'历史访问',exact:true}).click()
  await expect(page.getByRole('option',{name:'115 / C',exact:true})).toBeVisible()
})

test('a delayed directory response merges history written after the page mounted',async({page})=>{
  let started, release
  const pending = new Promise(resolve=>{started=resolve})
  const gate = new Promise(resolve=>{release=resolve})
  await page.route('**/api/**',async r=>{
    const u=new URL(r.request().url())
    let data={}
    if(u.pathname==='/api/auth/status')data={initialized:true,authenticated:true}
    if(u.pathname==='/api/state')data={username:'history-delayed',storages:[{id:'one',type:'quark',name:'夸克',enabled:true},{id:'two',type:'115',name:'115',enabled:true}],tasks:[],settings:{},cache:{},traffic:{}}
    if(u.pathname==='/api/files'){started();await gate;data=[]}
    return r.fulfill({json:data})
  })
  await page.goto('/files');await pending
  await page.evaluate(()=>localStorage.setItem('aether-file-history:history-delayed',JSON.stringify([{storage:'two',id:'cid-c',history:[{id:'/',name:'A'},{id:'cid-a',name:'C'}]}])))
  release()
  await page.getByRole('button',{name:'历史访问',exact:true}).click()
  await expect(page.getByRole('option',{name:'115 / C',exact:true})).toBeVisible()
  await expect.poll(()=>page.evaluate(()=>JSON.parse(localStorage.getItem('aether-file-history:history-delayed')).some(v=>v.storage==='two'&&v.id==='cid-c'))).toBe(true)
})
