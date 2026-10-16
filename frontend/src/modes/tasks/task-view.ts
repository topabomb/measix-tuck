import type { DockerTask, AssistantCatalog } from '../../shared/workspace/runtime'
import type { LocationQuery, RouteLocationNormalizedLoaded } from 'vue-router'
import { isChannelTask, taskName, taskIsActive, taskHasEnded, channelLimit } from '../../../shared/task-channel.mjs'
export { isChannelTask, taskName, taskIsActive, taskHasEnded, channelLimit }

export const taskPath = (id: string) => `/tasks/${encodeURIComponent(id)}`
export const tasksAssistantPath = (agent: string) => `/tasks/assistants/${encodeURIComponent(agent)}`
export const taskNeedsConversation = (task: DockerTask) => !taskIsActive(task) && (task.lastTurnReason === 'error' || !!task.queuePaused && task.queue.length > 0)
export function taskListQuery(query: LocationQuery) {
  const period = query.period === 'all' || query.period === '7d' ? query.period : '24h'
  const value = typeof query.page === 'string' && /^\d+$/.test(query.page) ? Number(query.page) : 1
  return {period, page:Number.isSafeInteger(value) && value > 0 ? value : 1}
}
export function taskListParams(period: string, page = 1) {
  return {...(period === 'all' || period === '7d' ? {period} : {}), ...(page > 1 ? {page:String(page)} : {})}
}
// Return destinations are task lists only: no external URLs or recursive trails.
export function taskReturnTo(value: unknown): string | undefined {
  if (typeof value !== 'string' || !/^\/tasks\/assistants\/[^/?#]+(?:\?[^#]*)?$/.test(value)) return
  const [path, search] = value.split('?')
  const query = taskListQuery(Object.fromEntries(new URLSearchParams(search)))
  const params = new URLSearchParams(taskListParams(query.period,query.page)).toString()
  return path + (params ? `?${params}` : '')
}
export function taskLink(path: string, route: Pick<RouteLocationNormalizedLoaded,'path'|'query'>, from?: 'supplement' | 'detail') {
  const list = /^\/tasks\/assistants\/[^/]+$/.test(route.path)
  const {period,page} = taskListQuery(route.query)
  const returnTo = list ? taskReturnTo(route.path + '?' + new URLSearchParams(taskListParams(period,page))) : taskReturnTo(route.query.returnTo)
  return {path,query:{...(returnTo ? {returnTo} : {}),...(from ? {from} : {}),...(from === 'supplement' && route.query.from === 'detail' ? {via:'detail'} : {})}}
}
export type Assistant = AssistantCatalog['items'][number] & { unavailable?: boolean }
export interface TaskDraft {
  workspaceMode?: import('../../../shared/workspace-mode.mjs').WorkspaceMode; directory?: string;
  title: string; prompt: string; files: File[]; taskId: string; modelId?: string
  busy: boolean; error: string; uncertain: boolean
}
export interface TaskViewState { offsets: Record<string, number>; drafts: Record<string, TaskDraft>; trail: string[] }
export const createTaskViewState = (): TaskViewState => ({offsets:{},drafts:{},trail:[]})
export const createTaskDraft = (): TaskDraft => ({workspaceMode:undefined,directory:undefined,title:'',prompt:'',files:[],taskId:'',modelId:undefined,busy:false,error:'',uncertain:false})
export function taskDuration(milliseconds: number): string {
  const seconds = Math.max(0,Math.floor(milliseconds/1000))
  const hours = Math.floor(seconds/3600), minutes = Math.floor(seconds%3600/60)
  return `${hours ? `${hours}小时` : ''}${hours || minutes ? `${minutes}分` : ''}${seconds%60}秒`
}
export function sortedTasks(tasks: readonly DockerTask[]): DockerTask[] {
  return tasks.filter(task => isChannelTask(task) && !task.archived).sort((a,b) => b.createdAt.localeCompare(a.createdAt) || a.id.localeCompare(b.id))
}
export function taskState(task: DockerTask): {label: string; icon: string} {
  const pending = task.pendingInteraction
  if (pending) return {label:pending.kind === 'question' ? '等待回答' : pending.purpose === 'iterations' ? '等待继续' : '等待确认',icon:'waiting'}
  if (task.status === 'waiting') return {label:'等待处理',icon:'waiting'}
  if (taskIsActive(task)) return {label:'执行中',icon:'working'}
  if (task.queuePaused && task.queue.length) return {label:'队列已暂停',icon:'waiting'}
  if (task.lastTurnReason === 'completed') return {label:'已完成',icon:'completed'}
  if (task.lastTurnReason === 'error') return {label:'失败',icon:'failed'}
  if (task.lastTurnReason === 'max-tokens') return {label:'输出截断',icon:'max-tokens'}
  if (task.lastTurnReason) return {label:'已停止',icon:'interrupted'}
  return {label:'未开始',icon:'idle'}
}
