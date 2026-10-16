import { test, expect, type Page } from '@playwright/test'
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, relative } from 'node:path'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>,directory:string
test.beforeAll(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform});directory=await mkdtemp(join(tmpdir(),'tuck-workspace-drop-'))})
test.afterAll(async()=>{await app?.close();await core?.close();if(directory){expect(relative(tmpdir(),directory)).toMatch(/^tuck-workspace-drop-[^\\/]+$/);await rm(directory,{recursive:true,force:true})}})
async function workspace(page:Page){
  const task=await app.manager.create({title:'系统文件拖放验收'})
  await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1`)
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  return panel
}
async function dropFiles(page:Page,files:string[],target='.ew-body',text?:string){
  const body=await page.locator(target).boundingBox()
  expect(body).not.toBeNull()
  const session=await page.context().newCDPSession(page)
  const drag={x:body!.x+Math.min(50,body!.width/2),y:body!.y+Math.min(10,body!.height/2),data:{items:text===undefined?[]:[{mimeType:'text/plain',data:text}],files,dragOperationsMask:1}}
  try{for(const type of ['dragEnter','dragOver','drop'] as const)await session.send('Input.dispatchDragEvent',{type,...drag})}finally{await session.detach()}
}

test('native file and folder drops preserve bytes, nested paths and empty directories',async({page},info)=>{
  const panel=await workspace(page)
  const folder=join(directory,'拖放文件夹'),nested=join(folder,'子目录'),empty=join(folder,'空目录'),plain=join(directory,'单独.txt')
  await mkdir(nested,{recursive:true});await mkdir(empty);await writeFile(join(nested,'中文.txt'),'nested bytes\r\n中文');await writeFile(plain,'standalone bytes')
  await dropFiles(page,[folder,plain])
  await expect.poll(()=>core.files.get('拖放文件夹/子目录/中文.txt')?.toString()).toBe('nested bytes\r\n中文')
  await expect.poll(()=>core.files.get('单独.txt')?.toString()).toBe('standalone bytes')
  expect(core.directories.has('拖放文件夹/空目录')).toBe(true)
  await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText(['已完成','已完成'])
  await expect(page.locator('.attachment-drop')).toHaveCount(0)
  await expect(page.getByRole('group',{name:'待发送附件'})).toHaveCount(0)
  await page.screenshot({path:info.outputPath('workspace-native-drop-completed.png')})
})

test('native file drags move between chat and workspace without duplicate attachments or stale overlays',async({page})=>{
  const panel=await workspace(page),chatFile=join(directory,'chat-attachment.txt'),workspaceFile=join(directory,'cross-panel-drop.txt')
  await writeFile(chatFile,'chat attachment bytes');await writeFile(workspaceFile,'workspace bytes')
  const input=page.getByTestId('composer-input')
  await input.fill('preserved chat draft')
  await dropFiles(page,[chatFile],'.workbench-input-box')
  await expect(page.getByRole('group',{name:'待发送附件'})).toContainText('chat-attachment.txt')
  expect(core.files.has('chat-attachment.txt')).toBe(false)
  await page.getByRole('button',{name:'移除 chat-attachment.txt',exact:true}).click()
  const chatBox=(await page.locator('.workbench-input-box').boundingBox())!,workspaceBox=(await panel.locator('.ew-body').boundingBox())!
  const session=await page.context().newCDPSession(page),data={items:[],files:[workspaceFile],dragOperationsMask:1}
  const chat={x:chatBox.x+20,y:chatBox.y+10,data},workspaceDrop={x:workspaceBox.x+20,y:workspaceBox.y+10,data}
  try{
    await session.send('Input.dispatchDragEvent',{type:'dragEnter',...chat})
    await session.send('Input.dispatchDragEvent',{type:'dragOver',...chat})
    await expect(page.locator('.attachment-drop')).toBeVisible()
    await session.send('Input.dispatchDragEvent',{type:'dragOver',...workspaceDrop})
    await expect(panel.locator('.ew-drop')).toBeVisible()
    await expect(page.locator('.attachment-drop')).toHaveCount(0)
    await session.send('Input.dispatchDragEvent',{type:'drop',...workspaceDrop})
    await expect.poll(()=>core.files.get('cross-panel-drop.txt')?.toString()).toBe('workspace bytes')
    await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText('已完成')
    await expect(page.getByRole('group',{name:'待发送附件'})).toHaveCount(0)
    await expect(page.locator('.attachment-drop')).toHaveCount(0)
    await expect(input).toHaveValue('preserved chat draft')
    const backData={items:[],files:[chatFile],dragOperationsMask:1},backWorkspace={...workspaceDrop,data:backData},backChat={...chat,data:backData}
    await session.send('Input.dispatchDragEvent',{type:'dragEnter',...backWorkspace})
    await session.send('Input.dispatchDragEvent',{type:'dragOver',...backWorkspace})
    await expect(panel.locator('.ew-drop')).toBeVisible()
    await session.send('Input.dispatchDragEvent',{type:'dragOver',...backChat})
    await expect(panel.locator('.ew-drop')).toHaveCount(0)
    await expect(page.locator('.attachment-drop')).toBeVisible()
    await session.send('Input.dispatchDragEvent',{type:'drop',...backChat})
    await expect(page.getByRole('group',{name:'待发送附件'})).toContainText('chat-attachment.txt')
    await expect(page.locator('.attachment-drop')).toHaveCount(0)
    await expect(input).toHaveValue('preserved chat draft')
    expect(core.files.has('chat-attachment.txt')).toBe(false)
  }finally{await session.detach()}
})

test('returning after a cancelled native drag clears its hover and refreshes normally',async({page})=>{
  const panel=await workspace(page),file=join(directory,'cancel-hover.txt')
  await writeFile(file,'not dropped')
  const box=(await panel.getByRole('option',{name:/hello\.txt/}).boundingBox())!
  const session=await page.context().newCDPSession(page),drag={x:box.x+10,y:box.y+10,data:{items:[],files:[file],dragOperationsMask:1}}
  try{
    await session.send('Input.dispatchDragEvent',{type:'dragEnter',...drag})
    await session.send('Input.dispatchDragEvent',{type:'dragOver',...drag})
    await expect(panel.locator('.ew-drop')).toContainText('释放以上传到')
    await session.send('Input.dispatchDragEvent',{type:'dragCancel',...drag})
    const refresh=page.waitForResponse(response=>response.url()===app.url+'/api/enterprise-workspace')
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
    await expect(panel.locator('.ew-drop')).toHaveCount(0)
    expect((await refresh).ok()).toBe(true)
    expect(core.files.has('cancel-hover.txt')).toBe(false)
  }finally{await session.detach()}
})

test('a failed focus refresh stays visible while a later file drop uses the authorized workspace',async({page})=>{
  const panel=await workspace(page),file=join(directory,'failed-refresh-drop.txt')
  await writeFile(file,'upload after background failure')
  let release=()=>{}
  const gate=new Promise<void>(resolve=>{release=resolve})
  await page.route('**/api/enterprise-workspace',async route=>{await gate;await route.fulfill({status:502,contentType:'application/json',body:JSON.stringify({error:'无法连接企业服务，请检查网络后重试'})})})
  try{
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
    await expect(panel.getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    release()
    await expect(panel.getByRole('status').filter({hasText:'自动刷新失败'})).toContainText('无法连接企业服务')
    await dropFiles(page,[file])
    await expect.poll(()=>core.files.get('failed-refresh-drop.txt')?.toString()).toBe('upload after background failure')
  }finally{release()}
})

test('closing the panel during automatic refresh does not upload an undropped file',async({page})=>{
  const panel=await workspace(page),file=join(directory,'closed-panel-drop.txt')
  await writeFile(file,'must not upload after close')
  let release=()=>{}
  const gate=new Promise<void>(resolve=>{release=resolve})
  await page.route('**/api/enterprise-workspace',async route=>{await gate;await route.continue()})
  try{
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
    await expect(panel.getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    await expect(panel.getByRole('status').filter({hasText:'正在自动刷新'})).toBeVisible()
    await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click();release()
    await page.getByRole('button',{name:'工作区',exact:true}).click()
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await expect(panel.getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    expect(core.files.has('closed-panel-drop.txt')).toBe(false)
  }finally{release()}
})

test('file drops upload without changing an open editor, while text drops remain editable',async({page})=>{
  const panel=await workspace(page),file=join(directory,'editor-drop.txt')
  await writeFile(file,'uploaded file bytes')
  await panel.getByRole('option',{name:/hello\.txt/}).dblclick()
  const editor=panel.locator('.cm-content')
  await expect(editor).toContainText('hello')
  const original=await editor.innerText()
  await dropFiles(page,[file],'.cm-content')
  await expect.poll(()=>core.files.get('editor-drop.txt')?.toString()).toBe('uploaded file bytes')
  await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText('已完成')
  await expect.poll(()=>editor.innerText()).toBe(original)
  await dropFiles(page,[],'.cm-content','text dropped into editor')
  await expect(editor).toContainText('text dropped into editor')
  await expect(panel.locator('.ew-drop')).toHaveCount(0)
  expect(core.files.get('hello.txt')?.toString()).toBe('hello\r\nworld\r\n')
})

test('returning with dragged files cancels focus refresh and uploads without waiting',async({page})=>{
  const panel=await workspace(page),file=join(directory,'focus-dropped.txt')
  await writeFile(file,'drop after focus refresh')
  let release=()=>{},requests=0
  const gate=new Promise<void>(resolve=>{release=resolve})
  await page.route('**/api/enterprise-workspace',async route=>{requests++;await gate;await route.continue()})
  try{
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
    await expect.poll(()=>requests).toBe(1)
    await expect(panel.getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    await dropFiles(page,[file])
    await expect.poll(()=>core.files.get('focus-dropped.txt')?.toString()).toBe('drop after focus refresh')
    release()
    await expect(panel.getByRole('option',{name:/focus-dropped\.txt/})).toBeVisible()
    await expect(panel.getByText('正在处理其他文件操作，请稍后重新拖入文件', {exact:true})).toHaveCount(0)
  }finally{release()}
})
