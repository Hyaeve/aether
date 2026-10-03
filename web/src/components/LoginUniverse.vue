<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { drivers } from '../lib'
import ProviderIcon from './ProviderIcon.vue'
import { createMeteorBatch, meteorOpacity } from '../meteor'

const universe = ref(), canvas = ref(), scene = ref()
defineProps({ decorative: Boolean })
const reducedMotion = ref(false)
const rings = [0.29, 0.36, 0.44]
const ringByProvider = [0, 2, 1, 2, 1, 2, 0]
const satellites = []
let observer, motionPreference, frame = 0, elapsed = 0, previous = 0
let width = 0, height = 0, sceneWidth = 0, sceneHeight = 0, ctx
let stars = []
let galaxy
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
  paint(elapsed)
}

function buildGalaxy(random, scale) {
  galaxy = document.createElement('canvas')
  galaxy.width = canvas.value.width
  galaxy.height = canvas.value.height
  const dust = galaxy.getContext('2d')
  if (!dust) return
  dust.scale(scale, scale)
  // A cached band of tiny stellar particles forms a textured galaxy, not blurred blobs.
  const count = Math.min(42000, Math.floor(width * height / 18))
  for (let i = 0; i < count; i++) {
    const along = random()
    const spread = Math.sqrt(-2 * Math.log(Math.max(random(), .0001))) * Math.cos(random() * Math.PI * 2)
    const x = width * (1.12 - along * 1.3) + spread * width * .055
    const y = height * (along + .055 * Math.sin(along * 8)) + spread * height * .025
    // Dark lanes break up the star cloud, giving the band an irregular structure.
    if (Math.abs(spread + .24 * Math.sin(along * 26)) < .15) continue
    const core = Math.exp(-spread * spread * .7)
    const alpha = (.06 + random() * .28) * core
    dust.fillStyle = i % 7 === 0 ? `rgba(220,194,163,${alpha})` : `rgba(158,178,222,${alpha})`
    const radius = .4 + random() * 1.1
    dust.fillRect(x, y, radius, radius)
  }
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
    const x = width * meteor.x - progress * travel
    const y = height * meteor.y + progress * travel * .42
    const tailX = x + travel * meteor.tail
    const tailY = y - travel * meteor.tail * .42
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
  if (galaxy) {
    ctx.save()
    ctx.globalAlpha = .8 + Math.sin(time * .16) * .15
    ctx.drawImage(galaxy, Math.sin(time * .04) * 5, Math.cos(time * .04) * 3, width, height)
    ctx.restore()
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
    [[.1, .2], [.18, .16], [.26, .22], [.31, .18]],
    [[.73, .72], [.82, .76], [.86, .69], [.93, .73]]
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
        <div class="orbital-center"><img src="/aether.svg" alt="Aether" /></div>
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
.universe-decorative .orbital-system { visibility: hidden; }
.starfield { position: absolute; inset: 0; width: 100%; height: 100%; z-index: -1; pointer-events: none; }
.login-brand, .universe-caption, footer { position: relative; z-index: 1; }
.orbital-system { max-width: 650px; aspect-ratio: 1.15; width: 100%; margin: auto; }
.orbital-float { position: absolute; inset: 0; animation: orbital-float 14s ease-in-out infinite; }
.orbit { border-color: #8e9fc331; transform: translate(-50%, -50%) rotate(-18deg); }
.orbit:nth-child(2) { border-color: #8e9fc33b; }
.orbit:nth-child(3) { border-color: #8e9fc32b; }
.orbital-center { border: 0; background: transparent; box-shadow: none; border-radius: 0; }
.satellite { left: 0; top: 0; display: block; width: 0; height: 0; will-change: transform; }
.satellite-body { position: absolute; left: 0; top: 0; transform: translate(-50%, -50%); display: grid; justify-items: center; }
.satellite .provider-icon { width: 48px; height: 48px; padding: 0; background: transparent; border: 0; box-shadow: none; border-radius: 0; }
.satellite :deep(.provider-logo) { width: 100%; height: 100%; }
.satellite :deep(svg) { width: 36px; height: 36px; }
.satellite-framed .provider-icon { background: #fff; border-radius: 8px; padding: 7px; box-sizing: border-box; }
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
