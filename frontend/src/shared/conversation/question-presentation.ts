import type { PendingInteraction } from '../../engine/conversation/contracts'

// A built frontend can be refreshed while the older backend is still running
// an agent. Its question detail contains the schema and its title the message.
export function presentQuestion(pending: PendingInteraction | null | undefined) {
  if (pending?.kind !== 'question' || pending.schema) return pending
  try {
    const raw = JSON.parse(pending.detail)
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return pending
    const key = typeof raw.title === 'string' && raw.title ? raw.title : 'value'
    const schema = raw.properties && Object.keys(raw.properties).length ? raw : raw.type && raw.type !== 'object'
      ? {type:'object',properties:{[key]:raw},required:[key]}
      : {type:'object',properties:{response:{type:'string',title:'回答问题'}},required:['response']}
    return {...pending,title:'需要你回答',detail:pending.title,schema}
  } catch { return pending }
}
