import { test, expect } from '@playwright/test'

test('saved storage cards use comparable local and WebDAV icon sizes', async ({ page }, info) => {
  await page.route('**/api/auth/status', route => route.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', route => route.fulfill({ json: {
    username: 'icon-test', tasks: [], settings: {}, cache: {}, traffic: {}, logs: [],
    storages: ['local', 'webdav', 'quark', '115', 'mobile', 'tianyi'].map(type => ({ id: type, type, name: type, enabled: true, config: {} }))
  } }))
  await page.goto('/storage')
  await expect(page.locator('.storage-card')).toHaveCount(6)
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 })
    const tile = page.getByRole('button', { name:'添加存储池', exact:true })
    const centered = await tile.evaluate(el => {
      const card=el.getBoundingClientRect()
      return [...el.children].map(child => {
        const rect=child.getBoundingClientRect()
        return Math.abs(rect.y+rect.height/2-card.y-card.height/2)
      })
    })
    for (const offset of centered) expect(offset).toBeLessThan(1)
    for (const type of ['local', 'webdav']) {
      const card = page.locator('.storage-card').filter({ has: page.getByRole('heading', { name: type, exact: true }) })
      const icon = card.locator('.provider-icon svg')
      await expect(icon).toHaveCSS('width', type === 'local' ? '39px' : '44px')
      await expect(icon).toHaveCSS('height', type === 'local' ? '39px' : '44px')
      await expect(card).toHaveCSS('height', '84px')
      expect(await card.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    }
    const visible = await page.locator('.storage-card .provider-logo').evaluateAll(async images => {
      return Promise.all(images.map(async img => {
        await img.decode()
        const canvas = document.createElement('canvas'); canvas.width = img.naturalWidth; canvas.height = img.naturalHeight
        const ctx = canvas.getContext('2d'); ctx.drawImage(img, 0, 0)
        const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data
        let left = canvas.width, right = -1, top = canvas.height, bottom = -1
        for (let y = 0; y < canvas.height; y++) for (let x = 0; x < canvas.width; x++) {
          const i = (y * canvas.width + x) * 4, channels = [pixels[i], pixels[i+1], pixels[i+2]]
          if (pixels[i+3] > 80 && Math.max(...channels) - Math.min(...channels) > 40) { left = Math.min(left, x); right = Math.max(right, x); top = Math.min(top, y); bottom = Math.max(bottom, y) }
        }
        const box = img.getBoundingClientRect()
        return { type: img.parentElement.dataset.provider, width: (right-left+1)/canvas.width*box.width, height: (bottom-top+1)/canvas.height*box.height }
      }))
    })
    const a = visible.find(item => item.type === '115'), b = visible.find(item => item.type === 'mobile')
    expect(Math.abs(a.width-b.width), JSON.stringify(visible)).toBeLessThan(3)
    expect(Math.abs(a.height-b.height), JSON.stringify(visible)).toBeLessThan(3)
    await page.screenshot({ path: info.outputPath(`storage-icons-${width}.png`) })
  }
})

test('file selectors and history normalize provider artwork without changing storage cards', async ({page}, info) => {
  const types=['mobile','115','local','webdav','quark']
  await page.addInitScript(types => localStorage.setItem('aether-file-history:compact-icons',JSON.stringify(types.map(type=>({storage:type,id:'b',history:[{id:'/',name:'A'},{id:'a',name:'B'}]})))),types)
  await page.route('**/api/**',r=>{
    const pathname=new URL(r.request().url()).pathname
    if(pathname==='/api/files/visits')return r.fulfill({json:r.request().postDataJSON().items})
    const json=pathname==='/api/auth/status'?{initialized:true,authenticated:true}:pathname==='/api/state'?{username:'compact-icons',storages:types.map(type=>({id:type,type,name:type,enabled:true,config:{}})),tasks:[],settings:{},traffic:{},cache:{}}:pathname==='/api/files'?[]:{}
    return r.fulfill({json})
  })
  await page.goto('/files')
  const verify=async selector=>{
    await expect(page.locator(`${selector} .provider-icon`)).toHaveCount(5)
    const measurements=await page.locator(`${selector} .provider-icon`).evaluateAll(async icons=>{
      return await Promise.all(icons.map(async el=>{
        const rect=el.getBoundingClientRect(),label=el.nextElementSibling.getBoundingClientRect(),art=el.querySelector('img,svg'),box=art.getBoundingClientRect()
        let width=box.width
        if(art.tagName==='IMG'){
          await art.decode()
          const canvas=document.createElement('canvas');canvas.width=art.naturalWidth;canvas.height=art.naturalHeight
          const ctx=canvas.getContext('2d');ctx.drawImage(art,0,0)
          const pixels=ctx.getImageData(0,0,canvas.width,canvas.height).data
          let min=canvas.width,max=-1
          for(let y=0;y<canvas.height;y++)for(let x=0;x<canvas.width;x++){
            const i=(y*canvas.width+x)*4,channels=[pixels[i],pixels[i+1],pixels[i+2]]
            if(pixels[i+3]>80&&Math.max(...channels)-Math.min(...channels)>40){min=Math.min(min,x);max=Math.max(max,x)}
          }
          width=(max-min+1)/canvas.width*box.width
        }else width=art.getBBox().width/24*box.width
        return {type:el.dataset.provider,width,offset:Math.abs(rect.y+rect.height/2-label.y-label.height/2)}
      }))
    })
    const mobile=measurements.find(v=>v.type==='mobile').width
    for(const item of measurements){expect(Math.abs(item.width-mobile),item.type).toBeLessThan(2);expect(item.offset,item.type).toBeLessThan(1)}
  }
  for(const theme of ['light','dark']){
    await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
    await page.getByRole('button',{name:'选择存储池',exact:true}).click()
    await page.locator('.file-pool-select [role=listbox]').hover()
    await verify('.file-pool-select [role=listbox]')
    await page.screenshot({path:info.outputPath(`pool-${theme}.png`)})
    await page.keyboard.press('Escape')
    await page.getByRole('button',{name:'历史访问',exact:true}).click()
    await page.locator('.file-visit-history [role=listbox]').hover()
    await verify('.file-visit-history [role=listbox]')
    await page.screenshot({path:info.outputPath(`history-${theme}.png`)})
    await page.keyboard.press('Escape')
  }
  await page.setViewportSize({width:390,height:844})
  await page.getByRole('button',{name:'选择存储池',exact:true}).click()
  await page.locator('.file-pool-select [role=listbox]').hover()
  await verify('.file-pool-select [role=listbox]')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('pool-mobile.png')})
})
