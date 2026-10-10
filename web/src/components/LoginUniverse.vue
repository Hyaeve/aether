<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { drivers } from '../lib'
import ProviderIcon from './ProviderIcon.vue'
import GravityWell from './GravityWell.vue'
import { createMeteorBatch, meteorOpacity, rockPosition } from '../meteor'

const universe = ref(), canvas = ref(), scene = ref()
const props = defineProps({ decorative: Boolean })
const reducedMotion = ref(false)
const rings = [0.29, 0.36, 0.44]
const ringByProvider = [0, 2, 1, 2, 1, 2, 2]
const satellites = []
let observer, motionPreference, frame = 0, elapsed = 0, previous = 0
let width = 0, height = 0, sceneWidth = 0, sceneHeight = 0, ctx
let stars = []
let galaxy, spiral, moon, rocks = []
let meteorCycle = -1, meteors = []
const tilt = -18 * Math.PI / 180

function randomGenerator() {
  let seed = 15151
  return () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 4294967296 }
}

function resize() {
  if (!canvas.value || !scene.value) return
  width = universe.value.clientWidth
  height = universe.value.clientHeight
  sceneWidth = scene.value.clientWidth
  sceneHeight = scene.value.clientHeight
  const scale = Math.min(window.devicePixelRatio || 1, 2)
  canvas.value.width = Math.round(width * scale)
  canvas.value.height = Math.round(height * scale)
  ctx = canvas.value.getContext('2d')
  if (!ctx) return
  ctx.setTransform(scale, 0, 0, scale, 0, 0)
  const random = randomGenerator()
  stars = Array.from({ length: Math.min(850, Math.floor(width * height / 1000)) }, () => ({
    x: random(), y: random(), radius: .35 + random() ** 4 * 1.3,
    opacity: .2 + random() * .7, phase: random() * Math.PI * 2,
    speed: .45 + random() * 1.1, warm: random() > .89
  }))
  buildGalaxy(random, scale)
  buildMoon(random)
  buildRocks(random)
  paint(elapsed)
}

function buildMoon(random) {
  moon = document.createElement('canvas'); moon.width = 220; moon.height = 220
  const m = moon.getContext('2d'), cx = 110, radius = 100
  m.save(); m.beginPath(); m.arc(cx, cx, radius, 0, Math.PI * 2); m.clip()
  const surface = m.createRadialGradient(66, 58, 8, 130, 135, 142)
  surface.addColorStop(0, '#d2d4db'); surface.addColorStop(.6, '#7e8599'); surface.addColorStop(1, '#20283f')
  m.fillStyle = surface; m.fillRect(0, 0, 220, 220)
  for (let i = 0; i < 110; i++) {
    const x = random() * 220, y = random() * 220, r = 1.5 + random() ** 3 * 16
    const crater = m.createRadialGradient(x - r * .2, y - r * .3, 0, x, y, r)
    crater.addColorStop(0, '#30374970'); crater.addColorStop(.7, '#444b6040'); crater.addColorStop(.88, '#dde2ed65'); crater.addColorStop(1, '#68718b00')
    m.fillStyle = crater; m.beginPath(); m.arc(x, y, r, 0, Math.PI * 2); m.fill()
  }
  m.restore()
}

