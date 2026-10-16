import type { DockerTask } from './runtime'

export function activityLabel(task: DockerTask): string {
  const activity = task.activity
  if (task.compaction?.status === 'running') return '正在压缩上下文'
  if (activity?.phase === 'processing' && activity.afterCompaction) return '压缩完成，继续处理中…'
  if (activity?.phase === 'tool') return `正在执行 · ${activity.detail || '工具'}`
  if (activity?.phase === 'preparing' && activity.detail) return activity.detail
  return { preparing:'准备执行…', processing:'正在请求…', reasoning:'思考中…', responding:'正在回复…', tool:'执行工具…', compacting:'正在压缩上下文', finishing:'正在收尾…' }[activity?.phase || 'processing']
}
