import { block, type AppendCanonicalMessage, type ContentBlock, type ResourceRef } from '../engine/model/conversation'

export interface ReferenceScenarioMessage extends AppendCanonicalMessage {
  /** Reference/test locator only; never enters the canonical message model. */
  fixtureId?: string
}

export const MARKDOWN_COMPATIBILITY_FIXTURES = [
  { id: 'gfm-basics', title: 'GFM basics', markdown: `# Markdown compatibility\n\n## Typography\n\n**bold**, *italic*, ~~strike~~, \`inline code\`, and [a link](https://example.com).\n\n> Blockquotes should wrap naturally without affecting the viewport contract.\n\n---\n\nParagraphs can contain very_long_unbroken_identifiers_that_must_not_expand_the_page_width_beyond_the_conversation_viewport.` },
  { id: 'lists-tasks', title: 'Lists and tasks', markdown: `## Lists and tasks\n\n- first item\n  - nested item\n    - deeply nested item\n- second item\n\n1. ordered\n2. ordered\n\n- [x] projected from canonical content\n- [ ] preserve semantic anchor during reflow\n- [ ] keep physical DOM bounded` },
  { id: 'wide-table', title: 'Wide GFM table', markdown: `## Wide table\n\n| Layer | Ownership | Durable | Framework | Notes |\n| --- | --- | --- | --- | --- |\n| SessionKernel | execution, turns, blockers, usage | yes | no | can outlive every viewport |\n| Projection | bounded keyed nodes | rebuildable | no | hot only |\n| Viewport | reader, anchor, follow | snapshot | no | semantic coordinates |\n| Adapter | physical measurement | no | yes | replaceable |\n| Renderer | presentation | no | yes | extensible registry |` },
  { id: 'fences', title: 'Fenced code', markdown: `## Fenced code\n\n\`\`\`ts\ninterface ConversationRenderer {\n  id: string\n  render(node: RenderUnit): unknown\n}\n\nconst stable = true\n\`\`\`\n\n\`\`\`json\n{ "cacheHit": 0.83, "input": 125000, "output": 8400 }\n\`\`\`\n\nA fence must never be split in the middle by the presentation chunker.` },
  { id: 'raw-html', title: 'HTML sanitization', markdown: `## Embedded HTML\n\n<div class="markdown-inline-artifact"><strong>Allowed semantic HTML</strong><span> stays inside the Markdown renderer.</span></div>\n\n<script>window.__markdownUnsafe = true</script>\n\nThe script element must not survive sanitization.` },
  { id: 'long-document', title: 'Long document', markdown: Array.from({ length: 24 }, (_, i) => `### Section ${i + 1}\n\nA long assistant document exercises headings, paragraphs, inline \`code_${i}\`, **emphasis**, links and variable wrapping. ${'conversation projection semantic viewport renderer cache '.repeat(12)}`).join('\n\n') },
] as const

export function createMarkdownGalleryTurn(scope: string, ordinal: number): ReferenceScenarioMessage[] {
  const turnId = `${scope}:fixture-markdown-${ordinal}`
  const stepId = `${turnId}:step-0`
  return MARKDOWN_COMPATIBILITY_FIXTURES.map(item => ({
    fixtureId: `markdown:${item.id}`,
    turnId,
    stepId,
    role: 'assistant' as const,
    blocks: [block(item.id, 'markdown', { markdown: item.markdown, flavor: 'gfm' })],
  }))
}

