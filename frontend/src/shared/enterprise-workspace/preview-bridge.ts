import type { PreparedPreview } from './preview'

// This is application-owned code, added after sanitization. Document scripts
// remain forbidden. The frame has an opaque origin and no network permissions.
export function previewBridge(preview:PreparedPreview,token:string,fragment=''):string {
  if(!/^[a-zA-Z0-9_-]+$/.test(token))throw new Error('预览会话标识无效')
  const doc=new DOMParser().parseFromString(preview.html,'text/html')
  const policy=doc.querySelector('meta[http-equiv="Content-Security-Policy"]')
  if(!policy)throw new Error('预览缺少安全策略')
  policy.setAttribute('content',`${policy.getAttribute('content')}; script-src 'nonce-${token}'`)
  const script=doc.createElement('script')
  script.setAttribute('nonce',token)
  const encode=(value:unknown)=>JSON.stringify(value).replace(/</g,'\\u003c')
  const placeholder=(label:string,failed=false)=>'data:image/svg+xml;base64,'+btoa(String.fromCharCode(...new TextEncoder().encode(`<svg xmlns="http://www.w3.org/2000/svg" width="640" height="400" viewBox="0 0 640 400"><rect width="640" height="400" fill="${failed?'#fff1f0':'#f1f3f5'}"/><rect x="277" y="125" width="86" height="65" rx="6" fill="none" stroke="${failed?'#c74646':'#8190a1'}" stroke-width="3"/><path d="M286 181l24-26 18 18 16-12 10 20" fill="none" stroke="${failed?'#c74646':'#8190a1'}" stroke-width="3"/><text x="320" y="240" text-anchor="middle" font-family="system-ui,sans-serif" font-size="22" fill="${failed?'#b33939':'#536174'}">${label}</text></svg>`)))
  script.textContent=`(() => {
    const token=${encode(token)}, initialFragment=${encode(fragment)};
    const loading=${encode(placeholder('图片加载中…'))}, deferred=${encode(placeholder('滚动或点击加载图片'))}, unavailable=${encode(placeholder('加载失败，点击重试',true))}, external=${encode(placeholder('外部或无效图片已阻止',true))};
    const send=(type,extra={})=>parent.postMessage({workspacePreview:token,type,...extra},'*');
    const resources=new Map(${encode(preview.resources.filter(resource=>resource.kind!=='stylesheet'))}.map(resource=>[resource.index,resource]));
    const images=new Map(Array.from(resources.keys()).map(index=>[index,document.querySelector('[data-workspace-image="'+index+'"]')]).filter(([,image])=>image));
    const setSource=(image,url)=>{if(resources.get(Number(image.dataset.workspaceImage)).kind==='background')image.style.backgroundImage='url("'+url+'")';else image.src=url;};
    const near=new Set(),dimensions=new Map(),containers=new Map();let scheduled=false,active=false;
    const remember=event=>{let node=event.target;if(!active||!(node instanceof Element)||node===document.body||node===document.documentElement)return;const path=[],element=node;while(node&&node!==document.body&&path.length<32){const owner=node.parentElement;if(!owner)return;path.unshift(Array.prototype.indexOf.call(owner.children,node));node=owner;}if(node!==document.body)return;const key=path.join('.');containers.delete(key);containers.set(key,{path,left:element.scrollLeft,top:element.scrollTop});if(containers.size>16)containers.delete(containers.keys().next().value);};
    const report=()=>{if(scheduled||!active)return;scheduled=true;requestAnimationFrame(()=>{scheduled=false;if(!active)return;send('position',{left:scrollX,top:scrollY,containers:Array.from(containers.values())});const demands=[];for(const index of near){const image=images.get(index),box=image.getBoundingClientRect();if(!box.width||!box.height)continue;demands.push({index,visible:box.bottom>0&&box.top<innerHeight&&box.right>0&&box.left<innerWidth,edge:Math.ceil(Math.max(box.width,box.height)*Math.min(devicePixelRatio||1,2))});}send('images',{demands});});};
    const observer=new IntersectionObserver(entries=>{for(const entry of entries){const index=Number(entry.target.dataset.workspaceImage);if(entry.isIntersecting)near.add(index);else near.delete(index);}report();},{rootMargin:'600px 0px'});
    const resize=new ResizeObserver(report);
    const setPlaceholder=(image,url)=>{const size=dimensions.get(Number(image.dataset.workspaceImage));if(size){const text=atob(url.split(',')[1]);url='data:image/svg+xml;base64,'+btoa(text.replace('width="640" height="400"','width="'+size.width+'" height="'+size.height+'"'));}setSource(image,url);};
    const scroll=fragment=>{if(!fragment){window.scrollTo(0,0);return;}const target=document.getElementById(fragment)||Array.from(document.querySelectorAll('[name]')).find(node=>node.getAttribute('name')===fragment);if(target)target.scrollIntoView();};
    const open=node=>{
      const requested=node.closest('[data-workspace-image][data-workspace-path]');
      if(requested&&requested.dataset.workspaceState!=='loaded'){send('retryImage',{index:Number(requested.dataset.workspaceImage)});return true;}
      const link=node.closest('a[data-workspace-link]');
      if(link){send('open',{path:link.dataset.workspaceLink,fragment:link.dataset.workspaceFragment||''});return true;}
      const image=node.closest('[data-workspace-image][data-workspace-path]');
      if(image){send('open',{path:image.dataset.workspacePath,fragment:''});return true;}
      return false;
    };
    for(const [index,image] of images){setSource(image,image.dataset.workspacePath?deferred:external);image.dataset.workspaceState='deferred';if(image.dataset.workspacePath){image.tabIndex=0;image.setAttribute('role','button');image.style.cursor='pointer';image.title='图片随滚动加载，点击立即加载';image.addEventListener('load',()=>{if(image.dataset.workspaceState==='loaded')dimensions.set(index,{width:image.naturalWidth,height:image.naturalHeight});});image.addEventListener('error',()=>{if(image.dataset.workspaceState==='loaded')send('imageError',{index});});observer.observe(image);resize.observe(image);}}
    document.addEventListener('scroll',event=>{remember(event);report();},{passive:true,capture:true});addEventListener('resize',report);
    document.addEventListener('pointerdown',()=>send('interact'));
    document.addEventListener('click',event=>{if(event.target instanceof Element&&open(event.target)){event.preventDefault();event.stopPropagation();}});
    document.addEventListener('keydown',event=>{if(event.target instanceof Element&&event.target.matches('[data-workspace-image][data-workspace-path]')&&(event.key==='Enter'||event.key===' ')){event.preventDefault();open(event.target);}});
    addEventListener('message',event=>{
      const data=event.data;
      if(event.source!==parent||!data||data.workspacePreview!==token)return;
      if(data.type==='active'){active=data.active===true;if(active)report();return;}
      if(data.type==='restore'){if(Array.isArray(data.containers))for(const item of data.containers){let node=document.body;for(const index of item.path)node=node?.children[index];if(node){node.scrollTo(item.left,item.top);containers.set(item.path.join('.'),item);}}if(data.fragment)scroll(data.fragment);else if(Number.isFinite(data.left)&&Number.isFinite(data.top))window.scrollTo(data.left,data.top);return;}
      if(data.type==='scroll'&&typeof data.fragment==='string'){scroll(data.fragment);return;}
      if(data.type!=='image'||!Number.isInteger(data.index))return;
      const image=images.get(data.index);
      if(!image)return;
      image.dataset.workspaceState=data.state;
      image.removeAttribute('data-workspace-unavailable');
      if(data.state==='loaded'&&typeof data.url==='string'&&/^data:image\\/(png|jpeg|gif|webp|bmp|svg\\+xml);base64,/.test(data.url)){setSource(image,data.url);image.style.cursor='zoom-in';image.title='在工作区打开原图';}
      else if(data.state==='failed'){setPlaceholder(image,unavailable);image.setAttribute('data-workspace-unavailable','');image.style.cursor='pointer';image.title=(data.error||'图片读取失败')+'；点击重试，可从预览说明打开原图';}
      else{setPlaceholder(image,data.state==='loading'?loading:deferred);image.style.cursor='pointer';image.title=data.state==='loading'?'正在加载图片':'图片随滚动加载，点击立即加载';}
    });
    if(initialFragment)scroll(initialFragment);
    send('ready');
  })();`
  doc.body.append(script)
  return '<!doctype html>'+doc.documentElement.outerHTML
}
