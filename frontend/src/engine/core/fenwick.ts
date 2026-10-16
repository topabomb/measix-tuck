export class FenwickTree {
  private readonly tree: Float64Array
  private readonly values: Float64Array

  constructor(size: number, initialValue = 0) {
    if (!Number.isInteger(size) || size <= 0) throw new RangeError('size must be a positive integer')
    this.tree = new Float64Array(size + 1)
    this.values = new Float64Array(size)
    if (initialValue !== 0) {
      for (let i = 0; i < size; i += 1) this.set(i, initialValue)
    }
  }

  get size(): number { return this.values.length }

  get(index: number): number { return this.values[index] ?? 0 }

  set(index: number, value: number): void {
    if (index < 0 || index >= this.size) throw new RangeError('index out of bounds')
    const delta = value - this.values[index]
    this.values[index] = value
    for (let i = index + 1; i <= this.size; i += i & -i) this.tree[i] += delta
  }

  prefix(endExclusive: number): number {
    let i = Math.max(0, Math.min(this.size, endExclusive))
    let sum = 0
    while (i > 0) {
      sum += this.tree[i]
      i -= i & -i
    }
    return sum
  }

  total(): number { return this.prefix(this.size) }

  lowerBound(target: number): number {
    if (target <= 0) return 0
    if (target >= this.total()) return this.size - 1
    let index = 0
    let sum = 0
    let bit = 1
    while ((bit << 1) <= this.size) bit <<= 1
    for (; bit !== 0; bit >>= 1) {
      const next = index + bit
      if (next <= this.size && sum + this.tree[next] <= target) {
        index = next
        sum += this.tree[next]
      }
    }
    return Math.min(index, this.size - 1)
  }
}
