import { deriveSessionIndicator, type SessionIndicator } from '../../engine/conversation/session-semantics'
import type { ConversationDescriptor } from '../../engine/conversation/contracts'
import type { DockerTask } from '../../shared/workspace/runtime'

export { hasReportedUsage } from '../../shared/token-format'

export function indicatorLabel(state: SessionIndicator): string {
  return { idle:'空闲', working:'执行中', blocked:'等待处理', failed:'失败', interrupted:'已停止', 'max-tokens':'已达输出上限', completed:'已完成' }[state]
}

export function taskStatus(task: ConversationDescriptor & Pick<Partial<DockerTask>, 'compaction'>): string {
  if (task.pendingInteraction) return task.pendingInteraction.kind === 'question' ? '等待回答' : task.pendingInteraction.purpose === 'iterations' ? '等待继续确认' : '等待审批'
  if (task.compaction?.status === 'running' && task.status === 'working') return '压缩上下文中'
  return indicatorLabel(deriveSessionIndicator(task))
}

/** Product navigation reads descriptors only, never messages or child viewports. */
export function sessionFamily(tasks: readonly DockerTask[], activeId: string) {
  const byId = new Map(tasks.map(task => [task.id, task]))
  const ancestry: DockerTask[] = []
  const seen = new Set<string>()
  let cursor = byId.get(activeId)
  while (cursor && !seen.has(cursor.id)) {
    seen.add(cursor.id); ancestry.unshift(cursor)
    cursor = cursor.parentTaskId ? byId.get(cursor.parentTaskId) : undefined
  }
  const root = ancestry[0]
  const children = new Map<string, DockerTask[]>()
  for (const task of tasks) {
    if (!task.parentTaskId || task.archived && task.id !== activeId) continue
    const group = children.get(task.parentTaskId) || []
    group.push(task); children.set(task.parentTaskId, group)
  }
  const rows: { task: DockerTask; depth: number }[] = []
  const visited = new Set<string>()
  const stack = root ? [{ task: root, depth: 0 }] : []
  while (stack.length) {
    const row = stack.pop()!
    if (visited.has(row.task.id)) continue
    visited.add(row.task.id); rows.push(row)
    const group = children.get(row.task.id) || []
    for (let i = group.length - 1; i >= 0; i--) stack.push({ task: group[i]!, depth: row.depth + 1 })
  }
  return { ancestry, rows, root }
}
