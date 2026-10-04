<script setup>
import { ref } from 'vue'
import { api, notify } from '../lib'
import Icon from '../components/Icon.vue'
import LoginUniverse from '../components/LoginUniverse.vue'
const busy = ref(false)
async function check() {
  busy.value = true
  try { const result = await api('/version/check'); notify(result.available ? '发现新版本，可更新镜像' : result.revision && result.message.includes('一致') ? '当前版本已是最新' : result.message) }
  catch (e) { notify(e.message, true) }
  finally { busy.value = false }
}
</script>
<template>
  <section class="about-page">
    <div class="about-universe"><LoginUniverse decorative /></div>
    <div class="about-copy">
      <h1>Aether 以太</h1>
      <p>聚合多网盘的自托管存储管理服务，连接云端与本地，统一管理文件、存储与媒体任务。</p>
    </div>
    <div class="about-actions"><button class="btn primary" :disabled="busy" @click="check"><Icon name="RefreshCw" :class="{ spin: busy }" />{{ busy ? '正在检查…' : '检查更新' }}</button><a class="btn" href="https://github.com/Hyaeve/aether" target="_blank" rel="noopener noreferrer"><Icon name="ArrowUpRight" />GitHub 仓库</a></div>
  </section>
</template>