function buildGalaxy(random, scale) {
  galaxy = document.createElement('canvas')
  galaxy.width = canvas.value.width
  galaxy.height = canvas.value.height
  const dust = galaxy.getContext('2d')
  if (!dust) return
  dust.scale(scale, scale)
  // A cached band of tiny stellar particles forms a textured galaxy, not blurred blobs.
  const count = Math.min(76000, Math.floor(width * height / 10))
  for (let i = 0; i < count; i++) {
    const along = random()
    const spread = Math.sqrt(-2 * Math.log(Math.max(random(), .0001))) * Math.cos(random() * Math.PI * 2)
    const x = width * (.94 - along * .88) + spread * width * .19
    const y = height * (along + .055 * Math.sin(along * 8)) + spread * height * .13
    // Dark lanes break up the star cloud, giving the band an irregular structure.
    const lane = Math.abs(spread + .3 * Math.sin(along * 26))
    const core = Math.exp(-spread * spread * .55)
    const alpha = (.1 + random() * .38) * core * (.12 + .88 * Math.min(1, lane / .28))
    dust.fillStyle = i % 4 === 0 ? `rgba(232,218,204,${alpha})` : `rgba(173,185,234,${alpha})`
    const radius = .4 + random() * 1.1
    dust.fillRect(x, y, radius, radius)
  }
  // One cached face-on stellar disk rotates within a fixed inclined projection.
  spiral = document.createElement('canvas')
  spiral.width = spiral.height = 640
  const disk = spiral.getContext('2d')
  if (!disk) return
  disk.translate(320, 320)
  for (let i = 0; i < 68000; i++) {
    const core = i < 12000
    const r = core ? random() ** 1.6 * 125 : Math.sqrt(random()) * 312
    const scatter = Math.sqrt(-2 * Math.log(Math.max(random(), .0001))) * Math.cos(random() * Math.PI * 2)
    const angle = core || i % 3 === 0 ? random() * Math.PI * 2 : i % 4 * Math.PI / 2 + 3.1 * Math.sqrt(r / 286) + scatter * .65
    const distance = Math.max(0, r + (core ? 0 : scatter * 30))
    const edge = Math.min(1, Math.max(0, (distance - 250) / 70))
    const fade = Math.exp(-distance / 125) * (1 - edge * edge * (3 - 2 * edge))
    const alpha = (.14 + random() * .42) * fade * (1 + 1.3 * Math.exp(-distance / 40))
    const warmth = Math.exp(-distance / 110)
    disk.fillStyle = `rgba(${174 + Math.round(81 * warmth)},${207 + Math.round(30 * warmth)},${249 - Math.round(40 * warmth)},${alpha})`
    const size = .5 + random() * 1.3
    disk.fillRect(Math.cos(angle) * distance, Math.sin(angle) * distance, size, size)
  }
}

function buildRocks(random) {
  rocks = Array.from({length:7}, () => {
    const texture=document.createElement('canvas'); texture.width=texture.height=128
    const c=texture.getContext('2d'); c.translate(64,64); c.beginPath()
    for(let j=0;j<24;j++) { const a=j*Math.PI/12, r=43+random()*12; const x=Math.cos(a)*r,y=Math.sin(a)*r*.85; if(j)c.lineTo(x,y); else c.moveTo(x,y) }
    c.closePath(); c.clip()
    const shade=c.createRadialGradient(-24,-28,2,18,22,82)
    shade.addColorStop(0,'#777a80'); shade.addColorStop(.45,'#353b46'); shade.addColorStop(1,'#090e1b')
    c.fillStyle=shade; c.fillRect(-64,-64,128,128)
    for(let j=0;j<7000;j++) { const x=random()*128-64,y=random()*128-64; c.fillStyle=random()>.5?'#b8bfc818':'#070b1438'; c.fillRect(x,y,.5+random()*1.4,.5+random()*1.4) }
    for(let j=0;j<45;j++) { const x=random()*104-52,y=random()*104-52,r=1+random()**2*10; const crater=c.createRadialGradient(x-r*.3,y-r*.3,0,x,y,r); crater.addColorStop(0,'#080d18b0'); crater.addColorStop(.7,'#151c2869'); crater.addColorStop(.88,'#a5aeba40'); crater.addColorStop(1,'#0000'); c.fillStyle=crater;c.beginPath();c.ellipse(x,y,r,r*.8,.3,0,Math.PI*2);c.fill() }
    return texture
  })
}

