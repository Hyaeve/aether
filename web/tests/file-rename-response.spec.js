import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page,{type='local',mode='list',folder=false}={}){
  const actions=[],reads=[],unexpected=[],pending=[]
  const entries=[{id:'123',name:folder?'原目录':'原文件.mkv',isDir:folder,size:128},{id:'other',name:'其他文件.txt',size:20}]
  await page.addInitScript(mode=>localStorage.setItem('aether-files-view',mode),mode)
  await page.route('**/api/**',async route=>{
    const url=new URL(route.request().url()),p=url.pathname
    let json
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'rename-response',storages:[{id:'s',type,name:'原存储',enabled:true,config:{}},{id:'t',type:'local',name:'另一存储',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{}}
    else if(p==='/api/files'){reads.push(Object.fromEntries(url.searchParams));json=url.searchParams.get('storage')==='s'?entries:[{id:'123',name:'另一存储文件.txt',size:10}]}
    else if(p==='/api/files/action'){
      const input=route.request().postDataJSON();actions.push(input)
      return new Promise(resolve=>pending.push(async(status=200,result)=>{
        const renamed={id:'456',name:input.name,pickCode:'new-pick-code',url:'/stream/new-identity',isDir:folder}
        const output=result??{processed:1,renamed}
        if(status===200&&output.renamed)Object.assign(entries[0],output.renamed)
        try{await route.fulfill({status,json:output})}finally{resolve()}
      }))
    }else if(['/api/file-favorites','/api/file-visits'].includes(p))json=[]
    else if(['/api/tmdb','/api/traffic','/api/plugins/proxy'].includes(p))json={}
    else if(p==='/api/strm-replace')json={tasks:[]}
    else{unexpected.push(p);return route.fulfill({status:500,json:{error:'Unmocked '+p}})}
    return route.fulfill({json})
  })
  return{actions,reads,unexpected,finish:async(status,result)=>{await expect.poll(()=>pending.length).toBeGreaterThan(0);await pending.shift()(status,result)},cleanup:async()=>{for(const finish of pending.splice(0))await finish(400,{error:'test cleanup'})}}
}
async function edit(page,oldName,newName){
  await page.getByRole('button',{name:oldName,exact:true}).click()
  await page.keyboard.press('F2')
  const input=page.getByRole('textbox',{name:'新名称',exact:true})
  await input.fill(newName)
  return input
}

for(const [mode,folder,width,theme] of [['list',false,1440,'light'],['grid',true,390,'dark']]){
  test('rename previews immediately while storage is pending '+mode,async({page},info)=>{
    const mock=await fixture(page,{mode,folder})
    try{
      await page.setViewportSize({width,height:900});await page.goto('/files')
      await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
      const oldName=folder?'原目录':'原文件.mkv',newName=folder?'新目录':'新文件.mkv'
      const input=await edit(page,oldName,newName)
      const reads=mock.reads.length
      await input.press('Enter')
      await expect(page.getByRole('button',{name:newName,exact:true})).toBeVisible({timeout:1000})
      await expect(input).toHaveCount(0)
      await expect(page.getByRole('status',{name:'正在同步名称'})).toBeVisible()
      await expect(page.locator('.toast-stack')).not.toContainText('文件操作完成')
      await page.keyboard.press('F2');await page.keyboard.press('Enter')
      await page.getByRole('button',{name:newName,exact:true}).dblclick()
      expect(mock.actions).toHaveLength(1);expect(mock.reads).toHaveLength(reads)
      await expect(page.locator('.file-row[aria-busy=true],.file-grid-item[aria-busy=true]')).toHaveCount(1)
      await page.getByRole('textbox',{name:'搜索当前目录',exact:true}).fill(newName)
      await expect(page.getByRole('button',{name:newName,exact:true})).toBeVisible()
      await page.getByRole('textbox',{name:'搜索当前目录',exact:true}).fill('')
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
      await captureScreenshot(page,{path:info.outputPath('pending-'+mode+'.png')})
      await mock.finish()
      await expect(page.getByRole('status',{name:'正在同步名称'})).toHaveCount(0)
      await expect(page.getByRole('button',{name:newName,exact:true})).toBeVisible()
      await expect(page.locator('.toast-stack')).toContainText('文件操作完成')
      expect(mock.reads).toHaveLength(reads);expect(mock.unexpected).toEqual([])
    }finally{await mock.cleanup()}
  })
}

test('rename confirmation closes immediately and rejected writes restore the original label',async({page})=>{
  const mock=await fixture(page)
  try{
    await page.goto('/files');await edit(page,'原文件.mkv','未保存的名称.mkv')
    await page.locator('.file-view').click({position:{x:300,y:350}})
    const dialog=page.getByRole('dialog',{name:'确认修改名称'})
    await dialog.getByRole('button',{name:'确认修改',exact:true}).click()
    await expect(dialog).toHaveCount(0,{timeout:1000})
    await expect(page.getByRole('button',{name:'未保存的名称.mkv',exact:true})).toBeVisible()
    await mock.finish(409,{error:'目标目录已有同名文件'})
    await expect(page.getByRole('button',{name:'原文件.mkv',exact:true})).toBeVisible()
    await expect(page.getByRole('status',{name:'正在同步名称'})).toHaveCount(0)
    await expect(page.locator('.toast-stack')).toContainText('目标目录已有同名文件')
    await expect(page.locator('.toast-stack')).toContainText('请刷新核对存储结果')
    await expect(page.locator('.toast-stack')).not.toContainText('文件操作完成')
    expect(mock.actions).toHaveLength(1)
    await page.keyboard.press('F2');await expect(page.getByRole('textbox',{name:'新名称',exact:true})).toHaveValue('原文件.mkv')
    expect(mock.unexpected).toEqual([])
  }finally{await mock.cleanup()}
})

test('115 optimistic extension rename keeps confirmation and adopts acknowledged IDs',async({page})=>{
  const mock=await fixture(page,{type:'115'})
  try{
    await page.goto('/files');const input=await edit(page,'原文件.mkv','新文件.mp4');await input.press('Enter')
    const dialog=page.getByRole('dialog',{name:'确认修改名称'})
    await expect(dialog).toContainText('秒传失败保留原文件');expect(mock.actions).toHaveLength(0)
    await dialog.getByRole('button',{name:'确认修改',exact:true}).click()
    await expect(dialog).toHaveCount(0,{timeout:1000})
    await expect(page.getByRole('button',{name:'新文件.mp4',exact:true})).toBeVisible()
    await mock.finish()
    await expect(page.getByRole('status',{name:'正在同步名称'})).toHaveCount(0)
    await page.keyboard.press('F2');await input.fill('再次命名.mp4');await input.press('Enter')
    await expect.poll(()=>mock.actions.length).toBe(2)
    expect(mock.actions[0]).toMatchObject({ids:['123'],name:'新文件.mp4'})
    expect(mock.actions[1]).toMatchObject({ids:['456'],name:'再次命名.mp4'})
    await mock.finish();await expect(page.getByRole('status',{name:'正在同步名称'})).toHaveCount(0)
    expect(mock.reads).toHaveLength(1);expect(mock.unexpected).toEqual([])
  }finally{await mock.cleanup()}
})

test('late rename responses do not overwrite a different storage with matching IDs',async({page})=>{
  const mock=await fixture(page)
  try{
    await page.goto('/files');const input=await edit(page,'原文件.mkv','新文件.mkv');await input.press('Enter')
    await expect(page.getByRole('button',{name:'新文件.mkv',exact:true})).toBeVisible()
    await page.getByRole('button',{name:'选择存储池',exact:true}).click()
    await page.getByRole('option',{name:'另一存储',exact:true}).click()
    await expect(page.getByRole('button',{name:'另一存储文件.txt',exact:true})).toBeVisible()
    await mock.finish()
    await expect(page.getByRole('button',{name:'另一存储文件.txt',exact:true})).toBeVisible()
    await expect(page.getByRole('button',{name:'新文件.mkv',exact:true})).toHaveCount(0)
    await expect(page.locator('.toast-stack')).not.toContainText('文件操作完成')
    expect(mock.unexpected).toEqual([])
  }finally{await mock.cleanup()}
})

test('missing rename acknowledgements restore the label without retrying writes',async({page})=>{
  const mock=await fixture(page)
  try{
    await page.goto('/files');const input=await edit(page,'原文件.mkv','新文件.mkv');await input.press('Enter')
    await expect(page.getByRole('button',{name:'新文件.mkv',exact:true})).toBeVisible()
    await mock.finish(200,{processed:1})
    await expect(page.getByRole('button',{name:'原文件.mkv',exact:true})).toBeVisible()
    await expect(page.locator('.toast-stack')).toContainText('服务响应未包含新文件信息')
    await expect(page.locator('.toast-stack')).not.toContainText('文件操作完成')
    expect(mock.actions).toHaveLength(1);expect(mock.unexpected).toEqual([])
  }finally{await mock.cleanup()}
})

test('returning to the renamed directory reconciles its fresh listing and ID',async({page})=>{
  const mock=await fixture(page)
  try{
    await page.goto('/files');const input=await edit(page,'原文件.mkv','新文件.mkv');await input.press('Enter')
    await expect(page.getByRole('button',{name:'新文件.mkv',exact:true})).toBeVisible()
    for(const name of ['另一存储','原存储']){
      await page.getByRole('button',{name:'选择存储池',exact:true}).click()
      await page.getByRole('option',{name,exact:true}).click()
      await expect(page.getByRole('button',{name:name==='原存储'?'新文件.mkv':'另一存储文件.txt',exact:true})).toBeVisible()
    }
    await expect(page.getByRole('status',{name:'正在同步名称'})).toBeVisible()
    await page.getByRole('button',{name:'工具',exact:true}).click()
    await page.getByRole('menuitem',{name:'命名工作台',exact:true}).click()
    await expect(page.locator('.rename-workbench')).toHaveCount(0)
    await mock.finish()
    await expect(page.getByRole('status',{name:'正在同步名称'})).toHaveCount(0)
    await expect(page.getByRole('button',{name:'新文件.mkv',exact:true})).toBeVisible()
    await expect.poll(()=>mock.reads.length).toBe(4)
    await edit(page,'新文件.mkv','再次修改.mkv');await input.press('Enter')
    await expect.poll(()=>mock.actions.length).toBe(2)
    expect(mock.actions[1].ids).toEqual(['456'])
    await mock.finish();await expect(page.getByRole('status',{name:'正在同步名称'})).toHaveCount(0)
    expect(mock.unexpected).toEqual([])
  }finally{await mock.cleanup()}
})

test('leaving file management discards stale rename UI feedback',async({page})=>{
  const mock=await fixture(page)
  try{
    await page.goto('/files');const input=await edit(page,'原文件.mkv','新文件.mkv');await input.press('Enter')
    await expect.poll(()=>mock.actions.length).toBe(1)
    await page.getByRole('link',{name:'信息总览',exact:true}).click()
    await expect(page.locator('.file-view')).toHaveCount(0)
    const response=page.waitForResponse(r=>r.url().endsWith('/api/files/action'))
    await mock.finish(500,{error:'后台拒绝本次修改'})
    await (await response).finished()
    await page.evaluate(()=>new Promise(requestAnimationFrame))
    await expect(page.locator('.toast-stack')).not.toContainText('后台拒绝本次修改')
    expect(mock.unexpected).toEqual([])
  }finally{await mock.cleanup()}
})
