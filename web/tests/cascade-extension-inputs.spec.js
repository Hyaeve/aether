import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

for(const provider of ['115','mobile','quark','cas'])for(const width of [1440,390]){
 test(`cascade extensions persist independently ${provider} ${width}`,async({page},info)=>{
  const writes=[],unexpected=[],card={id:'one',storageId:'pan',url:provider==='115'?'https://115.com/s/abc':provider==='quark'?'https://pan.quark.cn/s/abc':'https://yun.139.com/shareweb/abc',name:'分享作品',directory:'/media',directoryName:'作品',enabled:false,cascadeCleanup:'off',result:{status:'idle'}}
  await page.route('**/api/**',route=>{
   const r=route.request(),p=new URL(r.url()).pathname
   if(p==='/api/auth/status')return route.fulfill({json:{initialized:true,authenticated:true}})
   if(p==='/api/state')return route.fulfill({json:{username:'cascade',storages:[],tasks:[],settings:{},cache:{},traffic:{}}})
   if(p==='/api/traffic'&&r.method()==='GET')return route.fulfill({json:{}})
   if(p==='/api/strm-replace'&&r.method()==='GET')return route.fulfill({json:[]})
   if(p===`/api/virtual-library/${provider}`)return route.fulfill({json:{cards:[card],settings:{storageIds:[],directory:'/media'},stats:{}}})
   if(p===`/api/virtual-library/${provider}/one`&&r.method()==='PUT'){const body=r.postDataJSON();writes.push(body);Object.assign(card,body);return route.fulfill({json:{id:'one'}})}
   unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})
  })
  await page.setViewportSize({width,height:950});await page.goto(`/virtual/${provider}`)
  await page.evaluate(t=>document.documentElement.dataset.theme=t,width===390?'dark':'light')
  const open=async()=>{await page.getByRole('button',{name:'编辑 分享作品',exact:true}).click();const d=page.getByRole('dialog');await d.getByRole('button',{name:'更多选项',exact:true}).click();return d}
  let d=await open(),input=d.getByRole('textbox',{name:'级联删除扩展名',exact:true})
  await expect(input).toHaveValue('');await d.getByRole('button',{name:'级联删除图片扩展名',exact:true}).click();await expect(input).toHaveValue('jpg;jpeg;png;webp')
  await d.getByRole('button',{name:'级联删除数据扩展名',exact:true}).click();await expect(input).toHaveValue('jpg;jpeg;png;webp;nfo;ass;srt;ssa;sub')
  await input.fill('nfo;png');await input.scrollIntoViewIfNeeded()
  expect(await input.evaluate(el=>{const field=el.closest('label');return field.previousElementSibling.textContent.includes('链库刮削')&&Math.abs(field.getBoundingClientRect().width-field.parentElement.getBoundingClientRect().width)<1})).toBe(true)
  expect(await d.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath('cascade-input.png')})
  await d.getByRole('button',{name:'保存链接',exact:true}).click();await expect(d).toHaveCount(0);expect(writes[0].cascadeCleanup).toBe('nfo;png')
  d=await open();input=d.getByRole('textbox',{name:'级联删除扩展名',exact:true});await expect(input).toHaveValue('nfo;png');await input.fill('')
  await d.getByRole('button',{name:'保存链接',exact:true}).click();await expect(d).toHaveCount(0);expect(writes[1].cascadeCleanup).toBe('off')
  d=await open();await expect(d.getByRole('textbox',{name:'级联删除扩展名',exact:true})).toHaveValue('');await d.getByRole('button',{name:'取消',exact:true}).click()
  card.cascadeCleanup='on';await page.reload();d=await open()
  await expect(d.getByRole('textbox',{name:'级联删除扩展名',exact:true})).toHaveValue('jpg;jpeg;png;webp;nfo;srt;ass;ssa;sub;vtt')
  if(provider!=='115')await d.getByRole('textbox',{name:'Cron表达式',exact:true}).fill('0 12 * * *')
  await d.getByRole('button',{name:'保存链接',exact:true}).click();await expect(d).toHaveCount(0);expect(writes[2].cascadeCleanup).toBe('on')
  expect(unexpected).toEqual([])
 })
}
