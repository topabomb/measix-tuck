/** Clipboard API needs HTTPS; the selection fallback also supports LAN HTTP. */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) { try { await navigator.clipboard.writeText(text); return } catch { /* selection fallback */ } }
  const active = document.activeElement as HTMLElement | null
  const input = document.createElement('textarea')
  input.value = text; input.style.cssText = 'position:fixed;left:-9999px;top:0'; document.body.append(input); input.select()
  try { if (!document.execCommand('copy')) throw new Error('复制失败，请从地址栏复制链接') }
  finally { input.remove(); active?.focus({ preventScroll:true }) }
}