function paintMeteor(time) {
  const cycle = Math.floor(time / 12)
  if (cycle !== meteorCycle) {
    meteorCycle = cycle
    meteors = createMeteorBatch()
  }
  for (const meteor of meteors) {
    const progress = (time % 12 - meteor.delay) / meteor.duration
    if (progress <= 0 || progress >= 1) continue
    // Equal pixel offsets keep every trail parallel at all viewport aspect ratios.
    const travel = width * meteor.distance
    const x = width * meteor.x - (progress - .06) * travel
    const y = height * meteor.y + progress * travel * .3
    const tailX = x + travel * meteor.tail
    const tailY = y - travel * meteor.tail * .3
    ctx.save()
    ctx.globalAlpha = meteorOpacity(progress, meteor.fadeStart, meteor.fadeEnd) * .75
    const trail = ctx.createLinearGradient(tailX, tailY, x, y)
    trail.addColorStop(0, 'rgba(173,200,234,0)')
    trail.addColorStop(1, 'rgba(218,230,249,.95)')
    ctx.strokeStyle = trail
    ctx.lineWidth = 1.2
    ctx.beginPath(); ctx.moveTo(tailX, tailY); ctx.lineTo(x, y); ctx.stroke()
    ctx.fillStyle = '#e4edff'
    ctx.beginPath(); ctx.arc(x, y, 1.3, 0, Math.PI * 2); ctx.fill()
    ctx.restore()
  }
}

function positionSatellites(time) {
  for (let i = 0; i < satellites.length; i++) {
    const el = satellites[i]
    if (!el) continue
    if (props.decorative) {
      const angle = i * Math.PI * 2 / drivers.length - Math.PI / 2 + time * Math.PI * 2 / 240
      const rx = sceneWidth * .4, ry = sceneHeight * .36
      el.style.transform = `translate3d(${sceneWidth / 2 + Math.cos(angle) * rx}px, ${sceneHeight / 2 + Math.sin(angle) * ry}px, 0)`
      continue
    }
    // Equal angular speed preserves spacing; elliptic coordinates match the visible rings.
    const angle = i * Math.PI * 2 / drivers.length - Math.PI / 2 + time * Math.PI * 2 / 180
    const radius = rings[ringByProvider[i]] * sceneWidth
    const x = Math.cos(angle) * radius
    const y = Math.sin(angle) * radius * .7
    const rotatedX = x * Math.cos(tilt) - y * Math.sin(tilt)
    const rotatedY = x * Math.sin(tilt) + y * Math.cos(tilt)
    el.style.transform = `translate3d(${sceneWidth / 2 + rotatedX}px, ${sceneHeight / 2 + rotatedY}px, 0)`
  }
}

