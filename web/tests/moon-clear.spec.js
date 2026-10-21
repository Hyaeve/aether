import {test,expect} from '@playwright/test'

for(const viewport of [{width:1440,height:1000},{width:390,height:844}])test(`moon surface has no twinkling stellar overlay at ${viewport.width}px`,async({page},info)=>{
  await page.setViewportSize(viewport)
  await page.route('**/api/**',r=>r.fulfill({json:new URL(r.request().url()).pathname==='/api/auth/status'?{initialized:true,authenticated:true}:new URL(r.request().url()).pathname==='/api/state'?{username:'moon-test',storages:[],tasks:[],settings:{},cache:{},traffic:{},logs:[]}:[]}))
  await page.goto('/settings/about')
  await expect.poll(()=>page.locator('.page-content').evaluate(el=>el.getAnimations().every(a=>a.playState==='finished'))).toBe(true)
  const canvas=page.locator('.login-universe canvas:visible').first()
  await expect(canvas).toBeVisible()
  await expect.poll(()=>canvas.evaluate(el=>el.width)).toBeGreaterThan(0)
  const sample=()=>canvas.evaluate(el=>{
    const w=el.clientWidth,h=el.clientHeight,scale=el.width/w,size=Math.min(92,w*.14),cx=(w*.84+size/2)*scale,cy=(h*.08+size/2)*scale,r=size*100/220*scale-3
    const data=el.getContext('2d').getImageData(0,0,el.width,el.height).data,moon=[]
    for(let y=Math.ceil(cy-r);y<cy+r;y++)for(let x=Math.ceil(cx-r);x<cx+r;x++)if((x-cx)**2+(y-cy)**2<r*r){const i=(y*el.width+x)*4;moon.push(...data.slice(i,i+4))}
    return {moon,opaque:moon.filter((v,i)=>i%4===3&&v>0).length}
  })
  const before=await sample()
  expect(before.opaque).toBeGreaterThan(200)
  await page.waitForTimeout(1800)
  expect((await sample()).moon).toEqual(before.moon)
  await page.screenshot({path:info.outputPath(`moon-${viewport.width}.png`)})
})
