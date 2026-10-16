// Synthetic enterprise server for browser tests; Tuck itself runs in Go.
import {createServer} from 'node:http';
import {readFile,mkdtemp} from 'node:fs/promises';
import {resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {createRequire} from 'node:module';
import {createHash} from 'node:crypto';
import {launch} from './bridge.mjs';
const require=createRequire(new URL('../../frontend/package.json',import.meta.url));
const canonicalize=require('canonicalize').default;
const toolHash=tool=>'sha256:'+createHash('sha256').update(canonicalize(tool)).digest('hex');
const vector=async name=>JSON.parse(await readFile(new URL(`../contracts/${name}.json`,import.meta.url),'utf8'));
const listen=server=>new Promise((done,fail)=>server.once('error',fail).listen(0,'127.0.0.1',done));
export async function platformFixture({native=false,version=5}={}) {
  const directory = await mkdtemp(resolve(tmpdir(), 'tuck-platform-'));
  const discovery = await vector('discovery');
  const snapshot = await vector(`snapshot-v${version}`);
  const bootstrap = await vector('bootstrap');
  const credentials = await vector('enrollment-response');
  const now = Date.now();
  credentials.accessTokenExpiresAt = new Date(now + 300000).toISOString(); credentials.refreshExpiresAt = credentials.sessionIdleExpiresAt = new Date(now + 86400000).toISOString();
  const initialModel = snapshot.models[0]; initialModel.upstreamModelKey = 'public-chat-alias';
  const responses = (await vector('snapshot-v4-responses')).models[0];
  snapshot.providers.push({ ...snapshot.providers[0], providerId: 'prv_cccccccc-cccc-4ccc-8ccc-cccccccccccc', displayName: 'Responses provider', clientProtocol: 'OPENAI_RESPONSES' });
  snapshot.models.push({ ...responses, modelId: 'mdl_cccccccc-cccc-4ccc-8ccc-cccccccccccc', providerId: snapshot.providers[1].providerId, displayName: 'Responses model', upstreamModelKey: 'public-responses-alias' });
  const controls = { state: { runtimeStatus: 'READY', activeManagedGeneration: snapshot.managedGeneration, managedStateRevision: 1, syncRequired: false, runtimeBlocked: false }, etag: `"${snapshot.snapshotHash}"`, badResponse: false, refreshDrop: false, refreshCode: null, token: credentials.accessToken, enrollmentUsed: false };
  controls.mcpSession = 'upstream-fixture-session';
  controls.mcpTools = [{name:'search_records',description:'Search fixture records',inputSchema:{type:'object',properties:{query:{type:'string'}}},annotations:{readOnlyHint:true},extension:{mustBeHashed:true}},{name:'not_granted',inputSchema:{type:'object'}}];
  if (version === 5) snapshot.mcp[0].allowedTools[0].contractHash = toolHash(controls.mcpTools[0]);
  const requests = [];
  const platform = createServer(async (request, response) => {
    if(request.url.includes('/dav/') && controls.dav)return controls.dav(request,response);
    let text = ''; for await (const bytes of request) text += bytes;
    const body = text ? JSON.parse(text) : null;
    requests.push({ path: request.url, method: request.method, headers: request.headers, body });
    const json = (status, value, headers = {}) => { response.writeHead(status, { 'Content-Type': 'application/json', ...headers }); response.end(JSON.stringify(value)); };
    if (request.url === '/.well-known/measix') return json(200, discovery);
    if (request.url.endsWith('/enrollments/exchange')) { if (controls.enrollmentUsed) return json(409, { code: 'enrollment_already_used' }); controls.enrollmentUsed = true; return json(201, credentials); }
    if (request.url.endsWith('/sessions/refresh')) {
      if (controls.refreshCode) return json(409, { code: controls.refreshCode });
      if (controls.refreshDrop) { controls.refreshDrop = false; request.socket.destroy(); return; }
      controls.token = 'synthetic-rotated-access';
      return json(200, { ...credentials, accessToken: controls.token, refreshToken: 'synthetic-rotated-refresh', accessTokenExpiresAt: new Date(Date.now() + 300000).toISOString() });
    }
    if (request.url.endsWith('/sessions/logout')) { response.writeHead(204); response.end(); return; }
    if (request.headers.authorization !== `Bearer ${controls.token}`) return json(401, { code: 'invalid_session' });
    if(request.url.endsWith('/workspace') && controls.workspace)return json(200,controls.workspace);
    if (request.url.endsWith('/bootstrap')) { bootstrap.managedState = controls.state; if (controls.employeeName !== undefined) bootstrap.user.displayName = controls.employeeName; return json(200, bootstrap); }
    if (request.url.endsWith('/managed/state')) return json(200, controls.state);
    if (request.url.includes('/managed/snapshots/')) { if (request.headers['if-none-match'] === controls.etag) { response.writeHead(304, { ETag: controls.etag }); response.end(); return; } return json(200, controls.badResponse ? { ...snapshot, policy: { ...snapshot.policy, allowLocalProviders: 'false' } } : snapshot, { ETag: controls.etag }); }
    if (request.url.endsWith('/managed/applied')) { response.writeHead(204); response.end(); return; }
    if (request.url.startsWith('/runtime/v1/resources/')) {
      if (controls.runtimeRejection) return json(controls.runtimeRejection.status, controls.runtimeRejection.body, controls.runtimeRejection.headers);
      if (request.url.includes('/mcp_')) {
        if (controls.mcpStrictSession && body?.method !== 'initialize' && request.headers['mcp-session-id'] !== controls.mcpSession) { response.writeHead(controls.mcpMissingSessionStatus || 401); response.end(); return; }
        if (controls.mcpFailure && controls.mcpFailure.method === body?.method) {
          const failure = controls.mcpFailure; controls.mcpFailure = null;
          if (failure.drop) { request.socket.destroy(); return; }
          response.writeHead(failure.status); response.end(); return;
        }
        if (request.method === 'GET') {
          if (!controls.mcpNotifications) { response.writeHead(405); response.end(); return; }
          response.writeHead(200,{'Content-Type':'text/event-stream'});response.write(': connected\n\n');
          controls.mcpNotify=()=>response.write('id: notice-1\nevent: message\nretry: 1500\ndata: '+JSON.stringify({jsonrpc:'2.0',method:'notifications/tools/list_changed'})+'\n\n');
          controls.mcpCloseStream=()=>response.end();return;
        }
        if (request.method === 'DELETE' || body?.method?.startsWith('notifications/')) { response.writeHead(202); response.end(); return; }
        let result;
        if (body?.method === 'initialize') result = {protocolVersion:controls.mcpVersion || '2025-11-25',capabilities:{tools:{}},serverInfo:{name:'fixture-mcp',version:'1.0.0'}};
        else if (body?.method === 'tools/list') result = {tools:controls.mcpTools};
        else if (body?.method === 'tools/call') {
          if(controls.mcpHoldCall) { await new Promise(done=>{(controls.mcpCallReleases||=[]).push(done);response.once('close',()=>{controls.mcpCallDisconnected=true;done();});});if(response.destroyed)return; }
          if(controls.mcpCallDelay) await new Promise(done=>{const timer=setTimeout(done,controls.mcpCallDelay);response.once('close',()=>{clearTimeout(timer);done();});});
          if(response.destroyed)return;
          if (controls.mcpCallError) return json(200,{jsonrpc:'2.0',id:body.id,error:controls.mcpCallError});
          result = {content:[{type:'text',text:`MCP fixture result: ${body.params.name}`}],isError:false};
        }
        else result = {};
        if (controls.mcpRaw && body.method === 'tools/list') { response.writeHead(200,{'Content-Type':controls.mcpSse?'text/event-stream':'application/json'}); response.end(controls.mcpSse ? `data: ${controls.mcpRaw}\n\n` : controls.mcpRaw); return; }
        if (controls.mcpSse && body.method === 'tools/list') { response.writeHead(200,{'Content-Type':'text/event-stream'}); response.end(`data: ${JSON.stringify({jsonrpc:'2.0',id:body.id,result})}\n\n`); return; }
        return json(200,{jsonrpc:'2.0',id:body.id,result},body.method === 'initialize' ? {'Mcp-Session-Id':controls.mcpSession} : {});
      }
      if (request.url.endsWith('/responses')) {
        const hasResult = body.input.some(item => item.type === 'function_call_output');
        const tools = body.tools?.filter(item => item.type === 'function') || [];
        const item = tools.length && !hasResult ? { id: 'fc-platform', type: 'function_call', status: 'completed', call_id: 'responses-call-1', name: tools.find(item => item.name.endsWith('write_file'))?.name, arguments: JSON.stringify({ path: 'responses-proof.md', content: '# Responses 实际工具执行' }) } : { id: 'msg-platform', type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'Responses 执行完成。', annotations: [] }] };
        const result = { id: 'resp-fixture', object: 'response', status: 'completed', output: [item], usage: { input_tokens: 100, output_tokens: 20, total_tokens: 120 } };
        if (!body.stream) return json(200, result);
        response.writeHead(200, { 'Content-Type': 'text/event-stream' });
        const emit = event => response.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
        emit({ type: 'response.created', response: { ...result, status: 'in_progress', output: [] } });
        emit({ type: 'response.output_item.added', output_index: 0, item });
        if (item.type === 'message') emit({ type: 'response.output_text.delta', output_index: 0, content_index: 0, item_id: item.id, delta: item.content[0].text });
        emit({ type: 'response.output_item.done', output_index: 0, item });
        emit({ type: 'response.completed', response: result }); response.end(); return;
      }
      if (controls.chatReply) {
        const delta = controls.chatReply(body);
        response.writeHead(200, { 'Content-Type': 'text/event-stream' });
        response.end(`data: ${JSON.stringify({ id: 'fixture', object: 'chat.completion.chunk', choices: [{ index: 0, delta, finish_reason: delta.tool_calls ? 'tool_calls' : 'stop' }] })}\n\ndata: [DONE]\n\n`); return;
      }
      const mcp = body.messages.some(item => String(item.content).includes('MCP_LOOP'));
      const completedTools = mcp ? body.messages.slice(body.messages.findLastIndex(item=>item.role==='user')+1) : body.messages;
      if (body.tools?.length && !completedTools.some(item => item.role === 'tool')) {
        const name = body.tools.find(item => mcp ? item.function.name.includes('search_records') : item.function.name.endsWith('write_file'))?.function.name;
        const call = { index: 0, id: 'platform-call-'+requests.length, type: 'function', function: { name, arguments: JSON.stringify({ path: 'platform-proof.md', content: '# 平台工具执行\n\n这是原生 Docker Agent 实际写入的测试文件。' }) } };
        response.writeHead(200, { 'Content-Type': 'text/event-stream' }); response.end(`data: ${JSON.stringify({ id: 'fixture', object: 'chat.completion.chunk', choices: [{ index: 0, delta: { tool_calls: [call] }, finish_reason: 'tool_calls' }] })}\n\ndata: [DONE]\n\n`); return;
      }
      const content = body.tools?.length ? '平台工具执行完成：[文件](platform-proof.md)' : '平台测试任务';
      if (body.stream) { response.writeHead(200, { 'Content-Type': 'text/event-stream' }); response.end(`data: ${JSON.stringify({ id: 'fixture', object: 'chat.completion.chunk', choices: [{ index: 0, delta: { content }, finish_reason: 'stop' }], usage: { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120 } })}\n\ndata: [DONE]\n\n`); }
      else json(200, { choices: [{ message: { role: 'assistant', content }, finish_reason: 'stop' }] });
      return;
    }
    json(404, { code: 'not_found' });
  });
  await listen(platform);
  const origin = `http://127.0.0.1:${platform.address().port}`;
  const material = JSON.stringify({ formatVersion: 1, kind: 'PLATFORM_ENROLLMENT', platformUrl: origin, code: 'synthetic-one-use-code', expiresAt: new Date(Date.now() + 3600000).toISOString() });

 const config=JSON.parse(await readFile(new URL('../../internal/tuck/contract/default-config.json',import.meta.url),'utf8'));
 config.providers=[];config.defaultModelId='';config.localMcp=[];
 config.workbench.defaultAgent='root';
 for(const assistant of config.workbench.assistants){assistant.workspace={mode:'auto'};assistant.deliveryTool=true;}
 let app;
 try{app=await launch(config,{directory})}catch(error){platform.closeAllConnections();platform.close();throw error}
 return {directory,models:app.models,snapshot,requests,material,controls,app,url:native?app.url:origin,async close(){await app.close();platform.closeAllConnections();await new Promise(done=>platform.close(done))}};
}