function paint(time) {
  positionSatellites(time)
  if (!ctx || !width || !height) return
  ctx.clearRect(0, 0, width, height)
  if (moon) {
    const size = Math.min(92, width * .14)
    ctx.save(); ctx.globalAlpha = .62; ctx.drawImage(moon, width * .84, height * .08, size, size); ctx.restore()
  }
  if (galaxy) {
    ctx.save()
    ctx.globalAlpha = .8 + Math.sin(time * .16) * .15
    ctx.drawImage(galaxy, Math.sin(time * .04) * 5, Math.cos(time * .04) * 3, width, height)
    ctx.restore()
  }
  if (spiral) {
    for (const [x, y, size, inclination, phase, speed, alpha] of [
      [.76, .77, 1, -.35, 0, .035, 1],
      [.22, .88, .62, .5, 1.8, -.022, .9]
    ]) {
      const radius = Math.min(width * .2, height * .23, 210) * size
      ctx.save()
      ctx.globalAlpha = alpha
      ctx.translate(width * x, height * y)
      ctx.rotate(inclination)
      ctx.scale(1, .62)
      ctx.rotate(phase + time * speed)
      ctx.drawImage(spiral, -radius, -radius, radius * 2, radius * 2)
      ctx.restore()
    }
  }
  for (const star of stars) {
    const drift = time * (star.radius > 1 ? 1.1 : .3)
    const x = (star.x * width + drift) % width
    const y = star.y * height
    const twinkle = .56 + Math.sin(time * star.speed + star.phase) * .44
    ctx.fillStyle = star.warm ? `rgba(232,209,177,${star.opacity * twinkle})` : `rgba(199,214,243,${star.opacity * twinkle})`
    ctx.beginPath()
    ctx.arc(x, y, star.radius, 0, Math.PI * 2)
    ctx.fill()
    if (star.radius > 1.45) {
      ctx.strokeStyle = `rgba(199,214,243,${star.opacity * twinkle * .24})`
      ctx.lineWidth = .7
      ctx.beginPath()
      ctx.moveTo(x - 4, y); ctx.lineTo(x + 4, y)
      ctx.moveTo(x, y - 4); ctx.lineTo(x, y + 4)
      ctx.stroke()
    }
  }
  if (!reducedMotion.value) paintMeteor(time)
  // Faint constellation links stay away from the central brand and form.
  for (const points of [
    [[.07, .17], [.12, .15], [.17, .18], [.21, .23], [.29, .24], [.3, .17], [.23, .16], [.21, .23]],
    [[.72, .72], [.77, .79], [.82, .7], [.87, .77], [.92, .68]]
  ]) {
    ctx.strokeStyle = 'rgba(151,170,214,.13)'
    ctx.lineWidth = .7
    ctx.beginPath()
    points.forEach(([x, y], i) => {
      if (i === 0) ctx.moveTo(x * width, y * height)
      else ctx.lineTo(x * width, y * height)
    })
    ctx.stroke()
    for (const [x, y] of points) {
      ctx.fillStyle = 'rgba(189,204,239,.65)'
      ctx.beginPath(); ctx.arc(x * width, y * height, 1.2, 0, Math.PI * 2); ctx.fill()
    }
  }
  // Cached rocks drift along the left, lower and right edges.
  for (let i = 0; i < rocks.length; i++) {
    ctx.save()
    const position = rockPosition(i, time, width, height)
    ctx.translate(position.x, position.y)
    ctx.rotate(time * .012 + i)
    const size=10+i%3*4
    if(rocks[i])ctx.drawImage(rocks[i],-size/2,-size/2,size,size)
    ctx.restore()
  }
}

function tick(now) {
  if (!previous) previous = now
  elapsed += Math.min(now - previous, 100) / 1000
  previous = now
  paint(elapsed)
  frame = requestAnimationFrame(tick)
}

function syncMotion() {
  cancelAnimationFrame(frame)
  previous = 0
  const hidden = document.hidden || !universe.value?.clientWidth
  if (!reducedMotion.value && !hidden) frame = requestAnimationFrame(tick)
  else paint(elapsed)
}
function preferenceChanged() {
  reducedMotion.value = motionPreference.matches
  syncMotion()
}

onMounted(() => {
  motionPreference = window.matchMedia('(prefers-reduced-motion: reduce)')
  reducedMotion.value = motionPreference.matches
  motionPreference.addEventListener('change', preferenceChanged)
  document.addEventListener('visibilitychange', syncMotion)
  observer = new ResizeObserver(() => { resize(); syncMotion() })
  observer.observe(universe.value)
  observer.observe(scene.value)
})
onUnmounted(() => {
  cancelAnimationFrame(frame)
  observer?.disconnect()
  motionPreference?.removeEventListener('change', preferenceChanged)
  document.removeEventListener('visibilitychange', syncMotion)
})
</script>

