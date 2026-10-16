export interface PhysicalListPort {
  readonly scrollOffset: number
  readonly scrollSize: number
  readonly viewportSize: number
  findItemIndex(offset: number): number
  scrollTo(offset: number): void
  scrollBy(offset: number): void
  scrollToIndex(index: number, options?: { align?: 'start' | 'center' | 'end'; offset?: number }): void
}

export interface ViewportRowSample { id: string; messageIndex: number; top: number; bottom: number }
export interface CommittedViewportAnchor { id: string; offsetPx: number; viewportTopPx: number }

export const VIEWPORT_POLICY = Object.freeze({
  bottomTolerancePx: 2,
  edgeThresholdPx: 900,
  anchorTolerancePx: 0.75,
  userIntentMs: 650,
  restoreAttempts: 12,
  stableLayoutFrames: 3,
  jumpAttempts: 10,
})

export function remainingToBottom(port: Pick<PhysicalListPort, 'scrollSize' | 'scrollOffset' | 'viewportSize'>): number {
  return Math.max(0, port.scrollSize - port.scrollOffset - port.viewportSize)
}

export function messagesAfter(reader: number, logicalCount: number): number {
  if (logicalCount <= 0) return 0
  return Math.max(0, logicalCount - 1 - clampLogicalIndex(reader, logicalCount))
}

export function clampLogicalIndex(index: number, logicalCount: number): number {
  if (logicalCount <= 0) return 0
  return Math.max(0, Math.min(logicalCount - 1, Math.floor(Number(index) || 0)))
}

/**
 * Select the committed row whose leading edge is nearest the viewport leading edge.
 * A giant row may start hundreds of pixels above the viewport while still intersecting it;
 * preserving that distant edge produces visible jumps during responsive reflow. Measurement
 * probes are rejected by the semantic-reader filter before geometric ranking.
 */
export function selectCommittedAnchor(rows: readonly ViewportRowSample[], viewportTop: number, viewportBottom: number, reader: number): CommittedViewportAnchor | null {
  const candidate = rows
    .filter(row => row.messageIndex <= reader + 1 && row.bottom > viewportTop && row.top < viewportBottom)
    .sort((a, b) => Math.abs(a.top - viewportTop) - Math.abs(b.top - viewportTop))[0]
  if (!candidate) return null
  const top = candidate.top - viewportTop
  return { id: candidate.id, offsetPx: top, viewportTopPx: top }
}

export function isMessageCommittedVisible(rows: readonly ViewportRowSample[], target: number, viewportTop: number, viewportBottom: number): boolean {
  return rows.some(row => row.messageIndex === target && row.bottom > viewportTop && row.top < viewportBottom)
}
