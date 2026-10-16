// Development/build orchestration. The installed application is a Go executable.
import {spawn} from 'node:child_process';
import {access,mkdir,cp,open,readFile,writeFile,mkdtemp,rm} from 'node:fs/promises';
import {resolve,join,dirname,basename} from 'node:path';
import {fileURLToPath} from 'node:url';
import {gzipSync} from 'node:zlib';
const root=fileURLToPath(new URL('../',import.meta.url));
const command=process.argv[2]||'verify';
const executable=resolve(root,'.local',process.platform==='win32'?'tuck.exe':'tuck');
const fixture=resolve(root,'.local',process.platform==='win32'?'tuck-fixture.exe':'tuck-fixture');
async function run(program,args,options={}){await new Promise((done,fail)=>{const child=spawn(program,args,{cwd:root,stdio:'inherit',windowsHide:true,...options});child.once('error',fail);child.once('exit',(code,signal)=>code===0?done():fail(new Error(`${program} exited ${code??signal}`)))})}
const pnpm=(...args)=>run(process.platform==='win32'?'pnpm.cmd':'pnpm',args,{shell:process.platform==='win32'});
const go=(...args)=>run('go',args);
async function configureTestEngine(){
 if(process.env.TUCK_TEST_ENGINE)return;
 const name=process.platform==='win32'?'docker-agent.exe':'docker-agent';
 try{await access(join(root,'tests','runtime',name));return}catch{}
 try{
  const config=JSON.parse(await readFile(join(root,'tuck.config.json'),'utf8'));
  const binary=config.engine.binary?resolve(root,config.engine.binary):resolve(root,config.storage.dataDir,'engines',config.engine.version,name);
  await access(binary);process.env.TUCK_TEST_ENGINE=binary;
 }catch{throw new Error('Official test engine missing: run npm run setup or set TUCK_TEST_ENGINE to the official executable')}
}
async function build(){await pnpm('--dir','frontend','build');await mkdir(join(root,'.local'),{recursive:true});await go('build','-trimpath','-o',executable,'./cmd/tuck')}
async function buildFixture(){await mkdir(join(root,'.local'),{recursive:true});await go('test','-c','-o',fixture,'./internal/tuck')}
// Windows tar derives file modes from Windows attributes, losing Linux's
// executable bit. Normalize archive metadata without changing host files.
async function packageArchive(staging,name,destination){
 const temporary=join(staging,`${name}.tar`);
 await run('tar',['-cf',temporary,'-C',staging,name]);
 const archive=await readFile(temporary);
 for(let offset=0;offset+512<=archive.length;){
  const header=archive.subarray(offset,offset+512);
  if(header.every(byte=>byte===0))break;
  const field=(start,length)=>header.subarray(start,start+length).toString('utf8').replace(/\0.*$/s,'');
  const size=Number.parseInt(field(124,12).trim()||'0',8);
  if(!Number.isSafeInteger(size)||size<0)throw new Error('Invalid release tar entry size');
  const path=field(0,100),kind=String.fromCharCode(header[156]);
  const executable=path===`${name}/tuck`||path===`${name}/tuck.exe`;
  header.write(kind==='5'||executable?'0000755\0':'0000644\0',100,8,'ascii');
  header.fill(32,148,156);
  const checksum=header.reduce((sum,byte)=>sum+byte,0);
  header.write(checksum.toString(8).padStart(6,'0')+'\0 ',148,8,'ascii');
  offset+=512+Math.ceil(size/512)*512;
 }
 await writeFile(destination,gzipSync(archive));
}
async function verifiedGoTests(){await mkdir(join(root,'.local'),{recursive:true});const path=join(root,'.local','go-test.json'),file=await open(path,'w');try{await run('go',['test','-json','./...'],{stdio:['inherit',file.fd,'inherit']})}finally{await file.close()}}
if(['test','verify','e2e'].includes(command))await configureTestEngine();
if(command==='setup'){await pnpm('--dir','frontend','install','--frozen-lockfile');await go('mod','download');await go('run','./cmd/tuck','init');await go('run','./cmd/tuck','engine','install')}
else if(command==='build')await build();
else if(command==='start')await run(executable,['serve',...process.argv.slice(3)]);
else if(command==='dev'){await build();await run(executable,['serve',...process.argv.slice(3)])}
else if(command==='test'){await go('test','./...');await pnpm('--dir','frontend','test')}
else if(command==='deploy:init'){await mkdir(join(root,'deploy','data'),{recursive:true});await go('run','./cmd/tuck','init','--container','--config','deploy/config/tuck.config.json')}
else if(command==='e2e'){await buildFixture();await pnpm('--dir','frontend','exec','playwright','test','--workers=2',...process.argv.slice(3))}
else if(command==='verify'){await go('vet','./...');await verifiedGoTests();await pnpm('--dir','frontend','test');await build();await buildFixture();await pnpm('--dir','frontend','exec','playwright','test','--workers=2')}
else if(command==='package'){
 await pnpm('--dir','frontend','build');await mkdir(join(root,'release'),{recursive:true});await mkdir(join(root,'.local'),{recursive:true});const staging=await mkdtemp(join(root,'.local','package-'));
 try{
 for(const [os,arch]of [['windows','amd64'],['linux','amd64'],['linux','arm64']]){
  const name=`measix-tuck-${os}-${arch}`,directory=join(staging,name);
  await mkdir(directory,{recursive:true});await run('go',['build','-trimpath','-ldflags=-s -w','-o',join(directory,os==='windows'?'tuck.exe':'tuck'),'./cmd/tuck'],{env:{...process.env,GOOS:os,GOARCH:arch,CGO_ENABLED:'0'}});await cp(join(root,'frontend','dist'),join(directory,'frontend','dist'),{recursive:true});await writeFile(join(directory,'README.md'),"# Measix Tuck\n\n使用 [Docker Agent](https://github.com/docker/docker-agent) 作为执行引擎的自托管浏览器工作台。感谢 Docker 团队与社区提供原生执行基础；[官方文档](https://docker.github.io/docker-agent/)介绍上游能力。\n\n在解包目录运行，Windows 使用 `.\\tuck.exe`，Linux 使用 `./tuck`；下文 `tuck` 表示对应程序。运行无需 Go 或 Node.js。\n\n## 全新安装\n\n```text\ntuck init\ntuck engine install\ntuck serve\n```\n\n打开 <http://127.0.0.1:8090/>，在设置中配置模型或接入平台。首次官方引擎安装需要网络访问。已有安装按下方升级步骤操作。\n\n## 配置与密码\n\n唯一用户配置是 `tuck.config.json`，可用 `--config <path>` 选择；相对路径从配置目录解析。Tuck 业务库为 `<storage.dataDir>/tuck.db`，默认 `data/tuck.db`，引擎独立管理不透明会话资产。\n\n开放局域网前停服执行 `tuck password login`。设置保护密码使用 `tuck password settings`，忘记后停服执行 `tuck password settings --reset`。选择其他配置时，这些维护命令也须携带相同的 `--config`。密码从终端或标准输入读取，不进入参数。\n\n## 已有安装升级\n\n先停止 Tuck 及对应引擎，完整备份原配置、整个 dataDir 和所有实际工作目录，再替换程序与前端。正式 Go v1 升级 v2，使用目标版本程序和原配置执行：\n\n```text\ntuck migrate --check --config <原配置绝对路径>\ntuck migrate --backup <新的外部备份目录> --config <原配置绝对路径>\ntuck migrate --check --config <原配置绝对路径>\ntuck serve --config <原配置绝对路径>\n```\n\n占位符须替换为实际路径，含空格的路径加引号。备份目录必须尚不存在、父目录已存在，并位于 dataDir 之外；迁移输入备份仅含配置与 Tuck 库，不能替代完整冷备份。封存 Node 格式不导入。新程序写入后回退须恢复匹配整套资产，不只换程序或库文件。\n");
  await packageArchive(staging,name,join(root,'release',`${name}.tar.gz`));
 }
 }finally{if(dirname(staging)!==resolve(root,'.local')||!basename(staging).startsWith('package-'))throw new Error('Invalid package staging path');await rm(staging,{recursive:true,force:true})}
}
else if(command.startsWith('docker:')){const action=command.slice(7);await run('docker',action==='build'?['compose','build']:action==='up'?['compose','up','-d']:action==='down'?['compose','down']:['compose','logs','-f'])}
else throw new Error(`Unknown project command: ${command}`);
