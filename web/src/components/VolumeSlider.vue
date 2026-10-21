<script setup>
defineProps({ modelValue: { type: Number, default: 1 }, label: String })
const emit = defineEmits(['update:modelValue'])
</script>
<template>
  <div class="volume-slider">
    <small>{{ Math.round(modelValue * 100) }}</small>
    <div class="volume-slider-control">
      <span class="volume-slider-track" aria-hidden="true"><i :style="{ height: `${modelValue * 100}%` }" /></span>
      <input type="range" min="0" max="1" step="0.01" :value="modelValue" :aria-label="label" @input="emit('update:modelValue', Number($event.target.value))" />
    </div>
  </div>
</template>
<style scoped>
.volume-slider { display:flex; flex-direction:column; align-items:center; gap:8px; width:100%; height:100%; min-height:0; color:#f1f4ff; }
.volume-slider small { flex:none; font-size:12px; line-height:16px; font-variant-numeric:tabular-nums; }
.volume-slider-control { position:relative; flex:1; min-height:0; width:26px; }
.volume-slider-track { position:absolute; inset:0 auto 0 11px; width:4px; border-radius:4px; overflow:hidden; background:#ffffff35; pointer-events:none; }
.volume-slider-track i { position:absolute; bottom:0; left:0; right:0; background:#c0ccff; border-radius:inherit; }
.volume-slider-control input { position:absolute; inset:0; appearance:none; writing-mode:vertical-lr; direction:rtl; width:26px; height:100%; min-height:0; margin:0; padding:0; border:0; border-radius:0; box-shadow:none; background:transparent; cursor:pointer; }
.volume-slider-control input::-webkit-slider-runnable-track { background:transparent; border:0; width:4px; }
.volume-slider-control input::-webkit-slider-thumb { appearance:none; width:0; height:0; border:0; background:transparent; }
.volume-slider-control input::-moz-range-track,.volume-slider-control input::-moz-range-progress { background:transparent; border:0; width:4px; }
.volume-slider-control input::-moz-range-thumb { width:0; height:0; border:0; background:transparent; }
.volume-slider-control input:focus-visible { outline:2px solid #c0ccff; outline-offset:3px; border-radius:4px; }
</style>
