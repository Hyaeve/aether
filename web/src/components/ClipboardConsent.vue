<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import Modal from './Modal.vue'
import Icon from './Icon.vue'

const emit = defineEmits(['close', 'accept'])
const body = ref(), remaining = ref(10), read = ref(false)
const ready = computed(() => remaining.value === 0 && read.value)
let deadline, timer, observer
function measure() {
  const el = body.value
  if (el && el.scrollHeight - el.clientHeight - el.scrollTop <= 2) read.value = true
}
function accept() { if (ready.value) emit('accept') }
onMounted(() => {
  deadline = performance.now() + 10000
  timer = setInterval(() => {
    remaining.value = Math.max(0, Math.ceil((deadline - performance.now()) / 1000))
    if (remaining.value === 0) clearInterval(timer)
  }, 100)
  observer = new ResizeObserver(measure)
  observer.observe(body.value)
  measure()
})
onUnmounted(() => { clearInterval(timer); observer?.disconnect() })
</script>
<template>
  <Modal title="剪贴板监听与隐私说明" standard confirmation @close="emit('close')">
    <div ref="body" class="modal-body clipboard-consent" tabindex="0" aria-label="剪贴板隐私说明正文" @scroll="measure">
      <p>开启后，在浏览器允许读取的情况下，Aether 会在管理页面处于前台及恢复焦点时读取剪贴板，识别 115、移动云盘和夸克的分享链接及提取码，并打开转存窗口。不会自动提交转存操作。</p>
      <p>链接识别在当前浏览器本地进行。Aether 不会将原始剪贴板内容上传到开发者服务器、广告平台或分析服务，也不会将完整剪贴板内容持久化保存。</p>
      <p>转存窗口中的单条分享链接会在输入稳定后，将链接和提取码发送至您部署的 Aether 服务，只读获取默认保存目录。点击解析会读取分享目录树；仅在确认转存后，才会创建目标目录并提交转存。执行结果可能出现在任务日志和通知中。请勿提交无权访问的内容，妥善保护分享链接、提取码及服务访问权限。</p>
      <p>您可随时关闭此开关，立即停止监听并清空待处理批次。浏览器读取授权另由浏览器管理；HTTPS、安全上下文及权限限制仍然适用。开启即表示您已了解上述处理范围并同意使用该功能。</p>
    </div>
    <footer class="modal-footer"><button class="btn" @click="emit('close')">取消</button><button class="btn primary consent-accept" aria-label="我已阅读并同意" :disabled="!ready" @click="accept"><span v-if="remaining" class="consent-countdown" aria-hidden="true">{{ remaining }}</span><Icon v-else name="Check" /><span>我已阅读并同意</span></button></footer>
  </Modal>
</template>
<style scoped>
.clipboard-consent { min-height:0; overflow-y:auto; overscroll-behavior:contain; scrollbar-width:none; }
.clipboard-consent::-webkit-scrollbar { display:none; }
.clipboard-consent p { margin:0 0 14px; font-size:14px; line-height:1.7; }.clipboard-consent p:last-child { margin-bottom:0; }
.consent-countdown { display:inline-grid; place-items:center; width:18px; height:18px; font-size:13px; font-variant-numeric:tabular-nums; }
</style>
