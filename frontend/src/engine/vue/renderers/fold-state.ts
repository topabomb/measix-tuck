import { computed, ref, type WritableComputedRef } from 'vue'

/**
 * User-touched disclosure state is presentation preference memory:
 * - it survives virtual row unmount/remount;
 * - stable RenderUnit IDs already include session/message scope;
 * - untouched history allocates no state;
 * - the LRU bound prevents long-lived workspaces from accumulating unbounded UI memory.
 */
export const MAX_TOUCHED_DISCLOSURES = 2048
const touched = new Map<string, boolean>()

function readTouched(id: string): boolean | undefined {
  const value = touched.get(id)
  if (value === undefined) return undefined
  touched.delete(id)
  touched.set(id, value)
  return value
}

function writeTouched(id: string, value: boolean): void {
  touched.delete(id)
  touched.set(id, value)
  while (touched.size > MAX_TOUCHED_DISCLOSURES) {
    const oldest = touched.keys().next().value as string | undefined
    if (!oldest) break
    touched.delete(oldest)
  }
}

export function useFoldState(id: string, defaultOpen = false): WritableComputedRef<boolean> {
  const local = ref(readTouched(id) ?? defaultOpen)
  return computed({
    get: () => local.value,
    set: (value: boolean) => {
      // Write through before Vue patches height: ResizeObserver/virtualizer may
      // legally unmount the row during the same frame.
      writeTouched(id, value)
      local.value = value
    },
  })
}

export function touchedFoldStateCount(): number { return touched.size }
