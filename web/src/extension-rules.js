const extensionPattern = /^[A-Za-z0-9_-]{1,32}$/

export function extensionTokens(value) {
  return String(value || '').split(';').map(token => token.trim().replace(/^\./, '')).filter(Boolean)
}

export function invalidExtensionTokens(value) {
  return extensionTokens(value).filter(token => !extensionPattern.test(token))
}

export function extensionError(label, value) {
  const invalid = invalidExtensionTokens(value)
  return invalid.length ? `${label}不合规范：${invalid.join('、')}。仅允许英文字母、数字、下划线和连字符` : ''
}
