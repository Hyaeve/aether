import { shallowReactive } from 'vue'

export const audioSession = shallowReactive({ file: null, queue: [], activation: 0 })

export function openAudio(file, queue) {
  audioSession.queue = queue.map(item => ({ ...item }))
  audioSession.file = { ...file }
  audioSession.activation++
}

export function closeAudio() {
  audioSession.file = null
  audioSession.queue = []
}
