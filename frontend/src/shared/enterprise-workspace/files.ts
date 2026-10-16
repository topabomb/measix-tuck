export interface Entry { path:string; kind:'FILE'|'DIRECTORY'; size?:number; modifiedAt?:string; etag?:string }
export interface Projection { agentSpaceId?:string; subject:string; employeeName:string; serviceState?:'NOT_CONFIGURED'|'DISABLED'|'ENABLED'; filesAvailable:boolean; filesReason:string; state:string }
export const basename = (path:string) => path.split('/').pop() || ''
export const parent = (path:string) => path.split('/').slice(0,-1).join('/')
export const join = (dir:string,name:string) => dir ? `${dir}/${name}` : name
export function copyPath(path:string,exists:(candidate:string)=>boolean,directory=false):string {const name=basename(path),at=directory?-1:name.lastIndexOf('.'),stem=at>0?name.slice(0,at):name,extension=at>0?name.slice(at):'';let n=1,candidate=path;while(exists(candidate))candidate=join(parent(path),`${stem} (${n++})${extension}`);return candidate}
export function validPath(path:string,root = true):boolean { return path.length <= 4096 && !/[\\\x00-\x1f\x7f]/.test(path) && (path === '' ? root : path.split('/').every(p => !!p && p !== '.' && p !== '..')) }
export function kind(path:string):string {
  const ext = basename(path).split('.').pop()!.toLowerCase()
  if (['png','jpg','jpeg','gif','webp','bmp'].includes(ext)) return 'image'
  if (ext === 'pdf') return 'pdf'
  if (['mp3','wav','ogg','m4a','flac','aac'].includes(ext)) return 'audio'
  if (['mp4','webm','ogv','mov'].includes(ext)) return 'video'
  if (['html','htm','svg'].includes(ext)) return 'html'
  if (['md','markdown'].includes(ext)) return 'markdown'
  if (['csv','tsv'].includes(ext)) return 'csv'
  if (['txt','log','json','jsonl','yaml','yml','xml','toml','ini','cfg','conf','env','js','jsx','ts','tsx','css','scss','less','vue','svelte','py','rs','go','java','kt','kts','c','h','cpp','hpp','cs','sh','bash','ps1','sql','rb','php','swift','r','dart','lua','dockerfile','makefile','gitignore','properties'].includes(ext) || ['LICENSE','README'].includes(basename(path))) return 'text'
  return 'binary'
}
export function bytes(size?:number):string { if (size === undefined) return '—'; if (size < 1024) return `${size} B`; const unit = Math.min(3,Math.floor(Math.log(size)/Math.log(1024))); return `${(size/1024**unit).toFixed(1)} ${['B','KiB','MiB','GiB'][unit]}` }
export function decodeText(data:Uint8Array, truncated = false) {
  const bom = data[0] === 239 && data[1] === 187 && data[2] === 191
  let text:string, invalid = false
  try { text = new TextDecoder('utf-8',{fatal:true}).decode(data,{stream:truncated}) } catch { text = new TextDecoder().decode(data); invalid = true }
  if (text.includes('\0')) throw new Error('这是二进制文件，无法作为文本查看')
  const endings = new Set(text.match(/\r\n|\r|\n/g) || [])
  return {text:text.replace(/\r\n|\r/g,'\n'),bom,newline:endings.size === 1 ? [...endings][0]! : '\n',readonly:invalid || endings.size > 1 || truncated,reason:truncated ? '大型文件：仅显示开头 256 KiB，不能保存截断内容' : invalid ? '非 UTF-8 文本，仅供查看；转换后可另存' : endings.size > 1 ? '混合换行：转换为 LF 后可另存' : ''}
}
export function encodeText(text:string,bom:boolean,newline:string):Uint8Array { return new TextEncoder().encode((bom ? '\ufeff' : '')+text.replace(/\r\n|\r/g,'\n').replace(/\n/g,newline)) }
export function parseCSV(text:string, delimiter:string):string[][] {
  const rows:string[][] = []; let row:string[] = [], cell = '', quoted = false, closed = false
  for (let i=0;i<text.length;i++) {
    const c = text[i]!
    if (quoted) { if (c === '"') { if (text[i+1] === '"') { cell += '"'; i++ } else { quoted = false; closed = true } } else cell += c; continue }
    if (c === '"' && !cell && !closed) quoted = true
    else if (c === delimiter) { row.push(cell); cell = ''; closed = false }
    else if (c === '\n') { row.push(cell); rows.push(row); row = []; cell = ''; closed = false }
    else if (closed) throw new Error('CSV 引号后存在无效字符，请在源码中修正')
    else cell += c
  }
  if (quoted) throw new Error('CSV 引号未闭合，请在源码中修正')
  if (cell || row.length || closed || !rows.length) { row.push(cell); rows.push(row) }
  return rows
}
export function serializeCSV(rows:string[][],delimiter:string,trailing:boolean):string { return rows.map(row => row.map(cell => /["\n\r]/.test(cell) || cell.includes(delimiter) ? `"${cell.replace(/"/g,'""')}"` : cell).join(delimiter)).join('\n')+(trailing ? '\n' : '') }
export function resolveRelative(base:string,target:string):string | null {
  if (!target || /^[a-z][a-z\d+.-]*:|^\/\//i.test(target)) return null
  let decoded:string; try { decoded = decodeURIComponent(target.split(/[?#]/)[0]!) } catch { return null }
  if (!decoded) return validPath(base,false) ? base : null
  const parts = decoded.startsWith('/') ? [] : parent(base).split('/').filter(Boolean)
  for (const part of decoded.split('/')) { if (!part || part === '.') continue; if (part === '..') { if (!parts.length) return null; parts.pop() } else parts.push(part) }
  const path = parts.join('/'); return validPath(path,false) ? path : null
}
