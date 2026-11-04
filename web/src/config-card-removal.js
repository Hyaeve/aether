// Configuration deletion feedback only. No file-operation hooks or background rendering.
const active = new Set()
const DURATION = 1120
const MAX_EFFECTS = 2
const MAX_PIXELS = 600000

function reduced() { return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches }
function pathOf(value) { try { return new URL(value, 'http://aether.local').pathname.replace(/^\/api(?=\/)/, '').replace(/\/$/, '') } catch { return '' } }

export function isConfigRemoval(card, url, method, body) {
  const target = pathOf(url)
  if (method === 'DELETE') return !!card.configPath && pathOf(card.configPath) === target
  if (method === 'POST') { const action=target.split('/').at(-1);return ['remove','delete'].includes(action)&&!!card.configActions?.split(' ').includes(action)&&pathOf(card.configPath)===target.slice(0,target.lastIndexOf('/')) }
  if (method !== 'PUT' || card.configDeleting !== 'true' || !card.configCollection || pathOf(card.configCollection) !== target || !body) return false
  const list = card.configList ? body[card.configList] : body
  if (Array.isArray(list)) return !list.some(value => String(value?.[card.configKey || 'id']) === card.configId)
  return card.configList === '' && typeof list === 'object' && !Object.hasOwn(list, card.configId)
}

export function particleBudget(width, height, dpr = 1) {
  const count = Math.min(1280, Math.max(180, Math.round(width * height / 36)))
  const scale = Math.min(1.5, Math.max(1, dpr), Math.sqrt(MAX_PIXELS / ((width + 96) * (height + 96))))
  return { count, scale, width: Math.max(1, Math.floor((width + 96) * scale)), height: Math.max(1, Math.floor((height + 96) * scale)) }
}

export function cancelCardRemovals() { for (const cancel of [...active]) cancel() }

export function installCardRemovalCleanup(router) {
  router.afterEach(cancelCardRemovals)
  document.addEventListener('visibilitychange', () => { if (document.hidden) cancelCardRemovals() })
  window.addEventListener('pagehide', cancelCardRemovals)
  window.addEventListener('resize', cancelCardRemovals, {passive:true})
  matchMedia('(prefers-reduced-motion: reduce)').addEventListener?.('change', event => { if (event.matches) cancelCardRemovals() })
}

// Capture identities before the request; navigation, filtering and failed requests cannot
// cause an exit animation on newly mounted cards with the same configuration ID.
export function prepareConfigRemoval(url, method, body) {
  if (!['DELETE', 'PUT', 'POST'].includes(method) || method==='POST'&&!/\/(remove|delete)$/.test(pathOf(url)) || typeof document === 'undefined' || reduced()) return () => {}
  const candidates = [...document.querySelectorAll('[data-config-path],[data-config-collection]')]
    .filter(el => isConfigRemoval(el.dataset, url, method, body))
  return () => {
    if (document.hidden || reduced()) return
    for (const card of candidates) {
      if (!card.isConnected || card.classList.contains('config-card-removing') || active.size >= MAX_EFFECTS) continue
      try { dissolve(card) } catch { /* Visual feedback must never turn a committed delete into an API failure. */ }
    }
  }
}

const copyStyles = ['display','position','box-sizing','padding','margin','border','border-radius','background','background-color','box-shadow','color','font','line-height','letter-spacing','text-align','text-decoration','white-space','text-overflow','overflow','opacity','gap','align-items','justify-content','flex','flex-direction','flex-wrap','grid-template-columns','grid-template-rows','width','height','min-width','min-height','max-width','max-height','object-fit']

function cloneCard(card, rect) {
  const clone = card.cloneNode(true)
  const sources = [card, ...card.querySelectorAll('*')]
  const copies = [clone, ...clone.querySelectorAll('*')]
  // Large DOM cards fall back to a bounded fade; ordinary cards have far fewer nodes.
  if (sources.length > 350) return null
  for (let i = 0; i < sources.length; i++) {
    const style = getComputedStyle(sources[i]), node = copies[i]
    for (const property of copyStyles) node.style.setProperty(property, style.getPropertyValue(property))
    for(const attr of ['id','autofocus','name','data-id','data-config-id','data-config-path','data-config-collection'])node.removeAttribute(attr)
    node.style.setProperty('animation', 'none', 'important'); node.style.setProperty('transition', 'none', 'important')
    if ('disabled' in node) node.disabled = true
  }
  clone.querySelectorAll('.storage-menu,.task-menu,.context-menu,[role=menu]').forEach(el => el.remove())
  Object.assign(clone.style, { position:'absolute', left:'48px', top:'48px', width:`${rect.width}px`, height:`${rect.height}px`, minHeight:'0', maxHeight:'none', margin:'0', transform:'none', pointerEvents:'none' })
  return clone
}

function regions(card, rect) {
  const result = []
  const add = (r, color) => {
    const x = Math.max(0, r.left - rect.left), y = Math.max(0, r.top - rect.top)
    const width = Math.min(r.right, rect.right) - Math.max(r.left, rect.left)
    const height = Math.min(r.bottom, rect.bottom) - Math.max(r.top, rect.top)
    if (width > 0 && height > 0) result.push({ x, y, width, height, color })
  }
  const walker = document.createTreeWalker(card, NodeFilter.SHOW_TEXT)
  for (let node = walker.nextNode(), count = 0; node && count < 100; node = walker.nextNode()) {
    if (!node.textContent.trim() || !node.parentElement?.getClientRects().length || getComputedStyle(node.parentElement).visibility === 'hidden') continue
    const range = document.createRange(); range.selectNodeContents(node)
    for (const r of range.getClientRects()) add(r, getComputedStyle(node.parentElement).color)
    count++
  }
  for (const node of [...card.querySelectorAll('svg,img,progress')].slice(0, 24)) add(node.getBoundingClientRect(), getComputedStyle(node).color)
  return result
}

