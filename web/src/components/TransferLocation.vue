<script setup>
import { computed } from 'vue'
import Icon from './Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
const props=defineProps({ location:{type:Object,required:true} })
const provider=computed(()=>['115','quark','mobile','tianyi','local','webdav','openlist'].includes(props.location.type))
const directory=computed(()=>props.location.directory || (['browser','client'].includes(props.location.type)?'目录不可获取':'目录未提供'))
const hint=computed(()=>[props.location.storage,props.location.path || directory.value].filter(Boolean).join(' / '))
</script>
<template>
  <div class="transfer-location" :data-tooltip="hint">
    <span class="transfer-location-storage"><ProviderIcon v-if="provider" :type="location.type" compact /><Icon v-else :name="location.type==='browser'?'Globe':location.type==='client'?'Monitor':'FolderSearch'" :size="18" /><span>{{location.storage || '目标未提供'}}</span></span>
    <small><Icon name="FolderOpen" :size="13" /><span>{{directory}}</span></small>
  </div>
</template>
<style scoped>
.transfer-location{min-width:0;display:grid;gap:3px}
.transfer-location-storage,.transfer-location small{display:flex;align-items:center;gap:6px;min-width:0}
.transfer-location-storage>svg{flex:none;margin:0 2px}
.transfer-location-storage>span:last-child,.transfer-location small>span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.transfer-location small{font-size:12px;color:var(--muted);line-height:16px;margin:0;padding-left:3px}
.transfer-location small>svg{flex:none}
</style>
