export function createMeteorBatch(random = Math.random) {
  return Array.from({ length: 2 + Math.floor(random() * 4) }, () => ({
    x: .72 + random() * .27,
    y: .01 + random() * .22,
    distance: .65 + random() * .35,
    fadeStart: 1 / 3 + random() * .27,
    fadeEnd: .7 + random() * .3,
    delay: random() * .6,
    duration: (1.8 + random() * .8) * [1, 2, 3][Math.floor(random() * 3)],
    tail: .055 + random() * .045
  }))
}

export function meteorOpacity(progress, fadeStart = 1 / 3, fadeEnd = 1) {
  if (progress <= 0 || progress >= 1) return 0
  const smooth = value => value * value * (3 - 2 * value)
  return smooth(Math.min(progress / .15, 1)) * (1 - smooth(Math.min(1, Math.max(0, (progress - fadeStart) / (fadeEnd - fadeStart)))))
}
