import { test, expect, type Page } from '@playwright/test'
import { deflateSync } from 'node:zlib'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>, core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeAll(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterAll(async()=>{await app?.close();await core?.close()})

async function workspace(page:Page, title:string) {
  const task=await app.manager.create({title})
  await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1&wide=1`)
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  return panel
}

// A real, compressed, narrow PNG makes the decoder and output dimensions part
// of the regression check instead of relying on a fabricated image header.
function narrowPNG(height:number) {
  const crc32=(data:Buffer)=>{let crc=0xffffffff;for(const byte of data){crc^=byte;for(let bit=0;bit<8;bit++)crc=(crc>>>1)^((crc&1)?0xedb88320:0)}return (crc^0xffffffff)>>>0}
  const chunk=(type:string,data:Buffer)=>{const name=Buffer.from(type),length=Buffer.alloc(4),crc=Buffer.alloc(4);length.writeUInt32BE(data.length);crc.writeUInt32BE(crc32(Buffer.concat([name,data])));return Buffer.concat([length,name,data,crc])}
  const header=Buffer.alloc(13);header.writeUInt32BE(1);header.writeUInt32BE(height,4);header[8]=8;header[9]=6
  const pixels=Buffer.alloc(height*5);for(let row=0;row<height;row++){pixels[row*5+2]=128;pixels[row*5+3]=128;pixels[row*5+4]=255}
  return Buffer.concat([Buffer.from([137,80,78,71,13,10,26,10]),chunk('IHDR',header),chunk('IDAT',deflateSync(pixels)),chunk('IEND',Buffer.alloc(0))])
}

function multipagePDF(pages:number) {
  const objects=['<< /Type /Catalog /Pages 2 0 R >>',`<< /Type /Pages /Kids [${Array.from({length:pages},(_,i)=>`${4+i*2} 0 R`).join(' ')}] /Count ${pages} >>`,'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>']
  for(let index=0;index<pages;index++){
    objects.push(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 3 0 R >> >> /Contents ${5+index*2} 0 R >>`)
    const content=`BT /F1 14 Tf 20 170 Td (Preview lifecycle page ${index+1}) Tj ET`
    objects.push(`<< /Length ${Buffer.byteLength(content)} >>\nstream\n${content}\nendstream`)
  }
  let result='%PDF-1.4\n';const offsets:number[]=[]
  objects.forEach((object,index)=>{offsets.push(Buffer.byteLength(result));result+=`${index+1} 0 obj\n${object}\nendobj\n`})
  const xref=Buffer.byteLength(result)
  result+=`xref\n0 ${objects.length+1}\n0000000000 65535 f \n${offsets.map(offset=>`${String(offset).padStart(10,'0')} 00000 n \n`).join('')}trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`
  return result
}

test('CSV above the table row limit remains available in source mode',async({page})=>{
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  core.put('many-rows.csv','value\n'+'a\n'.repeat(200000))
  const panel=await workspace(page,'超多行 CSV')
  await panel.getByRole('option',{name:/many-rows\.csv/}).dblclick()
  await expect(panel.getByRole('alert')).toContainText('表格过大')
  await panel.getByRole('button',{name:'源码',exact:true}).click()
  await expect(panel.locator('.cm-content')).toContainText('value')
  await expect(panel.getByRole('button',{name:'保存',exact:true})).toBeDisabled()
  expect(errors).toEqual([])
})

test('malformed CSV cannot discard source through table actions and becomes editable after correction',async({page})=>{
  const original='id,name\n1,"unterminated'
  core.put('malformed.csv',original)
  const panel=await workspace(page,'CSV 无效内容保护')
  await panel.getByRole('option',{name:/malformed\.csv/}).dblclick()
  await expect(panel.getByRole('alert')).toContainText('CSV 引号未闭合')
  for(const name of ['加行','加列','删当前行','删当前列'])await expect(panel.getByRole('button',{name,exact:true})).toBeDisabled()
  await expect(panel.locator('.csv-scroll textarea')).toHaveCount(0)
  await panel.getByRole('button',{name:'源码',exact:true}).click()
  const editor=panel.locator('.cm-content')
  await expect.poll(()=>editor.innerText()).toBe(original)
  expect(core.files.get('malformed.csv')?.toString()).toBe(original)
  await editor.fill('id,name\n1,fixed\n')
  await panel.getByRole('button',{name:'预览',exact:true}).click()
  await expect(panel.getByRole('textbox',{name:'第 2 行第 2 列',exact:true})).toHaveValue('fixed')
  await expect(panel.getByRole('button',{name:'加列',exact:true})).toBeEnabled()
  await panel.getByRole('button',{name:'加列',exact:true}).click()
  await expect(panel.getByRole('textbox',{name:'第 2 行第 3 列',exact:true})).toHaveValue('')
  await panel.getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(()=>core.files.get('malformed.csv')?.toString()).toBe('id,name,\n1,fixed,\n')
})

