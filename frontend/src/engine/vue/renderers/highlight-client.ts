interface Pending {
  resolve: (value: string) => void
  reject: (reason?: unknown) => void
}

const MAX_CACHE = 256
const cache = new Map<string, string>()
const pending = new Map<number, Pending>()
const inFlight = new Map<string, Promise<string>>()
let sequence = 0
let worker: Worker | null = null

function failWorker(reason: unknown): void {
  const error = reason instanceof Error ? reason : new Error(String(reason || 'Code highlighting worker failed'))
  const failed = worker
  worker = null
  failed?.terminate()
  for (const request of pending.values()) request.reject(error)
  pending.clear()
}

function getWorker(): Worker {
  if (worker) return worker
  const instance = new Worker(new URL('../../workers/highlight.worker.ts', import.meta.url), { type: 'module' })
  worker = instance
  instance.onmessage = (event: MessageEvent<{ id: number; html?: string; error?: string }>) => {
    const request = pending.get(event.data.id)
    if (!request) return
    pending.delete(event.data.id)
    if (event.data.error) request.reject(new Error(event.data.error))
    else request.resolve(event.data.html ?? '')
  }
  instance.onerror = event => failWorker(event.error ?? new Error(event.message || 'Code highlighting worker failed'))
  instance.onmessageerror = () => failWorker(new Error('Code highlighting worker returned an unreadable message'))
  return instance
}

function remember(key: string, html: string): void {
  cache.delete(key)
  cache.set(key, html)
  if (cache.size <= MAX_CACHE) return
  const oldest = cache.keys().next().value as string | undefined
  if (oldest) cache.delete(oldest)
}

export function highlightCode(code: string, language: string): Promise<string> {
  const key = `${language}\u0000${code}`
  const hit = cache.get(key)
  if (hit !== undefined) {
    cache.delete(key)
    cache.set(key, hit)
    return Promise.resolve(hit)
  }

  const running = inFlight.get(key)
  if (running) return running

  const id = ++sequence
  const promise = new Promise<string>((resolve, reject) => {
    pending.set(id, {
      resolve: html => {
        remember(key, html)
        resolve(html)
      },
      reject,
    })
    try {
      getWorker().postMessage({ id, code, language })
    } catch (error) {
      pending.delete(id)
      reject(error)
    }
  })
  inFlight.set(key, promise)
  const cleanup = () => { if (inFlight.get(key) === promise) inFlight.delete(key) }
  void promise.then(cleanup, cleanup)
  return promise
}

export function highlightCacheSize(): number {
  return cache.size
}
