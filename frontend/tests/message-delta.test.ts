import { describe, expect, it } from 'vitest'
import { block, type LogicalMessage } from '../src/engine/model/conversation'
import { applyMessageDelta, type MessageDelta } from '../src/shared/workspace/message-delta'
import { createWorkbenchProjection } from '../src/shared/workspace/projection'
const message = (): LogicalMessage => ({ id:'m',index:0,role:'assistant',turnId:'t',revision:0,live:true,blocks:[block('thought','reasoning',{text:'',status:'streaming'},0),block('answer','markdown',{markdown:''},0)] })
const delta: MessageDelta = { messageId:'m',index:0,blockId:'thought',field:'text',text:'最新内容',baseRevision:0,revision:1,blockRevision:1 }
describe('producer-owned text deltas', () => {
  it('updates only the addressed block without mutating the prior snapshot', () => {
    const before = message(), result = applyMessageDelta(before,delta)!
    expect(result.patch).toEqual({kind:'append-reasoning',blockId:'thought',delta:'最新内容'})
    expect(result.message.blocks[1]).toBe(before.blocks[1])
    expect(before.blocks[0]!.data).toEqual({text:'',status:'streaming'})
    expect(result.message.blocks[0]!.data).toEqual({text:'最新内容',status:'streaming'})
  })
  it('rejects missing, duplicate, wrong-identity and mismatched block predecessors for snapshot recovery', () => {
    for (const patch of [{baseRevision:1,revision:2},{messageId:'other'},{index:1},{field:'output' as const},{blockRevision:5},{revision:7}]) expect(applyMessageDelta(message(),{...delta,...patch})).toBeNull()
    expect(applyMessageDelta(applyMessageDelta(message(),delta)!.message,delta)).toBeNull()
  })
  it('omits empty placeholders and introduces their stable nodes when the first content arrives', () => {
    const projection = createWorkbenchProjection(), before = message()
    expect(projection.projectMessage(before)).toEqual([])
    const first = applyMessageDelta(before,delta)!.message
    const units = projection.appendReasoningDelta(first,'thought',delta.text)
    expect(units).toHaveLength(1)
    const next = applyMessageDelta(first,{...delta,baseRevision:1,revision:2,blockRevision:2,text:'追加'})!.message
    const later = projection.appendReasoningDelta(next,'thought','追加')
    expect(later[0]!.id).toBe(units[0]!.id)
    expect(later[0]!.payload.text).toBe('最新内容追加')
    expect(projection.stats.incrementalPatches).toBe(1)
    const answered = applyMessageDelta(next,{...delta,blockId:'answer',field:'markdown',text:'正文',baseRevision:2,revision:3,blockRevision:1})!.message
    expect(projection.appendMarkdownDelta(answered,'answer','正文')).toHaveLength(2)
  })
})

it('uses persisted block provenance without inferring automation from text or message metadata',()=>{
 const projection=createWorkbenchProjection();
 const original={id:'source-test',index:0,turnId:'turn',role:'user' as const,blocks:[{id:'prompt',type:'markdown' as const,data:{markdown:'**Tuck 自动要求补交付**'}}]};
 expect(projection.projectMessage(original)[0]?.payload.provenance).toBeUndefined();
 const metadataOnly={...original,id:'metadata-only',source:{kind:'delivery_required'}};
 expect(projection.projectMessage(metadataOnly)[0]?.payload.provenance).toBeUndefined();
 const annotated={...original,id:'annotated',blocks:[{...original.blocks[0]!,data:{markdown:'Actual reminder',provenance:{origin:'delivery-required'}}}]};
 expect(projection.projectMessage(annotated)[0]?.payload.provenance).toEqual({origin:'delivery-required'});
});
