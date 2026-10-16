import { createHighlighterCoreSync } from '@shikijs/core'
import { createJavaScriptRegexEngine } from '@shikijs/engine-javascript'
import bash from '@shikijs/langs/bash'
import javascript from '@shikijs/langs/javascript'
import json from '@shikijs/langs/json'
import typescript from '@shikijs/langs/typescript'
import darkPlus from '@shikijs/themes/dark-plus'
import lightPlus from '@shikijs/themes/light-plus'

interface WorkerPort {
  onmessage: ((event: MessageEvent<{ id: number; code: string; language: string }>) => void) | null
  postMessage(data: { id: number; html?: string; error?: string }): void
}

// Keep this source in the application's normal DOM TypeScript program. Vite still
// bundles it as a dedicated Worker via new Worker(new URL(...)); this narrow port
// avoids globally loading lib.webworker alongside lib.dom.
const port = globalThis as unknown as WorkerPort

const highlighter = createHighlighterCoreSync({
  themes: [darkPlus, lightPlus],
  langs: [typescript, javascript, json, bash],
  engine: createJavaScriptRegexEngine(),
})

const aliases: Record<string, string> = {
  ts: 'typescript',
  typescript: 'typescript',
  js: 'javascript',
  javascript: 'javascript',
  json: 'json',
  sh: 'bash',
  bash: 'bash',
  shell: 'bash',
}

port.onmessage = (event) => {
  const { id, code, language } = event.data
  try {
    const lang = aliases[language] ?? 'typescript'
    const html = highlighter.codeToHtml(code, { lang, themes: { light: 'light-plus', dark: 'dark-plus' }, defaultColor: 'light-dark()' })
    port.postMessage({ id, html })
  } catch (error) {
    port.postMessage({ id, error: error instanceof Error ? error.message : String(error) })
  }
}
