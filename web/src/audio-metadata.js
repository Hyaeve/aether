export function audioSource(file) {
  return '/api/files/audio-source?' + new URLSearchParams({ storage: file.storage, parent: file.parent, id: file.id })
}

export function parseLyrics(text = '') {
  const lines = [], offset = Number(text.match(/\[offset:([+-]?\d+)\]/i)?.[1] || 0) / 1000
  for (const row of text.slice(0, 200000).split(/\r?\n/).slice(0, 5000)) {
    const times = [...row.matchAll(/\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]/g)]
    const words = row.replace(/\[[^\]]*\]/g, '').trim()
    if (!words) continue
    if (times.length) for (const t of times) {
      if (Number(t[2]) < 60) lines.push({ time: Math.max(0, Number(t[1]) * 60 + Number(t[2]) + Number('0.' + (t[3] || '0')) + offset), text: words })
    } else if (!/^\[\w+:/.test(row)) lines.push({ time: null, text: words })
  }
  if (lines.some(line => line.time !== null)) return lines.filter(line => line.time !== null).sort((a, b) => a.time - b.time)
  return lines
}

export async function readAudioMetadata(file, signal) {
  const [{ parseFromTokenizer, selectCover }, { HttpClient }, { tokenizer }] = await Promise.all([import('music-metadata'), import('@tokenizer/http'), import('@tokenizer/range')])
  const client = new HttpClient(audioSource(file)), get = client.getResponse.bind(client)
  let total = 0, requests = 0, reader
  // Bounded Range reads prevent a tag parser from downloading an entire track.
  client.getResponse = async (method, range) => {
    if (!range || range[1] - range[0] >= 1 << 20 || ++requests > 64 || (total += range[1] - range[0] + 1) > 16 << 20) throw new Error('Metadata limit')
    return get(method, range)
  }
  const abort = () => client.abort()
  signal.addEventListener('abort', abort, { once: true })
  try {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    reader = await tokenizer(client, { abortSignal: signal, timeoutInSec: 15, initialChunkSize: 65536, minimumChunkSize: 65536 })
    const metadata = await parseFromTokenizer(reader, { duration: false, skipPostHeaders: true })
    const common = metadata.common, picture = selectCover(common.picture)
    const lyric = common.lyrics?.[0]
    const lines = lyric?.syncText?.length && lyric.timeStampFormat === 2
      ? lyric.syncText.slice(0, 5000).map(line => ({ time: line.timestamp / 1000, text: line.text }))
      : parseLyrics(lyric?.text)
    return { title: common.title, artist: common.artist, album: common.album, lines, bitrate: metadata.format.bitrate, duration: metadata.format.duration,
      picture: picture && picture.data.length <= 8 << 20 && /^image\/(jpeg|png|webp)$/.test(picture.format) ? new Blob([picture.data], { type: picture.format }) : null }
  } finally {
    signal.removeEventListener('abort', abort)
    client.abort()
    await reader?.close()
  }
}

export async function readLRC(url, signal) {
  const response = await fetch(url, { signal, credentials: 'same-origin' })
  if (!response.ok) throw new Error('Lyrics unavailable')
  const reader = response.body.getReader(), chunks = []
  let size = 0
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      size += value.length
      if (size > 2 << 20) throw new Error('Lyrics limit')
      chunks.push(value)
    }
  } finally { await reader.cancel() }
  const data = new Uint8Array(size)
  let offset = 0
  for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.length }
  let text
  try { text = new TextDecoder('utf-8', { fatal: true }).decode(data) } catch { text = new TextDecoder('gb18030').decode(data) }
  return parseLyrics(text)
}
