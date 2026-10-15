import { shallowReactive } from 'vue'

export const audioSession = shallowReactive({ file: null, queue: [], activation: 0 })

export function openAudio(file) {
  const incoming = Array.isArray(file.queue) && file.queue.length ? file.queue : [file]
  const queue = [...audioSession.queue]
  for (const item of incoming) {
    const index = queue.findIndex(entry => entry.id === item.id && entry.storage === item.storage)
    if (index < 0) queue.push({ ...item })
    else queue[index] = { ...queue[index], ...item }
  }
  audioSession.queue = queue
  audioSession.file = { ...file }
  audioSession.activation++
}

export function removeAudio(file) {
  const index = audioSession.queue.findIndex(item => item.id === file.id && item.storage === file.storage)
  if (index < 0) return
  const current = audioSession.file?.id === file.id && audioSession.file?.storage === file.storage
  const queue = audioSession.queue.filter((_, i) => i !== index)
  audioSession.queue = queue
  if (!current) return
  if (!queue.length) { closeAudio(); return }
  audioSession.file = { ...queue[index % queue.length] }
}

export function closeAudio() {
  audioSession.file = null
  audioSession.queue = []
}
