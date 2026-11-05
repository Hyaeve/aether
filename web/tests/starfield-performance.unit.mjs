import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {compileScript,parse} from 'vue/compiler-sfc'
import {transformSync} from 'esbuild'
import * as meteor from '../src/meteor.js'
import * as starfield from '../src/starfield.js'

const descriptor=parse(readFileSync(new URL('../src/components/LoginUniverse.vue',import.meta.url),'utf8')).descriptor
const code=transformSync(compileScript(descriptor,{id:'starfield-budget-test'}).content,{format:'cjs'}).code

function sceneHarness(width=1920,height=1080,dpr=2){
  const mounted=[],unmounted=[],textures=[],scheduled=new Map(),events=new Map()
  let nextFrame=0,observer,preferenceListener
  const preference={matches:false,addEventListener:(_,fn)=>{preferenceListener=fn},removeEventListener:()=>{preferenceListener=null}}
  function makeCanvas(){
    const canvas={width:300,height:150,paints:0,fills:0}
    const noop=()=>{},gradient=()=>({addColorStop:noop})
    const context=new Proxy({clearRect:()=>canvas.paints++,fillRect:()=>canvas.fills++,createRadialGradient:gradient,createLinearGradient:gradient},{get:(target,key)=>target[key]||noop})
    canvas.getContext=()=>context
    textures.push(canvas)
    return canvas
  }
  const document={hidden:false,createElement:type=>{assert.equal(type,'canvas');return makeCanvas()},addEventListener:(name,fn)=>events.set(name,fn),removeEventListener:name=>events.delete(name)}
  const window={devicePixelRatio:dpr,matchMedia:()=>preference}
  class ResizeObserver{
    constructor(fn){this.notify=fn;observer=this}
    observe(){}
    disconnect(){this.disconnected=true}
  }
  const module={exports:{}}
  const require=name=>{
    if(name==='vue')return {ref:value=>({value}),onMounted:fn=>mounted.push(fn),onBeforeUnmount:fn=>unmounted.push(fn),onUnmounted:fn=>unmounted.push(fn)}
    if(name==='../lib')return {drivers:Array.from({length:7},(_,id)=>({id}))}
    if(name==='../meteor')return meteor
    if(name==='../starfield')return starfield
    if(name.endsWith('.vue'))return {}
    throw Error(`Unexpected component dependency ${name}`)
  }
  new Function('require','module','exports','window','document','ResizeObserver','requestAnimationFrame','cancelAnimationFrame',code)(
    require,module,module.exports,window,document,ResizeObserver,
    fn=>{scheduled.set(++nextFrame,fn);return nextFrame},id=>scheduled.delete(id))
  const component=module.exports.default.setup({decorative:true},{expose(){}})
  const panel={clientWidth:width,clientHeight:height},orbit={clientWidth:600,clientHeight:400},canvas=makeCanvas()
  component.universe.value=panel;component.scene.value=orbit;component.canvas.value=canvas
  const meteorCanvas=makeCanvas();component.meteorCanvas.value=meteorCanvas
  component.satellites.push({style:{}})
  mounted.forEach(fn=>fn());observer.notify()
  return {component,canvas,meteorCanvas,textures,panel,orbit,scheduled,
    resize:(w,h)=>{panel.clientWidth=w;panel.clientHeight=h;observer.notify()},
    notify:()=>observer.notify(),
    frame:now=>{const callbacks=[...scheduled.values()];scheduled.clear();callbacks.forEach(fn=>fn(now))},
    hidden:value=>{document.hidden=value;events.get('visibilitychange')?.()},
    reduced:value=>{preference.matches=value;preferenceListener?.()},
    unmount:()=>{unmounted.forEach(fn=>fn());assert.equal(events.size,0);assert.equal(preferenceListener,null);assert.equal(observer.disconnected,true)}
  }
}

test('cached sky stays at 30fps while the small meteor layer follows 60/120/144Hz',()=>{
  const positions=[]
  for(const hz of [60,120,144]){
    const sky=sceneHarness(),before=sky.canvas.paints
    const original=sky.component.satellites[0].style.transform
    for(let i=0;i<=hz*2;i++)sky.frame(i*1000/hz)
    const paints=sky.canvas.paints-before
    assert.ok(paints>=50&&paints<=61,`${hz}Hz produced ${paints} paints in two seconds`)
    assert.equal(sky.meteorCanvas.paints,hz*2+1)
    assert.ok(sky.meteorCanvas.width*sky.meteorCanvas.height<=800000)
    assert.notEqual(sky.component.satellites[0].style.transform,original)
    positions.push(sky.component.satellites[0].style.transform.match(/-?[\d.]+(?=px)/g).map(Number))
    sky.unmount();assert.equal(sky.scheduled.size,0)
  }
  for(const position of positions)for(let i=0;i<2;i++)assert.ok(Math.abs(position[i]-positions[0][i])<.25,'frame cap changed orbital speed')
})

test('large and retina skies bound pixels and keep fixed textures across real resizes',()=>{
  for(const [width,height,dpr] of [[1920,1080,2],[3840,2160,2],[390,844,3]]){
    const sky=sceneHarness(width,height,dpr)
    assert.ok(sky.canvas.width*sky.canvas.height<=3000000)
    const backgrounds=sky.textures.slice(2,4)
    for(const canvas of backgrounds)assert.ok(canvas.width*canvas.height<=1500000)
    const fixed=sky.textures.slice(4),counts=fixed.map(canvas=>canvas.fills),total=sky.textures.length,paints=sky.canvas.paints
    const fills=sky.textures.map(canvas=>canvas.fills)
    sky.notify();sky.notify()
    assert.equal(sky.canvas.paints,paints)
    assert.deepEqual(sky.textures.map(canvas=>canvas.fills),fills)
    sky.orbit.clientWidth=500;sky.notify()
    assert.deepEqual(sky.textures.map(canvas=>canvas.fills),fills)
    sky.resize(width-20,height-20)
    assert.equal(sky.textures.length,total,'resize allocated new texture canvases')
    assert.deepEqual(fixed.map(canvas=>canvas.fills),counts,'fixed artwork was rebuilt')
    sky.unmount()
    assert.ok(sky.textures.every(canvas=>canvas.width===0&&canvas.height===0))
  }
})

test('hidden, reduced, responsive and unmounted skies release or pause their rendering work',()=>{
  const sky=sceneHarness()
  assert.equal(sky.scheduled.size,1)
  sky.frame(100);sky.hidden(true)
  const paints=sky.canvas.paints
  assert.equal(sky.scheduled.size,0)
  sky.frame(1000);assert.equal(sky.canvas.paints,paints)
  sky.hidden(false);assert.equal(sky.scheduled.size,1)
  sky.reduced(true);assert.equal(sky.scheduled.size,0)
  const staticPaints=sky.canvas.paints
  sky.frame(2000);assert.equal(sky.canvas.paints,staticPaints)
  sky.reduced(false);assert.equal(sky.scheduled.size,1)
  sky.resize(0,0)
  assert.equal(sky.scheduled.size,0)
  assert.ok(sky.textures.every(canvas=>canvas.width===0&&canvas.height===0))
  const total=sky.textures.length;sky.notify();assert.equal(sky.textures.length,total)
  sky.resize(1440,900);assert.equal(sky.scheduled.size,1)
  assert.ok(sky.canvas.width>0)
  sky.unmount();assert.equal(sky.scheduled.size,0)
  assert.ok(sky.textures.every(canvas=>canvas.width===0&&canvas.height===0))
})
