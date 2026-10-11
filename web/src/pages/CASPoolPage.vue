<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { date, state } from '../lib'
import CASPool from '../components/CASPool.vue'
import Icon from '../components/Icon.vue'
import ScrollRail from '../components/ScrollRail.vue'

const router = useRouter(), pool = ref(null), backButton = ref(null), temporaryList = ref(null)
const busy = ref(false), ready = ref(false), statusBusy = ref(false), cleaning = ref(false)
const items = ref([]), statusError = ref(''), cleanupMessage = ref('')
const playing = computed(() => items.value.filter(item => item.active).length)
const expired = computed(() => items.value.filter(item => !item.active && Date.parse(item.expiresAt) <= Date.now()).length)
let active = true, generation = 0, controller, timer
function back() { router.push('/tasks/cas') }
function storageName(id) { return state.storages.find(storage => storage.id === id)?.name || id }
async function statusRequest(cleanup = false) {
  if (cleaning.value || (!cleanup && statusBusy.value)) return
  controller?.abort()
  const token = ++generation, abort = new AbortController()
  controller = abort; statusBusy.value = true; cleaning.value = cleanup; statusError.value = ''
  if (cleanup) cleanupMessage.value = ''
  async function read(path, method = 'GET') {
    const response = await fetch(`/api/cas/${path}`, { method, credentials: 'same-origin', signal: abort.signal })
    let result
    try { result = await response.json() } catch { throw new Error('服务响应异常，请检查后端连接') }
    if (!response.ok) throw new Error(result.error || '请求失败')
    return result
  }
  try {
    let removed
    if (cleanup) removed = (await read('cleanup', 'POST')).removed
    const result = await read('status')
    if (!active || token !== generation) return
    items.value = Array.isArray(result.items) ? result.items : []
    if (cleanup) cleanupMessage.value = `已清理 ${removed ?? 0} 个过期临时文件`
  } catch (error) {
    if (active && token === generation && error.name !== 'AbortError') statusError.value = error.message
  } finally {
    if (active && token === generation) { statusBusy.value = false; cleaning.value = false; controller = undefined }
  }
}
watch(() => state.authenticated, () => { generation++; controller?.abort(); items.value = []; back() }, { flush: 'sync' })
onMounted(() => { backButton.value?.focus({ preventScroll: true }); statusRequest(); timer = setInterval(() => statusRequest(), 10000) })
onBeforeUnmount(() => { active = false; generation++; controller?.abort(); clearInterval(timer) })
</script>
<template>
  <div class="cas-pool-page" @keydown.esc="!$event.defaultPrevented && back()">
    <header class="cas-pool-page-heading">
      <button ref="backButton" type="button" class="icon-btn" aria-label="返回 CAS 任务" @click="back"><Icon name="ChevronRight" :size="24" /></button>
      <h2>存储绑定池</h2>
      <button type="button" class="btn primary" :disabled="busy || !ready" @click="pool?.save()"><Icon :name="busy ? 'LoaderCircle' : 'Save'" />保存绑定池</button>
    </header>
    <section class="cas-temporary-strip" aria-label="CAS 临时文件" :aria-busy="statusBusy">
      <header>
        <Icon name="Database" /><h3>临时文件</h3>
        <dl><div><dt>总计</dt><dd>{{ items.length }}</dd></div><div><dt>播放中</dt><dd>{{ playing }}</dd></div><div><dt>已过期</dt><dd>{{ expired }}</dd></div></dl>
        <div class="cas-temporary-actions"><button class="icon-btn" aria-label="刷新临时文件" :disabled="statusBusy" @click="statusRequest()"><Icon name="RefreshCw" /></button><button class="btn" :disabled="statusBusy" @click="statusRequest(true)"><Icon name="Trash2" />{{ cleaning ? '清理中...' : '清理过期项' }}</button></div>
      </header>
      <div v-if="items.length" class="cas-temporary-frame">
        <div ref="temporaryList" class="cas-temporary-list" tabindex="0">
          <article v-for="(item, index) in items" :key="`${item.storageId}-${item.name}-${index}`" class="cas-temporary-item"><span class="cas-temporary-name">{{ item.name }}</span><span>{{ storageName(item.storageId) }}</span><span>{{ item.retentionHours }} h</span><time :datetime="item.expiresAt">{{ date(item.expiresAt) }}</time><span :class="{ active: item.active }">{{ item.active ? '播放中' : Date.parse(item.expiresAt) <= Date.now() ? '已过期' : '保留中' }}</span></article>
        </div>
        <ScrollRail :element="temporaryList" :thickness="3" />
      </div>
      <p v-if="statusError" role="alert" class="cas-temporary-error">{{ statusError }}</p>
      <p v-if="cleanupMessage" role="status" class="cas-temporary-result">{{ cleanupMessage }}</p>
    </section>
    <CASPool ref="pool" @close="back" @busy="busy = $event" @ready="ready = $event" />
  </div>
</template>
<style scoped>
.cas-pool-page { min-width:0; padding-bottom:8px; }
.cas-pool-page-heading { display:flex; align-items:center; gap:10px; margin-bottom:16px; }
.cas-pool-page-heading h2 { font-size:16px; font-weight:600; }
.cas-pool-page-heading>.btn { margin-left:auto; }
.cas-pool-page-heading>.icon-btn { color:var(--primary); }
.cas-temporary-strip { border:1px solid var(--border); border-radius:8px; background:var(--surface); padding:12px 16px; margin-bottom:12px; }
.cas-temporary-strip>header { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
.cas-temporary-strip h3 { font-size:14px; }
.cas-temporary-strip dl { display:flex; gap:20px; margin:0 12px; }
.cas-temporary-strip dl>div { display:flex; gap:8px; align-items:baseline; }
.cas-temporary-strip dt { font-size:12px; color:var(--muted); }.cas-temporary-strip dd { margin:0; font-variant-numeric:tabular-nums; }
.cas-temporary-actions { display:flex; align-items:center; gap:8px; margin-left:auto; }
.cas-temporary-frame { position:relative; margin-top:12px; }
.cas-temporary-list { max-height:160px; overflow:auto; scrollbar-width:none; overscroll-behavior:contain; }
.cas-temporary-list::-webkit-scrollbar { display:none; }
.cas-temporary-item { display:grid; grid-template-columns:minmax(120px,1fr) minmax(90px,.6fr) 60px 160px 60px; gap:12px; align-items:center; font-size:13px; min-height:40px; border-top:1px solid var(--border); }
.cas-temporary-item>* { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.cas-temporary-item time { color:var(--muted); }.cas-temporary-item .active,.cas-temporary-result { color:var(--green); }
.cas-temporary-error { color:var(--red); }.cas-temporary-error,.cas-temporary-result { font-size:13px; margin-top:8px; }
@media(max-width:600px) { .cas-temporary-strip { padding:12px; }.cas-temporary-strip dl { width:100%; margin:0; }.cas-temporary-actions { margin-left:0; }.cas-temporary-item { grid-template-columns:minmax(100px,1fr) 80px 50px; }.cas-temporary-item time,.cas-temporary-item>span:nth-child(2) { display:none; } }
</style>
