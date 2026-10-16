import type { DockerTask } from '../../frontend/src/shared/workspace/runtime'
import type { LogicalMessage, ContentBlock } from '../../frontend/src/engine/model/conversation'
import type { LlmFailure, TurnEndReasonKind } from '../../frontend/src/engine/conversation/contracts'
export function until(fn: () => unknown | Promise<unknown>, timeout?: number): Promise<unknown>
export function fixture(options?: { imageGeneration?:string; attachmentInspection?:boolean; inspectionText?:string; inspectionAttachments?:{source?:string;path:string}[]; beforeResponse?: (prompt: string, body: { messages?: { role: string; tool_calls?: { function: { name: string } }[] }[]; tools?: { function?: { name?: string } }[] }) => void | Promise<void>; maxIterations?: number; attachmentModels?: boolean; localAssistants?: boolean; delivery?:boolean; enterprisePlatform?: unknown; security?: {listenHost?:string;auth:{enabled:boolean;username:string;passwordHash:string}} }): Promise<{
  url: string
  request(data:Record<string,unknown>):any
  requests: Record<string, unknown>[]
  deliver(task: DockerTask, summary: string, paths?: string[]): Promise<void>
  server: { closeAllConnections(): void }
  manager: {
    normalizer: { handle(task: DockerTask, event: Record<string, unknown>): Promise<void> }
    create(options?: { title?: string; approvalMode?: 'manual' | 'auto'; agent?: string; workspaceMode?:'auto'|'specified'|'none'; workspace?:string }): Promise<DockerTask>
    createChild(parent: DockerTask, nativeId: string, agent: string, mode?: 'foreground' | 'background'): DockerTask
    settle(task: DockerTask, reason: TurnEndReasonKind, failure?: LlmFailure): void
    baseTask(options: { id: string; nativeSessionId: string; title: string; workspace: string }): DockerTask
    append(task: DockerTask, message: { source?:{kind:string}; role: LogicalMessage['role']; turnId?: string; live?: boolean; blocks: ContentBlock[] }): LogicalMessage
    appendText(task: DockerTask, message: LogicalMessage, blockId: string, field: 'text' | 'markdown' | 'output', delta: string): void
    get(id: string): DockerTask
    selectModel(id:string,modelId:string,effort?:string):DockerTask
    replace(task: DockerTask, message: LogicalMessage): void
    publish(task: DockerTask, event: Record<string, unknown>): unknown
    submit(id: string, prompt: string): unknown
    runs: Map<string, unknown>
    interactions: Map<string, {pending: NonNullable<DockerTask['pendingInteraction']>}[]>
    tasks: Map<string, DockerTask>
  }
  docker: { request(path: string): Promise<{ title: string; messages: { message?: { content?: string } }[] }> }
  store: { tasks(): DockerTask[]; saveTask(task: DockerTask): void; saveMessage(id: string, message: LogicalMessage): void; message(id: string, index: number): LogicalMessage; transaction(fn: () => void): void }
  close(): Promise<void>
}>
