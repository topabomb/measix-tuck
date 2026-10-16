import { reactive } from 'vue'

interface WorkspaceDialogRequest {
  id:number
  kind:'prompt'|'confirm'|'conflict'
  message:string
  value:string
  detail:string
  allowReplace:boolean
}

export const workspaceDialogState = reactive<{current:WorkspaceDialogRequest|null}>({current:null})
let serial = 0
let complete:((value:string|null)=>void)|undefined

export function answerWorkspaceDialog(id:number, value:string|null):void {
  if (workspaceDialogState.current?.id !== id) return
  const resolve = complete
  complete = undefined
  workspaceDialogState.current = null
  resolve?.(value)
}

export function cancelWorkspaceDialog():void {
  const current = workspaceDialogState.current
  if (current) answerWorkspaceDialog(current.id, null)
}

function request(kind:WorkspaceDialogRequest['kind'], message:string, initial:string, detail='', allowReplace=true):Promise<string|null> {
  cancelWorkspaceDialog()
  return new Promise(resolve => {
    complete = resolve
    workspaceDialogState.current = {id:++serial, kind, message, value:initial, detail, allowReplace}
  })
}

export function workspacePrompt(message:string, initial = ''):Promise<string|null> {
  return request('prompt', message, initial)
}

export async function workspaceConfirm(message:string):Promise<boolean> {
  return await request('confirm', message, '') !== null
}

export function workspaceConflict(message:string, options:{preferred?:'副本'|'替换';allowReplace?:boolean;detail?:string}={}):Promise<string|null> {
  const allowReplace=options.allowReplace!==false
  return request('conflict', message, allowReplace?options.preferred||'副本':'副本', options.detail||'', allowReplace)
}
