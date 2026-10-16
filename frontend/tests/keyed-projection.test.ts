import { describe, expect, it } from 'vitest'
import { KeyedConversationProjection } from '../src/engine/presentation/keyed-node-store'
import type { RenderUnit } from '../src/engine/presentation/render-unit'

function node(id: string, revision = 0): RenderUnit {
  return {
    id,
    messageId: id.split(':')[0]!,
    messageIndex: Number(id.match(/(\d+)/)?.[1] ?? 0),
    turnId: 'session:turn-1',
    stepId: 'session:turn-1:step-0',
    blockId: 'md',
    kind: 'markdown',
    revision,
    estimatePx: 180,
    payload: { markdown: id },
  }
}

async function flush(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
}

describe('KeyedConversationProjection', () => {
  it('patches one streaming node without invalidating stable order or sibling seats', async () => {
    const projection = new KeyedConversationProjection()
    const a = node('session:m-1:md')
    const b = node('session:m-2:md')
    projection.replace([a, b])
    await flush()

    const originalOrder = projection.order
    let orderPublishes = 0
    let aPublishes = 0
    let bPublishes = 0
    projection.subscribeOrder(() => { orderPublishes += 1 })
    projection.subscribeNode(a.id, () => { aPublishes += 1 })
    projection.subscribeNode(b.id, () => { bPublishes += 1 })

    projection.patch({ ...a, revision: 1, payload: { markdown: 'stream delta' } })
    await flush()

    expect(projection.order).toBe(originalOrder)
    expect(orderPublishes).toBe(0)
    expect(aPublishes).toBe(1)
    expect(bPublishes).toBe(0)
    expect(projection.getNode(a.id)?.revision).toBe(1)
    expect(projection.getNode(a.id)).toMatchObject({ turnId: 'session:turn-1', stepId: 'session:turn-1:step-0', blockId: 'md' })
  })

  it('publishes metadata/payload changes even when a content-derived revision stays equal', async () => {
    const projection = new KeyedConversationProjection()
    const a = { ...node('session:m-1:md', 42), payload: { markdown: 'done', live: true } }
    projection.replace([a])
    await flush()

    let publishes = 0
    projection.subscribeNode(a.id, () => { publishes += 1 })
    projection.replace([{ ...a, payload: { markdown: 'done', live: false } }])
    await flush()

    expect(publishes).toBe(1)
    expect(projection.getNode(a.id)?.payload.live).toBe(false)
  })

  it('microtask-batches repeated patches to the same visible node', async () => {
    const projection = new KeyedConversationProjection()
    const a = node('session:m-1:md')
    projection.replace([a])
    await flush()

    let publishes = 0
    projection.subscribeNode(a.id, () => { publishes += 1 })
    for (let revision = 1; revision <= 20; revision += 1) projection.patch({ ...a, revision })
    await flush()

    expect(publishes).toBe(1)
    expect(projection.getNode(a.id)?.revision).toBe(20)
  })

  it('publishes order only when membership/order changes', async () => {
    const projection = new KeyedConversationProjection()
    const a = node('session:m-1:md')
    const b = node('session:m-2:md')
    projection.replace([a, b])
    await flush()

    let publishes = 0
    projection.subscribeOrder(() => { publishes += 1 })
    projection.replace([{ ...a }, { ...b }])
    await flush()
    expect(publishes).toBe(0)

    projection.replace([node('session:m-0:md'), a, b])
    await flush()
    expect(publishes).toBe(1)
  })
})
