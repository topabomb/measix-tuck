import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { resolveRelative } from './files'

const localFragment=(value:string)=>/^#[A-Za-z_][\w:.-]*$/.test(value)
// CSS is serialized back into a raw-text <style> element: escape HTML syntax
// after filtering resources so a stylesheet cannot close that element.
function staticCSS(value:string):string {
  // Normalize escaped function names before resource filtering. CSP permits our
  // image data URLs, so source CSS must not smuggle its own unbounded data image.
  const clean=value.replace(/\/\*[\s\S]*?\*\//g,'').replace(/\\\r?\n/g,'').replace(/\\([\da-f]{1,6})(?:\r\n|[\t\n\f\r ])?|\\([^\r\n\f])/gi,(_match,hex,char)=>hex?String.fromCodePoint(Math.min(parseInt(hex,16)||0xfffd,0x10ffff)):char).replace(/@import[^;]*;?/gi,'')
  // Do not let serialization introduce another CSS escape decoding pass.
  if(clean.includes('\\'))return ''
  const functions=/(?:url|(?:-webkit-)?image-set|image|src)\(/gi
  let result='',offset=0,match:RegExpExecArray|null
  while((match=functions.exec(clean))){
    let end=functions.lastIndex,depth=1,quote=''
    for(;end<clean.length&&depth;end++){
      const char=clean[end]!
      if(quote){if(char===quote)quote=''}else if(char==='"'||char==="'")quote=char;else if(char==='(')depth++;else if(char===')')depth--
    }
    const raw=clean.slice(functions.lastIndex,end-1).trim(),target=raw.replace(/^(['"])([\s\S]*)\1$/,'$2')
    result+=clean.slice(offset,match.index)+(match[0].toLowerCase()==='url('&&depth===0&&localFragment(target)?`url(${target})`:'none')
    offset=end;functions.lastIndex=end
  }
  return (result+clean.slice(offset)).replace(/</g,'\\3c ')
}
const forbiddenTags=['script','iframe','object','embed','form','input','button','textarea','select','meta','base','foreignObject','animate','animateMotion','animateTransform','set','discard']
export interface PreviewLink {text:string;path:string;fragment?:string}
export interface PreviewImageResource {kind:'image'|'background';index:number;path:string|null}
export type PreviewResource=PreviewImageResource|{kind:'stylesheet';path:string|null}
export interface PreparedPreview {html:string;links:PreviewLink[];resources:PreviewResource[];warnings:string[]}
function localLink(path:string,href:string):Omit<PreviewLink,'text'>|null {
  const target=href.trim();if(!target||/^[a-z][a-z\d+.-]*:|^\/\//i.test(target))return null
  const hash=target.indexOf('#');let fragment:string|undefined
  try{if(hash>=0)fragment=decodeURIComponent(target.slice(hash+1))}catch{return null}
  if(fragment!==undefined&&(/[\x00-\x1f\x7f]/.test(fragment)||fragment.length>512))return null
  const destination=/^[#?]/.test(target)?path:resolveRelative(path,target)
  return destination?{path:destination,...(fragment!==undefined?{fragment}:{})}:null
}
function documentLinks(doc:Document,path:string,source:Document):PreviewLink[] {
  const links:PreviewLink[]=[],seen=new Set<string>()
  const add=(href:string,label:string)=>{const link=localLink(path,href);if(!link)return;const text=label.trim().slice(0,200)||href,key=link.path+'\0'+(link.fragment||'')+'\0'+text;if(!seen.has(key)){seen.add(key);links.push({text,...link})}}
  for(const node of doc.querySelectorAll('a[href]'))add(node.getAttribute('href')||'',node.textContent||'')
  // Automatic redirects are removed; only their explicit local destinations
  // remain available in the application-owned link menu.
  for(const node of source.querySelectorAll('meta[http-equiv]'))if(node.getAttribute('http-equiv')?.toLowerCase()==='refresh'){
    const match=/^\s*\d+(?:\.\d+)?\s*;\s*url\s*=\s*(?:"([^"]*)"|'([^']*)'|(.+?))\s*$/i.exec(node.getAttribute('content')||'')
    if(match)add(match[1]??match[2]??match[3]??'','打开跳转目标')
  }
  return links
}
function cleanAttributes(doc:Document) {
  for(const node of doc.querySelectorAll('*'))for(const attr of [...node.attributes]){
    const name=attr.name.toLowerCase()
    if(/^on|^data-workspace-/i.test(name)||(node.namespaceURI==='http://www.w3.org/2000/svg'&&attr.value.includes('\\')))node.removeAttribute(attr.name)
    else if(['href','xlink:href','src','poster','background'].includes(name)){
      if(!(node.namespaceURI==='http://www.w3.org/2000/svg'&&['href','xlink:href'].includes(name)&&localFragment(attr.value)))node.removeAttribute(attr.name)
    }else if(name==='style'||/url\(/i.test(attr.value))node.setAttribute(attr.name,staticCSS(attr.value))
  }
  for(const style of doc.querySelectorAll('style'))style.textContent=staticCSS(style.textContent||'')
}
export interface StaticPreviewOptions {
  stylesheet?:(path:string)=>Promise<string>
  signal?:AbortSignal
}
// The document stays inert. Only workspace-owned readers may load resources;
// source scripts, external URLs and source-owned bridge markers never survive.
export async function staticPreview(text:string,path:string,markdown:boolean,options:StaticPreviewOptions={}):Promise<PreparedPreview> {
  options.signal?.throwIfAborted()
  const input=markdown?await marked.parse(text):text
  options.signal?.throwIfAborted()
  const resources:PreviewResource[]=[],warnings:string[]=[]
  const clean=DOMPurify.sanitize(input,{WHOLE_DOCUMENT:true,ADD_TAGS:['use','link'],FORBID_TAGS:forbiddenTags,FORBID_ATTR:['srcset','ping','action','formaction','target']})
  const doc=new DOMParser().parseFromString(clean,'text/html')
  const links=documentLinks(doc,path,new DOMParser().parseFromString(input,'text/html'))
  const images:{node:Element;source:string;kind:PreviewImageResource['kind']}[]=[...doc.querySelectorAll('img')].map(node=>({node,source:node.getAttribute('src')||'',kind:'image'}))
  // Single inline background images use the same protected, demand-driven
  // reader as img elements. Other CSS resource functions remain stripped.
  for(const node of doc.querySelectorAll<HTMLElement>('[style]')){
    if(node.namespaceURI!=='http://www.w3.org/1999/xhtml'||['HTML','BODY','IMG'].includes(node.tagName))continue
    const match=/^url\((?:"([^"]*)"|'([^']*)'|([^()"']*))\)$/i.exec(node.style.backgroundImage)
    const source=match?.[1]??match?.[2]??match?.[3]
    if(source&&!source.trim().startsWith('#'))images.push({node,source,kind:'background'})
  }
  const anchors=[...doc.querySelectorAll('a[href]')].map(node=>({node,link:localLink(path,node.getAttribute('href')||'')}))
  const sheets=[...doc.querySelectorAll('link')].map(node=>({node,target:node.rel.toLowerCase().split(/\s+/).includes('stylesheet')?resolveRelative(path,(node.getAttribute('href')||'').trim()):null}))
  cleanAttributes(doc)
  for(const {node,link} of anchors)if(link){node.setAttribute('data-workspace-link',link.path);if(link.fragment!==undefined)node.setAttribute('data-workspace-fragment',link.fragment);node.setAttribute('href','#')}
  const cssCache=new Map<string,Promise<string>>()
  const sheetResults=await Promise.all(sheets.map(async({target})=>{
    if(!target||!options.stylesheet)return null
    if(!cssCache.has(target))cssCache.set(target,Promise.resolve().then(()=>options.stylesheet!(target)))
    try{return staticCSS(await cssCache.get(target)!)}catch{return null}
  }))
  for(const [index,{node,target}] of sheets.entries()){
    options.signal?.throwIfAborted()
    resources.push({kind:'stylesheet',path:target})
    if(target&&options.stylesheet){
      const css=sheetResults[index]
      if(css!==null&&css!==undefined){const style=doc.createElement('style');style.textContent=css;node.replaceWith(style)}else{node.remove();warnings.push(`样式不可用：${target}`)}
    }else{node.remove();if(target)warnings.push(`未加载样式：${target}`)}
  }
  const defaults=doc.createElement('style');defaults.textContent=markdown?':where(body){font:14px/1.65 system-ui,sans-serif;padding:20px;color:#243044;overflow-wrap:anywhere}:where(img,svg){max-width:100%}:where(pre){overflow:auto;background:#f4f5f7;padding:12px}:where(table){border-collapse:collapse}:where(td,th){border:1px solid #ccd2da;padding:6px}':':where(body){overflow-wrap:anywhere}:where(img,svg){max-width:100%}'
  const csp=doc.createElement('meta');csp.httpEquiv='Content-Security-Policy';csp.content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"
  doc.head.prepend(csp,defaults)
  let blocked=0
  for(let index=0;index<images.length;index++){
    const {node,source,kind}=images[index]!,target=source.trim().startsWith('#')?null:resolveRelative(path,source.trim())
    if(source.trim()&&!target)blocked++
    resources.push({kind,index,path:target})
    node.setAttribute('data-workspace-image',String(index));if(target)node.setAttribute('data-workspace-path',target)
  }
  if(blocked)warnings.push(`${blocked} 个外部或无效图片已阻止`)
  return {html:'<!doctype html>'+doc.documentElement.outerHTML,links,resources,warnings}
}
export function rasterMetadata(data:Uint8Array):{type:string;width:number;height:number}{
 const view=new DataView(data.buffer,data.byteOffset,data.byteLength)
 const ascii=(at:number,n:number)=>String.fromCharCode(...data.subarray(at,at+n))
 let type='',width=0,height=0
 if(data.length>=24&&ascii(1,3)==='PNG'&&ascii(12,4)==='IHDR'){
  type='image/png';width=view.getUint32(16);height=view.getUint32(20)
 }else if(data.length>=10&&/^GIF8[79]a$/.test(ascii(0,6))){
  type='image/gif';width=view.getUint16(6,true);height=view.getUint16(8,true)
 }else if(data.length>=26&&ascii(0,2)==='BM'){
  type='image/bmp';const header=view.getUint32(14,true)
  if(header===12){width=view.getUint16(18,true);height=view.getUint16(20,true)}
  else if(header>=40){width=view.getInt32(18,true);height=Math.abs(view.getInt32(22,true))}
 }else if(data.length>=30&&ascii(0,4)==='RIFF'&&ascii(8,4)==='WEBP'){
  type='image/webp';const chunk=ascii(12,4)
  if(chunk==='VP8X'){width=1+data[24]!+(data[25]!<<8)+(data[26]!<<16);height=1+data[27]!+(data[28]!<<8)+(data[29]!<<16)}
  else if(chunk==='VP8L'&&data[20]===47){const bits=view.getUint32(21,true);width=(bits&16383)+1;height=((bits>>>14)&16383)+1}
  else if(chunk==='VP8 '&&data[23]===157&&data[24]===1&&data[25]===42){width=view.getUint16(26,true)&16383;height=view.getUint16(28,true)&16383}
 }else if(data.length>=4&&data[0]===255&&data[1]===216){
  type='image/jpeg'
  for(let at=2;at+3<data.length;){
   if(data[at++]!==255)break
   while(data[at]===255)at++
   const marker=data[at++]!;if(marker===217||marker===218)break
   if(marker===1||(marker>=208&&marker<=215))continue
   if(at+2>data.length)break
   const length=view.getUint16(at);if(length<2||at+length>data.length)break
   if([192,193,194,195,197,198,199,201,202,203,205,206,207].includes(marker)&&length>=7){height=view.getUint16(at+3);width=view.getUint16(at+5);break}
   at+=length
  }
 }
 if(!type||width<1||height<1)throw new Error('图片格式或尺寸无效，请下载后打开。')
 if(width*height>16000000)throw new Error('图片超过 1600 万像素预览限制，请下载后打开。')
 return{type,width,height}
}

export async function imageURL(data:Uint8Array):Promise<{url:string;width:number;height:number}> {
 const {type,width,height}=rasterMetadata(data);return {url:URL.createObjectURL(new Blob([data as BlobPart],{type})),width,height}
}

// An opaque-origin sandbox cannot reliably dereference its parent's blob URL.
// Embed only validated raster bytes already read within the preview's budget;
// never pass through a document-owned data URL or grant same-origin permission.
export async function imageDataURL(data:Uint8Array,signal:AbortSignal):Promise<string> {
 const {type}=rasterMetadata(data)
 return encodedImage(data,type,signal)
}

// SVG image resources get the same inert policy as inline SVG. No source data
// URL is accepted, and nested image/style resources cannot request other files.
export async function previewImageDataURL(data:Uint8Array,signal:AbortSignal):Promise<string> {
 signal.throwIfAborted()
 if(!new TextDecoder().decode(data.subarray(0,512)).trimStart().startsWith('<'))return imageDataURL(data,signal)
 const text=new TextDecoder('utf-8',{fatal:true}).decode(data)
 const input=new DOMParser().parseFromString(text,'image/svg+xml')
 if(input.querySelector('parsererror')||input.documentElement.localName!=='svg'||input.documentElement.namespaceURI!=='http://www.w3.org/2000/svg')throw new Error('SVG 图片格式无效')
 const clean=DOMPurify.sanitize(text,{USE_PROFILES:{svg:true,svgFilters:true},ADD_TAGS:['use'],FORBID_TAGS:[...forbiddenTags,'link'],FORBID_ATTR:['srcset','ping','action','formaction','target']})
 const doc=new DOMParser().parseFromString(clean,'image/svg+xml')
 if(doc.querySelector('parsererror')||doc.documentElement.localName!=='svg')throw new Error('SVG 图片无法安全预览')
 cleanAttributes(doc)
 // Escaped CSS functions can disguise external URLs. SVG image resources use
 // a deliberately smaller CSS subset; ordinary SVG attributes still render.
 for(const style of doc.querySelectorAll('style'))if(/\\/.test(style.textContent||''))style.remove()
 for(const node of doc.querySelectorAll('*'))for(const attr of [...node.attributes])if(/\\/.test(attr.value))node.removeAttribute(attr.name)
 const svg=doc.documentElement,viewBox=(svg.getAttribute('viewBox')||'').trim().split(/[\s,]+/).map(Number)
 const dimension=(name:string,fallback:number)=>{const value=svg.getAttribute(name)||'';return /^\d+(?:\.\d+)?(?:px)?$/.test(value)?parseFloat(value):fallback}
 const width=dimension('width',viewBox.length===4?viewBox[2]!:300),height=dimension('height',viewBox.length===4?viewBox[3]!:150)
 if(!Number.isFinite(width)||!Number.isFinite(height)||width<=0||height<=0||width*height>16000000)throw new Error('SVG 图片超过预览尺寸限制')
 // Browser intrinsic units must not exceed the dimensions validated above.
 // Relative/physical units use the viewBox extent, or the SVG default size.
 svg.setAttribute('width',String(width));svg.setAttribute('height',String(height))
 const layout=(svg as unknown as SVGSVGElement).style
 for(const prefix of ['','min-','max-']){layout.setProperty(`${prefix}width`,`${width}px`,'important');layout.setProperty(`${prefix}height`,`${height}px`,'important')}
 return encodedImage(new TextEncoder().encode(new XMLSerializer().serializeToString(svg)),'image/svg+xml',signal)
}

// Embedded raster images use the displayed size, capped at 2048px per side.
// This keeps a long photo album useful on mobile without retaining full-size
// decodes for every nearby photo. The original remains available in its viewer.
export async function embeddedImageDataURL(data:Uint8Array,signal:AbortSignal,edge:number):Promise<string> {
 signal.throwIfAborted()
 if(new TextDecoder().decode(data.subarray(0,512)).trimStart().startsWith('<'))return previewImageDataURL(data,signal)
 const {type,width,height}=rasterMetadata(data),scale=Math.min(1,edge/Math.max(width,height))
 const bitmap=await createImageBitmap(new Blob([data as BlobPart],{type}),{resizeWidth:Math.max(1,Math.round(width*scale)),resizeHeight:Math.max(1,Math.round(height*scale))})
 try {
  signal.throwIfAborted()
  // Keep small images and animated GIFs in their original validated format.
  if(scale===1||type==='image/gif')return encodedImage(data,type,signal)
  const canvas=document.createElement('canvas');canvas.width=bitmap.width;canvas.height=bitmap.height
  canvas.getContext('2d')!.drawImage(bitmap,0,0)
  const blob=await new Promise<Blob|null>(resolve=>canvas.toBlob(resolve,'image/webp',.9))
  if(!blob)throw new Error('图片无法生成预览')
  return encodedImage(new Uint8Array(await blob.arrayBuffer()),blob.type,signal)
 } finally {bitmap.close()}
}

async function encodedImage(data:Uint8Array,type:string,signal:AbortSignal):Promise<string> {
 if(signal.aborted)throw new DOMException('预览已关闭','AbortError')
 return new Promise((resolve,reject)=>{
  const reader=new FileReader(),abort=()=>reader.abort(),release=()=>signal.removeEventListener('abort',abort)
  reader.onload=()=>{release();if(signal.aborted)reject(new DOMException('预览已关闭','AbortError'));else resolve(String(reader.result))}
  reader.onerror=()=>{release();reject(reader.error || new Error('图片编码失败'))}
  reader.onabort=()=>{release();reject(new DOMException('预览已关闭','AbortError'))}
  signal.addEventListener('abort',abort,{once:true})
  reader.readAsDataURL(new Blob([data as BlobPart],{type}))
 })
}
