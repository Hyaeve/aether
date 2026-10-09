import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

function wav() {
  const rate=8000, b=Buffer.alloc(44+rate*30*2)
  b.write('RIFF');b.writeUInt32LE(b.length-8,4);b.write('WAVEfmt ',8);b.writeUInt32LE(16,16)
  b.writeUInt16LE(1,20);b.writeUInt16LE(1,22);b.writeUInt32LE(rate,24);b.writeUInt32LE(rate*2,28)
  b.writeUInt16LE(2,32);b.writeUInt16LE(16,34);b.write('data',36);b.writeUInt32LE(b.length-44,40)
  return b
}
async function setup(page,count=2,cover) {
  const data=wav()
  const tracks=Array.from({length:count},(_,i)=>({id:`song${i}`,name:`星际回声 ${i+1}.wav`,url:`/song${i}.wav`,size:data.length}))
  await page.route('**/api/**', r=>{
    const url=new URL(r.request().url())
    let body={}
    if(url.pathname==='/api/auth/status')body={initialized:true,authenticated:true}
    if(url.pathname==='/api/state')body={username:'audio-workspace',storages:[{id:'local',type:'local',name:'音乐',enabled:true,config:{}}],tasks:[],settings:{},cache:{}}
    if(url.pathname==='/api/files')body=[...tracks,{id:'lrc',name:'星际回声 1.lrc'},...(cover?[{id:'cover',name:'cover.jpg'}]:[])]
    if(url.pathname==='/api/files/audio-source'){
      if(url.searchParams.get('id')==='cover')return r.fulfill({body:cover,contentType:'image/jpeg'})
      if(url.searchParams.get('id')==='lrc')return r.fulfill({body:'[00:00]星光穿过夜空\n[00:08]回声抵达远方\n[00:18]银河仍在流淌',contentType:'text/plain'})
      const range=r.request().headers().range
      if(r.request().method()==='HEAD')return r.fulfill({headers:{'Content-Length':String(data.length),'Accept-Ranges':'bytes','Content-Type':'audio/wav'}})
      const match=range?.match(/bytes=(\d+)-(\d*)/),start=Number(match?.[1]||0),end=match?.[2]?Math.min(Number(match[2]),data.length-1):data.length-1
      return r.fulfill({status:206,body:data.subarray(start,end+1),headers:{'Content-Type':'audio/wav','Accept-Ranges':'bytes','Content-Range':`bytes ${start}-${end}/${data.length}`}})
    }
    return r.fulfill({json:body})
  })
  await page.route('**/song*.wav',r=>{
    const match=r.request().headers().range?.match(/bytes=(\d+)-(\d*)/),start=Number(match?.[1]||0),end=match?.[2]?Math.min(Number(match[2]),data.length-1):data.length-1
    return r.fulfill({status:match?206:200,body:data.subarray(start,end+1),headers:{'Content-Type':'audio/wav','Accept-Ranges':'bytes',...(match?{'Content-Range':`bytes ${start}-${end}/${data.length}`}:{})}})
  })
  await page.goto('/files')
  await page.getByRole('button',{name:tracks[0].name,exact:true}).dblclick()
  const panel=page.locator('.audio-player'),audio=panel.locator('audio')
  await expect.poll(()=>audio.evaluate(el=>el.readyState)).toBeGreaterThanOrEqual(2)
  return {panel,audio,tracks}
}

test('custom timeline seeks with clicks, dragging and keyboard; lyrics and bitrate use real audio data',async({page},info)=>{
  const {panel,audio}=await setup(page,1)
  await expect(panel.getByRole('button',{name:'上一首',exact:true})).toBeDisabled()
  await expect(panel.getByRole('button',{name:'下一首',exact:true})).toBeDisabled()
  await expect(panel.locator('.audio-format')).toHaveText('WAV · 128 kbps')
  const slider=panel.getByRole('slider',{name:'播放进度'}),rect=await slider.boundingBox()
  await audio.evaluate(el=>el.pause())
  await slider.click({position:{x:rect.width*.5,y:12}})
  await expect.poll(()=>audio.evaluate(el=>el.currentTime)).toBeGreaterThan(13)
  await slider.focus();const before=await audio.evaluate(el=>el.currentTime)
  await page.keyboard.press('ArrowRight')
  await expect.poll(()=>audio.evaluate(el=>el.currentTime)).toBeGreaterThan(before)
  await page.mouse.move(rect.x+rect.width*.5,rect.y+12);await page.mouse.down();await page.mouse.move(rect.x+rect.width*.8,rect.y+12,{steps:10});await page.mouse.up()
  await expect.poll(()=>audio.evaluate(el=>el.currentTime)).toBeGreaterThan(22)
  await panel.getByRole('button',{name:'切换到歌词页',exact:true}).click()
  await expect(panel.getByRole('button',{name:'银河仍在流淌',exact:true})).toHaveAttribute('aria-current','true')
  await panel.getByRole('button',{name:'回声抵达远方',exact:true}).click()
  await expect.poll(()=>audio.evaluate(el=>el.currentTime)).toBeCloseTo(8,0)
  await panel.getByRole('button',{name:'播放模式：顺序',exact:true}).click()
  await expect(panel.getByRole('button',{name:'播放模式：循环',exact:true})).toBeVisible()
  await audio.evaluate(el=>el.dispatchEvent(new Event('ended')))
  await expect.poll(()=>audio.evaluate(el=>el.currentTime)).toBeLessThan(2)
  await page.setViewportSize({width:390,height:780})
  await page.getByRole('button',{name:'主题：日光',exact:true}).click()
  await page.screenshot({path:info.outputPath('music-lyrics-mobile.png')})
})

