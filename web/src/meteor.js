export function createMeteorBatch(random = Math.random) {
  return Array.from({ length: 2 + Math.floor(random() * 4) }, () => ({
    x: .72 + random() * .27,
    y: .01 + random() * .22,
    dx: -(.55 + random() * .18),
    dy: .53 + random() * .22,
    delay: random() * .6,
    duration: 2.4 + random() * .8,
    tail: .12 + random() * .08
  }))
}