test('static SVG keeps internal symbols and gradients while rejecting remote references',async({page})=>{
  core.put('internal-refs.svg','<svg xmlns="http://www.w3.org/2000/svg" width="160" height="100"><defs><linearGradient id="paint"><stop offset="0" stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient><symbol id="tile"><rect width="70" height="70" style="fill:url(#paint)"/></symbol></defs><use id="local" href="#tile"/><use id="remote" href="https://bad.invalid/shape.svg#tile"/><rect id="external-paint" x="80" width="60" height="60" style="fill:url(https://bad.invalid/paint.svg#paint)"/></svg>')
  const external:string[]=[];page.on('request',request=>{if(request.url().includes('bad.invalid'))external.push(request.url())})
  const panel=await workspace(page,'SVG 内部引用')
  await panel.getByRole('option',{name:/internal-refs\.svg/}).dblclick()
  const frame=panel.frameLocator('iframe')
  await expect(frame.locator('use#local')).toHaveAttribute('href','#tile')
  await expect(frame.locator('symbol#tile rect')).toHaveAttribute('style',/fill:\s*url\(#paint\)/)
  await expect(frame.locator('linearGradient#paint stop')).toHaveCount(2)
  expect(await frame.locator('[href*="bad.invalid"], [style*="bad.invalid"]').count()).toBe(0)
  expect(external).toEqual([])
})

test('PDF thumbnail refresh, hide and document close cancel cleanly',async({page})=>{
  core.put('many-pages.pdf',multipagePDF(24))
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  const panel=await workspace(page,'PDF 缩略图生命周期')
  await panel.getByRole('option',{name:/many-pages\.pdf/}).dblclick()
  await expect(panel.locator('.textLayer')).toContainText('Preview lifecycle page 1')
  const toggle=panel.getByRole('button',{name:'缩略图',exact:true})
  for(let attempt=0;attempt<3;attempt++){
    await toggle.click()
    const update=panel.getByRole('button',{name:'更新附近页面',exact:true})
    await expect(update).toBeVisible()
    await update.click({clickCount:3})
    await toggle.click()
    await expect(panel.locator('.pdf-body aside')).toHaveCount(0)
  }
  await toggle.click()
  await expect(panel.locator('.pdf-body aside img').first()).toBeVisible()
  await panel.getByRole('button',{name:'更新附近页面',exact:true}).click({clickCount:3})
  await panel.getByRole('button',{name:'关闭 many-pages.pdf',exact:true}).click()
  await panel.getByRole('option',{name:/hello\.txt/}).dblclick()
  await expect(panel.locator('.cm-content')).toContainText('hello')
  // Let cancelled worker/render promises settle after the PDF owner unmounts.
  await page.waitForTimeout(150)
  expect(errors).toEqual([])
})

test('a delayed PDF search cannot replace the result of a newer query',async({page})=>{
  await page.addInitScript(()=>{
    const state={armed:false,pending:0,release:()=>{}}
    Reflect.set(window,'pdfSearchDelivery',state)
    const NativeWorker=window.Worker
    let held:{worker:Worker;streamId:number;messages:unknown[]}|undefined
    // Hold the next PDF.js text-content stream, while leaving a later search's
    // independent stream and the real parser/rendering worker fully functional.
    window.Worker=class extends NativeWorker {
      constructor(url:string|URL,options?:WorkerOptions){
        super(url,options)
        this.addEventListener('message',event=>{
          if(held?.worker===this&&event.data?.streamId===held.streamId&&event.data?.stream){
            event.stopImmediatePropagation();held.messages.push(event.data);state.pending=held.messages.length
          }
        })
      }
      postMessage(message:unknown,options:Transferable[]|StructuredSerializeOptions=[]){
        const data=message as {action?:string;streamId?:number}
        if(state.armed&&data.action==='GetTextContent'&&data.streamId){
          state.armed=false;held={worker:this,streamId:data.streamId,messages:[]}
        }
        if(Array.isArray(options))super.postMessage(message,options);else super.postMessage(message,options)
      }
    }
    state.release=()=>{const stream=held;held=undefined;state.pending=0;for(const data of stream?.messages||[])stream!.worker.dispatchEvent(new MessageEvent('message',{data}))}
  })
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  const panel=await workspace(page,'PDF 搜索结果顺序')
  await panel.getByRole('option',{name:/document\.pdf/}).dblclick()
  await expect(panel.locator('.textLayer')).toContainText('Workspace PDF search')
  const query=panel.getByRole('textbox',{name:'查找 PDF 文字'})
  await page.evaluate(()=>{Reflect.get(window,'pdfSearchDelivery').armed=true})
  await query.fill('Workspace')
  await panel.getByRole('button',{name:'查找',exact:true}).click()
  await expect.poll(()=>page.evaluate(()=>Reflect.get(window,'pdfSearchDelivery').pending)).toBeGreaterThan(0)
  await query.fill('there-is-no-such-phrase')
  await panel.getByRole('button',{name:'查找',exact:true}).click()
  await expect(panel.getByText('没有匹配内容',{exact:true})).toBeVisible()
  await page.evaluate(async()=>{Reflect.get(window,'pdfSearchDelivery').release();await new Promise(resolve=>setTimeout(resolve,100))})
  await expect(panel.getByText('没有匹配内容',{exact:true})).toBeVisible()
  await expect(panel.getByRole('combobox',{name:'匹配页面'})).toHaveCount(0)
  expect(errors).toEqual([])
})

test('PDF page input shows the normalized page actually rendered',async({page})=>{
  core.put('page-selection.pdf',multipagePDF(3))
  const panel=await workspace(page,'PDF 页码归一化')
  await panel.getByRole('option',{name:/page-selection\.pdf/}).dblclick()
  await expect(panel.locator('.textLayer')).toContainText('Preview lifecycle page 1')
  const input=panel.getByRole('spinbutton',{name:'PDF 页码'})
  for(const [value,expected] of [['999','3'],['','1'],['2.8','2'],['-4','1']]){
    await input.fill(value!)
    await expect(input).toHaveValue(expected!)
    await expect(panel.locator('.textLayer')).toContainText(`Preview lifecycle page ${expected}`)
  }
})

test('failed media metadata reads do not start playback until a versioned retry succeeds',async({page})=>{
  let failHead=true
  const mediaReads:string[]=[]
  page.on('request',request=>{const url=new URL(request.url());if(request.method()==='GET'&&url.pathname.endsWith('/enterprise-workspace/content')&&url.searchParams.get('path')==='sound.wav')mediaReads.push(request.url())})
  await page.route('**/api/enterprise-workspace/content?**',async route=>{
    const request=route.request(),url=new URL(request.url())
    if(failHead&&request.method()==='HEAD'&&url.searchParams.get('path')==='sound.wav')await route.fulfill({status:503,headers:{'Content-Type':'application/json'}})
    else await route.continue()
  })
  const panel=await workspace(page,'媒体读取失败恢复')
  await panel.getByRole('option',{name:/sound\.wav/}).dblclick()
  await expect(panel.getByRole('alert')).toContainText('503')
  await expect(panel.locator('audio,video')).toHaveCount(0)
  expect(mediaReads).toEqual([])
  failHead=false
  await panel.getByRole('button',{name:'文档操作',exact:true}).click()
  await panel.getByRole('button',{name:'重新读取',exact:true}).click()
  await expect.poll(()=>panel.locator('audio').evaluate((audio:HTMLAudioElement)=>audio.readyState)).toBeGreaterThanOrEqual(1)
  expect(mediaReads.length).toBeGreaterThan(0)
  for(const request of mediaReads)expect(new URL(request).searchParams.get('version')).toMatch(/^"[^"\r\n]+"$/)
})

test('a very narrow image produces a bounded, usable thumbnail without upscaling',async({page})=>{
  core.put('needle.png',narrowPNG(16000))
  await page.addInitScript(()=>{
    const calls:{width:number;height:number}[]=[];Reflect.set(window,'workspaceBitmapSizes',calls)
    const original=window.createImageBitmap.bind(window)
    window.createImageBitmap=(async(image:ImageBitmapSource,options?:ImageBitmapOptions)=>{
      const width=options?.resizeWidth||0,height=options?.resizeHeight||0;calls.push({width,height})
      // Fail safely on the regression: width-only resizing of the fixture would
      // otherwise request more than 260 million pixels in the browser process.
      if(!width||!height||width>128||height>128)throw new Error('Unbounded thumbnail resize')
      return original(image,options)
    }) as typeof window.createImageBitmap
  })
  const panel=await workspace(page,'细长图片缩略图')
  await panel.getByRole('button',{name:'图标',exact:true}).click()
  const thumbnail=panel.getByRole('option',{name:/needle\.png/}).locator('img.ew-thumb')
  await expect(thumbnail).toBeVisible()
  await expect.poll(()=>thumbnail.evaluate((image:HTMLImageElement)=>({width:image.naturalWidth,height:image.naturalHeight}))).toEqual({width:1,height:128})
  const calls=await page.evaluate(()=>Reflect.get(window,'workspaceBitmapSizes') as {width:number;height:number}[])
  expect(calls).toContainEqual({width:1,height:128})
  expect(calls.every(({width,height})=>width>=1&&width<=128&&height>=1&&height<=128)).toBe(true)
})

test('narrow file grids retain names inside non-overlapping tiles and reveal active tabs',async({page},testInfo)=>{
  await page.setViewportSize({width:390,height:844})
  for(let index=0;index<12;index++)core.put(`mobile-grid-${index}.txt`,'mobile grid content')
  const panel=await workspace(page,'窄屏图标与标签')
  const fileTab=panel.getByRole('tab',{name:'返回文件列表',exact:true})
  for(const name of ['hello.txt','mobile-grid-0.txt','mobile-grid-1.txt','mobile-grid-2.txt']){
    await panel.getByRole('option',{name:new RegExp(name.replace(/\./g,'\\.'))}).dblclick()
    const active=panel.getByRole('tab',{name,exact:true})
    await expect(active).toHaveAttribute('aria-selected','true')
    await expect.poll(()=>active.evaluate(element=>{const tab=element.getBoundingClientRect(),list=element.closest('.ew-tabs')!.getBoundingClientRect();return tab.left>=list.left-1&&tab.right<=list.right+1})).toBe(true)
    await fileTab.click()
  }
  expect(await fileTab.evaluate(element=>element.getBoundingClientRect().width)).toBeGreaterThanOrEqual(28)
  await panel.getByRole('button',{name:'图标',exact:true}).click()
  await panel.getByRole('option',{name:/sample\.png/}).scrollIntoViewIfNeeded()
  await expect(panel.getByRole('option',{name:/sample\.png/}).locator('.ew-thumb')).toBeVisible()
  const tiles=await panel.locator('.ew-grid .ew-file').evaluateAll(elements=>elements.map(element=>{
    const box=element.getBoundingClientRect(),name=element.querySelector('.ew-file-name')!.getBoundingClientRect()
    return {left:box.left,right:box.right,top:box.top,bottom:box.bottom,nameHeight:name.height,nameTop:name.top,nameBottom:name.bottom}
  }))
  expect(tiles.length).toBeGreaterThanOrEqual(12)
  for(const [index,tile] of tiles.entries()){
    expect(tile.nameHeight).toBeGreaterThan(0)
    expect(tile.nameTop).toBeGreaterThanOrEqual(tile.top)
    expect(tile.nameBottom).toBeLessThanOrEqual(tile.bottom)
    for(const other of tiles.slice(index+1)){
      const horizontal=Math.min(tile.right,other.right)-Math.max(tile.left,other.left)
      const vertical=Math.min(tile.bottom,other.bottom)-Math.max(tile.top,other.top)
      expect(horizontal>1&&vertical>1).toBe(false)
    }
  }
  await page.screenshot({path:testInfo.outputPath('workspace-mobile-grid.png')})
})
