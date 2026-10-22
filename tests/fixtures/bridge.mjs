// Browser-test driver only. All product behavior runs in the Go test executable.
import {spawn,spawnSync} from 'node:child_process';
import {createInterface} from 'node:readline';
import {readFile,writeFile,mkdtemp,mkdir} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {randomBytes} from 'node:crypto';
import {createServer} from 'node:net';

const root=fileURLToPath(new URL('../../',import.meta.url));
const executable=resolve(root,'.local',process.platform==='win32'?'tuck-fixture.exe':'tuck-fixture');
const official=process.env.TUCK_TEST_ENGINE||resolve(root,'tests/runtime',process.platform==='win32'?'docker-agent.exe':'docker-agent');
const pause=ms=>new Promise(done=>setTimeout(done,ms));
export function defaultConfig(){const result=spawnSync(executable,['-test.run=^TestFixtureDefaultConfig$'],{cwd:root,env:{...process.env,TUCK_FIXTURE_DEFAULT_CONFIG:'1'},encoding:'utf8',windowsHide:true});if(result.error)throw result.error;if(result.status!==0)throw new Error(result.stderr||result.stdout||'Go default configuration failed');return JSON.parse(result.stdout)}
export async function until(fn,timeout=20000){const end=Date.now()+timeout;while(Date.now()<end){const result=await fn();if(result)return result;await pause(25)}throw new Error('Condition timed out')}
export async function freePort(){const server=createServer();await new Promise((done,fail)=>server.once('error',fail).listen(0,'127.0.0.1',done));const port=server.address().port;await new Promise(done=>server.close(done));return port}
export async function launch(config,options={}){
 const directory=options.directory||await mkdtemp(resolve(tmpdir(),'tuck-browser-'));
 config=structuredClone(config);config.server={listenHost:'127.0.0.1',port:0,frontendDir:resolve(root,'frontend/dist')};config.storage={dataDir:resolve(directory,'data'),workspaceRoot:resolve(directory,'workspaces')};config.engine={mode:'managed',version:'v1.152.0',binary:official,url:`http://127.0.0.1:${await freePort()}`,desktopProxy:false};config.security||={};config.security.auth||={enabled:false,username:'admin',passwordHash:''};config.security.credentialKey=randomBytes(32).toString('base64');
 const configPath=resolve(directory,'tuck.config.json');await mkdir(directory,{recursive:true});await writeFile(configPath,JSON.stringify(config));
 const token=randomBytes(32).toString('base64url');const child=spawn(executable,['-test.run=^TestFixtureProcess$','-test.v'],{cwd:root,env:{...process.env,TUCK_FIXTURE_CONFIG:configPath,TUCK_FIXTURE_TOKEN:token,TUCK_TEST_ENGINE:official},stdio:['pipe','pipe','pipe'],windowsHide:true});
 let diagnostics='';child.stderr.on('data',b=>{diagnostics=(diagnostics+b).slice(-16000)});
 const url=await new Promise((done,fail)=>{const lines=createInterface({input:child.stdout});const timer=setTimeout(()=>{child.kill();fail(new Error('Go fixture startup timed out: '+diagnostics))},45000);child.once('error',error=>{clearTimeout(timer);fail(error)});child.once('exit',code=>{clearTimeout(timer);fail(new Error(`Go fixture exited (${code}): ${diagnostics}`))});lines.on('line',line=>{if(line.startsWith('TUCK_FIXTURE_READY ')){clearTimeout(timer);done(line.slice(19))}else diagnostics+='\n'+line})});
 let batch=null;const cache=new Map(),rawTasks=new Map();
 const request=(data)=>{if(batch&&['saveMessage','saveTask'].includes(data.op)){batch.push(data);return null}const result=spawnSync(executable,['-test.run=^TestFixtureRequest$'],{cwd:root,env:{...process.env,TUCK_FIXTURE_REQUEST_URL:url,TUCK_FIXTURE_TOKEN:token,TUCK_FIXTURE_CONFIG:''},input:JSON.stringify(data),encoding:'utf8',windowsHide:true,maxBuffer:128*1024*1024});if(result.error)throw result.error;if(result.status!==0)throw new Error(result.stdout||result.stderr||'Go fixture request failed');return JSON.parse(result.stdout)};
 const asyncRequest=async data=>{const response=await fetch(url+'/__fixture',{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:JSON.stringify(data)});const result=await response.json();if(!response.ok)throw Object.assign(new Error(result.error),result);return result};
 let suppress=false;
 const tracked=(value,onChange)=>{if(!value||typeof value!=='object')return value;return new Proxy(value,{get(object,key){const result=Reflect.get(object,key);return result&&typeof result==='object'?tracked(result,onChange):result},set(object,key,value){Reflect.set(object,key,value);if(!suppress)onChange();return true},deleteProperty(object,key){Reflect.deleteProperty(object,key);if(!suppress)onChange();return true}})};
 const remember=task=>{if(!task)return task;const id=task.id;if(!rawTasks.has(id)){rawTasks.set(id,task);cache.set(id,tracked(task,()=>request({op:'saveTask',task:rawTasks.get(id)})))}else{const raw=rawTasks.get(id);for(const key of Object.keys(raw))if(!(key in task))delete raw[key];Object.assign(raw,task)}return cache.get(id)};
 const allTasks=()=>{for(const task of request({op:'tasks'}))remember(task);return cache};
 const tasks={get:id=>remember(request({op:'get',id})),set(id,task){remember(task);return tasks},values:()=>allTasks().values(),keys:()=>allTasks().keys(),get size(){return allTasks().size},[Symbol.iterator]:()=>allTasks()[Symbol.iterator]()};
 const defaults=request({op:'defaults'});const agentDefaults=tracked(defaults,()=>request({op:'setDefaults',config:defaults}));
 const manager={agentDefaults,tasks,runs:{has:id=>{const running=request({op:'running',id}).running;remember(request({op:'get',id}));return running}},interactions:{get:id=>{const interactions=request({op:'interactions',id});remember(request({op:'get',id}));return interactions},has:id=>request({op:'interactions',id}).length>0},
  async create(input={}){return remember(await asyncRequest({op:'create',input}))},get:id=>remember(request({op:'get',id})),baseTask:input=>request({op:'baseTask',input}),
  createChild(parent,nativeId,agent,mode='foreground'){return remember(request({op:'child',id:parent.id,nativeId,agent,mode}))},
  append(task,message){const result=request({op:'append',id:task.id,task:rawTasks.get(task.id)||task,message});remember(result.task);return result.message},
  appendText(task,message,block,field,delta){const result=request({op:'appendText',id:task.id,index:message.index,block,field,delta});remember(result.task);Object.assign(message,result.message)},
  replace(task,message){request({op:'replace',id:task.id,message})},
  publish(task,event){return request({op:'publish',id:task.id,task:rawTasks.get(task.id)||task,event})},
  settle(task,reason,failure){request({op:'settle',id:task.id,task:rawTasks.get(task.id)||task,reason,failure});remember(request({op:'get',id:task.id}))},
  submit(id,prompt,attachmentIds,expectedTurnCount){return request({op:'submit',id,prompt,attachmentIds,expectedTurnCount})},
  selectModel(id,modelId,effort='default'){return remember(request({op:'selectModel',id,modelId,effort}))},
  async steer(id,prompt,attachmentIds,source){return asyncRequest({op:'steer',id,prompt,attachmentIds,source})},
  async forkOptions(id){return asyncRequest({op:'forkOptions',id})},async fork(id,index){const value=await asyncRequest({op:'fork',id,index});return {task:remember(value.task||value)}},
  normalizer:{async handle(task,event){await asyncRequest({op:'event',id:task.id,event});remember(request({op:'get',id:task.id}))}},
 };
 const store={tasks:()=>[...allTasks().values()],saveTask(task){return request({op:'saveTask',task:rawTasks.get(task.id)||task})},saveMessage(id,message){return request({op:'saveMessage',id,message})},message:(id,index)=>request({op:'message',id,index}),messages:(id,start=0,count=2048)=>request({op:'messages',id,start,count}),orchestration:()=>request({op:'orchestration'}),transaction(fn){if(batch)return fn();batch=[];try{fn();const operations=batch;batch=null;return request({op:'batch',operations})}finally{batch=null}}};
 const streamAbort=new AbortController();const stream=fetch(url+'/__fixture/events?since=0',{signal:streamAbort.signal,headers:{Authorization:'Bearer '+token}}).then(async response=>{if(!response.ok)return;const reader=response.body.getReader();const decoder=new TextDecoder();let pending='';for(;;){const {value,done}=await reader.read();if(done)break;pending+=decoder.decode(value,{stream:true});let boundary;while((boundary=pending.indexOf('\n\n'))>=0){const packet=pending.slice(0,boundary);pending=pending.slice(boundary+2);const data=packet.split('\n').filter(l=>l.startsWith('data:')).map(l=>l.slice(5).trimStart()).join('\n');if(!data)continue;const event=JSON.parse(data);if(event.task)remember(request({op:'get',id:event.task.id}))}}}).catch(()=>{});
 const platform={async enroll(material){await asyncRequest({op:'platformEnroll',material})},async sync(){return asyncRequest({op:'platformSync'})},async logout(){return asyncRequest({op:'platformLogout'})},get binding(){return request({op:'binding'})}};
 const models={agentDefaults,platform,catalog:{delete(modelId){request({op:'removeModel',modelId})}},rebuild(){request({op:'rebuild'})},summary:()=>request({op:'connection'})};
 const app={url,directory,configPath,workspaceRoot:config.storage.workspaceRoot,manager,store,models,request,asyncRequest,server:{closeAllConnections(){streamAbort.abort();request({op:'closeConnections'})}},docker:{request:async path=>asyncRequest({op:'native',path})},async deliver(task,summary,paths=[]){const result=await asyncRequest({op:'delivery',id:task.id,input:{summary,files:paths.map(path=>({path}))}});if(result.status!=='completed')throw new Error(JSON.stringify(result));remember(request({op:'get',id:task.id}))},async close(){streamAbort.abort();await stream;child.stdin.end();await new Promise(done=>{if(child.exitCode!==null)return done();const timer=setTimeout(()=>{child.kill();done()},15000);child.once('exit',()=>{clearTimeout(timer);done()})})}};
 if(options.enterprisePlatform){const binding=structuredClone(options.enterprisePlatform.binding);binding.accessToken=await options.enterprisePlatform.access();binding.accessTokenExpiresAt=new Date(Date.now()+86400000).toISOString();binding.snapshot||={schemaVersion:5,models:[],providers:[],mcp:[],policy:{},managedGeneration:1};await asyncRequest({op:'setBinding',binding})}
 return app;
}
