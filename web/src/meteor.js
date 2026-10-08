export function createMeteorBatch(random = Math.random) {
  const count = 2 + Math.floor(random() * 4)
  return Array.from({ length: count }, (_, index) => ({
    x: 1 / 6 + ((index + random()) / count) * (5 / 6),
    y: .01 + random() * .08,
    distance: .65 + random() * .35,
    fadeStart: 1 / 3 + random() * .27,
    fadeEnd: .7 + random() * .3,
    delay: random() * .6,
    duration: (1.8 + random() * .8) * [1, 2, 3][Math.floor(random() * 3)],
    tail: .04 + random() * .025
  }))
}

export function rockPosition(index, time, width, height) {
  const wrap = (value, size) => ((value % size) + size) % size
  return { x: wrap(width * (.12 + index * .29) + time * (3 + index * 1.1) + 24, width + 48) - 24,
    y: wrap(height * (.68 - index * .18) + time * (.7 + index * .22) + 24, height + 48) - 24 }
}

export function meteorOpacity(progress, fadeStart = 1 / 3, fadeEnd = 1) {
  if (progress <= 0 || progress >= 1) return 0
  const smooth = value => value * value * (3 - 2 * value)
  return smooth(Math.min(progress / .15, 1)) * (1 - smooth(Math.min(1, Math.max(0, (progress - fadeStart) / (fadeEnd - fadeStart)))))
}
