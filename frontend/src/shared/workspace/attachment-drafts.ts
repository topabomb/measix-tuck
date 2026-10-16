import { authFetch } from '../../app/auth'
export interface UploadedAttachment { id: string; name: string; path: string; mime: string; size: number; kind: 'image' | 'file' }
export interface DraftAttachment { id: string; file: File; previewUrl?: string; status: 'uploading' | 'ready' | 'error'; error?: string; uploaded?: UploadedAttachment }
type Upload = (taskId: string, file: File, signal: AbortSignal) => Promise<UploadedAttachment>
async function upload(taskId: string, file: File, signal: AbortSignal): Promise<UploadedAttachment> {
  const base64 = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '')
    reader.onerror = () => reject(new Error('文件读取失败'))
    reader.readAsDataURL(file)
  })
  const response = await authFetch(`/api/tasks/${taskId}/upload`, { method: 'POST', signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: file.name, mime: file.type, base64 }) })
  const value = await response.json()
  if (!response.ok) throw new Error(value.error || `上传失败 (${response.status})`)
  if (typeof value.id !== 'string' || typeof value.path !== 'string' || !['image', 'file'].includes(value.kind)) throw new Error('上传接口版本不一致，请在任务结束后重启后端')
  return value as UploadedAttachment
}

// Product-owned draft state outlives the three hot rendering runtimes.
// Only this small list changes during an upload; history is untouched.
export class AttachmentDrafts {
  #items = new Map<string, readonly DraftAttachment[]>()
  #listeners = new Map<string, Set<() => void>>()
  #pending: { taskId: string; id: string }[] = []
  #running = new Map<string, AbortController>()
  #disposed = false
  constructor(private readonly send: Upload = upload) {}
  get closed(): boolean { return this.#disposed }
  list(taskId: string): readonly DraftAttachment[] { return this.#items.get(taskId) || [] }
  subscribe(taskId: string, listener: () => void): () => void {
    const listeners = this.#listeners.get(taskId) || new Set<() => void>()
    listeners.add(listener); this.#listeners.set(taskId, listeners)
    return () => { listeners.delete(listener); if (!listeners.size) this.#listeners.delete(taskId) }
  }
  #set(taskId: string, items: readonly DraftAttachment[]): void {
    if (items.length) this.#items.set(taskId, items); else this.#items.delete(taskId)
    for (const listener of this.#listeners.get(taskId) || []) listener()
  }
  add(taskId: string, files: readonly File[]): string | null {
    if (this.#disposed) return '工作台已关闭'
    if (this.list(taskId).length + files.length > 8) return '每条消息最多 8 个附件'
    if (files.some(file => file.size > 16 * 1024 * 1024)) return '单个文件最多 16 MiB'
    // randomUUID is unavailable on plain-HTTP LAN origins; getRandomValues is
    // available there and these IDs are only local draft identities.
    const items = files.map(file => ({ id: Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2,'0')).join(''), file, status: 'uploading' as const, ...(/^image\/(png|jpeg|webp|gif)$/.test(file.type) ? { previewUrl: URL.createObjectURL(file) } : {}) }))
    this.#set(taskId, [...this.list(taskId), ...items])
    this.#pending.push(...items.map(item => ({ taskId, id: item.id })))
    this.#pump()
    return null
  }
  remove(taskId: string, id: string): void {
    const item = this.list(taskId).find(item => item.id === id)
    if (item?.previewUrl) URL.revokeObjectURL(item.previewUrl)
    this.#running.get(id)?.abort()
    this.#pending = this.#pending.filter(entry => entry.id !== id)
    this.#set(taskId, this.list(taskId).filter(item => item.id !== id))
  }
  retry(taskId: string, id: string): void {
    if (this.#disposed || this.#running.has(id)) return
    const item = this.list(taskId).find(item => item.id === id)
    if (item?.status !== 'error') return
    this.#set(taskId, this.list(taskId).map(item => item.id === id ? { ...item, status: 'uploading', error: undefined } : item))
    this.#pending.push({ taskId, id }); this.#pump()
  }
  #pump(): void {
    while (!this.#disposed && this.#running.size < 2 && this.#pending.length) {
      const { taskId, id } = this.#pending.shift()!
      const item = this.list(taskId).find(item => item.id === id)
      if (!item) continue
      const controller = new AbortController(); this.#running.set(id, controller)
      void this.send(taskId, item.file, controller.signal).then(uploaded => {
        if (!this.#disposed && this.list(taskId).some(item => item.id === id)) this.#set(taskId, this.list(taskId).map(item => item.id === id ? { ...item, status: 'ready', uploaded } : item))
      }, (error: Error) => {
        if (!this.#disposed && this.list(taskId).some(item => item.id === id)) this.#set(taskId, this.list(taskId).map(item => item.id === id ? { ...item, status: 'error', error: error.message } : item))
      }).finally(() => { this.#running.delete(id); this.#pump() })
    }
  }
  dispose(): void {
    this.#disposed = true
    for (const controller of this.#running.values()) controller.abort()
    for (const items of this.#items.values()) for (const item of items) if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
    this.#items.clear(); this.#pending = []
    for (const listeners of this.#listeners.values()) for (const listener of [...listeners]) listener()
    this.#listeners.clear()
  }
}
