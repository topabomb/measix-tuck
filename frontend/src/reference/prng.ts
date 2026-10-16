export function hash32(input: number): number {
  let x = input | 0
  x ^= x >>> 16
  x = Math.imul(x, 0x7feb352d)
  x ^= x >>> 15
  x = Math.imul(x, 0x846ca68b)
  x ^= x >>> 16
  return x >>> 0
}

export function unitFloat(seed: number): number { return hash32(seed) / 0xffffffff }
export function intBetween(seed: number, min: number, max: number): number {
  return min + Math.floor(unitFloat(seed) * (max - min + 1))
}