export function createMixedReferenceTurn(scope: string, ordinal: number): ReferenceScenarioMessage[] {
  const turnId = `${scope}:fixture-mixed-${ordinal}`
  const stepId = `${turnId}:step-0`
  const callId = `fixture_call_${ordinal}`
  const visual = ordinal % 4
  const path = `/workspace/fixture-${ordinal}.ts`
  const fixtureResource: ResourceRef = { id: `fixture-resource-${ordinal}`, kind: 'file', uri: path, label: path }
  const rich: ContentBlock[] = [
    block('reasoning', 'reasoning', { text: `Inspect session state, preserve stable identities, and verify viewport invariants for mixed turn ${ordinal}.`, tokenCount: 58 + ordinal, durationMs: 1200 + ordinal * 73, defaultOpen: false, status: 'complete' }),
    block('answer', 'markdown', { markdown: `## Runtime-injected heterogeneous turn ${ordinal}\n\nThis content entered through the canonical SessionKernel and the same projection/renderer path used by real history.`, flavor: 'gfm' }),
  ]
  if (visual === 0) rich.push(block('code', 'code', { language: 'typescript', filename: `src/runtime-fixture-${ordinal}.ts`, resource: fixtureResource, code: Array.from({ length: 46 }, (_, i) => `const fixture_${ordinal}_${i} = project(blocks[${i}], { stable: true })`).join('\n') }))
  if (visual === 1) rich.push(block('diff', 'diff', { resource: fixtureResource, lines: Array.from({ length: 64 }, (_, i) => `${i % 3 === 0 ? '+' : i % 5 === 0 ? '-' : ' '} line ${i + 1}: renderer registry invariant ${ordinal}`) }))
  if (visual === 2) rich.push(block('image', 'image', { width: 1440, height: 810, seed: 9000 + ordinal, alt: `Synthetic responsive image fixture ${ordinal}` }))
  if (visual === 3) rich.push(block('html', 'html', { html: `<section class="synthetic-html"><h3>Runtime artifact ${ordinal}</h3><p>Sanitized HTML follows the same ContentBlock contract.</p><div class="html-chip">responsive container</div><script>window.__fixtureUnsafe=true</script></section>` }))

  const presentation = { kind: 'resources' as const, resources: [fixtureResource] }
  return [
    { fixtureId: `mixed:${ordinal}:user`, turnId, stepId, role: 'user', blocks: [block('request', 'markdown', { markdown: `Please execute runtime fixture **${ordinal}** and demonstrate a mixed Agent turn.` })] },
    { fixtureId: `mixed:${ordinal}:assistant`, turnId, stepId, role: 'assistant', blocks: rich },
    { fixtureId: `mixed:${ordinal}:tool-call`, turnId, stepId, role: 'assistant', blocks: [block('tool-call', 'tool-call', { name: ordinal % 2 ? 'search' : 'read_file', callId, category: ordinal % 2 ? 'search' : 'filesystem', presentation, resources: [fixtureResource], input: { path, query: `renderer ${ordinal}`, limit: 20 }, durationMs: 18, status: 'success', defaultOpen: false })] },
    { fixtureId: `mixed:${ordinal}:tool-result`, turnId, stepId, role: 'tool', blocks: [block('tool-result', 'tool-result', { name: ordinal % 2 ? 'search' : 'read_file', callId, category: ordinal % 2 ? 'search' : 'filesystem', presentation, resources: [fixtureResource], output: { rows: [{ line: 12, preview: 'stable keyed content' }, { line: 41, preview: 'responsive renderer containment' }], exitCode: 0 }, durationMs: 32, status: 'success', defaultOpen: false })] },
    { fixtureId: `mixed:${ordinal}:summary`, turnId, stepId, role: 'assistant', blocks: [block('summary', 'markdown', { markdown: `### Verified\n\nFixture ${ordinal} crossed **SessionKernel → Projector Registry → keyed projection → virtualizer → Renderer Registry**.` })] },
  ]
}

export function createMixedReferenceTurns(scope: string, startOrdinal: number, count: number): ReferenceScenarioMessage[] {
  return Array.from({ length: count }, (_, index) => createMixedReferenceTurn(scope, startOrdinal + index)).flat()
}

export const AGENT_SCENARIO_MESSAGE_COUNT = 13

