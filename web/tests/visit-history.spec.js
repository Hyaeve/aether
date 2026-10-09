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
  entries=rememberVisit(entries,visit('other','cid-c',[]));expect(entries).toHaveLength(3)
  for(let i=0;i<8;i++)entries=rememberVisit(entries,visit(`pool-${i}`,'/',[]))
  expect(entries).toHaveLength(5)
  expect(validVisits([null,{storage:'bad',id:'/',history:[null]},c])).toEqual([c])
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
  await page.evaluate(()=>localStorage.setItem('aether-file-history:history-test',JSON.stringify([{storage:'two',id:'cid-c',history:[{id:'/',name:'A'},{id:'cid-a',name:'B'},{id:'cid-b',name:'C'}]}])))
  await page.reload()
  await page.getByRole('button',{name:'历史访问',exact:true}).click()
  await page.getByRole('option',{name:'115 / C',exact:true}).click()
  await expect(page.getByRole('button',{name:'选择存储池',exact:true})).toContainText('115')
  await expect(page.getByRole('button',{name:'命中目录.txt',exact:true})).toBeVisible()
  await expect(page.locator('.path-bar')).toContainText('C')
  await page.reload()
  await page.getByRole('button',{name:'历史访问',exact:true}).click()
  await expect(page.getByRole('option',{name:'115 / C',exact:true})).toBeVisible()
})
