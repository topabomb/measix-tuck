import {test,expect,type Page} from '@playwright/test'
import {createReadStream} from 'node:fs'
import {mkdtemp,stat,rm,writeFile} from 'node:fs/promises'
import {tmpdir} from 'node:os'
import {join,relative,resolve} from 'node:path'
import {spawn} from 'node:child_process'
import {once} from 'node:events'
import {setTimeout as delay} from 'node:timers/promises'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

// Playwright 1.62.1 tracing reads HEAD response bodies. For an empty body with
// Content-Length, Chromium's response-body fallback calls Network.loadNetworkResource:
// an extra GET without the original method/Range/If-Match, unrelated to the player.
// That artificial full download invalidates byte/ETag/cancellation assertions here.
// Keep fixture network metrics and explicit screenshots as the evidence instead.
test.use({trace:'off'})

// Opt in to the 532 MiB case; ordinary runs exercise the same codec/lifecycle at low bitrate.
const large=process.env.TUCK_TEST_LARGE_MEDIA==='1',rate=4*1024*1024
let source='',ownedDirectory='',size=0
test.beforeAll(async()=>{
  test.setTimeout(600_000)
  source=process.env.TUCK_TEST_MEDIA_SOURCE||''
  if(!source){
    ownedDirectory=await mkdtemp(join(tmpdir(),'tuck-media-e2e-'));source=join(ownedDirectory,'video.mp4')
    const bitrate=large?'64M':'512k'
    const child=spawn(process.env.TUCK_TEST_FFMPEG||'ffmpeg',['-hide_banner','-loglevel','error','-f','lavfi','-i','testsrc2=size=640x360:rate=10','-t','70','-c:v','libx264','-preset','ultrafast','-pix_fmt','yuv420p','-b:v',bitrate,'-minrate',bitrate,'-maxrate',bitrate,'-bufsize',large?'128M':'1024k','-x264-params','nal-hrd=cbr:force-cfr=1','-movflags','+faststart','-y',source],{windowsHide:true})
    let diagnostic='';child.stderr.on('data',chunk=>{diagnostic+=String(chunk).slice(-4000)})
    const [code]=await once(child,'close');expect(code,diagnostic).toBe(0)
  }
  size=(await stat(source)).size
  if(large)expect(size).toBeGreaterThanOrEqual(256*1024*1024)
})
test.afterAll(async()=>{
  if(ownedDirectory){expect(relative(resolve(tmpdir()),resolve(ownedDirectory))).toMatch(/^tuck-media-e2e-[^\\/]+$/);await rm(ownedDirectory,{recursive:true,force:true})}
})

