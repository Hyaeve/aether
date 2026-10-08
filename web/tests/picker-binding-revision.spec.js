import {test,expect} from '@playwright/test'

test('simulcast binding uses logo toggle and breadcrumb picker creates in a modal',async({page},info)=>{
  let created
  await page.route('**/api/**',r=>{
    const u=new URL(r.request().url())
    let data={}
    if(u.pathname==='/api/auth/status')data={initialized:true,authenticated:true}
    if(u.pathname==='/api/state')data={storages:[{id:'pan',type:'115',enabled:true,name:'115媒体',config:{}}],tasks:[],settings:{},cache:{},logs:[]}
    if(u.pathname==='/api/115-simulcast')data={pan:{enabled:true,directory:'123',directoryLabel:'复制目录'}}
    if(u.pathname==='/api/files')data=[{id:'456',name:'子目录',isDir:true}]
    if(u.pathname==='/api/files/action'){created=r.request().postDataJSON();data={ok:true}}
    return r.fulfill({json:data})
  })
  await page.goto('/tools')
  await page.getByRole('button',{name:'115 同播复制',exact:true}).click()
  await expect(page.getByText('存储设置',{exact:true})).toHaveCount(0)
  await expect(page.getByRole('button',{name:'存储绑定',exact:true})).toBeVisible()
  await page.getByRole('button',{name:'编辑 115媒体',exact:true}).click()
  await expect(page.getByRole('switch')).toHaveCount(0)
  await page.getByRole('button',{name:'选择复制目录',exact:true}).click()
  await expect(page.getByRole('button',{name:'上级目录',exact:true})).toHaveCount(0)
  await page.getByRole('navigation',{name:'存储目录路径'}).getByRole('button',{name:'根目录',exact:true}).click()
  await page.getByRole('button',{name:'新建文件夹',exact:true}).click()
  const modal=page.getByRole('dialog').filter({has:page.getByRole('heading',{name:'新建文件夹',exact:true})})
  await modal.getByLabel('文件夹名称').fill('新目录')
  await modal.getByRole('button',{name:'确认创建',exact:true}).click()
  await expect(modal).toHaveCount(0)
  expect(created).toMatchObject({storageId:'pan',source:'/',action:'mkdir',name:'新目录'})
  await page.screenshot({path:info.outputPath('picker-path.png')})
})
