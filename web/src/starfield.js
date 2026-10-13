export function createSpiralPair(random = Math.random) {
  return Array.from({ length:2 }, () => {
    let u = random(), v = random()
    if (u + v > 1) { u = 1 - u; v = 1 - v }
    const x = .12 + .74 * u
    const y = .14 + .73 * (u + v)
    return [x, y, .65 + random() * .85, (random() - .5) * .7, random() * 6, .016 + random() * .012, .9]
  })
}

export function moonRegion(width, height) {
  const size = Math.min(92, width * .14)
  return { x: width * .84, y: height * .08, size, cx: width * .84 + size / 2, cy: height * .08 + size / 2, radius: size * 100 / 220 }
}
