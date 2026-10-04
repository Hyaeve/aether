<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
const canvas = ref(null)
let observer
function draw() {
  const el = canvas.value
  const width = el.clientWidth, height = el.clientHeight
  if (!width || !height) return
  const scale = Math.min(devicePixelRatio || 1, 2)
  el.width = Math.round(width * scale); el.height = Math.round(height * scale)
  const ctx = el.getContext('2d')
  if (!ctx) return
  ctx.scale(scale, scale)
  ctx.translate(width / 2, height * .35)
  const radius = width * .46, depth = height * .38
  // Project a depressed membrane: outer rings stay flat, inner rings sink.
  function point(r, angle) {
    const depression = depth * Math.exp(-Math.pow(r / (radius * .38), 2))
    return [Math.cos(angle) * r, Math.sin(angle) * r * .38 + depression]
  }
  const shade = ctx.createRadialGradient(0, depth * .75, 4, 0, 0, radius)
  shade.addColorStop(0, '#030714f5')
  shade.addColorStop(.32, '#080f26e6')
  shade.addColorStop(.74, '#111c3540')
  shade.addColorStop(1, '#111c3500')
  ctx.fillStyle = shade
  ctx.fillRect(-radius, -height * .35, radius * 2, height)
  for (let i = 1; i <= 22; i++) {
    const r = radius * i / 22
    ctx.beginPath()
    for (let step = 0; step <= 96; step++) {
      const [x, y] = point(r, step / 96 * Math.PI * 2)
      if (!step) ctx.moveTo(x, y); else ctx.lineTo(x, y)
    }
    ctx.strokeStyle = `rgba(144,166,223,${.10 + .22 * Math.sin(i / 22 * Math.PI)})`
    ctx.lineWidth = .7
    ctx.stroke()
  }
  for (let ray = 0; ray < 28; ray++) {
    ctx.beginPath()
    for (let step = 2; step <= 50; step++) {
      const [x, y] = point(radius * step / 50, ray / 28 * Math.PI * 2)
      if (step === 2) ctx.moveTo(x, y); else ctx.lineTo(x, y)
    }
    ctx.strokeStyle = 'rgba(128,153,215,.16)'
    ctx.stroke()
  }
}
onMounted(() => { observer = new ResizeObserver(draw); observer.observe(canvas.value); draw() })
onUnmounted(() => observer?.disconnect())
</script>
<template><canvas ref="canvas" class="gravity-well" aria-hidden="true" /></template>
<style scoped>
.gravity-well { position: absolute; width: 310px; height: 190px; left: 50%; top: 50%; transform: translate(-50%,-44%); pointer-events: none; z-index: -1; }
@media (max-width:960px) { .gravity-well { width: 230px; height: 150px; } }
@media (max-width:480px) { .gravity-well { width: 200px; height: 135px; } }
</style>
