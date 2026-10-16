// Deterministic model fixture. The backend under test is always Go.
import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
import {launch,until} from './bridge.mjs';
export {until};
const pause=ms=>new Promise(done=>setTimeout(done,ms));
const listen=server=>new Promise((done,fail)=>server.once('error',fail).listen(0,'127.0.0.1',done));
export async function fixture(options={}) {
 const {beforeResponse,maxIterations=16,attachmentModels=false,localAssistants=false,delivery=false,enterprisePlatform,security}=options;
  const requests = [];
  const model = createServer(async (req, res) => {
    let text = ''; for await (const bytes of req) text += bytes;
    const body = JSON.parse(text); requests.push(body);
    const history = body.messages || [];
    const lastUser = history.findLastIndex(item => item.role === 'user');
    const recent = history.slice(lastUser + 1);
    const done = recent.flatMap(item => item.tool_calls || []).map(call => call.function.name);
    const toolOutputs = recent.filter(item => item.role === 'tool');
    const prompt = JSON.stringify(history[lastUser] || history);
    await beforeResponse?.(prompt, body);
    const tool = (name, args) => ({ id: `test-call-${requests.length}`, type: 'function', function: { name, arguments: JSON.stringify(args) } });
    let calls, content = '测试端点执行完成：已观察真实工具结果。';
    if (prompt.includes('QUEUE_DELAY')) await pause(600);
    if (prompt.includes('STEER_TARGET')) await pause(2000);
    if (prompt.includes('SLOW')) { await pause(10000); }
    else if (JSON.stringify(history).includes('CHILD_QUESTION') && !(body.tools || []).some(tool => tool.function?.name === 'transfer_task')) { if (!done.includes('user_prompt')) calls = [tool('user_prompt',{message:'子助手需要确认素材',schema:{type:'object',properties:{material:{type:'string',title:'素材名称'}},required:['material']}})]; }
    else if (JSON.stringify(history).includes('CHILD_ONLY') && !(body.tools || []).some(tool => tool.function?.name === 'transfer_task')) content = '子 Agent 已独立完成检查。';
    else if (prompt.includes('FAIL_MODEL')) { res.writeHead(400); res.end(JSON.stringify({ error: { message: 'Intentional test provider failure', type: 'invalid_request_error' } })); return; }
    else if (prompt.includes('MULTI_QUESTION')) {
      if (!done.includes('user_prompt')) {
        calls = ['素材','尺寸'].map((title,index)=>({...tool('user_prompt',{title,message:`请提供${title}`,schema:{type:'object',properties:{value:{type:'string',title}},required:['value']}}),id:`question-${requests.length}-${index}`}));
        if (prompt.includes('WITH_APPROVAL')) calls.push(tool('write_file',{path:'confirmed.txt',content:'confirmed'}));
      }
    }
    else if (prompt.includes('CHILD_QUESTION')) { if (!done.includes('transfer_task')) calls = [tool('transfer_task',{agent:'worker',task:'CHILD_QUESTION 确认素材',expected_output:'已确认的素材'})]; }
    else if (prompt.includes('SCHEMA_QUESTION')) { if (!done.includes('user_prompt')) calls = [tool('user_prompt', {title:'生成设置',message:'选择风格和数量。',schema:{type:'object',properties:{style:{type:'string',title:'风格',enum:['简洁','详细']},confirm:{type:'boolean',title:'是否附带说明'},count:{type:'integer',title:'数量',minimum:1,maximum:5,default:2},note:{type:'string',title:'备注'}},required:['style','confirm','count']}})]; }
    else if (prompt.includes('REJECT_ONLY')) { if (!done.includes('write_file')) calls = [tool('write_file', { path: 'rejected.txt', content: 'must not be written' })]; }
    else if (prompt.includes('BACKGROUND_LOOP')) {
      if (!done.includes('run_background_agent')) calls = [tool('run_background_agent', { agent: 'worker', task: 'CHILD_ONLY 后台检查' })];
      else if (!done.includes('wait_background_agents')) { const id = toolOutputs.map(item => String(item.content).match(/started with ID: (\S+)/)?.[1]).find(Boolean); calls = [tool('wait_background_agents', { task_ids: [id], timeout: 5 })]; }
      else content = '后台检查已经返回实际结果。';
    }
    else if (prompt.includes('CROSS_SESSION_')) {
      if (!done.includes('list_sessions')) calls = [tool('list_sessions', {include_archived:true})];
      else if (!done.includes('send_session_message')) {
        const listed = toolOutputs.map(item => { try { return JSON.parse(item.content); } catch { return {}; } }).find(item => item.sessions);
        const target = listed?.sessions.find(item => item.title === '消息接收方');
        calls = [tool('send_session_message', { target_task_id: ` ${target?.task_id} `, message: prompt.includes('IDLE') ? '空闲协作测试消息' : '执行中协作测试消息', timeout_secs:30 })];
      } else content = '已通过实际工具投递协作消息。';
    }
    else if (delivery && prompt.includes('TEXT_DELIVERY')) { if (!done.includes('tuck_deliver_result')) calls=[tool('tuck_deliver_result',{summary:'核对完成，请查看交付。',files:[]})]; }
    else if (prompt.includes('FULL_LOOP')) {
      if (!done.includes('create_todos')) calls = [tool('create_todos', { descriptions: ['生成报告', '验证报告'] })];
      else if (!done.includes('user_prompt')) calls = [tool('user_prompt', { message: '报告标题是什么？', schema: { type: 'object', properties: { title: { type: 'string' } }, required: ['title'] } })];
      else if (!done.includes('write_file')) calls = [tool('write_file', { path: 'report.md', content: '# 真实工具报告\n\n此文件由 Docker Agent 的 write_file 生成。\n' })];
      else if (!done.includes('shell')) calls = [tool('shell', { cmd: process.platform === 'win32' ? 'powershell -NoProfile -Command "Get-Content report.md"' : 'cat report.md' })];
      else if (!done.includes('transfer_task')) calls = [tool('transfer_task', { agent: 'worker', task: 'CHILD_ONLY 检查报告', expected_output: '检查结果' })];
      else if (!done.includes('update_todos')) {
        const snapshot = toolOutputs.map(item => { try { return JSON.parse(item.content); } catch { return {}; } }).find(item => item.all_todos);
        calls = [tool('update_todos', { updates: (snapshot?.all_todos || []).map(todo => ({ id: todo.id, status: 'completed' })) })];
      } else if(delivery&&!done.includes('tuck_deliver_result')) calls=[tool('tuck_deliver_result',{summary:'已生成 [report.md](report.md)，终端和子任务验证完成。',files:[{path:'report.md',description:'通过工具生成并检查的报告'}]})];
      else content = '已生成 [report.md](report.md)，终端和子任务验证完成。\n\n```js\nconsole.log("verified")\n```';
    }
    const delta = calls ? { role: 'assistant', tool_calls: calls.map((call, index) => ({ index, ...call })) } : { role: 'assistant', content };
    if (body.stream) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream' });
      res.write(`data: ${JSON.stringify({ id: `completion-${requests.length}`, object: 'chat.completion.chunk', created: 1, model: 'gpt-4o-mini', choices: [{ index: 0, delta, finish_reason: null }] })}\n\n`);
      if (prompt.includes('METRICS_STREAM') && !calls) {
        await pause(200);
        res.write(`data: ${JSON.stringify({ id: `completion-${requests.length}`, object: 'chat.completion.chunk', created: 1, model: 'gpt-4o-mini', choices: [{ index: 0, delta: {content:'流式计时验收。'}, finish_reason:null }] })}\n\n`);
      }
      res.end(`data: ${JSON.stringify({ id: `completion-${requests.length}`, object: 'chat.completion.chunk', created: 1, model: 'gpt-4o-mini', choices: [{ index: 0, delta: {}, finish_reason: calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 120, completion_tokens: 30, total_tokens: 150 } })}\n\ndata: [DONE]\n\n`);
    } else { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ id: 'test', choices: [{ index: 0, message: { role: 'assistant', content, tool_calls: calls }, finish_reason: calls ? 'tool_calls' : 'stop' }] })); }
  });
  await listen(model);

 const config=JSON.parse(await readFile(new URL('../../internal/tuck/contract/default-config.json',import.meta.url),'utf8'));
 config.agents.root.instruction='Follow the requested test task.';
 config.agents.root.max_iterations=maxIterations;
 config.agents.root.sub_agents=['worker'];
 config.agents.worker={description:'Task specialist',instruction:'CHILD_ONLY independent reviewer.',max_iterations:16,toolsets:[{type:'user_prompt'}]};
 config.permissions={allow:['create_todos','update_todos','user_prompt','transfer_task','*_background_agent*','list_sessions','send_session_message'],ask:['write_file','shell']};
 config.workbench.defaultAgent='root';
 if(!localAssistants)config.workbench.assistants=config.workbench.assistants.filter(a=>a.agent==='root');
 for(const assistant of config.workbench.assistants){assistant.workspace={mode:'auto'};assistant.deliveryTool=delivery;}
 const ids=attachmentModels?['vision','text']:['fixture-model'];
 config.providers=[{id:'fixture',displayName:'Test provider',enabled:true,protocol:'OPENAI_CHAT_COMPLETIONS',baseUrl:`http://127.0.0.1:${model.address().port}/v1`,apiKey:'local-test-only',models:ids.map(modelId=>({modelId,displayName:modelId==='vision'?'测试视觉模型':modelId==='text'?'测试文本模型':'测试模型',enabled:true,upstreamModelKey:'gpt-4o-mini',inputModalities:modelId==='vision'?['TEXT','IMAGE']:['TEXT'],capabilities:['TOOL'],contextWindow:240640,maxOutputTokens:32768}))}];
 config.defaultModelId=ids[0];
 if(security)config.security.auth=security.auth;
 let app;
 try{app=await launch(config,{enterprisePlatform})}catch(error){model.closeAllConnections();model.close();throw error}
 return {...app,requests,async close(){await app.close();model.closeAllConnections();await new Promise(done=>model.close(done))}};
}
