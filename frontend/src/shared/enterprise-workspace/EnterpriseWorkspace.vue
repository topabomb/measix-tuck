<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { router } from '../../app/router'
import { copyText } from '../copy-text'
import { workspaceKey } from './state'
import { workspacePrompt, workspaceConfirm, workspaceConflict, workspaceDialogState } from './dialog'
import { basename, bytes, copyPath, join, kind, parent, validPath, type Entry } from './files'
import { rasterMetadata } from './preview'
import WorkspaceDocument from './WorkspaceDocument.vue'
const emit = defineEmits<{close:[];width:[value:number]}>()
const props=withDefaults(defineProps<{displayWidth?:number;maximumWidth?:number;standalone?:boolean}>(),{displayWidth:420,maximumWidth:900,standalone:false})
const route = useRoute(), owner = inject(workspaceKey)!, state = owner.state
const host = ref<HTMLElement>(), uploadInput = ref<HTMLInputElement>(), folderInput = ref<HTMLInputElement>(), selected = ref<string[]>([]), pathInput = ref(''), editingPath = ref(false), menu = ref(false), properties = ref(false), dragOver = ref(false), treeExpanded = ref(['']), history = ref<string[]>([]), historyIndex = ref(-1)
const transferList=ref<HTMLElement>();let choosingFiles=false,dragDepth=0,lastFocusRefresh=0
watch(()=>state.transfers.length,()=>{void nextTick(()=>{if(transferList.value)transferList.value.scrollTop=transferList.value.scrollHeight})})
const propertyDialog = ref<HTMLDialogElement>(), operationButton = ref<HTMLButtonElement>()
const pathEditor=ref<HTMLInputElement>(),pathToggle=ref<HTMLButtonElement>()
async function togglePath(){if(editingPath.value){cancelPath();return}pathInput.value=directoryPath.value||'/';editingPath.value=true;await nextTick();pathEditor.value?.focus();pathEditor.value?.select()}
function cancelPath(){editingPath.value=false;void nextTick(()=>pathToggle.value?.focus())}
const tabList = ref<HTMLElement>(),tabMenu=ref<HTMLDetailsElement>()
watch(()=>state.active,async()=>{await nextTick();tabList.value?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.scrollIntoView({block:'nearest',inline:'nearest'})})
const wide = computed(()=>props.standalone||route.query.wide==='1'), filter = computed(()=>String(route.query.filter || '')), hidden = computed(()=>route.query.hidden==='1'), sort = computed(()=>route.query.sort==='size'||route.query.sort==='date'?route.query.sort:'name'), grid = computed(()=>route.query.view==='grid')
const order=computed(()=>route.query.order==='asc'||route.query.order==='desc'?route.query.order:sort.value==='name'?'asc':'desc')
const active = computed(()=>state.tabs.find(d=>d.path===state.active)), selectedEntries = computed(()=>state.entries.filter(e=>selected.value.includes(e.path)))
// Keep just the active and previous viewer. Other tabs retain their lightweight
// reading state; closing/evicting a tab also releases its mounted resources.
const recentViewers=ref<number[]>([])
watch(()=>[state.active,state.tabs.map(doc=>doc.id)] as const,()=>{
  const ids=recentViewers.value.filter(id=>state.tabs.some(doc=>doc.id===id))
  recentViewers.value=(active.value?[active.value.id,...ids.filter(id=>id!==active.value!.id)]:ids).slice(0,2)
},{immediate:true})
const viewers=computed(()=>state.tabs.filter(doc=>recentViewers.value.includes(doc.id)))
const focused=computed(()=>!!active.value&&route.query.focus==='1'),treeVisible=computed(()=>wide.value&&!active.value&&route.query.tree!=='0')
const treeLoading=ref<string|null>(null),treeErrors=ref<Record<string,string>>({}),displayMenu=ref<HTMLDetailsElement>(),createMenu=ref<HTMLDetailsElement>()
let treeController:AbortController|undefined,lastTreeDir:string|undefined
const treeKnown=(path:string)=>Object.hasOwn(state.tree,path)
const hasSubdirectories=(path:string)=>state.tree[path]?.some(entry=>entry.kind==='DIRECTORY')===true
const entries = computed(()=>state.entries.filter(e=>(hidden.value || !basename(e.path).startsWith('.')) && basename(e.path).toLocaleLowerCase().includes(filter.value.toLocaleLowerCase())).sort((a,b)=>{
  if(a.kind!==b.kind)return a.kind==='DIRECTORY'?-1:1
  const name=basename(a.path).localeCompare(basename(b.path),undefined,{numeric:true})
  const value=sort.value==='size'?(a.size||0)-(b.size||0):sort.value==='date'?(a.modifiedAt||'').localeCompare(b.modifiedAt||''):name
  return (value||name)*(order.value==='asc'?1:-1)
}))
const directoryPath = computed(()=>state.directoryLoaded?state.dir:typeof route.query.dir==='string'?route.query.dir:'')
const crumbs = computed(()=>directoryPath.value.split('/').filter(Boolean).map((name,i,parts)=>({name,path:parts.slice(0,i+1).join('/')})))
const tree = computed(()=>{const result:{path:string;level:number}[]=[];const visit=(path:string,level:number)=>{result.push({path,level});if(treeExpanded.value.includes(path))for(const child of state.tree[path]||[])if(child.kind==='DIRECTORY')visit(child.path,level+1)};visit('',0);return result})
const gridPage=ref(1), thumbnails=ref<Record<string,string>>({})
const visibleEntries=computed(()=>grid.value?entries.value.slice((gridPage.value-1)*48,gridPage.value*48):entries.value)
let thumbnailController=new AbortController(),thumbnailRun=0;const thumbnailVersions=new Map<string,string>()
function clearThumbnails(){thumbnailController.abort();thumbnailController=new AbortController();for(const url of Object.values(thumbnails.value))URL.revokeObjectURL(url);thumbnails.value={};thumbnailVersions.clear()}
watch([visibleEntries,grid,()=>state.active,()=>state.projection?.filesAvailable],async()=>{
  thumbnailController.abort();thumbnailController=new AbortController()
  if(!grid.value || state.active || !state.projection?.filesAvailable){clearThumbnails();return}
  const signal=thumbnailController.signal,generation=owner.generation(),run=++thumbnailRun
  const images=visibleEntries.value.filter(entry=>entry.kind==='FILE'&&kind(entry.path)==='image'&&(entry.size===undefined||entry.size<=24*1024*1024))
  const versions=new Map(images.map(entry=>[entry.path,`${generation}:${entry.etag||(entry.modifiedAt?`${entry.size??''}:${entry.modifiedAt}`:`unversioned:${run}`)}`]))
  for(const [path,url] of Object.entries(thumbnails.value))if(!versions.has(path)||thumbnailVersions.get(path)!==versions.get(path)){URL.revokeObjectURL(url);delete thumbnails.value[path];thumbnailVersions.delete(path)}
  for(const entry of images){if(signal.aborted)return;if(thumbnails.value[entry.path])continue
    try{
      const {data}=await owner.bounded(entry.path,24*1024*1024,entry.etag?{'If-Match':entry.etag}:{},signal,true)
      if(signal.aborted)return
      const metadata=rasterMetadata(data),scale=Math.min(1,128/metadata.width,128/metadata.height)
      const bitmap=await createImageBitmap(new Blob([data as BlobPart],{type:metadata.type}),{resizeWidth:Math.max(1,Math.floor(metadata.width*scale)),resizeHeight:Math.max(1,Math.floor(metadata.height*scale))})
      const canvas=document.createElement('canvas');canvas.width=bitmap.width;canvas.height=bitmap.height
      try{canvas.getContext('2d')!.drawImage(bitmap,0,0)}finally{bitmap.close()}
      const blob=await new Promise<Blob|null>(resolve=>canvas.toBlob(resolve))
      if(blob&&!signal.aborted){thumbnails.value[entry.path]=URL.createObjectURL(blob);thumbnailVersions.set(entry.path,versions.get(entry.path)!)}
    }catch{/* The file icon remains the explicit fallback. */}
  }
})
watch([filter,()=>state.dir],()=>{gridPage.value=1})
watch(()=>entries.value.length,()=>{gridPage.value=Math.min(gridPage.value,Math.max(1,Math.ceil(entries.value.length/48)))})
watch(()=>state.entries,()=>{const paths=new Set(state.entries.map(entry=>entry.path));selected.value=selected.value.filter(path=>paths.has(path))})
let disposed = false, sync = 0, navigation=0
watch(properties,async open=>{await nextTick();if(disposed||properties.value!==open)return;if(open){if(!propertyDialog.value?.open)propertyDialog.value?.showModal()}else{propertyDialog.value?.close();operationButton.value?.focus()}})
async function query(value:Record<string,string|undefined>) {await router.replace({path:route.path,query:{...route.query,...value}})}
async function act(action:()=>Promise<unknown>,clearFeedback=true) {if(state.busy)return;owner.cancelRefresh();state.busy=true;if(clearFeedback){state.error='';state.notice=''}try{await action()}catch(error){state.error=(error as Error).message}finally{state.busy=false}}
async function navigate(path:string,record=true) {
  const current=++navigation
  if(path==='/')path=''
  if(!validPath(path))throw new Error('请输入工作区内的相对路径')
  await owner.list(path);delete treeErrors.value[path];if(current!==navigation||disposed)return;selected.value=[];state.active='';editingPath.value=false
  if(record){history.value=history.value.slice(0,historyIndex.value+1);if(history.value.at(-1)!==path)history.value.push(path);historyIndex.value=history.value.length-1}
  await query({ew:'1',space:state.projection?.agentSpaceId,dir:path||undefined,file:undefined,anchor:undefined})
  await nextTick();(host.value?.querySelector<HTMLElement>('.ew-files') || host.value)?.focus()
}
async function stepHistory(delta:number) {
  const index=historyIndex.value+delta
  if(state.busy || index<0 || index>=history.value.length)return
  await act(async()=>{await navigate(history.value[index]!,false);historyIndex.value=index})
}
function goUp() {if(directoryPath.value && !state.busy)void act(()=>navigate(parent(directoryPath.value)))}
async function reloadDirectory(){await owner.refresh(typeof route.query.space==='string'?route.query.space:undefined);await owner.list(directoryPath.value)}
async function open(path:string,fragment?:string) {owner.cancelRefresh();const current=++navigation;await owner.open(path);if(current===navigation&&!disposed&&state.active===path)await query({file:path,anchor:fragment||undefined,dir:state.dir||undefined,space:state.projection?.agentSpaceId})}
async function openLink(path:string,fragment?:string){const siblings=await owner.list(parent(path),true);const entry=siblings.find(e=>e.path===path);if(entry?.kind==='DIRECTORY')await navigate(path);else await open(path,fragment)}
async function restore() {
  const current=++sync
  try{state.error='';await owner.refresh(typeof route.query.space==='string'?route.query.space:undefined);if(disposed||current!==sync)return;const dir=typeof route.query.dir==='string'?route.query.dir:'';await owner.list(dir);delete treeErrors.value[dir];if(current!==sync||disposed)return;const file=typeof route.query.file==='string'?route.query.file:'';if(file)await owner.open(file);else state.active='';if(!history.value.length){history.value=[dir];historyIndex.value=0}if(!route.query.space)await query({space:state.projection?.agentSpaceId})}catch(error){
    if(disposed||current!==sync||(error as Error).name==='AbortError')return
    if((error as Error & {code?:string}).code==='workspace_link_changed'){
      history.value=[];historyIndex.value=-1
      state.notice='工作区链接已更新，已返回当前工作区根目录'
      await query({space:state.projection?.agentSpaceId,dir:undefined,file:undefined,anchor:undefined,filter:undefined})
    }else state.error=(error as Error).message
  }
}
watch(()=>[route.query.dir,route.query.file,route.query.space],()=>{if(!state.projection?.filesAvailable || String(route.query.dir||'')!==state.dir || String(route.query.file||'')!==state.active || route.query.space!==state.projection?.agentSpaceId)void restore()})
function choose(event:MouseEvent,entry:Entry) {
  (event.currentTarget as HTMLElement).focus()
  menu.value=false
  if(event.shiftKey && selected.value.length){const start=entries.value.findIndex(e=>e.path===selected.value[0]),end=entries.value.findIndex(e=>e.path===entry.path);selected.value=entries.value.slice(Math.min(start,end),Math.max(start,end)+1).map(e=>e.path)}
  else if(event.ctrlKey||event.metaKey)selected.value=selected.value.includes(entry.path)?selected.value.filter(p=>p!==entry.path):[...selected.value,entry.path]
  else selected.value=[entry.path]
}
function enter(entry:Entry) {void act(()=>entry.kind==='DIRECTORY'?navigate(entry.path):open(entry.path))}
async function closeTabs(paths:string[]) {
  if(state.busy)return
  const docs=state.tabs.filter(doc=>paths.includes(doc.path));if(!docs.length)return
  owner.cancelRefresh()
  const generation=owner.generation(),drafts=docs.filter(doc=>doc.text!==doc.original)
  if(drafts.length&&!await workspaceConfirm(docs.length===1?`放弃 ${basename(docs[0]!.path)} 的未保存修改？`:`关闭 ${docs.length} 个标签并放弃其中 ${drafts.length} 个文件的未保存修改？\n${drafts.map(doc=>doc.path).join('\n')}`))return
  if(generation!==owner.generation())return
  const ids=new Set(docs.map(doc=>doc.id)),index=state.tabs.findIndex(doc=>doc.path===state.active)
  const next=state.tabs.slice(0,index).reverse().find(doc=>!ids.has(doc.id))||state.tabs.slice(index+1).find(doc=>!ids.has(doc.id))
  const closingActive=docs.some(doc=>doc.path===state.active)
  state.tabs=state.tabs.filter(doc=>!ids.has(doc.id))
  if(closingActive){state.active=next?.path||'';await query({file:state.active||undefined,anchor:undefined})}
  await nextTick();(tabList.value?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')||host.value?.querySelector<HTMLElement>('.ew-files')||host.value)?.focus()
}
function closeTabGroup(others=false){dismissDetails(tabMenu.value);void closeTabs(state.tabs.filter(doc=>!others||doc.path!==state.active).map(doc=>doc.path))}
function create(directory:boolean) {void act(async()=>{const generation=owner.generation(),base=state.dir;const name=await workspacePrompt(directory?'新建文件夹名称':'新建文件名称',directory?'新建文件夹':'untitled.txt');if(name===null)return;owner.assertGeneration(generation);if(!validPath(name,false)||name.includes('/'))throw new Error('名称不能包含路径分隔符');const path=join(base,name);if(directory)await owner.mutate('MKCOL',path);else await owner.put(path,'');await owner.list();if(!directory)await open(path)})}
function rename() {const entry=selectedEntries.value[0];if(!entry)return;void act(async()=>{const generation=owner.generation();const name=await workspacePrompt('重命名',basename(entry.path));if(name===null||name===basename(entry.path))return;owner.assertGeneration(generation);if(!validPath(name,false)||name.includes('/'))throw new Error('文件名称无效');await owner.mutate('MOVE',entry.path,{destination:join(parent(entry.path),name),sourceEtag:entry.etag,overwrite:false});state.notice='已重命名；已打开的旧路径标签可另存草稿';await owner.list();selected.value=[];menu.value=false})}
async function batch(action:(entry:Entry,generation:number)=>Promise<void>,items=selectedEntries.value) {const generation=owner.generation(),results:string[]=[];for(const entry of [...items]){owner.assertGeneration(generation);try{await action(entry,generation);results.push(`${basename(entry.path)}：完成`)}catch(error){owner.assertGeneration(generation);results.push(`${basename(entry.path)}：${(error as Error).message}`)}}owner.assertGeneration(generation);state.notice=results.join('\n');await owner.list();selected.value=[]}
function remove() {if(!selected.value.length)return;void act(async()=>{const generation=owner.generation(),items=[...selectedEntries.value];if(!await workspaceConfirm(`永久删除 ${items.length} 个项目？${items.some(e=>e.kind==='DIRECTORY')?'文件夹内的全部内容也会删除。':''}此操作不可撤销。`))return;owner.assertGeneration(generation);await batch(entry=>owner.mutate('DELETE',entry.path,{sourceEtag:entry.etag,...(entry.kind==='DIRECTORY'?{recursiveConfirmed:true}:{})}),items)})}
function clipboard(cut:boolean) {state.clipboard={cut,entries:selectedEntries.value.map(e=>({...e}))};menu.value=false;state.notice=`已${cut?'剪切':'复制'} ${selected.value.length} 个项目`}
function paste(destination=state.dir) {
  const clip=state.clipboard;if(!clip)return
  void act(async()=>{
    if(!validPath(destination))throw new Error('目标目录路径无效')
    let policy=''
    await batch(async (entry,generation)=>{
      let target=join(destination,basename(entry.path));let overwrite=false,targetEtag:string|undefined
      if(destination===entry.path||destination.startsWith(entry.path+'/'))throw new Error('目标位于源目录内')
      const siblings=await owner.list(destination,true),existing=siblings.find(e=>e.path===target)
      if(existing){
        policy ||= await workspaceConflict('存在同名目标，请选择本次操作的处理方式') || '跳过'
        if(policy==='跳过')throw new Error('已跳过')
        if(policy==='替换'){if(entry.path===target||entry.kind!=='FILE'||existing.kind!=='FILE'||!existing.etag)throw new Error('不能替换源文件、目录或无版本文件');overwrite=true;targetEtag=existing.etag}
        else target=copyPath(target,candidate=>siblings.some(e=>e.path===candidate),entry.kind==='DIRECTORY')
      }
      owner.assertGeneration(generation)
      await owner.mutate(clip.cut?'MOVE':'COPY',entry.path,{destination:target,sourceEtag:entry.etag,overwrite,targetEtag})
    },clip.entries)
    if(clip.cut)state.clipboard=null
  })
}
async function moveTo() {const generation=owner.generation();const destination=await workspacePrompt('移动到目录（相对路径，留空为根目录）',state.dir);if(destination===null||generation!==owner.generation())return;clipboard(true);paste(destination)}
function copyLink() {void act(async()=>{const location=new URL(window.location.href);location.hash=router.resolve({path:route.path,query:{ew:'1',space:state.projection?.agentSpaceId,dir:selectedEntries.value[0]?.kind==='DIRECTORY'?selected.value[0]:state.dir,file:selectedEntries.value[0]?.kind==='FILE'?selected.value[0]:undefined}}).href.replace(/^#/, '');await copyText(location.href);state.notice='定位链接已复制'})}
function download() {if(selected.value.length) {const a=document.createElement('a');a.href=owner.download(selected.value);a.download='';a.click()}}
async function readTree(path:string) {
  const controller=new AbortController(),generation=owner.generation();treeController=controller;treeLoading.value=path;delete treeErrors.value[path]
  try{await owner.list(path,true,{signal:controller.signal});owner.assertGeneration(generation);return !disposed&&!controller.signal.aborted}
  catch(error){if(!disposed&&!controller.signal.aborted&&generation===owner.generation())treeErrors.value[path]=(error as Error).message;return false}
  finally{if(treeController===controller){treeController=undefined;treeLoading.value=null}}
}
async function expandTree(path:string) {
  if(treeExpanded.value.includes(path)&&!treeErrors.value[path])treeExpanded.value=treeExpanded.value.filter(p=>p!==path)
  else if((treeKnown(path)&&!treeErrors.value[path])||await readTree(path)){if(!treeExpanded.value.includes(path))treeExpanded.value.push(path)}
}
async function revealTree() {
  const path=state.dir,generation=owner.generation(),ancestors=['',...path.split('/').filter(Boolean).map((_,i,parts)=>parts.slice(0,i+1).join('/'))]
  const reveal=lastTreeDir!==path
  const update=async()=>{
    for(const ancestor of ancestors){if(disposed||!treeVisible.value||state.dir!==path||generation!==owner.generation())return;if(!treeKnown(ancestor)&&!treeErrors.value[ancestor]&&!await readTree(ancestor))return}
    if(disposed||!treeVisible.value||state.dir!==path||generation!==owner.generation())return
    if(reveal){treeExpanded.value=[...new Set([...treeExpanded.value,...ancestors])];lastTreeDir=path;await nextTick();host.value?.querySelector('.ew-tree [aria-current="location"]')?.scrollIntoView({block:'nearest'})}
  }
  if(ancestors.some(ancestor=>!treeKnown(ancestor)&&!treeErrors.value[ancestor]))await act(update,false);else await update()
}
watch([()=>state.dir,()=>state.tree,()=>state.busy,()=>state.loading,treeVisible,()=>state.projection?.filesAvailable],()=>{
  if(!treeVisible.value){treeController?.abort();return}
  if(state.projection?.filesAvailable&&!state.busy&&!state.loading)void revealTree()
},{immediate:true})
watch(()=>state.tree,()=>{treeErrors.value={};if(!state.projection)lastTreeDir=undefined})
watch(()=>[state.projection?.subject,state.projection?.agentSpaceId],()=>{treeController?.abort();lastTreeDir=undefined;treeExpanded.value=[''];treeErrors.value={}})
function dismissDetails(element?:HTMLDetailsElement){if(element){element.open=false;element.querySelector('summary')?.focus()}}
function dismissDisplay(){dismissDetails(displayMenu.value)}
function createMenuAction(directory:boolean){dismissDetails(createMenu.value);if(directory)create(true);else chooseUpload(true)}
function toggleOperations(){if(createMenu.value)createMenu.value.open=false;if(displayMenu.value)displayMenu.value.open=false;menu.value=!menu.value}
function closeMenus(event:PointerEvent){const target=event.target as Node;for(const details of [createMenu.value,displayMenu.value,tabMenu.value])if(details?.open&&!details.contains(target))details.open=false;if(menu.value&&!host.value?.querySelector('.ew-menu')?.contains(target)&&!operationButton.value?.contains(target))menu.value=false}
async function stepSelection(delta:number) {
  const index=entries.value.findIndex(entry=>entry.path===selected.value.at(-1))
  const nextIndex=Math.max(0,Math.min(entries.value.length-1,index+delta)),next=entries.value[nextIndex]
  if(!next)return
  selected.value=[next.path]
  if(grid.value)gridPage.value=Math.floor(nextIndex/48)+1
  await nextTick()
  const visibleIndex=visibleEntries.value.findIndex(entry=>entry.path===next.path)
  const option=host.value?.querySelectorAll<HTMLElement>('.ew-files [role="option"]')[visibleIndex]
  option?.focus({preventScroll:true});option?.scrollIntoView({block:'nearest',inline:'nearest'})
}
function keyboard(event:KeyboardEvent) {
  const target=event.target as HTMLElement
  if(target.closest('input,textarea,.cm-editor,select,[contenteditable=true],dialog'))return
  if(event.key==='Escape'&&menu.value){event.preventDefault();menu.value=false;operationButton.value?.focus();return}
  if(event.key==='Escape'&&focused.value){event.preventDefault();void query({focus:undefined});return}
  if(event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey && ['ArrowLeft','ArrowRight','ArrowUp'].includes(event.key)) {
    event.preventDefault()
    if(event.key==='ArrowUp')goUp()
    else void stepHistory(event.key==='ArrowLeft'?-1:1)
    return
  }
  if(event.key!=='Escape' && target.closest('button,a,summary,[role=separator],[role=tab]'))return
  const mod=event.ctrlKey||event.metaKey
  if(mod&&event.key==='a'){event.preventDefault();selected.value=entries.value.map(e=>e.path)}
  else if(mod&&event.key==='c'&&selected.value.length){event.preventDefault();clipboard(false)}
  else if(mod&&event.key==='x'&&selected.value.length){event.preventDefault();clipboard(true)}
  else if(mod&&event.key==='v'&&state.clipboard){event.preventDefault();paste()}
  else if(event.key==='Delete')remove()
  else if(event.key==='F2'){event.preventDefault();rename()}
  else if(event.key==='Enter'&&selectedEntries.value.length===1)enter(selectedEntries.value[0]!)
  else if(event.key==='Escape'){menu.value=false;if(!props.standalone){if(wide.value)void query({wide:undefined});else emit('close')}}
  else if(['ArrowDown','ArrowUp'].includes(event.key)){event.preventDefault();void stepSelection(event.key==='ArrowDown'?1:-1)}
}
function chooseUpload(directory=false){if(state.busy)return;owner.cancelRefresh();choosingFiles=true;const input=directory?folderInput.value:uploadInput.value;try{input?.click()}catch(error){choosingFiles=false;state.error=(error as Error).message}}
async function uploadFiles(files:File[]) {choosingFiles=false;if(!files.length)return;if(state.busy){state.notice='正在处理其他文件操作，请稍后重新选择上传文件';return}await act(()=>owner.upload(files))}
function fileDrag(event:DragEvent){return event.dataTransfer?.types.includes('Files')===true}
function clearDrag(){dragDepth=0;dragOver.value=false}
function dragEnter(event:DragEvent){if(fileDrag(event)){event.preventDefault();dragDepth++;dragOver.value=true}}
function dragMove(event:DragEvent){if(fileDrag(event)){event.preventDefault();dragOver.value=true;if(event.dataTransfer)event.dataTransfer.dropEffect=state.busy||!state.projection?.filesAvailable?'none':'copy'}}
function dragLeave(){dragDepth=Math.max(0,dragDepth-1);if(!dragDepth)dragOver.value=false}
async function drop(event:DragEvent) {
  if(!fileDrag(event))return
  event.preventDefault();event.stopPropagation();clearDrag()
  // Capture the protected drag data store synchronously before any asynchronous work.
  const roots=[...(event.dataTransfer?.items||[])].filter(item=>item.kind==='file').map(item=>item.webkitGetAsEntry()).filter(Boolean)
  const fallback=[...(event.dataTransfer?.files||[])],generation=owner.generation(),destination=state.dir
  if(!roots.length&&!fallback.length)return
  owner.cancelRefresh()
  if(disposed)return
  if(state.busy){state.notice='正在处理其他文件操作，请稍后重新拖入文件';return}
  const files:File[]=[],directories:string[]=[]
  interface DroppedEntry {isFile:boolean;isDirectory:boolean;name:string;file:(done:(file:File)=>void,fail:(error:Error)=>void)=>void;createReader:()=>{readEntries:(done:(items:DroppedEntry[])=>void,fail:(error:Error)=>void)=>void}}
  const assertTarget=()=>{owner.assertGeneration(generation);if(disposed)throw new Error('工作区面板已关闭，未开始上传');if(state.dir!==destination)throw new Error('当前目录已变化，请重新拖入文件')}
  const walk=async(entry:DroppedEntry,prefix:string)=>{
    assertTarget();if(files.length+directories.length>=10000)throw new Error('一次最多上传 10000 个文件')
    if(entry.isFile){const file=await new Promise<File>((resolve,reject)=>entry.file(resolve,reject));assertTarget();Object.defineProperty(file,'webkitRelativePath',{value:join(prefix,file.name)});files.push(file)}
    else if(entry.isDirectory){directories.push(join(prefix,entry.name));const reader=entry.createReader();while(true){const children=await new Promise<DroppedEntry[]>((resolve,reject)=>reader.readEntries(resolve,reject));assertTarget();if(!children.length)break;for(const child of children)await walk(child,join(prefix,entry.name))}}
  }
  await act(async()=>{assertTarget();if(!state.projection?.filesAvailable)throw new Error('企业工作区当前不可用，请刷新状态');if(roots.length)for(const root of roots)await walk(root as unknown as DroppedEntry,'');else files.push(...fallback);assertTarget();await owner.upload(files,directories)})
}
function resize(event:PointerEvent) {if(event.button!==0)return;event.preventDefault();const target=event.currentTarget as HTMLElement;target.setPointerCapture(event.pointerId);const start=event.clientX,initial=host.value?.getBoundingClientRect().width||420;const move=(e:PointerEvent)=>emit('width',Math.min(900,Math.max(340,initial+start-e.clientX)));const end=()=>{target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',end);target.removeEventListener('pointercancel',end)};target.addEventListener('pointermove',move);target.addEventListener('pointerup',end);target.addEventListener('pointercancel',end)}
function focusRefresh() {
  clearDrag()
  if(choosingFiles||state.busy||state.loading||state.refreshing||workspaceDialogState.current||document.querySelector('dialog[open]')||document.hidden||disposed||Date.now()-lastFocusRefresh<10000)return
  lastFocusRefresh=Date.now()
  void owner.refreshDirectory(typeof route.query.space==='string'?route.query.space:undefined)
}
onMounted(()=>{void restore();window.addEventListener('focus',focusRefresh);window.addEventListener('pointerdown',closeMenus);void nextTick(()=>host.value?.focus())})
onBeforeUnmount(()=>{disposed=true;sync++;treeController?.abort();owner.cancelRefresh();propertyDialog.value?.close();clearThumbnails();window.removeEventListener('focus',focusRefresh);window.removeEventListener('pointerdown',closeMenus)})
</script>
<template><component :is="props.standalone?'section':'aside'" id="enterprise-workspace-panel" ref="host" class="enterprise-workspace" :class="{'is-wide':wide,'ew-standalone':props.standalone,'is-focused':focused,'drag-over':dragOver}" :aria-label="props.standalone?'企业文件管理器':'企业工作区'" tabindex="-1" @keydown="keyboard" @dragenter="dragEnter" @dragover="dragMove" @dragleave="dragLeave" @drop.capture="drop">
  <div v-if="!props.standalone && !wide" class="ew-resize" role="separator" aria-label="调整工作区宽度" aria-orientation="vertical" :aria-valuenow="Math.round(props.displayWidth)" aria-valuemin="340" :aria-valuemax="Math.round(props.maximumWidth)" tabindex="0" title="拖动调整工作区宽度，双击恢复默认" @pointerdown="resize" @dblclick="emit('width',420)" @keydown.left.prevent="emit('width',(host?.getBoundingClientRect().width||420)+20)" @keydown.right.prevent="emit('width',(host?.getBoundingClientRect().width||420)-20)" />
  <header class="ew-heading"><strong>企业工作区</strong><span :title="focused?active?.path:undefined">{{focused&&active?basename(active.path)+(active.text!==active.original?' •':''):state.projection?.employeeName}}</span><button v-if="wide&&!active" class="ew-tree-toggle ew-icon-button" :title="treeVisible?'隐藏目录树':'显示目录树'" :aria-expanded="treeVisible" aria-controls="ew-directory-tree" :aria-label="treeVisible?'隐藏目录树':'显示目录树'" @click="query({tree:treeVisible?'0':undefined})"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M3 4h18v16H3zM9 4v16M5 8h2m-2 4h2m-2 4h2" /></svg></button><button v-if="active" class="ew-label-button" :aria-pressed="focused" :aria-label="focused?'退出专注内容':'专注内容'" :title="focused?'恢复标签和路径栏':'收起导航，扩大文件内容区域'" @click="query({focus:focused?undefined:'1'})"><svg aria-hidden="true" viewBox="0 0 24 24"><path :d="focused?'M8 3v5H3m13-5v5h5M3 16h5v5m13-5h-5v5':'M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5'" /></svg><span class="ew-focus-label">{{focused?'退出专注':'专注'}}</span></button><button v-if="!props.standalone" class="ew-icon-button" :aria-label="wide?'还原工作区':'展开工作区'" :title="wide?'还原工作区':'展开工作区'" @click="query({wide:wide?undefined:'1'})"><svg aria-hidden="true" viewBox="0 0 24 24"><path :d="wide?'M10 3v7H3m11 11v-7h7M10 10 3 3m11 11 7 7':'M3 10V3h7m11 11v7h-7M3 3l7 7m4 4 7 7'" /></svg></button><button v-if="!props.standalone" class="ew-icon-button" aria-label="收起企业工作区" title="收起企业工作区" @click="emit('close')"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m6 6 12 12M6 18 18 6" /></svg></button></header>
  <div class="ew-tools ew-location"><button class="ew-icon-button ew-history-button" :disabled="historyIndex<=0 || state.busy" aria-label="后退" aria-keyshortcuts="Alt+ArrowLeft" title="后退（Alt + ←）" @click="stepHistory(-1)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m14 5-7 7 7 7" /></svg></button><button class="ew-icon-button ew-history-button" :disabled="historyIndex>=history.length-1 || state.busy" aria-label="前进" aria-keyshortcuts="Alt+ArrowRight" title="前进（Alt + →）" @click="stepHistory(1)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m10 5 7 7-7 7" /></svg></button><button class="ew-icon-button" :disabled="!directoryPath || state.busy" aria-label="上一级" aria-keyshortcuts="Alt+ArrowUp" title="上一级（Alt + ↑）" @click="goUp"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m12 9-5-5-5 5M7 4v11a5 5 0 0 0 5 5h9" /></svg></button><form v-if="editingPath" @submit.prevent="act(()=>navigate(pathInput))" @keydown.esc.stop.prevent="cancelPath"><input ref="pathEditor" v-model="pathInput" aria-label="工作区路径" placeholder="根目录" title="输入相对目录路径，Enter 打开，Esc 取消" /><button type="submit" class="ew-icon-button" aria-label="打开路径" title="打开路径（Enter）" :disabled="state.busy"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m5 12 4 4L19 6" /></svg></button><button type="button" class="ew-icon-button" aria-label="取消路径编辑" title="取消路径编辑（Esc）" @click="cancelPath"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m6 6 12 12M6 18 18 6" /></svg></button></form><nav v-else aria-label="目录路径"><button class="ew-root" title="返回根目录" :disabled="state.busy" :aria-current="!directoryPath?'location':undefined" @click="act(()=>navigate(''))">根目录</button><template v-for="crumb in crumbs" :key="crumb.path"><span>/</span><button :disabled="state.busy" :aria-current="directoryPath===crumb.path?'location':undefined" @click="act(()=>navigate(crumb.path))">{{crumb.name}}</button></template></nav><button ref="pathToggle" class="ew-icon-button" aria-label="输入路径" :aria-pressed="editingPath" :title="editingPath?'取消路径编辑':'编辑目录路径'" @click="togglePath"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m14 5 5 5M4 20l5-1L21 7l-5-5L4 14z" /></svg></button><button v-if="active" :disabled="state.busy" class="ew-icon-button" aria-label="刷新" title="刷新工作区" @click="act(reloadDirectory)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M20 7v5h-5M4 17v-5h5M6 6a8 8 0 0 1 14 6M4 12a8 8 0 0 0 14 6" /></svg></button></div>
  <input ref="uploadInput" hidden type="file" multiple @cancel="choosingFiles=false" @change="uploadFiles([...(($event.target as HTMLInputElement).files||[])]);($event.target as HTMLInputElement).value=''" /><input ref="folderInput" hidden type="file" multiple webkitdirectory @cancel="choosingFiles=false" @change="uploadFiles([...(($event.target as HTMLInputElement).files||[])]);($event.target as HTMLInputElement).value=''" />
  <div v-if="state.error" class="ew-error" role="alert">{{state.error}}<button v-if="state.error.includes('其他企业工作区')" @click="query({space:undefined,dir:undefined,file:undefined})">打开当前工作区</button></div><p v-if="state.notice" class="ew-notice" role="status">{{state.notice}}<button aria-label="关闭提示" @click="state.notice=''">×</button></p>
  <p v-if="state.refreshError" class="ew-notice" role="status">{{state.refreshError}}</p>
  <div v-if="state.tabs.length" class="ew-tab-bar">
    <div ref="tabList" class="ew-tabs" role="tablist"><button class="ew-file-list-tab ew-label-button" :aria-selected="!active" role="tab" aria-label="返回文件列表" title="返回目录" @click="state.active='';query({file:undefined,anchor:undefined})"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M4 5h2m4 0h10M4 12h2m4 0h10M4 19h2m4 0h10" /></svg><span class="ew-file-list-label">目录</span></button><span v-for="doc in state.tabs" :key="doc.id" class="ew-document-tab" :class="{'is-active':state.active===doc.path}"><button role="tab" :aria-selected="state.active===doc.path" :title="doc.path" @click="open(doc.path)">{{basename(doc.path)}}{{doc.text!==doc.original?' •':''}}</button><button class="ew-tab-close" :disabled="state.busy" :aria-label="`关闭 ${basename(doc.path)}`" :title="`关闭 ${doc.path}`" @click="closeTabs([doc.path])"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m7 7 10 10M7 17 17 7" /></svg></button></span></div>
    <details ref="tabMenu" class="ew-more ew-tabs-more" @keydown.esc.stop.prevent="dismissDetails(tabMenu)">
      <summary role="button" aria-label="更多标签操作" title="更多标签操作" @click="menu=false"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M5 12h.1m6.9 0h.1m6.9 0h.1" stroke-width="4" /></svg></summary>
      <div><button :disabled="state.busy || !active || state.tabs.length<2" @click="closeTabGroup(true)">关闭其他</button><button :disabled="state.busy" @click="closeTabGroup()">关闭所有</button></div>
    </details>
  </div>
  <div v-if="!active" class="ew-tools ew-create-tools"><button class="ew-label-button" :disabled="state.busy || !state.projection?.filesAvailable" @click="create(false)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M14 3H5v18h14V8zM14 3v5h5M8 14h8m-4-4v8" /></svg>新建文件</button><button class="ew-label-button ew-folder-action" :disabled="state.busy || !state.projection?.filesAvailable" aria-label="新建文件夹" title="新建文件夹" @click="create(true)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M3 7V5h6l2 2h10v12H3zM12 10v6m-3-3h6" /></svg>新建文件夹</button><button class="ew-label-button" :disabled="state.busy || !state.projection?.filesAvailable" @click="chooseUpload()"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M12 16V3m-4 4 4-4 4 4M5 16v5h14v-5" /></svg>上传</button><button class="ew-label-button ew-folder-action" :disabled="state.busy || !state.projection?.filesAvailable" aria-label="上传文件夹" title="上传文件夹" @click="chooseUpload(true)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M2 9V7h6l2 2h6v12H2zM20 14V3m-3 3 3-3 3 3" /></svg>上传文件夹</button><button :disabled="state.busy" class="ew-icon-button" aria-label="刷新" title="刷新工作区" @click="act(reloadDirectory)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M20 7v5h-5M4 17v-5h5M6 6a8 8 0 0 1 14 6M4 12a8 8 0 0 0 14 6" /></svg></button><details ref="createMenu" class="ew-more ew-create-more" @keydown.esc.stop.prevent="dismissDetails(createMenu)"><summary role="button" aria-label="更多文件操作" title="更多文件操作" @click="menu=false;if(displayMenu)displayMenu.open=false"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M5 11v2m7-2v2m7-2v2" /></svg></summary><div><button class="ew-label-button" :disabled="state.busy || !state.projection?.filesAvailable" @click="createMenuAction(true)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M3 7V5h6l2 2h10v12H3zM12 10v6m-3-3h6" /></svg>新建文件夹</button><button class="ew-label-button" :disabled="state.busy || !state.projection?.filesAvailable" @click="createMenuAction(false)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M2 9V7h6l2 2h6v12H2zM20 14V3m-3 3 3-3 3 3" /></svg>上传文件夹</button></div></details></div>
  <div class="ew-body"><nav v-show="treeVisible" id="ew-directory-tree" class="ew-tree" aria-label="目录树"><div v-for="node in tree" :key="node.path" class="ew-tree-node" :class="{'is-current':state.dir===node.path}" :style="{paddingLeft:`${node.level*12+4}px`}">
    <div class="ew-tree-row" :aria-busy="treeLoading===node.path">
      <span v-if="treeLoading===node.path" class="ew-tree-marker ew-spinner" role="status" :aria-label="`正在读取目录 ${node.path||'根目录'}`" />
      <button v-else-if="!treeKnown(node.path)||hasSubdirectories(node.path)||treeErrors[node.path]" class="ew-tree-marker" :disabled="state.busy" :aria-expanded="treeExpanded.includes(node.path)" :aria-label="`${treeErrors[node.path]?'重试读取目录':treeExpanded.includes(node.path)?'折叠目录':'展开目录'} ${node.path||'根目录'}`" @click="act(()=>expandTree(node.path),false)">{{treeErrors[node.path]?'!':treeExpanded.includes(node.path)?'▾':'▸'}}</button>
      <span v-else class="ew-tree-marker ew-tree-leaf" role="img" aria-label="无子文件夹" title="无子文件夹，仍可打开查看文件">·</span>
      <button class="ew-tree-name" :disabled="state.busy" :aria-current="state.dir===node.path?'location':undefined" :title="node.path||'根目录'" @click="act(()=>navigate(node.path))"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M3 7V5h6l2 2h10v12H3z" /></svg><span>{{basename(node.path)||'根目录'}}</span></button>
    </div><p v-if="treeErrors[node.path]" class="ew-tree-error" :title="treeErrors[node.path]" role="status">读取失败，点击 ! 重试</p>
  </div></nav>
    <WorkspaceDocument v-for="doc in viewers" v-show="doc.path===state.active" :key="doc.id" :doc="doc" :active="doc.path===state.active" :fragment="doc.path===String(route.query.file||'')&&typeof route.query.anchor==='string'?route.query.anchor:undefined" @saved="path=>{if(state.active===path)query({file:path})}" @fragment="value=>query({anchor:value||undefined})" @link="(path,fragment)=>act(()=>openLink(path,fragment))" @open="path=>act(()=>open(path))" @text="path=>act(async()=>{state.tabs=state.tabs.filter(d=>d.path!==path);await owner.open(path,true)})" />
    <div v-if="!active" class="ew-browser"><div class="ew-tools ew-browser-tools" aria-label="文件筛选和操作"><input :value="filter" aria-label="筛选当前目录" placeholder="筛选当前目录" @input="query({filter:($event.target as HTMLInputElement).value||undefined})" />
      <button ref="operationButton" :disabled="!selected.length" :aria-expanded="menu" @click="toggleOperations">操作{{selected.length?` (${selected.length})`:''}}</button>
      <button class="ew-icon-button" :disabled="!state.clipboard || state.busy" aria-label="粘贴" title="粘贴" @click="paste()"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M9 5H5v16h14V5h-4M9 3h6v4H9zM9 12h6m-6 4h6" /></svg></button>
      <button class="ew-label-button" :disabled="!selected.length" aria-label="下载" title="下载所选项目" @click="download"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M12 3v12m-4-4 4 4 4-4M5 16v5h14v-5" /></svg><span class="ew-download-label">下载</span></button>
      <button class="ew-icon-button" :disabled="!selected.length || state.busy" aria-label="删除" title="删除所选项目" @click="remove"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M4 6h16M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7m4-7v7" /></svg></button>
      <details ref="displayMenu" class="ew-more ew-display-menu" @keydown.esc.stop.prevent="dismissDisplay"><summary role="button" aria-label="显示选项" title="排序和隐藏项" @click="menu=false;if(createMenu)createMenu.open=false">显示</summary><div><label>排序<select :value="sort" aria-label="文件排序" @change="query({sort:($event.target as HTMLSelectElement).value,order:undefined})"><option value="name">名称</option><option value="date">修改时间</option><option value="size">大小</option></select></label><div class="ew-sort-order" role="group" aria-label="排序方向"><button :aria-pressed="order==='asc'" @click="query({order:'asc'})">升序</button><button :aria-pressed="order==='desc'" @click="query({order:'desc'})">降序</button></div><label><input type="checkbox" :checked="hidden" @change="query({hidden:hidden?undefined:'1'})" />显示隐藏项</label></div></details>
      <button class="ew-icon-button" :aria-label="grid?'列表':'图标'" :title="grid?'切换到列表视图':'切换到图标视图'" @click="query({view:grid?undefined:'grid'})"><svg aria-hidden="true" viewBox="0 0 24 24"><path :d="grid?'M4 5h2m4 0h10M4 12h2m4 0h10M4 19h2m4 0h10':'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z'" /></svg></button></div>
      <div v-if="grid && entries.length>48" class="ew-tools"><button :disabled="gridPage<=1" @click="gridPage--">上页</button><span>{{gridPage}} / {{Math.ceil(entries.length/48)}}</span><button :disabled="gridPage*48>=entries.length" @click="gridPage++">下页</button></div>
      <div v-if="menu" class="ew-menu" role="group" aria-label="所选项目操作"><button :disabled="selected.length!==1" @click="rename">重命名</button><button @click="clipboard(false)">复制</button><button @click="clipboard(true)">剪切</button><button @click="moveTo">移动到…</button><button @click="copyText(selected.join('\n'));menu=false">复制路径</button><button :disabled="selected.length!==1" @click="copyLink">复制定位链接</button><button @click="properties=true;menu=false">属性</button><button @click="menu=false">关闭菜单</button></div>
      <p v-if="state.loading" class="ew-empty">正在读取目录…</p><div v-else-if="!state.directoryLoaded" class="ew-empty"><span>{{state.projection?.filesAvailable?'目录尚未读取成功':'企业工作区尚不可用'}}</span><button :disabled="state.busy" @click="act(reloadDirectory)">重试读取目录</button></div><p v-else-if="!entries.length" class="ew-empty">{{!state.projection?.filesAvailable?'企业工作区尚不可用':filter?'没有匹配项目':state.entries.length?'没有可显示的项目':'此目录为空，可拖入文件上传'}}</p>
      <div v-else class="ew-files" :class="{'ew-grid':grid}" role="listbox" tabindex="0" aria-label="企业文件" aria-multiselectable="true"><div v-for="entry in visibleEntries" :key="entry.path" class="ew-file" role="option" tabindex="-1" :aria-selected="selected.includes(entry.path)" :title="entry.path" @click="choose($event,entry)" @dblclick="enter(entry)" @contextmenu.prevent="selected=[entry.path];menu=true"><img v-if="grid && thumbnails[entry.path]" class="ew-thumb" :src="thumbnails[entry.path]" alt="" /><span v-else class="ew-file-icon" :class="{'ew-pdf-icon':kind(entry.path)==='pdf'}" aria-hidden="true"><svg v-if="entry.kind==='DIRECTORY'" viewBox="0 0 24 24"><path d="M3 7V5h6l2 2h10v12H3z" /></svg><span v-else-if="kind(entry.path)==='pdf'">PDF</span><svg v-else viewBox="0 0 24 24"><path :d="kind(entry.path)==='image'?'M3 3h18v18H3zM3 17l6-6 4 4 3-3 5 5M15 6a1 1 0 1 0 0 2 1 1 0 0 0 0-2':kind(entry.path)==='audio'?'M9 17V5l11-2v12M9 8l11-2M9 17a3 3 0 1 0-6 0 3 3 0 0 0 6 0M20 15a3 3 0 1 0-6 0 3 3 0 0 0 6 0':kind(entry.path)==='video'?'M3 4h18v16H3zM10 8l6 4-6 4z':kind(entry.path)==='binary'?'M14 3H5v18h14V8zM14 3v5h5':'M14 3H5v18h14V8zM14 3v5h5M8 12h8m-8 4h6'" /></svg></span><span class="ew-file-name">{{basename(entry.path)}}</span><span class="ew-file-size">{{entry.kind==='DIRECTORY'?'文件夹':bytes(entry.size)}}</span><time class="ew-file-date">{{entry.modifiedAt?new Date(entry.modifiedAt).toLocaleString():''}}</time><button class="ew-open" aria-label="打开" @click.stop="enter(entry)">打开</button></div></div>
    </div></div>
  <section v-if="state.transfers.length" class="ew-transfers" aria-label="上传进度"><header><strong>上传 · {{state.transfers.filter(t=>t.status==='服务端已确认保存').length}} / {{state.transfers.length}} 已确认保存</strong><button :disabled="state.busy || state.transfers.some(t=>t.cancel)" @click="state.transfers=[]">清除记录</button></header><div ref="transferList" class="ew-transfer-list"><div v-for="(transfer,i) in state.transfers" :key="i" class="ew-transfer"><span :title="transfer.path">{{basename(transfer.path)}}</span><button v-if="transfer.cancel" :aria-label="`取消上传 ${basename(transfer.path)}`" @click="transfer.cancel()">取消</button><progress :value="transfer.progress" max="100" :aria-label="`${basename(transfer.path)} 上传进度`" /><small>{{bytes(transfer.loaded)}} / {{bytes(transfer.total)}} · {{transfer.progress}}%</small><p role="status">{{transfer.status}}</p></div></div></section>
  <footer><button v-if="active" class="ew-current-file" :title="`打开所在目录：${active.path}`" :disabled="state.busy" @click="act(()=>navigate(parent(active!.path)))">{{active.path}}</button><template v-else>{{state.directoryLoaded?state.entries.length+' 项':'—'}} <span v-if="selected.length"> · 已选 {{selected.length}} 项</span><span v-if="state.available!==undefined"> · 可用 {{bytes(state.available)}}</span><span v-if="state.used!==undefined"> · 已用 {{bytes(state.used)}}</span></template><span v-if="state.refreshing" role="status"> · 正在自动刷新</span></footer>
  <dialog ref="propertyDialog" class="ew-property-dialog" aria-labelledby="ew-property-title" @cancel.prevent="properties=false" @close="properties=false" @keydown.stop><h3 id="ew-property-title">文件属性</h3><dl v-for="entry in selectedEntries" :key="entry.path"><dt>路径</dt><dd>{{entry.path}}</dd><dt>类型</dt><dd>{{entry.kind==='DIRECTORY'?'文件夹':kind(entry.path)}}</dd><dt>大小</dt><dd>{{bytes(entry.size)}}</dd><dt>修改时间</dt><dd>{{entry.modifiedAt||'—'}}</dd></dl><button autofocus @click="properties=false">关闭</button></dialog>
  <div v-if="dragOver" class="ew-drop">{{!state.projection?.filesAvailable?'企业工作区当前不可用':state.busy?'正在处理文件，请稍后上传':`释放以上传到 ${state.dir||'根目录'}`}}</div>
</component></template>
<style src="./workspace.css"></style>
<style scoped>
.ew-property-dialog{width:min(460px,calc(100vw - 32px));max-height:80dvh;box-sizing:border-box;overflow:auto;padding:var(--tuck-dialog-padding);color:var(--tuck-text);background:var(--tuck-surface);border:1px solid var(--tuck-border);border-radius:var(--tuck-dialog-radius)}.ew-property-dialog::backdrop{background:#0008}.ew-property-dialog h3{margin:0 0 var(--tuck-dialog-gap);font-size:var(--tuck-font-title);line-height:var(--tuck-line-title)}.ew-property-dialog button{min-height:var(--tuck-dialog-control-height);padding:5px 10px;border:1px solid var(--tuck-border);border-radius:6px;background:var(--tuck-raised);color:var(--tuck-text);font:inherit;cursor:pointer}.ew-property-dialog dl{display:grid;grid-template-columns:65px minmax(0,1fr);gap:8px}.ew-property-dialog dd{margin:0;overflow-wrap:anywhere}
</style>
