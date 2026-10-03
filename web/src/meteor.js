export function createMeteorBatch(random = Math.random) {
  return Array.from({ length: 2 + Math.floor(random() * 4) }, () => ({
    x: .72 + random() * .27,
    y: .01 + random() * .22,
    distance: .22 + random() * .16,
    delay: random() * .6,
    duration: 1.8 + random() * .8,
    tail: .18 + random() * .12
  }))
}

export function meteorOpacity(progress) {
  if (progress <= 0 || progress >= 1) return 0
  const smooth = value => value * value * (3 - 2 * value)
  return smooth(Math.min(progress / .15, 1)) * (1 - smooth(Math.max(0, (progress - .25) / .75)))
}
