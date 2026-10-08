import { ref, watch } from 'vue'
import { state } from './lib'

export const replacementJobs = ref([])
let pending = false, generation = 0
watch(() => state.authenticated, () => { generation++; replacementJobs.value = [] }, { flush: 'sync' })
export async function refreshReplacementNotices() {
  if (!state.authenticated || pending) return
  pending = true
  const run = generation
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 3000)
  try {
    const response = await fetch('/api/strm-replace', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
    if (!response.ok) return
    const data = await response.json()
    if (run === generation) replacementJobs.value = Array.isArray(data.tasks) ? data.tasks : []
  } catch { /* Normal status polling must not flood the toast stack. */ }
  finally { clearTimeout(timeout); pending = false }
}
