import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

for(const [kind,width,height,dpr] of [['login',1440,1000,2],['about',390,844,3],['about',3840,2160,2]]){
  test(`bounded sky retains visible artwork and releases canvases ${kind} ${width} dpr${dpr}`,async({browser},info)=>{
    const context=await browser.newContext({viewport:{width,height},deviceScaleFactor:dpr,serviceWorkers:'block'})
    const page=await context.newPage(),unexpected=[],errors=[]
    page.on('pageerror',error=>errors.push(error.message))
    await page.addInitScript(()=>{
      window.skyCanvases=[]
      const original=HTMLCanvasElement.prototype.getContext
      HTMLCanvasElement.prototype.getContext=function(...args){
        if(args[0]==='2d'&&!window.skyCanvases.includes(this))window.skyCanvases.push(this)
        return original.apply(this,args)
      }
    })
    await page.route('**/api/**',route=>{
      const path=new URL(route.request().url()).pathname
      if(path==='/api/auth/status')return route.fulfill({json:{initialized:true,authenticated:kind==='about'}})
      if(path==='/api/state')return route.fulfill({json:{username:'sky-performance',storages:[],tasks:[],settings:{},traffic:{},cache:{}}})
      if(['/api/traffic','/api/plugins/proxy'].includes(path))return route.fulfill({json:{}})
      if(path==='/api/strm-replace')return route.fulfill({json:{tasks:[]}})
      unexpected.push(path)
      return route.fulfill({status:500,json:{error:'Unexpected API'}})
    })
    try{
      await page.goto(kind==='login'?'/':'/settings/about')
      const canvas=page.locator('.starfield')
      await expect(canvas).toBeVisible()
      await expect.poll(()=>canvas.evaluate(el=>{
        if(!el.width||!el.height)return 0
        const pixels=el.getContext('2d').getImageData(0,0,el.width,el.height).data
        let visible=0
        for(let i=3;i<pixels.length;i+=4)if(pixels[i])visible++
        return visible/(pixels.length/4)
      })).toBeGreaterThan(.01)
      const pixels=await canvas.evaluate(el=>el.width*el.height)
      expect(pixels).toBeLessThanOrEqual(3000000)
      const allocated=()=>page.evaluate(()=>window.skyCanvases.reduce((total,el)=>total+el.width*el.height,0))
      expect(await allocated()).toBeLessThanOrEqual(6600000)
      const first=page.locator('.satellite').first(),position=await first.evaluate(el=>el.style.transform)
      await expect.poll(()=>first.evaluate(el=>el.style.transform)).not.toBe(position)
      const logos=page.locator('.satellite img')
      await expect.poll(()=>logos.evaluateAll(nodes=>nodes.every(el=>el.complete&&el.naturalWidth>0))).toBe(true)
      await page.emulateMedia({reducedMotion:'reduce'})
      await expect(page.locator('.login-universe')).toHaveClass(/motion-paused/)
      await captureScreenshot(page,{path:info.outputPath(`sky-${kind}-${width}.png`)})
      if(kind==='login'){
        await page.setViewportSize({width:390,height:844})
        await expect(canvas).not.toBeVisible()
        await expect.poll(()=>allocated()).toBe(0)
      }else{
        await page.getByRole('link',{name:'账号与安全',exact:true}).click()
        await expect(canvas).toHaveCount(0)
        await expect.poll(()=>allocated()).toBe(0)
      }
      expect(unexpected).toEqual([]);expect(errors).toEqual([])
    }finally{await context.close()}
  })
}
