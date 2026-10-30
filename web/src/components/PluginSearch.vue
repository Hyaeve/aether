<script setup>
import { nextTick, onUnmounted, ref, watch } from 'vue'
import Modal from './Modal.vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import Rapid115Icon from './Rapid115Icon.vue'

defineProps({ items: { type: Array, default: () => [] }, busy: Boolean, market: Boolean })
const emit = defineEmits(['search', 'select', 'repositories'])
const open = ref(false), query = ref(''), input = ref(null)
let timer
watch(query, value => { clearTimeout(timer); timer = setTimeout(() => emit('search', value), 200) })
onUnmounted(() => clearTimeout(timer))
async function show() {
  query.value = ''; open.value = true; emit('search', '')
  await nextTick(); input.value?.focus()
}
function select(item) { open.value = false; emit('select', item) }
function move(event) {
  const nodes = [...event.currentTarget.closest('[role=dialog]').querySelectorAll('input, .plugin-search-result')]
  const index = nodes.indexOf(document.activeElement)
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault(); nodes[(index + (event.key === 'ArrowDown' ? 1 : nodes.length - 1)) % nodes.length]?.focus()
  }
}
</script>
<template>
  <div class="plugin-float-tools">
    <button v-if="market" class="plugin-float-repository" aria-label="插件仓库" data-tooltip="插件仓库" @click="emit('repositories')"><Icon name="Warehouse" :size="21" /></button>
    <button class="plugin-float-search" :aria-label="market ? '搜索市场插件' : '搜索辅助工具'" :data-tooltip="market ? '搜索市场插件' : '搜索辅助工具'" @click="show"><Icon name="Search" :size="23" /></button>
  </div>
  <Modal v-if="open" standard :title="market ? '搜索市场插件' : '搜索辅助工具'" @close="open = false">
    <template #header><header class="plugin-search-header" @keydown="move"><Icon name="Search" :size="23" /><input ref="input" v-model="query" aria-label="插件搜索关键词" placeholder="按插件名称或描述搜索" autocomplete="off" /><button class="icon-btn" aria-label="关闭" @click="open=false"><Icon name="X" :size="23" /></button></header></template>
    <div class="modal-body plugin-search-body" @keydown="move">
      <div class="plugin-search-results" :aria-busy="busy">
        <p v-if="busy" class="small-empty">正在搜索…</p>
        <p v-else-if="!items.length" class="small-empty">没有匹配的插件</p>
        <button v-for="item in items" :key="item.repository ? item.repository + ':' + item.id : item.name" class="plugin-search-result" @click="select(item)">
          <span class="plugin-search-symbol"><Rapid115Icon v-if="item.name==='115 秒传中心'" /><ProviderIcon v-else-if="item.name.startsWith('115')" type="115" compact unframed /><ProviderIcon v-else-if="item.name==='夸克 STRM 接管'" type="quark" compact unframed /><template v-else><Icon :name="item.iconName || 'Puzzle'" :size="30" /><img v-if="market&&item.icon" :src="item.icon" alt="" referrerpolicy="no-referrer" @error="$event.target.hidden=true" /></template></span>
          <span class="plugin-search-copy"><span class="plugin-search-title"><strong>{{ item.name }}</strong><small v-if="item.version">v{{item.version}}</small></span><span class="plugin-search-detail"><small v-if="item.subtitle||item.source" class="plugin-search-category">{{item.subtitle||item.source}}</small><span>{{item.description||item.detail}}</span></span></span>
        </button>
      </div>
    </div>
  </Modal>
</template>
<style scoped>
.plugin-float-tools{position:fixed;bottom:calc(28px + env(safe-area-inset-bottom,0px));right:calc(28px + env(safe-area-inset-right,0px));z-index:32;display:flex;flex-direction:column;align-items:center;gap:8px}.plugin-float-tools>button{width:46px;height:46px;border:1px solid var(--border);background:var(--surface);color:var(--text);box-shadow:var(--shadow);border-radius:50%;display:grid;place-items:center;transition:color .16s,transform .16s,opacity .16s}.plugin-float-tools>button:hover{color:var(--primary);transform:translateY(-2px)}.plugin-float-repository{opacity:0;pointer-events:none;transform:translateY(8px)}.plugin-float-tools:hover .plugin-float-repository,.plugin-float-tools:focus-within .plugin-float-repository{opacity:1;pointer-events:auto;transform:none}
:global(.modal:has(.plugin-search-header)){display:flex;flex-direction:column;overflow:hidden;background:var(--layer-shell)}
.plugin-search-header{display:flex;align-items:center;gap:10px;padding:12px 16px;background:var(--input);flex:none}
.plugin-search-header>svg{color:var(--muted);flex:none}.plugin-search-header input{flex:1;min-width:0;width:0;height:40px;padding:0;border:0;box-shadow:none;background:transparent;font-size:16px}.plugin-search-header input:focus{box-shadow:none;outline:none}.plugin-search-header:focus-within{box-shadow:inset 0 -2px var(--primary)}.plugin-search-header .icon-btn{flex:none;width:40px;height:40px;color:var(--muted)}
.plugin-search-body{padding:8px;overflow:auto;min-height:0}.plugin-search-results{min-height:150px;max-height:min(560px,calc(100dvh - 170px))}.plugin-search-result{width:100%;display:flex;align-items:center;text-align:left;gap:14px;padding:14px 12px;min-height:88px;border:0;background:transparent;border-radius:8px;color:var(--text)}.plugin-search-result:hover,.plugin-search-result:focus-visible{background:var(--primary-soft)}.plugin-search-result:focus-visible{outline:2px solid var(--primary);outline-offset:-2px}
.plugin-search-symbol{width:44px;height:44px;flex:none;position:relative;display:grid;place-items:center;color:var(--primary)}.plugin-search-symbol img{position:absolute;inset:0;width:44px;height:44px;object-fit:contain}.plugin-search-symbol img[hidden]{display:none}.plugin-search-symbol :deep(.provider-icon){width:44px;height:44px}.plugin-search-symbol :deep(.provider-logo){width:40px;height:40px}.plugin-search-copy{min-width:0;flex:1;display:grid;gap:8px}.plugin-search-title{display:flex;align-items:center;gap:8px;min-width:0}.plugin-search-title strong{font-size:16px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.plugin-search-title>small{font-size:13px;color:var(--muted);flex:none}.plugin-search-detail{display:flex;align-items:baseline;gap:8px;min-width:0;font-size:13px;line-height:20px;color:var(--muted)}.plugin-search-category{font-size:12px;flex:none;max-width:45%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:1px 6px;border-radius:4px;background:var(--primary-soft);color:var(--primary)}.plugin-search-detail>span{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
@media(max-width:480px){.plugin-search-result{padding:12px 8px;gap:10px}.plugin-search-title strong{font-size:15px}.plugin-search-detail{flex-wrap:wrap;gap:3px}.plugin-search-category{max-width:100%}}
.plugin-search-symbol :deep(.provider-icon[data-provider="115"] .provider-logo){width:40px;height:40px}
@media(hover:none){.plugin-float-repository{opacity:1;pointer-events:auto;transform:none}}@media(prefers-reduced-motion:reduce){.plugin-float-tools>button{transition:none}}
</style>
