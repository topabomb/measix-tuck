import {test,expect} from '@playwright/test'
import {createReadStream} from 'node:fs'
import {stat} from 'node:fs/promises'
import {randomUUID} from 'node:crypto'
import {request as httpRequest} from 'node:http'

test('large video streams and seeks through the enrolled Core workspace',async({page},info)=>{
  const source=process.env.TUCK_LIVE_VIDEO
  const liveURL=process.env.TUCK_LIVE_URL
  test.skip(!source||!liveURL,'Set TUCK_LIVE_VIDEO and TUCK_LIVE_URL to use an enrolled Go service for real upload and cleanup')
  test.setTimeout(600_000)
  const size=(await stat(source!)).size;expect(size).toBeGreaterThan(256*1024*1024)
  const app={url:liveURL!,manager:{async create(input:{title:string}){const response=await fetch(liveURL+'/api/tasks',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});expect(response.ok).toBe(true);return response.json()}},async close(){}},directory=`tuck-video-verify-${randomUUID()}`,path=directory+'/large.mp4'
  let created=false,projection:{agentSpaceId:string;subject:string},peakRss=process.memoryUsage().rss
  const initialRss=peakRss,timer=setInterval(()=>{peakRss=Math.max(peakRss,process.memoryUsage().rss)},250)
  const url=(resource:string,file='',extra:Record<string,string>={})=>app.url+'/api/enterprise-workspace'+resource+'?'+new URLSearchParams({agentSpaceId:projection.agentSpaceId,subject:projection.subject,path:file,...extra})
  const mutation=async(action:string,file:string,extra:Record<string,unknown>={})=>{const res=await fetch(url('/files'),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action,path:file,...extra})});const value=await res.json();expect(res.ok,JSON.stringify(value)).toBe(true);expect(value.outcome).toBe('SUCCEEDED')}
  try{
    const summary=await fetch(app.url+'/api/enterprise-workspace');projection=await summary.json();expect(summary.ok).toBe(true)
    await mutation('MKCOL',directory);created=true
    console.log(`Uploading ${size} bytes to the isolated verification directory`)
    const body=createReadStream(source!);let sent=0,reported=0;body.on('data',chunk=>{sent+=chunk.length;if(sent-reported>=64*1024*1024){reported=sent;console.log(`Upload input consumed: ${sent} bytes, process buffers: ${process.memoryUsage().arrayBuffers}`)}})
    const uploaded=await new Promise<{status:number;text:string}>((resolve,reject)=>{
      const request=httpRequest(url('/content',path),{method:'PUT',headers:{'If-None-Match':'*','Content-Type':'application/octet-stream','Content-Length':String(size)},signal:AbortSignal.timeout(450000)},response=>{const chunks:Buffer[]=[];response.on('data',chunk=>chunks.push(chunk));response.on('error',reject);response.on('end',()=>resolve({status:response.statusCode||0,text:Buffer.concat(chunks).toString()}))});request.on('error',reject);body.on('error',reject);body.pipe(request)
    }).finally(()=>body.destroy())
    console.log('Upload response:',uploaded.status)
    expect(uploaded.status,uploaded.text).toBe(200);expect(JSON.parse(uploaded.text).outcome).toBe('SUCCEEDED')
    const task=await app.manager.create({title:'大视频流式验证'})
    const cdp=await page.context().newCDPSession(page);await cdp.send('Network.enable')
    const mediaIds=new Set<string>(),requests:{range:string;status?:number;contentRange?:string}[]=[],indices=new Map<string,number>();let received=0,cancelled=0
    cdp.on('Network.requestWillBeSent',event=>{if(event.request.url.includes('/enterprise-workspace/content')&&event.request.url.includes('preview=1')){mediaIds.add(event.requestId);indices.set(event.requestId,requests.length);requests.push({range:String(event.request.headers.Range||event.request.headers.range||'')})}})
    cdp.on('Network.responseReceived',event=>{const index=indices.get(event.requestId);if(index!==undefined)Object.assign(requests[index]!,{status:event.response.status,contentRange:event.response.headers['Content-Range']||event.response.headers['content-range']})})
    cdp.on('Network.dataReceived',event=>{if(mediaIds.has(event.requestId))received+=event.dataLength})
    cdp.on('Network.loadingFailed',event=>{if(mediaIds.has(event.requestId)&&event.canceled)cancelled++})
    await page.goto(app.url+'/#/chat/conversations/'+task.id+'?'+new URLSearchParams({ew:'1',wide:'1',dir:directory,file:path,space:projection.agentSpaceId}))
    const video=page.locator('.enterprise-workspace video')
    await expect.poll(()=>video.evaluate((v:HTMLVideoElement)=>v.readyState),{timeout:60000}).toBeGreaterThanOrEqual(1)
    const duration=await video.evaluate((v:HTMLVideoElement)=>v.duration);expect(duration).toBeGreaterThan(60)
    const metadataBytes=received;expect(metadataBytes).toBeLessThan(size/4)
    await video.evaluate(async(v:HTMLVideoElement)=>{v.muted=true;await v.play()})
    await expect.poll(()=>video.evaluate((v:HTMLVideoElement)=>v.currentTime),{timeout:30000}).toBeGreaterThan(1)
    await video.evaluate((v:HTMLVideoElement)=>{v.currentTime=v.duration-8})
    await expect.poll(()=>video.evaluate((v:HTMLVideoElement)=>!v.seeking&&v.currentTime>v.duration-7),{timeout:60000}).toBe(true)
    await video.evaluate((v:HTMLVideoElement)=>v.pause())
    expect(requests.some(r=>r.status===206 && /^bytes [1-9]\d*-/.test(r.contentRange||'')),JSON.stringify(requests)).toBe(true)
    expect(received).toBeLessThan(size/2)
    await page.screenshot({path:info.outputPath('large-video-playing.png')})
    await page.getByRole('button',{name:'收起企业工作区'}).click();await expect(video).toHaveCount(0)
    await expect.poll(()=>cancelled,{timeout:15000}).toBeGreaterThan(0)
    const browserHeap=await cdp.send('Runtime.getHeapUsage')
    const evidence={fileBytes:size,duration,metadataBytes,receivedBytes:received,peakNodeRssIncrease:peakRss-initialRss,browserHeap:browserHeap.usedSize,cancelled,requests}
    await info.attach('large-video-evidence',{body:JSON.stringify(evidence,null,2),contentType:'application/json'});console.log('Large video verification:',JSON.stringify(evidence))
    expect(peakRss-initialRss).toBeLessThan(size/2)
  }finally{
    clearInterval(timer)
    try{if(created){await mutation('DELETE',directory,{recursiveConfirmed:true});console.log('Large video verification directory cleaned')}}finally{await app.close()}
  }
})
