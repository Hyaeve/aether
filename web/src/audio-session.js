import { shallowReactive } from 'vue'

export const audioSession = shallowReactive({ file: null, queue: [], activation: 0 })

export function openAudio(file) {
  const index = audioSession.queue.findIndex(item => item.id === file.id && item.storage === file.storage)
  const queue = [...audioSession.queue]
  if (index < 0) queue.push({ ...file })
  else queue[index] = { ...file }
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