function dissolve(card) {
  const rect = card.getBoundingClientRect(), style = getComputedStyle(card)
  const opacity=Number.parseFloat(style.opacity)||1
  if (rect.width < 1 || rect.height < 1 || rect.bottom <= 0 || rect.top >= innerHeight || rect.right <= 0 || rect.left >= innerWidth || style.visibility === 'hidden') return
  const clone = cloneCard(card, rect)
  if (!clone) return
  const marks = regions(card, rect), budget = particleBudget(rect.width, rect.height, devicePixelRatio || 1)
  const layer = document.createElement('div'), panel = document.createElement('div'), canvas = document.createElement('canvas')
  layer.className = 'config-card-dissolve'; layer.setAttribute('aria-hidden', 'true'); layer.inert = true
  Object.assign(layer.style, { position:'fixed', inset:'0', pointerEvents:'none', zIndex:'10000', overflow:'hidden', contain:'strict' })
  Object.assign(panel.style,{position:'absolute',left:`${rect.left-48}px`,top:`${rect.top-48}px`,width:`${rect.width+96}px`,height:`${rect.height+96}px`,overflow:'hidden',contain:'strict'})
  canvas.width = budget.width; canvas.height = budget.height
  Object.assign(canvas.style, { position:'absolute', inset:'0', width:'100%', height:'100%', pointerEvents:'none' })
  panel.append(clone,canvas);layer.append(panel)
  const parent=card.parentElement
  const previous=new Map([...parent.children].filter(el=>el!==card).map(el=>[el,el.getBoundingClientRect()]))
  const movements=[]
  let observer, frame, timer, cleaned = false
  const cleanup = () => {
    if (cleaned) return; cleaned = true
    cancelAnimationFrame(frame); clearTimeout(timer)
    observer?.disconnect();for(const animation of movements)animation.cancel()
    layer.remove(); canvas.width = canvas.height = 0
    card.classList.remove('config-card-removing'); active.delete(cleanup)
  }
  try {
    document.body.append(layer); active.add(cleanup); card.classList.add('config-card-removing')
    observer=new MutationObserver(()=>{
      if(card.isConnected||cleaned)return
      observer.disconnect()
      const moves=[...previous].filter(([el])=>el.isConnected).map(([el,before])=>{const now=el.getBoundingClientRect();return{el,x:before.left-now.left,y:before.top-now.top}})
      for(const {el,x,y} of moves){if((Math.abs(x)>1||Math.abs(y)>1)&&el.animate)movements.push(el.animate([{transform:`translate(${x}px,${y}px)`},{transform:'translate(0,0)'}],{duration:280,delay:140,easing:'cubic-bezier(.2,.7,.3,1)',fill:'backwards'}))}
    })
    observer.observe(parent,{childList:true})
    const context = canvas.getContext('2d')
    if (!context) { cleanup(); return }
    context.scale(budget.scale, budget.scale)
    const particles = Array.from({length:budget.count}, (_, i) => {
      const mark = i % 5 && marks.length ? marks[Math.floor(Math.random() * marks.length)] : {x:0,y:0,width:rect.width,height:rect.height,color:style.borderTopColor}
      return { x:mark.x + Math.random() * mark.width, y:mark.y + Math.random() * mark.height, color:mark.color, size:1 + Math.random() * 2.2, dx:10 + Math.random() * 38, dy:-10 - Math.random() * 30, delay:Math.random() * .18, phase:Math.random() * 6.28 }
    })
    const start = performance.now()
    const draw = time => {
      const progress = Math.min(1, (time - start) / DURATION)
      if (progress >= 1 || document.hidden || reduced()) { cleanup(); return }
      context.clearRect(0, 0, rect.width + 96, rect.height + 96)
      const edge = 100 - progress * 125
      const mask = `linear-gradient(100deg,#000 ${Math.max(0, edge - 14)}%,transparent ${Math.max(0, edge + 16)}%)`
      clone.style.maskImage = mask; clone.style.webkitMaskImage = mask; clone.style.opacity = String(opacity*(1-progress)**2)
      for (const p of particles) {
        const local = Math.max(0, Math.min(1, (progress - p.delay - .32 * (1 - p.x / rect.width)) / .55))
        if (!local || local >= 1) continue
        const drift = 1 - (1 - local) ** 2
        context.globalAlpha = (1 - local) * .8 * opacity
        context.fillStyle = p.color
        const size = p.size * (1 - local * .7)
        context.fillRect(48 + p.x + p.dx * drift + Math.sin(local * 8 + p.phase) * 2, 48 + p.y + p.dy * drift, size, size)
      }
      context.globalAlpha = 1
      frame = requestAnimationFrame(draw)
    }
    frame = requestAnimationFrame(draw)
    timer = setTimeout(cleanup, DURATION + 250)
  } catch { cleanup() }
}
