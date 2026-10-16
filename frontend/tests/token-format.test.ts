import { describe, expect, it } from 'vitest'
import { compactTokens, exactTokens } from '../src/shared/token-format'

describe('product token amounts', () => {
  it('uses decimal units with consistent rounding at unit boundaries', () => {
    for (const [value, expected] of [[0,'0'],[999,'999'],[1000,'1K'],[14037,'14K'],[240640,'240.6K'],[32768,'32.8K'],[999950,'1M'],[1000000,'1M'],[1000000000,'1B']] as const) expect(compactTokens(value)).toBe(expected)
    expect(exactTokens(240640)).toBe('240,640')
  })
  it('distinguishes zero from unavailable or invalid values', () => {
    for (const value of [null, undefined, NaN, Infinity, -1]) {
      expect(compactTokens(value)).toBe('未上报')
      expect(exactTokens(value)).toBe('未上报')
    }
    expect(exactTokens(0)).toBe('0')
  })
})
