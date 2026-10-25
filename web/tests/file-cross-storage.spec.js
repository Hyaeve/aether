import {test,expect} from '@playwright/test'

for(const [action,label] of [['copy','复制到'],['move','移动到']]){
 test(`file ${action} can select a different storage type`,async({page})=>{
  const calls=[]
  await page.route('**/api/**',route=>{
   const req=route.request(),u=new URL(req.url())
   let json={}
   if(u.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
   if(u.pathname==='/api/state')json={username:'cross-storage',storages:[{id:'local',type:'local',name:'本机',enabled:true,config:{}},{id:'dav',type:'webdav',name:'远程',enabled:true,config:{}},{id:'quark',type:'quark',name:'夸克',enabled:true,config:{}},{id:'off',type:'mobile',name:'停用',enabled:false,config:{}}],tasks:[],settings:{},cache:{},traffic:{}}
   if(u.pathname==='/api/files')json=u.searchParams.get('storage')==='local'?[{id:'/one.txt',name:'one.txt',size:12,isDir:false}]:[]
   if(u.pathname==='/api/files/action'){calls.push(req.postDataJSON());json={processed:1}}
   if(u.pathname==='/api/files/visits')json=[]
   return route.fulfill({json})
  })
  await page.goto('/files')
  await page.getByRole('button',{name:'one.txt',exact:true}).click({button:'right'})
  await page.locator('.context-menu').getByRole('button',{name:label,exact:true}).click()
  const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true})
  await expect(picker.locator('.source-accounts button')).toHaveCount(3)
  await picker.locator('.source-accounts button').filter({hasText:'远程'}).click()
  await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
  await expect.poll(()=>calls.length).toBe(1)
  expect(calls[0]).toMatchObject({action,storageId:'local',targetStorage:'dav',target:'/',ids:['/one.txt']})
 })
}
