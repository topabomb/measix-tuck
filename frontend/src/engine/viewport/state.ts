/** Framework-neutral semantic viewport checkpoint. No product-specific controls belong here. */
export interface SemanticViewportSnapshot {
  logicalPosition: number
  anchorUnitId: string | null
  anchorOffsetPx: number
  followTail: boolean
  atVisualBottom: boolean
}

/** Small session-local interaction memory that survives view/runtime eviction. */
export interface SessionViewMemory extends SemanticViewportSnapshot {
  draftText: string
}
