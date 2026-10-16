import { onBeforeUnmount, onMounted } from 'vue'

/** Non-modal menus and inspectors: outside pointer closes without eating it. */
export function useLightDismiss(root: () => HTMLElement | null, isOpen: () => boolean, close: () => void): void {
  function pointer(event: PointerEvent) {
    if (isOpen() && !event.composedPath().includes(root()!)) close()
  }
  function key(event: KeyboardEvent) {
    if (event.key !== 'Escape' || event.defaultPrevented || !isOpen() || document.querySelector('dialog[open]')) return
    const trigger = root()?.querySelector<HTMLElement>('summary,button[aria-expanded="true"]')
    event.preventDefault(); close(); trigger?.focus()
  }
  onMounted(() => { document.addEventListener('pointerdown', pointer, true); document.addEventListener('keydown', key) })
  onBeforeUnmount(() => { document.removeEventListener('pointerdown', pointer, true); document.removeEventListener('keydown', key) })
}
