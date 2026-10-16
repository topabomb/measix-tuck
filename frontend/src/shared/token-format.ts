const integerFormat = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 })
const decimalFormat = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1, useGrouping: false })
export function hasReportedUsage(task: {usageSource?: string; modelCalls?: number | null; usage: Record<string, number>}): boolean {
  return task.usageSource === 'live' || task.usageSource === 'recovery' || (task.modelCalls ?? 0) > 0 || Object.values(task.usage).some(value => value > 0)
}
export function exactTokens(value: number | null | undefined): string {
  return value != null && Number.isFinite(value) && value >= 0 ? integerFormat.format(value) : '未上报'
}
export function compactTokens(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value) || value < 0) return '未上报'
  const n = Math.round(value)
  if (n < 1000) return String(n)
  if (n < 999950) return `${decimalFormat.format(n / 1000)}K`
  if (n < 999950000) return `${decimalFormat.format(n / 1000000)}M`
  return `${decimalFormat.format(n / 1000000000)}B`
}
