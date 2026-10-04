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
  ctx.translate(width / 2, height / 2)
  ctx.scale(1, .27)
  const radius = width * .48
  // A low elliptical pool of light sits beneath the emblem, without a mesh.
  const shade = ctx.createRadialGradient(0, 0, 0, 0, 0, radius)
  shade.addColorStop(0, '#8199d838')
  shade.addColorStop(.25, '#8199d830')
  shade.addColorStop(.43, '#8199d824')
  shade.addColorStop(.68, '#657dc510')
  shade.addColorStop(1, '#617dc500')
  ctx.fillStyle = shade
  ctx.fillRect(-radius, -radius, radius * 2, radius * 2)
}
onMounted(() => { observer = new ResizeObserver(draw); observer.observe(canvas.value); draw() })
onUnmounted(() => observer?.disconnect())
</script>
<template><canvas ref="canvas" class="gravity-well" aria-hidden="true" /></template>
<style scoped>
.gravity-well { position: absolute; width: 100px; height: 32px; left: 50%; top: calc(100% - 12px); transform: translateX(-50%); pointer-events: none; z-index: -1; }
@media (max-width:960px) { .gravity-well { width: 80px; height: 26px; } }
@media (max-width:480px) { .gravity-well { width: 76px; height: 24px; } }
</style>
