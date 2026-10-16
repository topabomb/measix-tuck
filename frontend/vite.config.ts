import { defineConfig } from 'vite'
import { fileURLToPath } from 'node:url'
import { readFileSync, readdirSync } from 'node:fs'
import vue from '@vitejs/plugin-vue'

function pagesBase(): string {
  if (process.env.GITHUB_PAGES !== 'true') return './'
  const repository = process.env.GITHUB_REPOSITORY?.split('/')[1]
  if (!repository || repository.endsWith('.github.io')) return '/'
  return `/${repository}/`
}

export default defineConfig({
  plugins: [vue(), {
    name:'workspace-pdf-resources',
    generateBundle() {
      for(const directory of ['cmaps','standard_fonts','wasm']) {
        const source=new URL(`./node_modules/pdfjs-dist/${directory}/`,import.meta.url)
        for(const name of readdirSync(source)) if(!name.endsWith('.md')) this.emitFile({type:'asset',fileName:`pdfjs/${directory}/${name}`,source:readFileSync(new URL(name,source))})
      }
    },
    configureServer(server) {
      server.middlewares.use('/pdfjs/',(req,res,next)=>{
        const match=req.url?.match(/^\/(cmaps|standard_fonts|wasm)\/([\w.-]+)$/)
        if(!match)return next()
        try{const content=readFileSync(new URL(`./node_modules/pdfjs-dist/${match[1]}/${match[2]}`,import.meta.url));res.setHeader('Content-Type',match[2].endsWith('.wasm')?'application/wasm':'application/octet-stream');res.end(content)}catch{next()}
      })
    },
  }],
  base: pagesBase(),
  server: { fs: { allow: [fileURLToPath(new URL('.', import.meta.url)), fileURLToPath(new URL('../shared', import.meta.url))] }, proxy: { '/api': { target: 'http://127.0.0.1:8090', changeOrigin: false } } },
  preview: { proxy: { '/api': { target: 'http://127.0.0.1:8090', changeOrigin: false } } },
  build: {
    outDir: process.env.TUCK_FRONTEND_DIR || 'dist',
    target: 'es2022',
    sourcemap: true,
  },
})
