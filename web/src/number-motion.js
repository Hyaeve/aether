export function numberEasing(progress) {
  const value = Math.max(0, Math.min(1, progress))
  return 1 - (1 - value) ** 4
}

export function numberDuration(duration = 1500) {
  return Number.isFinite(duration) ? Math.max(0, Math.min(1500, duration)) : 1500
}

export function numberFrameInterval(progress, difference) {
  const lastInterval = Math.min(96, 32 + Math.log10(Math.abs(difference) + 1) * 12)
  return 16 + (lastInterval - 16) * Math.max(0, Math.min(1, progress)) ** 2
}
