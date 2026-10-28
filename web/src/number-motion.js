export function numberEasing(progress) {
  const value = Math.max(0, Math.min(1, progress))
  return 1 - (1 - value) ** 4
}
