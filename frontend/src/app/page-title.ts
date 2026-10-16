import type { RouteLocationNormalizedLoaded } from 'vue-router'
import type { DockerWorkspaceRuntime } from '../shared/workspace/runtime'
import { isChannelTask, taskName } from '../../shared/task-channel.mjs'

// Route identity owns the title; loaded names enrich it without another fetch.
export function pageTitle(route: Pick<RouteLocationNormalizedLoaded,'name'|'path'|'params'|'meta'>, workspace?: DockerWorkspaceRuntime | null, authenticated?: boolean): string {
  if (authenticated === false) return '登录 · Tuck'
  if (route.path === '/' || route.path === '/chat') return 'Tuck · AI 工作代理'
  const task = typeof route.params.id === 'string' ? workspace?.tasks.get(route.params.id) : undefined
  const title = task && (route.meta.mode !== 'tasks' || isChannelTask(task) && !task.archived) ? taskName(task.title) : undefined
  const agent = typeof route.params.agent === 'string' ? route.params.agent : undefined
  const assistant = workspace?.runtimeInfo.assistants?.items.find(item => item.agent === agent)?.displayName
    || (agent ? [...(workspace?.tasks.values() || [])].find(task => task.agent === agent)?.assistantName : undefined)
  let parts: (string | undefined)[]
  switch (route.name) {
    case 'chat-delivery': parts = ['查看交付',title,'对话']; break
    case 'tasks': parts = ['任务']; break
    case 'task-list': parts = [assistant,'全部任务']; break
    case 'task-new': parts = ['新建任务',assistant]; break
    case 'task-detail': parts = [title,'任务详情']; break
    case 'task-delivery': parts = ['查看交付',title]; break
    case 'task-supplement': parts = ['补充任务',title]; break
    default:
      if (route.meta.mode === 'chat') parts = [title || assistant, typeof route.params.message === 'string' && /^\d+$/.test(route.params.message) ? `消息 ${route.params.message}` : undefined,'对话']
      else if (route.meta.mode === 'settings') parts = [({providers:'提供商与模型',agents:'Agent',mcp:'MCP',platform:'企业平台',orchestration:'编排',engine:'引擎',appearance:'外观',protection:'配置保护'} as Record<string,string>)[String(route.meta.settingsSection)],'设置']
      else if (route.meta.mode === 'goals') parts = ['目标']
      else if (route.meta.mode === 'orchestration') parts = ['编排']
      else if (route.meta.mode === 'workspace') parts = ['工作区']
      else if (route.path === '/reference') parts = ['参考工作台']
      else if (route.path === '/architecture') parts = ['架构说明']
      else parts = ['页面不存在']
  }
  return [...parts.filter(Boolean).map(part => part!.replace(/[\s\u0000-\u001f\u007f]+/g,' ').trim()),'Tuck'].join(' · ')
}
