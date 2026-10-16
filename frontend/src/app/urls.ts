import type { LogicalMessage } from '../engine/model/conversation'

// Ordinary document-relative URLs; no domain/prefix configuration or inference.
export function appUrl(path: string): string {
  if (!path.startsWith('/api/')) return path
  if (typeof document === 'undefined') return path
  const url = new URL(path.slice(1), document.baseURI)
  return url.pathname + url.search + url.hash
}

// History stays deployment-independent. Adapt local attachments at ingress;
// never scan text/tool output or streaming token deltas.
export function messageUrls(message: LogicalMessage): LogicalMessage {
  if (appUrl('/api/') === '/api/') return message
  return { ...message, blocks: message.blocks.map(block => block.type === 'attachments' ? {
    ...block, data: { ...block.data, items: block.data.items.map(item => ({
      ...item, ...(item.src ? { src: appUrl(item.src) } : {}),
      ...(item.resource ? { resource: { ...item.resource, uri: appUrl(item.resource.uri) } } : {}),
    })) },
  } : block) }
}
