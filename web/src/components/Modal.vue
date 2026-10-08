<script setup>
import { onMounted, onUnmounted, onUpdated, ref } from 'vue'
import Icon from './Icon.vue'
defineProps({ title: String, eyebrow: String, wide: Boolean, compact: Boolean, confirmation: Boolean })
const emit = defineEmits(['close'])
const panel = ref()
let previous
function styleActions() {
  panel.value?.querySelectorAll('.modal-footer .btn').forEach(button => {
    const text = button.textContent.trim()
    button.classList.toggle('cancel', /^(取消|关闭|放弃修改)$/.test(text))
    button.classList.toggle('test-connection', /^(测试连接|连接测试|测试)$/.test(text))
  })
}
onUpdated(styleActions)
onMounted(styleActions)
function key(event) {
  if ([...document.querySelectorAll('.modal')].at(-1) !== panel.value) return
  if (event.key === 'Escape') emit('close')
  if (event.key === 'Tab') {
    const nodes = [...panel.value.querySelectorAll('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea,a[href]')].filter(el => el.offsetParent !== null)
    if (!nodes.length) return
    if (event.shiftKey && document.activeElement === nodes[0]) { event.preventDefault(); nodes.at(-1).focus() }
    else if (!event.shiftKey && document.activeElement === nodes.at(-1)) { event.preventDefault(); nodes[0].focus() }
  }
}
onMounted(() => { previous = document.activeElement; document.body.style.overflow = 'hidden'; panel.value.focus(); document.addEventListener('keydown', key) })
onUnmounted(() => { if (!document.querySelector('.modal')) document.body.style.overflow = ''; document.removeEventListener('keydown', key); previous?.focus() })
</script>
<template>
  <Teleport to="body">
    <div class="modal-backdrop" :class="{ 'confirmation-backdrop': confirmation }" @mousedown.self="emit('close')">
      <section ref="panel" tabindex="-1" role="dialog" aria-modal="true" :aria-label="title" class="modal" :class="{ wide, 'compact-modal': compact }">
        <header class="modal-header"><div><span v-if="eyebrow" class="eyebrow">{{ eyebrow }}</span><h2>{{ title }}</h2></div><button class="icon-btn" aria-label="关闭" title="关闭" @click="emit('close')"><Icon name="X" /></button></header>
        <slot />
      </section>
    </div>
  </Teleport>
</template>
