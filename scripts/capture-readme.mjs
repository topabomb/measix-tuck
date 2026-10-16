// Capture the actual workbench with synthetic content and platform responses.
// Requires the built frontend, Go, official Docker Agent, Chromium and FFmpeg.
// No live model, production data or real enterprise service is used.
import { readFile, writeFile, mkdir, access } from 'node:fs/promises';
import { resolve } from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
const root = fileURLToPath(new URL('../', import.meta.url));
process.chdir(root);
const run = (command,args) => execFileSync(command,args,{cwd:root,windowsHide:true,stdio:'inherit'});
execFileSync('ffmpeg',['-version'],{windowsHide:true,stdio:'ignore'});
await mkdir('.local/readme-showcase',{recursive:true});
run('go',['test','-c','-o',resolve(root,'.local',process.platform==='win32'?'tuck-fixture.exe':'tuck-fixture'),'./internal/tuck']);
const require = createRequire(resolve(root, 'frontend/package.json'));
const { chromium } = require('@playwright/test');
if (!process.env.TUCK_TEST_ENGINE) {
 const installed = JSON.parse(await readFile('tuck.config.json','utf8'));
 const binary = process.platform==='win32'?'docker-agent.exe':'docker-agent';
 process.env.TUCK_TEST_ENGINE = installed.engine.binary ? resolve(root,installed.engine.binary) : resolve(root,installed.storage.dataDir,'engines',installed.engine.version,binary);
}
await access(process.env.TUCK_TEST_ENGINE);
const { launch } = await import('../tests/fixtures/bridge.mjs');
const { workspaceFixture } = await import('../tests/fixtures/workspace-fixture.mjs');
const config = JSON.parse(await readFile('internal/tuck/contract/default-config.json', 'utf8'));
config.providers = [{ id:'demo',displayName:'演示供应商',enabled:true,protocol:'OPENAI_CHAT_COMPLETIONS',baseUrl:'http://127.0.0.1:1/v1',apiKey:'demo-only',models:[{modelId:'demo-model',displayName:'演示模型',enabled:true,upstreamModelKey:'demo',inputModalities:['TEXT','IMAGE'],capabilities:['TOOL'],contextWindow:128000,maxOutputTokens:8192}]}];
config.defaultModelId = 'demo-model';
for (const assistant of config.workbench.assistants) { assistant.workspace={mode:'auto'}; assistant.deliveryTool=true; }
const core = await workspaceFixture();
core.files.clear(); core.directories.clear(); core.directories.add('');
core.platform.binding.employeeName = '演示工作室';
const brief = '# 周末，把时间交给一本书\n\n独立书店周末活动方案 · 演示项目\n\n## 活动安排\n\n| 时间 | 活动 | 形式 |\n| --- | --- | --- |\n| 周六 10:00 | 城市阅读漫步 | 书店集合，沿街区寻找故事 |\n| 周六 15:00 | 一人一本书 | 围坐分享，交换阅读笔记 |\n| 周日 14:00 | 给未来写封信 | 安静写作，封存一个愿望 |\n\n## 准备清单\n\n- 确认活动时间与参与人数\n- 准备桌签、便笺与报名页面\n- 核对文案、排期和现场分工\n\n所有日期、活动与安排均为演示内容。\n';
const report = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><style>*{box-sizing:border-box}body{margin:0;background:#f7f4ed;color:#233c36;font:15px/1.7 "Microsoft YaHei",sans-serif;padding:36px}small{font-size:11px;letter-spacing:2px;color:#677f70}h1{font:600 34px/1.45 "Microsoft YaHei",sans-serif;margin:20px 0 10px}p{color:#61746b}header{border-bottom:1px solid #d9ded2;padding-bottom:22px}aside{display:flex;gap:32px;margin:25px 0}aside strong{font-size:28px;display:block}aside span{font-size:12px;color:#61746b}article{display:flex;gap:16px;padding:18px 0;border-top:1px solid #d9ded2}b{color:#73917a;font-size:22px}h3{margin:0;font-size:17px}article p{margin:4px 0;font-size:13px}footer{margin-top:22px;font-size:11px;color:#7d887e}</style><header><small>WEEKEND READING CLUB · 活动提案</small><h1>周末，把时间<br>交给一本书。</h1><p>一间书店，三个相遇的理由。<br>让阅读从独处走向交流。</p></header><aside><div><strong>02</strong><span>天的慢生活</span></div><div><strong>03</strong><span>场主题活动</span></div><div><strong>01</strong><span>份完整执行方案</span></div></aside><article><b>01</b><div><h3>城市阅读漫步</h3><p>周六 10:00 · 带一本书，重新认识熟悉的街区。</p></div></article><article><b>02</b><div><h3>一人一本书</h3><p>周六 15:00 · 分享一段文字，交换一个新的视角。</p></div></article><article><b>03</b><div><h3>给未来写封信</h3><p>周日 14:00 · 留下此刻的想法，与未来的自己相见。</p></div></article><footer>TUCK SHOWCASE · 隔离演示数据 · 待确认后发布</footer></html>`;
core.put('活动方案.html',report); core.put('执行清单.md',brief);
core.put('活动排期.csv','时间,活动,负责人,状态\n周六 10:00,城市阅读漫步,活动协调,待确认\n周六 15:00,一人一本书,主持人,待确认\n周日 14:00,给未来写封信,活动协调,待确认\n');
let app, browser;
try {
app = await launch(config, { enterprisePlatform:core.platform, directory:resolve(root,'.local/readme-showcase/runtime-'+Date.now()) });
browser = await chromium.launch();
const page = await browser.newPage({viewport:{width:1440,height:960},deviceScaleFactor:1});
const errors=[]; page.on('pageerror',e=>errors.push(e.message));
const out=resolve(root,'docs/assets/readme'); await mkdir(out,{recursive:true});
const shot=async name=>{await page.evaluate(()=>document.fonts.ready);await page.waitForTimeout(800);await page.screenshot({path:resolve(out,name+'.png')});};
 const task=await app.manager.create({title:'独立书店 · 周末活动方案',agent:'root',modelId:'demo-model'});
 app.manager.append(task,{role:'user',blocks:[{id:'brief',type:'markdown',data:{markdown:'帮我策划书店的周末活动：安排三场轻松的阅读交流，整理成可以预览的页面，并附上执行清单。先给我看方案，暂时不要发布。'}}]});
 app.manager.append(task,{role:'assistant',blocks:[{id:'read',type:'tool-call',data:{name:'read_file',callId:'demo-read',input:{path:'活动需求.md'},status:'success'}},{id:'write',type:'tool-call',data:{name:'write_file',callId:'demo-write',input:{path:'活动方案.html'},status:'success'}}]});
 app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:'## 把一个周末，变成三次相遇\n\n方案已整理好。右侧可以直接预览活动页面，执行清单和排期也已放入工作区。\n\n| 活动 | 安排 |\n| --- | --- |\n| 城市阅读漫步 | 周六上午 · 边走边读 |\n| 一人一本书 | 周六下午 · 围坐分享 |\n| 给未来写封信 | 周日下午 · 安静写作 |\n\n**下一步**：确认每场人数与报名方式，我再完善现场分工。\n\n尚未对外发布，等你确认后继续。'}}]});
 app.manager.settle(task,'completed');
 await writeFile(resolve(task.workspace,'执行清单.md'),brief);
 await writeFile(resolve(task.workspace,'活动方案.html'),report);
 await app.deliver(task,'周末阅读活动方案已整理，包含活动页面与执行清单。\n\n- 三场活动覆盖漫步、分享与写作。\n- 参加人数、报名方式待确认。\n- 方案尚未对外发布。',['活动方案.html','执行清单.md']);
 const other=await app.manager.create({title:'每周阅读笔记',agent:'root',modelId:'demo-model'});
 app.manager.append(other,{role:'assistant',blocks:[{id:'note',type:'markdown',data:{markdown:'本周阅读笔记已整理为主题、摘录与行动清单。'}}]});app.manager.settle(other,'completed');
 await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1`);
 const panel=page.getByRole('complementary',{name:'企业工作区'});
 await panel.getByRole('option',{name:/活动方案.html/}).dblclick();
 await panel.frameLocator('iframe:visible').getByRole('heading',{name:'周末，把时间 交给一本书。'}).waitFor();
 await shot('workbench');
 await panel.getByRole('button',{name:'展开工作区'}).click();
 await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click();
 await panel.getByRole('option',{name:/执行清单.md/}).dblclick();
 await panel.frameLocator('iframe:visible').getByRole('heading',{name:'周末，把时间交给一本书'}).waitFor();
 // The full-width view is shown in the walkthrough below.
 await page.setViewportSize({width:1200,height:780});
 await page.goto(app.url+`/#/chat/conversations/${task.id}/delivery`);
 await page.getByRole('heading',{name:'交付',exact:true}).waitFor();
 await shot('delivery');
 await page.setViewportSize({width:390,height:844});
 await page.goto(app.url+`/#/chat/conversations/${task.id}`);
 await page.getByTestId('composer-input').waitFor();
 await shot('mobile');
 const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);
 if(overflow||errors.length)throw new Error(JSON.stringify({overflow,errors}));
 const videoContext=await browser.newContext({viewport:{width:1280,height:800},recordVideo:{dir:resolve(root,'.local/readme-showcase/video'),size:{width:1280,height:800}}});
 const demo=await videoContext.newPage();
 demo.on('pageerror',e=>errors.push(e.message));
 await demo.goto(app.url+`/#/chat/conversations/${task.id}?ew=1`);
 const workspace=demo.getByRole('complementary',{name:'企业工作区'});
 await workspace.getByRole('option',{name:/活动方案.html/}).dblclick();
 await workspace.frameLocator('iframe:visible').getByRole('heading',{name:'周末，把时间 交给一本书。'}).waitFor();
 await demo.waitForTimeout(2200);
 await workspace.getByRole('button',{name:'源码',exact:true}).click();
 await demo.waitForTimeout(1800);
 await workspace.getByRole('button',{name:'预览',exact:true}).click();
 await demo.waitForTimeout(1200);
 await workspace.getByRole('button',{name:'展开工作区'}).click();
 await demo.waitForTimeout(1300);
 await workspace.getByRole('tab',{name:'返回文件列表',exact:true}).click();
 await demo.waitForTimeout(1000);
 await workspace.getByRole('option',{name:/执行清单.md/}).dblclick();
 await workspace.frameLocator('iframe:visible').getByRole('heading',{name:'周末，把时间交给一本书'}).waitFor();
 await demo.waitForTimeout(2000);
 await workspace.getByRole('button',{name:'还原工作区'}).click();
 await workspace.getByRole('button',{name:'收起企业工作区'}).click();
 await demo.getByRole('button',{name:'交付摘要',exact:true}).click();
 await demo.getByRole('dialog',{name:'交付摘要',exact:true}).getByRole('link',{name:'查看交付',exact:true}).click();
 await demo.getByRole('heading',{name:'交付',exact:true}).waitFor();
 await demo.waitForTimeout(2500);
 const video=demo.video();await videoContext.close();
 await video.saveAs(resolve(root,'.local/readme-showcase/walkthrough.webm'));
 if(errors.length)throw new Error(JSON.stringify(errors));
 console.log(JSON.stringify({screenshots:['workbench','delivery','mobile'],video:'walkthrough.webm',errors,overflow}));
} finally {await browser?.close();await app?.close();await core.close();}

const videoFile='.local/readme-showcase/walkthrough.webm',palette='.local/readme-showcase/palette.png';
run('ffmpeg',['-hide_banner','-loglevel','error','-y','-i',videoFile,'-vf','fps=3,scale=960:-1:flags=lanczos,palettegen=max_colors=96:stats_mode=diff',palette]);
run('ffmpeg',['-hide_banner','-loglevel','error','-y','-i',videoFile,'-i',palette,'-lavfi','fps=3,scale=960:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=none:diff_mode=rectangle','-loop','0','docs/assets/readme/walkthrough.gif']);
