/** Clipboard API needs HTTPS; the selection fallback also supports LAN HTTP. */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) { try { await navigator.clipboard.writeText(text); return } catch { /* selection fallback */ } }
  const active = document.activeElement as HTMLElement | null
  const input = document.createElement('textarea')
  // Content outside a native modal is inert: selecting a textarea there can
  // leave the clipboard unchanged even when execCommand reports success.
  const owner = active?.closest('dialog[open]') ?? [...document.querySelectorAll('dialog[open]')].at(-1) ?? document.body
  input.value = text; input.style.cssText = 'position:fixed;left:-9999px;top:0'; owner.append(input); input.focus(); input.select()
  try { if (document.activeElement !== input || !document.execCommand('copy')) throw new Error('复制失败，请手动复制文本') }
  finally { input.remove(); active?.focus({ preventScroll:true }) }
}
