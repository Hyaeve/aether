import { onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'

export function usePageSlide(destination) {
  const router = useRouter(), leaving = ref(false)
  let timer
  function back() {
    if (leaving.value) return
    leaving.value = true
    timer = setTimeout(() => router.push(destination), matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 320)
  }
  onBeforeUnmount(() => clearTimeout(timer))
  return { leaving, back }
}
