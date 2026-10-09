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
  const seed = Math.sin((index + 1) * 127.1) * 43758.5453
  const fraction = seed - Math.floor(seed), phase = index * 1.7
  const driftX = Math.sin(time * (.025 + index * .003) + phase) * .04
  const driftY = Math.sin(time * .029 + phase) * .04
  const edge = index % 3
  return { x: width * ((edge === 0 ? .04 + fraction * .15 : edge === 1 ? .12 + fraction * .76 : .8 + fraction * .15) + driftX),
    y: height * ((edge === 1 ? .87 + fraction * .06 : .18 + fraction * .62) + driftY) }
}

export function meteorOpacity(progress, fadeStart = 1 / 3, fadeEnd = 1) {
  if (progress <= 0 || progress >= 1) return 0
  const smooth = value => value * value * (3 - 2 * value)
  return smooth(Math.min(progress / .15, 1)) * (1 - smooth(Math.min(1, Math.max(0, (progress - fadeStart) / (fadeEnd - fadeStart)))))
}
