import { shallowRef } from 'vue'

export const colorKeys = ['background', 'surface', 'raised', 'hover', 'border', 'text', 'muted', 'accent', 'accentText', 'success', 'warning', 'danger'] as const
export type ThemeColors = Record<typeof colorKeys[number], string>
export interface WorkbenchTheme { version: 1; id: string; name: string; scheme: 'dark' | 'light'; colors: ThemeColors }
const palette = (values: string[]): ThemeColors => Object.fromEntries(colorKeys.map((key, i) => [key, values[i]])) as ThemeColors
export const builtInThemes: WorkbenchTheme[] = [
  { version: 1, id: 'tuck', name: '墨蓝', scheme: 'light', colors: palette(['#faf9f6','#f0f3f6','#e7ecf1','#dce4ec','#bec9d4','#223247','#526174','#344a65','#ffffff','#286644','#805916','#a73834']) },
  { version: 1, id: 'graphite', name: '石墨', scheme: 'dark', colors: palette(['#16191e','#1e2228','#272d35','#343d48','#4b5766','#e8edf3','#afbaca','#abc4e4','#172536','#91d6a8','#e7c97d','#ffa29a']) },
  { version: 1, id: 'sand', name: '暖砂', scheme: 'light', colors: palette(['#fbf7ef','#f3eee3','#eae2d4','#ded4c2','#c4b8a3','#322c24','#635949','#80512c','#ffffff','#39623b','#7c4e0b','#993429']) },
  { version: 1, id: 'ember', name: '炭棕', scheme: 'dark', colors: palette(['#211e1b','#2a2622','#36302a','#423a32','#605448','#eee4d8','#c0b09e','#dab486','#302217','#a8c49a','#e4c078','#e6a296']) },
  { version: 1, id: 'moss', name: '苔灰', scheme: 'light', colors: palette(['#a9aea7','#b7bcb5','#c3c8c1','#cdd1cb','#7e877b','#252a24','#3b4238','#523b29','#ffffff','#233e2b','#51360f','#63241e']) },
  { version: 1, id: 'contrast', name: '曜黑（高对比）', scheme: 'dark', colors: palette(['#08090a','#131517','#1e2124','#2d3237','#7c858f','#ffffff','#cbd0d6','#e9edf2','#101217','#a0f4ac','#ffe68c','#ffb4b4']) },
]
export function parseTheme(value: unknown): WorkbenchTheme {
  const t = value as WorkbenchTheme
  if (!t || t.version !== 1 || typeof t.id !== 'string' || !/^[a-z][a-z0-9-]{0,47}$/.test(t.id) || typeof t.name !== 'string' || !t.name.trim() || t.name.length > 40 || !['dark', 'light'].includes(t.scheme)) throw new Error('主题需要 version=1、有效 id、名称和 dark/light scheme')
  const colors = {} as ThemeColors
  for (const key of colorKeys) {
    const color = t.colors?.[key]
    if (typeof color !== 'string' || !/^#[0-9a-fA-F]{6}$/.test(color)) throw new Error(`主题颜色 ${key} 必须是六位十六进制颜色`)
    colors[key] = color
  }
  return { version: 1, id: t.id, name: t.name.trim(), scheme: t.scheme, colors }
}
const storageKey = 'tuck.appearance.v1'
export const customThemes = shallowRef<WorkbenchTheme[]>([])
export const selectedTheme = shallowRef('tuck')
export function applyTheme(id: string) {
  const theme = [...builtInThemes, ...customThemes.value].find(t => t.id === id) || builtInThemes[0]!
  for (const key of colorKeys) { document.documentElement.style.setProperty(`--tuck-${key}`, theme.colors[key]); document.documentElement.style.setProperty(`--ce-theme-${key}`, theme.colors[key]) }
  document.documentElement.style.setProperty('--tuck-scheme', theme.scheme)
  // Keep syntax tokens paired with their Shiki palette, including external themes.
  document.documentElement.style.setProperty('--tuck-code-background', theme.scheme === 'light' ? '#ffffff' : '#1e1e1e')
  document.documentElement.style.setProperty('--ce-theme-scheme', theme.scheme)
  document.documentElement.dataset.tuckTheme = theme.id
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme.colors.accent)
  selectedTheme.value = theme.id
  try { localStorage.setItem(storageKey, JSON.stringify({ selected: theme.id, themes: customThemes.value })) } catch { /* In-memory appearance still works when storage is unavailable. */ }
}
export function initializeThemes() {
  try { const saved = JSON.parse(localStorage.getItem(storageKey) || '{}'); customThemes.value = (Array.isArray(saved.themes) ? saved.themes.slice(0, 12) : []).map(parseTheme).filter((t: WorkbenchTheme) => !builtInThemes.some(b => b.id === t.id)); selectedTheme.value = saved.selected || 'tuck' } catch { customThemes.value = []; selectedTheme.value = 'tuck' }
  applyTheme(selectedTheme.value)
}
export function importTheme(text: string) {
  if (text.length > 32768) throw new Error('主题文件最多 32 KiB')
  const theme = parseTheme(JSON.parse(text))
  if (builtInThemes.some(t => t.id === theme.id)) throw new Error('外部主题不能覆盖内置主题 ID')
  if (customThemes.value.length >= 12 && !customThemes.value.some(t => t.id === theme.id)) throw new Error('最多保存 12 个外部主题，请先移除不用的主题')
  customThemes.value = [...customThemes.value.filter(t => t.id !== theme.id), theme]
  applyTheme(theme.id)
}
export function removeTheme(id: string) { customThemes.value = customThemes.value.filter(t => t.id !== id); applyTheme(selectedTheme.value === id ? 'tuck' : selectedTheme.value) }
