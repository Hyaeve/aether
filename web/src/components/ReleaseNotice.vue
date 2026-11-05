<script setup>
import {onMounted,onUnmounted,ref} from 'vue'
import {api} from '../lib'
import Modal from './Modal.vue'
import {version as builtVersion} from '../../package.json'
const release=ref(null)
let alive=true
function close(){if(release.value){try{localStorage.setItem('aether-release-seen',release.value.version)}catch{}release.value=null}}
onMounted(async()=>{try{if(localStorage.getItem('aether-release-seen')===builtVersion)return}catch{}try{const value=await api('/version/notes');if(alive&&value.version&&value.body){let seen;try{seen=localStorage.getItem('aether-release-seen')}catch{}if(seen!==value.version)release.value=value}}catch{}})
onUnmounted(()=>{alive=false})
</script>
<template><Modal v-if="release" :title="`更新日志 · v${release.version}`" standard @close="close"><div class="modal-body release-notice-body"><template v-for="(line,i) in release.body.split('\n')" :key="i"><h3 v-if="/^#{1,4} /.test(line)">{{line.replace(/^#{1,4} /,'')}}</h3><p v-else-if="line.trim()">{{line.replace(/^[-*] /,'· ')}}</p></template></div><footer class="modal-footer"><button class="btn primary" @click="close">知道了</button></footer></Modal></template>
<style scoped>:global(.modal:has(.release-notice-body)){display:flex;flex-direction:column;overflow:hidden}.release-notice-body{min-height:0;overflow:auto;scrollbar-width:none}.release-notice-body::-webkit-scrollbar{display:none}.release-notice-body h3{font-size:16px;margin:14px 0}.release-notice-body p{font-size:13px;line-height:1.85;margin:7px 0;overflow-wrap:anywhere}</style>
