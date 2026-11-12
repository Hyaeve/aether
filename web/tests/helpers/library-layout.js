import {expect} from '@playwright/test'

export async function expectLibraryCascadePair(dialog) {
  const cascade=dialog.getByRole('textbox',{name:'级联删除扩展名',exact:true})
  const scrape=dialog.getByRole('button',{name:'链库刮削',exact:true})
  const layout=await cascade.evaluate(input=>{
    const field=input.closest('label'),previous=field.previousElementSibling
    const rect=node=>{
      const {x,y,width,right}=node.getBoundingClientRect()
      return {x,y,width,right}
    }
    return {cascade:rect(field),scrape:rect(previous),grid:rect(field.parentElement),
      gap:parseFloat(getComputedStyle(field.parentElement).columnGap),previousText:previous.textContent}
  })
  expect(layout.previousText).toContain('链库刮削')
  expect(Math.abs(layout.cascade.y-layout.scrape.y),'刮削与级联字段同行').toBeLessThan(1)
  expect(Math.abs(layout.cascade.width-layout.scrape.width),'两个字段等宽').toBeLessThan(1)
  expect(Math.abs(layout.scrape.x-layout.grid.x),'刮削字段位于左半栏').toBeLessThan(1)
  expect(Math.abs(layout.cascade.right-layout.grid.right),'级联字段位于右半栏').toBeLessThan(1)
  expect(layout.gap).toBeGreaterThan(0)
  expect(Math.abs(layout.cascade.x-layout.scrape.right-layout.gap),'两栏间距符合网格').toBeLessThan(1)
  expect(Math.abs(layout.scrape.width*2+layout.gap-layout.grid.width),'两栏填满同一行').toBeLessThan(1)
  await expect(cascade).toHaveCSS('height','40px')
  await expect(scrape).toHaveCSS('height','40px')
  expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth),'编辑窗口无横向溢出').toBe(true)
}
