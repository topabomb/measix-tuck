import { marked } from 'marked'

export interface MarkdownChunk {
  text: string
  index: number
  hash: number
}

/** Shared parser contract for Markdown chunking and rendering. */
export const MARKDOWN_OPTIONS = { gfm: true, breaks: false }

/**
 * Split long Markdown only between top-level GFM blocks. The lexer is the same
 * grammar used by the renderer, so lists, tables, blockquotes, fences and other
 * container blocks remain atomic even when they contain internal blank lines.
 *
 * Whitespace tokens stay attached to the preceding chunk. Oversized atomic
 * blocks are intentionally left intact rather than changing Markdown semantics
 * merely to hit the target size.
 */
export function splitMarkdown(source: string, targetChars = 6000): MarkdownChunk[] {
  if (!source) return [chunk('', 0)]

  const tokens = marked.lexer(source, MARKDOWN_OPTIONS)
  if (tokens.map(token => token.raw).join('') !== source) return [chunk(source, 0)]

  const chunks: string[] = []
  let buffer = ''

  for (const token of tokens) {
    const raw = token.raw
    if (token.type === 'space') {
      buffer += raw
      continue
    }

    if (buffer && buffer.length + raw.length > targetChars) {
      chunks.push(buffer)
      buffer = ''
    }
    buffer += raw
  }

  if (buffer || chunks.length === 0) chunks.push(buffer)
  return chunks.map((text, index) => chunk(text, index))
}

function chunk(text: string, index: number): MarkdownChunk {
  return { text, index, hash: hashText(text) }
}

export function hashText(value: string): number {
  let hash = 2166136261
  for (let i = 0; i < value.length; i += 1) {
    hash ^= value.charCodeAt(i)
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}
