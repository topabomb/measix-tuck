import type { DockerTask } from '../../frontend/src/shared/workspace/runtime'
export function platformFixture(options?: { native?: boolean; version?: number }): Promise<{
  url: string
  material: string
  snapshot: { managedGeneration: number; models: { modelId: string; displayName: string; inputModalities: string[]; enabled: boolean }[] }
  controls: { state: { activeManagedGeneration: number }; chatReply?: (body:any)=>any }
  models: { agentDefaults: { agents: Record<string, {modelId?: string}>; workbench:{assistants:{agent:string;deliveryTool?:boolean}[]} } }
  requests: { path: string; body: any; headers: Record<string, string> }[]
  app?: { store:{orchestration():{requireDelivery:boolean}}; manager: { forkOptions(id:string):Promise<{index:number}[]>; fork(id:string,index:number):Promise<{task:DockerTask}>; agentDefaults:{workbench:{assistants:{agent:string;deliveryTool?:boolean}[]}}; runs:Map<string,unknown>; create(options:{title:string;modelId:string;approvalMode:string}):Promise<DockerTask>; tasks: Map<string,DockerTask>; get(id:string):DockerTask; submit(id:string,prompt:string):unknown } }
  close(): Promise<void>
}>
