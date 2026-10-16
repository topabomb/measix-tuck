import { createServer } from 'node:http';
import { platformFixture } from './platform-fixture.mjs';

export async function settingsFixture() {
  const f=await platformFixture({native:true});
  const requests=[], controls={slow:false};
  const upstream=createServer(async(req,res)=>{
    let raw='';for await(const b of req)raw+=b;const body=raw?JSON.parse(raw):null;
    requests.push({path:req.url,body,authorization:req.headers.authorization});
    const json=value=>{res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
    if(new URL(req.url,'http://fixture').pathname==='/mcp'){
      if(controls.mcpOffline){req.socket.destroy();return;}
      if(controls.mcpToken&&req.headers.authorization!=='Bearer '+controls.mcpToken){res.writeHead(401);res.end();return;}
      if(req.method==='GET'){
        if(!controls.mcpNotifications){res.writeHead(405);res.end();return;}
        res.writeHead(200,{'Content-Type':'text/event-stream'});res.write(': connected\n\n');
        controls.mcpNotify=()=>res.write(`data: ${JSON.stringify({jsonrpc:'2.0',method:'notifications/tools/list_changed'})}\n\n`);
        controls.mcpCloseStream=()=>res.destroy();
        return;
      }
      if(req.method==='DELETE'){res.writeHead(200);res.end();return;}
      if(controls.mcpStrictSession&&body?.method!=='initialize'&&req.headers['mcp-session-id']!==(controls.mcpSession||'settings-fixture-session')){res.writeHead(404);res.end();return;}
      if(body?.id===undefined){res.writeHead(202);res.end();return;}
      if(body.method==='tools/call')controls.mcpExecuted=(controls.mcpExecuted||0)+1;
      if(controls.mcpFailure?.method===body.method){const failure=controls.mcpFailure;if(!failure.persistent)controls.mcpFailure=null;if(failure.drop){req.socket.destroy();return;}res.writeHead(failure.status);res.end();return;}
      if(body.method==='initialize'&&controls.mcpHoldInitialize){controls.mcpInitializeEntered=true;await new Promise(done=>{controls.mcpReleaseInitialize=done});if(res.destroyed)return;}
      if(body.method==='tools/call'&&controls.mcpHoldCall){controls.mcpCallEntered=true;await new Promise(done=>{controls.mcpReleaseCall=done;(controls.mcpCallReleases||=[]).push(done)});if(res.destroyed)return;}
      let result;
      if(body.method==='initialize'){res.setHeader('Mcp-Session-Id',controls.mcpSession||'settings-fixture-session');result={protocolVersion:controls.mcpVersion||'2025-03-26',capabilities:{tools:{listChanged:true}},serverInfo:{name:'settings-fixture',version:'1'}};}
      else if(body.method==='tools/list')result={tools:controls.mcpTools||[{name:'echo',description:'Return the settings verification result.',inputSchema:{type:'object',properties:{}},annotations:{readOnlyHint:true}}]};
      else if(body.method==='tools/call')result={content:[{type:'text',text:'MCP settings applied'}]};
      return json({jsonrpc:'2.0',id:body.id,result});
    }
    if(body?.stream===false){
      if(controls.modelStatus){res.writeHead(controls.modelStatus);res.end('synthetic error');return;}
      return json(req.url.endsWith('/responses')?{output:[{content:[{type:'output_text',text:'OK'}]}]}:{choices:[{message:{role:'assistant',content:'OK'}}]});
    }
    if(controls.slow){await new Promise(done=>{const timer=setTimeout(done,30000);res.once('close',()=>{clearTimeout(timer);done();});});if(res.destroyed)return;}
    const seenTool=body.messages?.some(m=>m.role==='tool');
    const next=controls.toolCalls?.shift();
    const delta=next ? {tool_calls:[{index:0,id:'proof_'+requests.length,type:'function',function:{name:next.name,arguments:JSON.stringify(next.arguments)}}]} : !seenTool && body.tools?.some(t=>t.function.name==='proof_echo') ? {tool_calls:[{index:0,id:'proof',type:'function',function:{name:'proof_echo',arguments:'{}'}}]} : {content:'Settings verification completed.'};
    res.writeHead(200,{'Content-Type':'text/event-stream'});res.end(`data: ${JSON.stringify({id:'settings',object:'chat.completion.chunk',choices:[{index:0,delta,finish_reason:delta.tool_calls?'tool_calls':'stop'}]})}\n\ndata: [DONE]\n\n`);
  });
  await new Promise(done=>upstream.listen(0,'127.0.0.1',done));const origin=`http://127.0.0.1:${upstream.address().port}`;
  const read=async()=> (await fetch(f.url+'/api/settings/runtime')).json();
  const save=async(data,interrupt=false)=>fetch(f.url+'/api/settings/runtime',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({...data,interrupt})});
  const initial=await read();initial.config.providers=[{id:'proof',displayName:'验证提供商',enabled:true,protocol:'OPENAI_CHAT_COMPLETIONS',baseUrl:origin+'/v1',apiKey:'synthetic-provider-key',models:[{modelId:'proof-model',upstreamModelKey:'upstream-proof',displayName:'验证模型',enabled:true,contextWindow:64000,maxOutputTokens:2048,temperature:0.3,topP:0.8,inputModalities:['TEXT'],capabilities:['TOOL']}]}];initial.config.defaultModelId='proof-model';
  const initialSave=await save(initial);if(!initialSave.ok)throw new Error(await initialSave.text());
  return {...f,platformRequests:f.requests,platformControls:f.controls,origin,requests,controls,read,save,async close(){await f.close();upstream.closeAllConnections();await new Promise(done=>upstream.close(done));}};
}