async function mediaFixture(){
  const metrics={bytes:0,active:0,canceled:0,completed:0,peakActive:0,requests:[] as {path:string;range:string;status:number;bytes:number}[]}
  let version='"immutable-video-v1"'
  const paths=['video.mp4','missing.mp4','weak.mp4','missing.pdf','weak.pdf']
  const fileEtag=(path:string)=>path.startsWith('missing')?undefined:path.startsWith('weak')?'W/"weak"':version
  const core=await workspaceFixture({
    extraEntries:directory=>directory?[]:paths.map(path=>({path,kind:'FILE' as const,size,etag:fileEtag(path),modifiedAt:'2026-10-11T00:00:00Z'})),
    content:async(req,res,path)=>{
      if(!paths.includes(path)||!['GET','HEAD'].includes(req.method||''))return false
      const etag=fileEtag(path),headers:Record<string,string>={'Accept-Ranges':'bytes','Content-Type':'application/octet-stream',...(etag?{ETag:etag}:{})}
      if(req.headers['if-match']&&req.headers['if-match']!==etag){res.writeHead(412,{'Content-Type':'application/json'});res.end(JSON.stringify({code:'file_version_conflict'}));return true}
      let start=0,end=size-1,status=200
      if(req.headers.range){
        const match=/^bytes=(\d*)-(\d*)$/.exec(req.headers.range)
        if(!match||!match[1]&&!match[2]){res.writeHead(416,{'Content-Range':`bytes */${size}`});res.end();return true}
        if(!match[1])start=Math.max(0,size-Number(match[2]));else{start=Number(match[1]);if(match[2])end=Math.min(end,Number(match[2]))}
        if(start> end||start>=size){res.writeHead(416,{'Content-Range':`bytes */${size}`});res.end();return true}
        status=206;headers['Content-Range']=`bytes ${start}-${end}/${size}`
      }
      res.writeHead(status,{...headers,'Content-Length':String(end-start+1)})
      if(req.method==='HEAD'){res.end();return true}
      const record={path,range:req.headers.range||'',status,bytes:0};metrics.requests.push(record)
      metrics.active++;metrics.peakActive=Math.max(metrics.peakActive,metrics.active)
      const input=createReadStream(source,{start,end,highWaterMark:64*1024}),controller=new AbortController(),began=performance.now()
      const close=()=>{controller.abort();input.destroy()};res.once('close',close)
      try{
        for await(const chunk of input){
          await delay(Math.max(0,record.bytes/rate*1000-(performance.now()-began)),undefined,{signal:controller.signal})
          if(res.destroyed)break
          const writable=res.write(chunk);record.bytes+=chunk.length;metrics.bytes+=chunk.length
          if(!writable)await once(res,'drain',{signal:controller.signal})
        }
        if(!res.destroyed){const finished=once(res,'finish',{signal:controller.signal});res.end();await finished}
      }catch(error){if(!controller.signal.aborted)res.destroy(error as Error)}
      finally{input.destroy();res.off('close',close);metrics.active--;if(res.writableFinished)metrics.completed++;else metrics.canceled++}
      return true
    },
  })
  const app=await fixture({enterprisePlatform:core.platform})
  const open=async(page:Page,path='video.mp4')=>{
    const task=await app.manager.create({title:large?'真实大视频带宽与交互':'媒体生命周期'})
    await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1&file=${encodeURIComponent(path)}`)
    return page.getByRole('complementary',{name:'企业工作区'})
  }
  return {core,app,metrics,open,changeVersion:()=>{version='"immutable-video-v2"'},close:async()=>{await app.close();await core.close()}}
}

test('media streaming preserves playback through refresh, releases stopped/background streams and keeps controls responsive',async({page},info)=>{
  test.setTimeout(large?240_000:120_000)
  const f=await mediaFixture()
  try{
    let chatStreams=0
    page.on('request',request=>{if(new URL(request.url()).pathname==='/api/events')chatStreams++})
    const panel=await f.open(page),video=panel.locator('video')
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement)=>el.readyState),{timeout:30_000}).toBeGreaterThanOrEqual(1)
    expect(await video.evaluate((el:HTMLVideoElement)=>el.duration)).toBeGreaterThanOrEqual(69)
    const metadataBytes=f.metrics.bytes
    if(large)expect(metadataBytes).toBeLessThan(size/4)
    const original=await video.elementHandle(),url=await video.getAttribute('src')
    await video.evaluate((el:HTMLVideoElement)=>{el.dataset.reloads='0';el.addEventListener('loadstart',()=>{el.dataset.reloads=String(Number(el.dataset.reloads)+1)});el.muted=true;void el.play()})
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement)=>el.currentTime),{timeout:30_000}).toBeGreaterThan(1)
    const beforeRefresh=await video.evaluate((el:HTMLVideoElement)=>el.currentTime)
    const summaryCount=()=>f.core.requests.filter(request=>request.url.endsWith('/workspace')).length
    const beforeSummary=summaryCount()
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
    await expect.poll(summaryCount).toBeGreaterThan(beforeSummary)
    await expect(panel.getByText('正在自动刷新',{exact:false})).toHaveCount(0)
    await panel.getByRole('button',{name:'刷新',exact:true}).click()
    await expect(panel.getByRole('button',{name:'刷新',exact:true})).toBeEnabled()
    await panel.getByRole('button',{name:'展开工作区',exact:true}).click()
    await expect(panel.getByRole('button',{name:/显示目录树|隐藏目录树/})).toHaveCount(0)
    await expect(panel.getByRole('navigation',{name:'目录树'})).toBeHidden()
    await panel.getByRole('button',{name:'专注内容',exact:true}).click()
    await panel.getByRole('button',{name:'退出专注内容',exact:true}).click()
    await panel.getByRole('button',{name:'还原工作区',exact:true}).click()
    expect(await original!.evaluate(el=>el===document.querySelector('.enterprise-workspace video'))).toBe(true)
    await expect(video).toHaveAttribute('src',url!)
    expect(await video.getAttribute('data-reloads')).toBe('0')
    expect(await video.evaluate((el:HTMLVideoElement)=>el.currentTime)).toBeGreaterThanOrEqual(beforeRefresh)
    await page.getByTestId('composer-input').fill('视频播放时保留的聊天草稿')
    await expect(page.getByTestId('composer-input')).toHaveValue('视频播放时保留的聊天草稿')

    // Use real same-origin control requests while the player remains mounted and streaming.
    await video.evaluate((el:HTMLVideoElement)=>{el.currentTime=35})
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement)=>!el.seeking&&el.currentTime>35),{timeout:40_000}).toBe(true)
    if(large)await expect.poll(()=>f.metrics.active).toBeGreaterThan(0)
    const bytesBeforeControl=f.metrics.bytes
    const controls=await page.evaluate(async mediaURL=>{
      const content=new URL(mediaURL!,location.href),list=new URL(content);list.pathname=list.pathname.replace(/content$/,'files');list.searchParams.set('path','');list.searchParams.delete('preview');list.searchParams.delete('version')
      content.searchParams.set('path','small-control.txt');content.searchParams.delete('preview');content.searchParams.delete('version')
      const began=performance.now(),responses=await Promise.all([fetch(list),fetch(content,{method:'PUT',headers:{'If-None-Match':'*','Content-Type':'application/octet-stream'},body:'small save during media'})])
      return {elapsed:performance.now()-began,statuses:responses.map(response=>response.status),bodies:await Promise.all(responses.map(response=>response.text()))}
    },url)
    expect(controls.statuses,JSON.stringify(controls)).toEqual([200,200]);expect(controls.elapsed).toBeLessThan(3000)
    expect(f.core.files.get('small-control.txt')?.toString()).toBe('small save during media')
    if(large)await expect.poll(()=>f.metrics.bytes).toBeGreaterThan(bytesBeforeControl)
    const modelStarted=performance.now()
    await page.getByTestId('composer-input').fill('METRICS_STREAM 视频播放时验证真实模型请求链路')
    await page.getByTestId('composer-input').press('Enter')
    await expect(page.getByTestId('markdown-block').filter({hasText:'流式计时验收。'})).toBeVisible()
    const modelLatencyMs=performance.now()-modelStarted
    expect(f.app.requests).toHaveLength(1);expect(chatStreams).toBe(1)
    expect(await original!.evaluate(el=>el===document.querySelector('.enterprise-workspace video'))).toBe(true)
    expect(await video.evaluate((el:HTMLVideoElement)=>el.paused)).toBe(false)
    await page.getByTestId('composer-input').fill('视频播放时保留的聊天草稿')
    const savedPosition=await video.evaluate((el:HTMLVideoElement)=>el.currentTime),canceled=f.metrics.canceled
    await panel.getByRole('button',{name:'停止读取',exact:true}).click()
    await expect(video).not.toHaveAttribute('src',/.+/)
    await expect.poll(()=>f.metrics.active).toBe(0)
    if(large)expect(f.metrics.canceled).toBeGreaterThan(canceled)
    const stoppedBytes=f.metrics.bytes;await page.waitForTimeout(500);expect(f.metrics.bytes).toBe(stoppedBytes)
    await expect(panel.getByRole('status')).toContainText('保留位置')
    await panel.getByRole('button',{name:'继续播放',exact:true}).click()
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement,position)=>!el.paused&&!el.seeking&&el.currentTime>=position-.25,savedPosition),{timeout:40_000}).toBe(true)

    // Explicit simulation: Chromium headless does not reliably background a tab on demand.
    const beforeHidden=await video.evaluate((el:HTMLVideoElement)=>el.currentTime)
    await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});document.dispatchEvent(new Event('visibilitychange'))})
    await expect(video).not.toHaveAttribute('src',/.+/);await expect.poll(()=>f.metrics.active).toBe(0)
    const hiddenBytes=f.metrics.bytes
    await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:false});document.dispatchEvent(new Event('visibilitychange'))})
    await page.waitForTimeout(500);expect(f.metrics.bytes).toBe(hiddenBytes)
    expect(await video.evaluate((el:HTMLVideoElement)=>el.paused)).toBe(true)
    await panel.getByRole('button',{name:'继续播放',exact:true}).click()
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement,position)=>!el.paused&&!el.seeking&&el.currentTime>=position-.25,beforeHidden),{timeout:40_000}).toBe(true)

    const projection=await (await page.request.get(f.app.url+'/api/enterprise-workspace')).json()
    await page.route('**/api/enterprise-workspace',route=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({...projection,filesAvailable:false,filesReason:'测试：工作区已停用'})}))
    await panel.getByRole('button',{name:'刷新',exact:true}).click()
    await expect(video).not.toHaveAttribute('src',/.+/);await expect.poll(()=>f.metrics.active).toBe(0)
    await expect(panel.getByRole('button',{name:'继续播放',exact:true})).toBeDisabled()
    await page.unroute('**/api/enterprise-workspace')
    await panel.getByRole('button',{name:'刷新',exact:true}).click()
    await expect(panel.getByRole('button',{name:'继续播放',exact:true})).toBeEnabled()
    await panel.getByRole('button',{name:'继续播放',exact:true}).click()
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement)=>!el.paused&&!el.seeking),{timeout:40_000}).toBe(true)
    if(large)await expect.poll(()=>f.metrics.active).toBeGreaterThan(0)
    await page.screenshot({path:info.outputPath('media-streaming-lifecycle.png')})
    await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click()
    await expect(video).toHaveCount(0);await expect.poll(()=>f.metrics.active).toBe(0)
    const closedBytes=f.metrics.bytes;await page.waitForTimeout(500);expect(f.metrics.bytes).toBe(closedBytes)
    expect(f.metrics.requests.some(request=>request.status===206&&/^bytes=[1-9]\d*-/.test(request.range))).toBe(true)
    const evidence=info.outputPath('media-network-evidence.json')
    await writeFile(evidence,JSON.stringify({large,sourceBytes:size,metadataBytes,controlLatencyMs:controls.elapsed,modelLatencyMs,chatStreams,simulatedVisibility:true,...f.metrics},null,2))
    await info.attach('media-network-evidence',{path:evidence,contentType:'application/json'})
  }finally{await f.close()}
})

test('missing and weak media/PDF ETags never start content GET requests',async({page})=>{
  const f=await mediaFixture()
  try{
    for(const path of ['missing.mp4','weak.mp4','missing.pdf','weak.pdf']){
      const panel=await f.open(page,path)
      await expect(panel.getByRole('alert')).toContainText(/版本|ETag/)
      await expect(panel.locator('video,audio')).toHaveCount(0)
      expect(f.core.requests.filter(request=>request.path===path&&request.method==='GET')).toEqual([])
    }
  }finally{await f.close()}
})

test('media revalidation rejects an old ETag after stopping and explicit reread loads the new version',async({page})=>{
  test.setTimeout(90_000)
  const f=await mediaFixture()
  try{
    const panel=await f.open(page),video=panel.locator('video')
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement)=>el.readyState),{timeout:30_000}).toBeGreaterThanOrEqual(1)
    await panel.getByRole('button',{name:'停止读取',exact:true}).click()
    await expect(video).not.toHaveAttribute('src',/.+/);await expect.poll(()=>f.metrics.active).toBe(0)
    f.changeVersion()
    await panel.getByRole('button',{name:'继续播放',exact:true}).click()
    await expect(panel.getByRole('alert')).toContainText('版本已变化')
    await expect(video).not.toHaveAttribute('src',/.+/);await expect.poll(()=>f.metrics.active).toBe(0)
    await panel.getByRole('button',{name:'文档操作',exact:true}).click()
  await panel.getByRole('button',{name:'重新读取',exact:true}).click()
    await expect.poll(()=>video.evaluate((el:HTMLVideoElement)=>el.readyState),{timeout:30_000}).toBeGreaterThanOrEqual(1)
    expect(new URL((await video.getAttribute('src'))!,f.app.url).searchParams.get('version')).toBe('"immutable-video-v2"')
  }finally{await f.close()}
})
