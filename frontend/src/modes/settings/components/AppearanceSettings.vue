<script setup lang="ts">
import { ref } from 'vue'
import TuckMark from '../../../shared/ui/TuckMark.vue'
import { applyTheme, builtInThemes, customThemes, importTheme, removeTheme, selectedTheme } from '../../../app/themes'
const error = ref(''), url = ref(''), loading = ref(false)
async function loadFile(event: Event) {
  if (loading.value) return
  loading.value = true
  error.value = ''
  const input = event.target as HTMLInputElement, file = input.files?.[0]
  try { if (file) { if (file.size > 32768) throw new Error('主题文件最多 32 KiB'); importTheme(await file.text()) } } catch (reason) { error.value = (reason as Error).message }
  input.value = ''; loading.value = false
}
async function loadUrl() {
  if (loading.value) return
  loading.value = true; error.value = ''
  try {
    const address = new URL(url.value)
    if (!['https:', 'http:'].includes(address.protocol) || address.username || address.password) throw new Error('请输入无凭据的 HTTP(S) 主题地址')
    const response = await fetch(address, { credentials: 'omit', referrerPolicy: 'no-referrer', signal: AbortSignal.timeout(10000) })
    if (!response.ok) throw new Error(`主题下载失败：HTTP ${response.status}`)
    const reader = response.body?.getReader(); if (!reader) throw new Error('主题响应为空')
    const chunks: Uint8Array[] = []; let length = 0
    while (true) { const { done, value } = await reader.read(); if (done) break; length += value.length; if (length > 32768) { await reader.cancel(); throw new Error('主题文件最多 32 KiB') } chunks.push(value) }
    importTheme(await new Blob(chunks as BlobPart[]).text())
  } catch (reason) { error.value = (reason as Error).message }
  finally { loading.value = false }
}
</script>
<template>
    <section class="appearance-settings" aria-label="外观设置">
      <header><h3>主题</h3></header><p class="hint">选择界面配色，立即应用。</p>
      <div class="theme-grid"><button v-for="theme in [...builtInThemes, ...customThemes]" :key="theme.id" :aria-label="theme.name" :aria-pressed="selectedTheme === theme.id" @click="applyTheme(theme.id)"><span class="theme-preview" aria-hidden="true" :style="{ background: theme.colors.background, color: theme.colors.text, borderColor: theme.colors.border }"><TuckMark :style="{ color: theme.colors.accent }" /><strong>Tuck</strong></span>{{ theme.name }}</button></div>
      <details><summary>加载外部主题</summary><p>导入 JSON 文件，或提供允许跨域读取的 HTTP(S) 地址。</p><input type="file" accept="application/json,.json" aria-label="导入主题文件" @change="loadFile" /><form @submit.prevent="loadUrl"><input v-model="url" type="url" aria-label="主题地址" placeholder="https://example.com/theme.json" required /><button :disabled="loading">{{ loading ? '加载中…' : '加载' }}</button></form><a href="themes/ocean.json" download>下载主题格式示例</a><div v-for="theme in customThemes" :key="theme.id"><span>{{ theme.name }}</span><button @click="removeTheme(theme.id)">移除</button></div></details>
      <p v-if="error" role="alert">{{ error }}</p>
    </section>
</template>
<style scoped>
.appearance-trigger { display:flex; align-items:center; gap:8px; }
.appearance-trigger > svg { width:18px; height:18px; flex:none; color:var(--tuck-accent); }
.appearance-trigger { width:100%; margin-top:6px; text-align:left; }
.appearance-settings header { display:flex; align-items:center; justify-content:space-between; }
.appearance-settings .theme-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px; }
.theme-grid button { display:grid; gap:8px; text-align:left; }
.theme-grid button[aria-pressed=true] { border-color:var(--tuck-accent); background:var(--tuck-hover); }
.theme-preview { display:flex; align-items:center; justify-content:flex-start; gap:8px; border:1px solid; border-radius:6px; padding:6px 10px; line-height:32px; }
.theme-preview > .tuck-mark { width:32px; height:32px; }
.theme-preview strong { font-size:20px; font-weight:650; letter-spacing:-.025em; }
details { font-size:var(--tuck-font-ui); } summary { cursor:pointer; } form { display:flex; gap:8px; margin:12px 0; } a { color:var(--tuck-accent); } [role=alert] { color:var(--tuck-danger); }
.appearance-settings { display:grid; gap:12px; }
button,input { font:inherit; border:1px solid var(--tuck-border); border-radius:7px; padding:8px 10px; background:var(--tuck-raised); color:var(--tuck-text); cursor:pointer; }
h2 { margin:0; font-size:var(--tuck-font-title); } .theme-grid button { min-width:0; } form input { min-width:0; flex:1; }
</style>
