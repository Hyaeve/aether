<script setup>
import { onMounted, ref } from 'vue'
import { api, notify } from '../lib'
import Icon from '../components/Icon.vue'
import LoginUniverse from '../components/LoginUniverse.vue'
const version = ref(''), result = ref(null), error = ref(''), busy = ref(false)
onMounted(async () => {
  try { version.value = (await api('/version')).version } catch (e) { error.value = e.message }
})
async function check() {
  busy.value = true; error.value = ''; result.value = null
  try { result.value = await api('/version/check'); version.value = result.value.current; notify(`${result.value.message}${result.value.latest ? `（${result.value.latest}）` : ''}`) }
  catch (e) { notify(e.message, true) }
  finally { busy.value = false }
}
</script>
<template>
  <section class="about-page">
    <div class="about-universe"><LoginUniverse decorative /></div>
    <div class="about-copy">
      <img src="/aether.svg" alt="" /><h1>Aether 以太</h1>
      <p>聚合多网盘的自托管存储管理服务，连接云端与本地，统一管理文件、存储与媒体任务。</p>
      <span class="about-version">当前版本 {{ version ? `v${version.replace(/^v/, '')}` : '读取中…' }}</span>
      <div class="about-actions"><button class="btn primary" :disabled="busy" @click="check"><Icon name="RefreshCw" :class="{ spin: busy }" />{{ busy ? '正在检查…' : '检查更新' }}</button><a class="btn" href="https://github.com/Hyaeve/aether" target="_blank" rel="noopener noreferrer"><Icon name="ArrowUpRight" />GitHub 仓库</a></div>
      <p v-if="error" class="error-message" role="alert">{{ error }}</p>
    </div>
  </section>
</template>
