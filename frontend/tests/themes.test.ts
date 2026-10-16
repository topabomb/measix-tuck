import { describe, expect, it } from 'vitest'
import { builtInThemes, parseTheme } from '../src/app/themes'

function luminance(hex: string) {
  const values = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4)
  return values[0]! * .2126 + values[1]! * .7152 + values[2]! * .0722
}
function contrast(a: string, b: string) { const x = luminance(a), y = luminance(b); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05) }
describe('theme contract', () => {
  it('keeps normal and secondary text readable on every panel', () => {
    for (const theme of builtInThemes) {
      for (const surface of ['background', 'surface', 'raised', 'hover'] as const) {
        for (const foreground of ['text','muted','accent','success','warning','danger'] as const) {
          expect(contrast(theme.colors[foreground], theme.colors[surface]), `${theme.id}/${surface}/${foreground}`).toBeGreaterThanOrEqual(4.5)
        }
      }
      expect(contrast(theme.colors.accentText,theme.colors.accent), `${theme.id}/primary-button`).toBeGreaterThanOrEqual(4.5)
    }
  })
  it('accepts only a bounded data palette and strips executable or unknown properties', () => {
    const theme = builtInThemes[0]!
    expect(parseTheme({ ...theme, script: 'bad', css: 'bad' })).toEqual(theme)
    for (const color of ['red', 'url(https://example.com)', '#123', 'var(--secret)', '#fff;display:none']) expect(() => parseTheme({ ...theme, colors: { ...theme.colors, text: color } })).toThrow()
    expect(() => parseTheme({ ...theme, id: '../theme' })).toThrow()
    expect(() => parseTheme({ ...theme, colors: {} })).toThrow()
  })
})
