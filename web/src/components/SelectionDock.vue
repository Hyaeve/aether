<script setup>
defineOptions({ inheritAttrs:false })
defineProps({ count: Number, label: String })
</script>
<template>
  <Teleport to="body"><div v-bind="$attrs" class="selection-dock" role="toolbar" :aria-label="label"><strong>已选 {{ count }} 项</strong><slot /></div></Teleport>
</template>
<style scoped>
.selection-dock{position:fixed;z-index:140;bottom:calc(12px + env(safe-area-inset-bottom,0px));left:50%;transform:translateX(-50%);display:flex;align-items:center;justify-content:center;gap:6px;max-width:calc(100vw - 24px);padding:4px 12px;border:1px solid color-mix(in srgb,var(--primary) 28%,var(--border));border-radius:999px;background:color-mix(in srgb,var(--primary) 12%,var(--surface));box-shadow:var(--shadow);font-size:13px;white-space:nowrap}
strong{font-size:13px;white-space:nowrap}
.selection-dock :deep(button.btn){display:inline-flex;align-items:center;justify-content:center;gap:6px;height:28px;min-height:28px;padding:3px 8px;border:0;border-radius:0;background:transparent;color:var(--text);box-shadow:none;font-size:13px}
.selection-dock :deep(button.btn svg){flex:none;width:16px;height:16px}
.selection-dock :deep(button.btn>span){display:inline-block;transition:transform .2s cubic-bezier(.22,1,.36,1)}
.selection-dock :deep(button.btn:is(:hover,:focus-visible):not(:disabled)){background:transparent;color:var(--primary)}
.selection-dock :deep(button.btn:is(:hover,:focus-visible):not(:disabled)>span){transform:scale(1.07)}
.selection-dock :deep(button.btn.danger-text){color:var(--red)}
.selection-dock :deep(button.btn.active){color:var(--primary)}
@media(max-width:600px){.selection-dock{gap:2px;padding:4px 8px;flex-wrap:wrap;width:max-content;max-width:calc(100vw - 16px);border-radius:24px}.selection-dock :deep(button.btn){min-height:34px;height:34px;padding-inline:6px;font-size:12px}strong{font-size:12px}}
@media(prefers-reduced-motion:reduce){.selection-dock :deep(button.btn>span){transition:none!important;transform:none!important}}
</style>