<template>
  <section ref="universe" class="login-universe" :class="{ 'motion-paused': reducedMotion, 'universe-decorative': decorative }">
    <canvas ref="canvas" class="starfield" aria-hidden="true" />
    <div v-if="!decorative" class="login-brand"><img src="/aether.svg" alt="" /><span>Aether<small>以太 · 存储工作空间</small></span></div>
    <div ref="scene" class="orbital-system" aria-label="围绕以太运行的存储连接器">
      <div class="orbital-float">
        <div v-for="(radius, i) in rings" :key="i" class="orbit" :style="{ width: `${radius * 200}%`, height: `${radius * 200 * .7 * 1.15}%` }" aria-hidden="true" />
        <div class="orbital-center"><GravityWell /><img src="/aether.svg" alt="Aether" /></div>
        <div v-for="(d, i) in drivers" :key="d.id" :ref="el => satellites[i] = el" class="satellite" :class="{ 'satellite-framed': ['openlist', 'webdav', 'local'].includes(d.id) }" :data-provider="d.id" role="img" :aria-label="d.name">
          <div class="satellite-body"><ProviderIcon :type="d.id" /></div>
        </div>
      </div>
    </div>
    <div v-if="!decorative" class="universe-caption"><span>EVERY SPACE, CONNECTED</span><h2>万千存储，同一片以太。</h2><p>让数据流动，让空间相连。</p></div>
    <footer v-if="!decorative">AETHER WORKSPACE <span>01 / CONNECT YOUR SPACE</span></footer>
  </section>
</template>

<style scoped>
.login-universe { background: #121827; isolation: isolate; }
.universe-decorative { display: flex; min-height: 100%; height: 100%; padding: 0; }
.universe-decorative .orbital-system { position: absolute; top: 16px; left: 50%; transform: translateX(-50%); width: calc(100% - 32px); height: max(230px, calc(100% - 220px)); max-height: 420px; max-width: 680px; aspect-ratio: auto; margin: 0; pointer-events: none; }
.universe-decorative .orbit { display: none; }
.universe-decorative .orbital-center { width: 72px; height: 72px; }
.universe-decorative .orbital-center img { width: 72px; height: 72px; }
.starfield { position: absolute; inset: 0; width: 100%; height: 100%; z-index: -1; pointer-events: none; }
.login-brand, .universe-caption, footer { position: relative; z-index: 1; }
.orbital-system { max-width: 650px; aspect-ratio: 1.15; width: 100%; margin: auto; }
.orbital-float { position: absolute; inset: 0; animation: orbital-float 14s ease-in-out infinite; }
.orbit { border-color: #8e9fc331; transform: translate(-50%, -50%) rotate(-18deg); }
.orbit:nth-child(2) { border-color: #8e9fc33b; }
.orbit:nth-child(3) { border-color: #8e9fc32b; }
.orbital-center { border: 0; background: transparent; box-shadow: none; border-radius: 0; isolation: isolate; }
.satellite { left: 0; top: 0; display: block; width: 0; height: 0; will-change: transform; }
.satellite-body { position: absolute; left: 0; top: 0; transform: translate(-50%, -50%); display: grid; justify-items: center; }
.satellite .provider-icon { width: 48px; height: 48px; padding: 0; background: transparent; border: 0; box-shadow: none; border-radius: 0; }
.satellite :deep(.provider-logo) { width: 100%; height: 100%; }
.satellite :deep(svg) { width: 36px; height: 36px; }
.satellite-framed .provider-icon { background: #fff; border-radius: 25%; padding: 7px; box-sizing: border-box; overflow: hidden; }
.satellite-framed :deep(svg) { width: 100%; height: 100%; }
.motion-paused .orbital-float { animation-play-state: paused; }
@keyframes orbital-float { 0%, 100% { transform: translateY(-5px); } 50% { transform: translateY(6px); } }
@media (max-width: 960px) {
  .orbital-center { width: 48px; height: 48px; border-radius: 12px; }
  .orbital-center img { width: 40px; height: 40px; }
  .satellite .provider-icon { width: 35px; height: 35px; }
  .satellite :deep(svg) { width: 29px; height: 29px; }
  .satellite-framed .provider-icon { padding: 5px; }
}
.satellite[data-provider="openlist"] .provider-icon { padding: 1px; }
@media (prefers-reduced-motion: reduce) { .orbital-float { animation: none; } }
</style>
