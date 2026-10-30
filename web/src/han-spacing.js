import { h, toDisplayString } from 'vue'

const han = /^\p{Unified_Ideograph}$/u

export function hanSpacingParts(text) {
  const characters = Array.from(text), parts = []
  let plain = ''
  for (let i = 0; i < characters.length; i++) {
    const character = characters[i]
    if (han.test(character) && han.test(characters[i + 1] || '')) {
      if (plain) { parts.push(plain); plain = '' }
      parts.push({ character })
    } else plain += character
  }
  if (plain) parts.push(plain)
  return parts
}

export function hanSpacingCount(text) {
  return hanSpacingParts(text).filter(part => typeof part !== 'string').length
}

export const HanText = {
  name: 'AetherHanText',
  props: { parts: { type: Array, required: true } },
  setup(props) {
    return () => {
      const text = props.parts.map(toDisplayString).join('')
      const parts = hanSpacingParts(text)
      if (parts.every(part => typeof part === 'string')) return text
      return h('aether-text', { class: 'han-text' }, parts.map(part =>
        typeof part === 'string' ? part : h('aether-char', { class: 'han-gap' }, part.character)))
    }
  }
}