test('artwork extends with blur, cover switches to full lyrics and short screens keep controls accessible',async({page},info)=>{
  const cover=process.env.AETHER_AUDIO_ARTWORK ? readFileSync(process.env.AETHER_AUDIO_ARTWORK) : readFileSync(new URL('../public/media/abs.png',import.meta.url))
  const {panel,audio}=await setup(page,1,cover)
  await expect.poll(()=>panel.locator('.audio-art img').evaluate(el=>el.naturalWidth)).toBeGreaterThan(0)
  await expect(panel.locator('.audio-ambient')).toHaveCSS('filter','blur(30px)')
  for(const viewport of [{width:1440,height:1000},{width:390,height:780},{width:844,height:390}]){
    await page.setViewportSize(viewport)
    await expect.poll(()=>panel.evaluate(el=>el.getBoundingClientRect().height)).toBe(viewport.height-48)
    await expect(panel.getByRole('slider',{name:'播放进度'})).toBeInViewport()
    await expect(panel.getByRole('button',{name:'歌曲列表',exact:true})).toBeInViewport()
    await page.screenshot({path:info.outputPath(`music-artwork-${viewport.width}.png`)})
  }
  await page.setViewportSize({width:390,height:780})
  await panel.getByRole('button',{name:'切换到歌词页',exact:true}).click()
  await expect(panel.locator('.audio-lyric-view')).toBeVisible()
  await panel.locator('.audio-lyric-view').evaluate(async el=>{await Promise.allSettled(el.getAnimations({subtree:true}).map(a=>a.finished))})
  await expect(panel.getByRole('button',{name:'回声抵达远方',exact:true})).toBeVisible()
  await page.screenshot({path:info.outputPath('music-full-lyrics.png')})
  await audio.evaluate(el=>el.play())
  await panel.getByRole('button',{name:'收起音频播放',exact:true}).click()
  const capsule=page.locator('.audio-capsule')
  await expect(capsule).toBeFocused()
  await page.keyboard.press('ArrowLeft');await page.keyboard.press('ArrowDown')
  await expect.poll(()=>capsule.evaluate(el=>el.getBoundingClientRect().left)).toBe(8)
  await page.getByRole('button',{name:'账号菜单',exact:true}).click()
  await page.getByRole('button',{name:'退出登录',exact:true}).click()
  await expect(panel).toHaveCount(0);await expect(capsule).toHaveCount(0)
})

test('queue navigation and ended modes work; capsule preserves playback across routes and docks on either edge',async({page},info)=>{
  const {panel,audio}=await setup(page,3)
  await panel.getByRole('button',{name:'下一首',exact:true}).click()
  await expect(panel.getByRole('heading')).toHaveText('星际回声 2.wav')
  await panel.getByRole('button',{name:'上一首',exact:true}).click()
  await expect(panel.getByRole('heading')).toHaveText('星际回声 1.wav')
  await panel.getByRole('button',{name:'歌曲列表',exact:true}).click()
  await expect(panel.getByRole('option')).toHaveCount(3)
  await panel.getByRole('option',{name:'星际回声 3.wav',exact:true}).click()
  await expect(panel.getByRole('heading')).toHaveText('星际回声 3.wav')
  await audio.evaluate(el=>{el.pause();el.dispatchEvent(new Event('ended'))})
  await expect(panel.getByRole('heading')).toHaveText('星际回声 3.wav')
  await panel.getByRole('button',{name:'播放模式：顺序',exact:true}).click()
  await audio.evaluate(el=>el.dispatchEvent(new Event('ended')))
  await expect(panel.getByRole('heading')).toHaveText('星际回声 1.wav')
  await panel.getByRole('button',{name:'播放模式：循环',exact:true}).click()
  await panel.getByRole('button',{name:'下一首',exact:true}).click()
  await expect(panel.getByRole('heading')).not.toHaveText('星际回声 1.wav')
  await expect.poll(()=>audio.evaluate(el=>el.readyState)).toBeGreaterThanOrEqual(2)
  await audio.evaluate(el=>el.play())
  const source=await audio.getAttribute('src')
  await page.locator('.breadcrumbs').click()
  const capsule=page.locator('.audio-capsule')
  await expect(capsule).toBeVisible();await expect(panel).not.toBeVisible()
  await expect.poll(()=>audio.evaluate(el=>el.paused)).toBe(false)
  await page.getByRole('link',{name:'存储管理',exact:true}).click()
  await expect(capsule).toBeVisible();await expect(audio).toHaveAttribute('src',source)
  let bounds=await capsule.boundingBox()
  await page.mouse.move(bounds.x+70,bounds.y+24);await page.mouse.down();await page.mouse.move(100,220,{steps:15});await page.mouse.up()
  await expect.poll(()=>capsule.evaluate(el=>el.getBoundingClientRect().left)).toBe(8)
  await expect(panel).not.toBeVisible()
  await capsule.focus();await page.keyboard.press('ArrowRight')
  await expect.poll(()=>capsule.evaluate(el=>innerWidth-el.getBoundingClientRect().right)).toBe(8)
  await page.screenshot({path:info.outputPath('music-capsule-right.png')})
  await page.keyboard.press('Enter')
  await expect(panel).toBeVisible()
  await panel.getByRole('button',{name:'暂停',exact:true}).click()
  await expect.poll(()=>audio.evaluate(el=>el.paused)).toBe(true)
  await panel.getByRole('button',{name:'关闭音频播放',exact:true}).click()
  await expect(panel).toHaveCount(0);await expect(capsule).toHaveCount(0)
})