/** Compact realistic Agent-workspace pack used by both public reference and E2E. */
export function createAgentScenarioPack(scope: string, ordinal: number): ReferenceScenarioMessage[] {
  const uploadTurn = `${scope}:scenario-upload-${ordinal}`
  const uploadStep = `${uploadTurn}:step-0`
  const imageTurn = `${scope}:scenario-image-gen-${ordinal}`
  const imageStep = `${imageTurn}:step-0`
  const imageCall = `image_gen_${ordinal}`
  const imagePrompt = 'A compact futuristic agent workstation on a dark desk, precise interface details, cinematic soft light.'
  const imageModel = 'image-gen-reference-v2'
  const ttsTurn = `${scope}:scenario-tts-${ordinal}`
  const ttsStep = `${ttsTurn}:step-0`
  const ttsCall = `tts_${ordinal}`
  const ttsText = 'The framework keeps media artifacts independent from tool execution state while preserving stable semantic identity.'
  const asrTurn = `${scope}:scenario-asr-${ordinal}`
  const asrStep = `${asrTurn}:step-0`
  const asrCall = `asr_${ordinal}`
  const transcript = 'Please summarize the uploaded meeting recording and list the concrete follow-up actions.'

  return [
    {
      fixtureId: 'scenario-upload-single', turnId: uploadTurn, stepId: uploadStep, role: 'user',
      blocks: [block('single-upload', 'attachments', { title: 'Single image upload', provenance: { origin: 'user-upload' }, items: [
        { id: `upload-${ordinal}-photo`, name: 'whiteboard.png', kind: 'image', mimeType: 'image/png', width: 1280, height: 860, sizeBytes: 684_220, seed: 1210 + ordinal },
      ] })],
    },
    {
      fixtureId: 'scenario-upload-multiple', turnId: uploadTurn, stepId: uploadStep, role: 'user',
      blocks: [block('multi-upload', 'attachments', { title: 'Multi-file upload', provenance: { origin: 'user-upload' }, items: [
        { id: `upload-${ordinal}-ui-a`, name: 'screen-a.png', kind: 'image', mimeType: 'image/png', width: 1170, height: 780, sizeBytes: 488_100, seed: 1310 + ordinal },
        { id: `upload-${ordinal}-ui-b`, name: 'screen-b.jpg', kind: 'image', mimeType: 'image/jpeg', width: 900, height: 1200, sizeBytes: 731_800, seed: 1410 + ordinal },
        { id: `upload-${ordinal}-pdf`, name: 'requirements.pdf', kind: 'file', mimeType: 'application/pdf', sizeBytes: 1_942_200 },
        { id: `upload-${ordinal}-audio`, name: 'meeting.m4a', kind: 'audio', mimeType: 'audio/mp4', sizeBytes: 3_804_100, durationMs: 91_000 },
      ] })],
    },
    {
      fixtureId: 'scenario-upload-response', turnId: uploadTurn, stepId: uploadStep, role: 'assistant',
      blocks: [
        block('reasoning', 'reasoning', { text: 'Inspect the screenshots, document and audio without treating physical attachment cards as model state.', tokenCount: 31, durationMs: 760, defaultOpen: true, status: 'complete' }),
        block('answer', 'markdown', { markdown: '### Uploads received\n\nThe uploaded artifacts retain individual identities while the upload action remains one grouped presentation block.' }),
      ],
    },
    {
      fixtureId: 'scenario-image-gen-call', turnId: imageTurn, stepId: imageStep, role: 'assistant',
      blocks: [block('image-gen-call', 'tool-call', { name: 'generate_images', callId: imageCall, category: 'image-generation', model: imageModel, status: 'running', progress: 42, input: { prompt: imagePrompt, count: 4 }, durationMs: 1480, defaultOpen: false })],
    },
    {
      fixtureId: 'scenario-image-gen-result', turnId: imageTurn, stepId: imageStep, role: 'tool',
      blocks: [block('image-gen-result', 'tool-result', { name: 'generate_images', callId: imageCall, category: 'image-generation', model: imageModel, status: 'success', progress: 100, output: { status: 'completed', files: Array.from({ length: 4 }, (_, i) => ({ path: `artifacts/images/img_${String(i + 1).padStart(16, '0')}.png`, prompt_path: `artifacts/images/img_${String(i + 1).padStart(16, '0')}.prompt.txt` })) }, durationMs: 8420, defaultOpen: false })],
    },
    {
      fixtureId: 'scenario-image-gen-artifacts', turnId: imageTurn, stepId: imageStep, role: 'assistant',
      blocks: [block('generated-images', 'attachments', {
        title: 'Generated image set',
        provenance: { origin: 'tool-output', toolCallId: imageCall, toolName: 'generate_images', model: imageModel, prompt: imagePrompt },
        items: Array.from({ length: 4 }, (_, i) => ({ id: `generated-${ordinal}-${i + 1}`, name: `agent-workstation-${i + 1}.png`, kind: 'image' as const, mimeType: 'image/png', width: 1024, height: 1024, sizeBytes: 1_100_000 + i * 73_000, seed: 8800 + ordinal * 10 + i, resource: { id: `generated-resource-${ordinal}-${i + 1}`, kind: 'artifact' as const, uri: `artifact://generated-${ordinal}-${i + 1}`, label: `agent-workstation-${i + 1}.png` } })),
      })],
    },
    {
      fixtureId: 'scenario-tts-call', turnId: ttsTurn, stepId: ttsStep, role: 'assistant',
      blocks: [block('tts-call', 'tool-call', { name: 'text_to_speech', callId: ttsCall, category: 'tts', model: 'tts-reference-1', status: 'running', progress: 65, input: { text: ttsText, voice: 'neutral', format: 'mp3' }, durationMs: 320, defaultOpen: false })],
    },
    {
      fixtureId: 'scenario-tts-result', turnId: ttsTurn, stepId: ttsStep, role: 'tool',
      blocks: [block('tts-result', 'tool-result', { name: 'text_to_speech', callId: ttsCall, category: 'tts', model: 'tts-reference-1', status: 'success', progress: 100, output: { artifactId: `tts-audio-${ordinal}`, durationMs: 12_800, format: 'mp3' }, durationMs: 1290, defaultOpen: false })],
    },
    {
      fixtureId: 'scenario-tts-artifact', turnId: ttsTurn, stepId: ttsStep, role: 'assistant',
      blocks: [block('tts-audio', 'audio', { title: 'Generated speech', purpose: 'tts', durationMs: 12_800, transcript: ttsText, model: 'tts-reference-1', status: 'ready', waveform: Array.from({ length: 42 }, (_, i) => 0.18 + ((i * 23 + ordinal * 7) % 74) / 100) })],
    },
    {
      fixtureId: 'scenario-asr-input', turnId: asrTurn, stepId: asrStep, role: 'user',
      blocks: [block('asr-input-audio', 'audio', { title: 'Voice message', purpose: 'asr-input', durationMs: 18_400, transcript: `Input preview for uploaded meeting recording. ${transcript}`, model: 'asr-reference-1', status: 'ready', waveform: Array.from({ length: 40 }, (_, i) => 0.2 + ((i * 31 + ordinal * 11) % 70) / 100) })],
    },
    {
      fixtureId: 'scenario-asr-call', turnId: asrTurn, stepId: asrStep, role: 'assistant',
      blocks: [block('asr-call', 'tool-call', { name: 'speech_to_text', callId: asrCall, category: 'asr', model: 'asr-reference-1', status: 'running', progress: 74, input: { artifactId: `upload-${ordinal}-audio`, language: 'auto' }, durationMs: 240, defaultOpen: false })],
    },
    {
      fixtureId: 'scenario-asr-result', turnId: asrTurn, stepId: asrStep, role: 'tool',
      blocks: [block('asr-result', 'tool-result', { name: 'speech_to_text', callId: asrCall, category: 'asr', model: 'asr-reference-1', status: 'success', progress: 100, output: { transcript, language: 'en', confidence: 0.97, durationMs: 18_400 }, durationMs: 870, defaultOpen: false })],
    },
    {
      fixtureId: 'scenario-summary', turnId: asrTurn, stepId: `${asrTurn}:step-1`, role: 'assistant',
      blocks: [block('scenario-summary', 'markdown', { markdown: '### Media workflows verified\n\nUploads, generated images, TTS audio and ASR transcripts all use stable canonical identities while their physical renderers remain independently replaceable.' })],
    },
  ]
}
