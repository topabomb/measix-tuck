import { createServer } from 'node:http';
import { createHash } from 'node:crypto';
import { posix } from 'node:path';

export async function workspaceFixture(options = {}) {
  const space = 'spc_11111111-1111-4111-8111-111111111111';
  const files = new Map(), directories = new Set(['','folder']); const requests = [];
  const etag = bytes => '"'+createHash('sha256').update(bytes).digest('hex')+'"';
  const entry = path => directories.has(path) ? {path,kind:'DIRECTORY',etag:'"dir"'} : {path,kind:'FILE',size:files.get(path).length,etag:etag(files.get(path)),modifiedAt:'2026-10-11T00:00:00Z'};
  const put = (path,value) => files.set(path,Buffer.from(value));
  put('hello.txt','hello\r\nworld\r\n'); put('table.csv','code,name\n001,"hello, world"\n');put('readme.md','# Workspace\n\n![sample](sample.png)\n\n[hello](hello.txt)');put('page.html','<h1>Static HTML</h1><script>parent.hacked=true</script><img src="https://bad.invalid/tracker"><img src="sample.png">');put('shape.svg','<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100"><rect width="200" height="100" fill="teal"/></svg>');put('office.docx','Office download only');
  put('sample.png',Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAACXBIWXMAAAPoAAAD6AG1e1JrAAAAEklEQVQImWMQWhImtCSMAUIBABxOBDE8jR2EAAAAAElFTkSuQmCC','base64'));
  const wave = Buffer.alloc(44+8000);wave.write('RIFF');wave.writeUInt32LE(wave.length-8,4);wave.write('WAVEfmt ',8);wave.writeUInt32LE(16,16);wave.writeUInt16LE(1,20);wave.writeUInt16LE(1,22);wave.writeUInt32LE(8000,24);wave.writeUInt32LE(8000,28);wave.writeUInt16LE(1,32);wave.writeUInt16LE(8,34);wave.write('data',36);wave.writeUInt32LE(8000,40);wave.fill(128,44);put('sound.wav',wave);
  // Minimal standards-compliant PDF with a searchable text layer.
  let pdf='%PDF-1.4\n';const offsets=[0];const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Kids [3 0 R] /Count 1 >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>','<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'];const content='BT /F1 18 Tf 30 150 Td (Workspace PDF search) Tj ET';objects.push(`<< /Length ${content.length} >>\nstream\n${content}\nendstream`);objects.forEach((o,i)=>{offsets.push(Buffer.byteLength(pdf));pdf+=`${i+1} 0 obj\n${o}\nendobj\n`});const xref=Buffer.byteLength(pdf);pdf+=`xref\n0 6\n0000000000 65535 f \n${offsets.slice(1).map(o=>String(o).padStart(10,'0')+' 00000 n \n').join('')}trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`;put('document.pdf',pdf);
  const core = createServer(async(req,res)=>{
    const url = new URL(req.url,'http://localhost'), path = url.searchParams.get('path') || ''; requests.push({method:req.method,path,headers:req.headers,url:req.url});
    const json=(status,value)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value))};const fail=(status,code)=>json(status,{code,message:code});const ok=()=>json(200,{outcome:'SUCCEEDED',failures:[],truncated:false});
    if(req.headers.authorization!=='Bearer fixture-token')return fail(401,'unauthorized');
    if(url.pathname.endsWith('/workspace'))return json(200,{schemaVersion:1,serviceState:'ENABLED',state:'CONNECTED',bindingRevision:1,agentSpaceId:space,mcpAvailable:false,filesAvailable:true,mcpReason:'',filesReason:''});
    if(url.searchParams.get('agentSpaceId')!==space)return fail(409,'workspace_space_mismatch');
    if(url.pathname.endsWith('/files')) {
      if(req.method==='GET'){if(!directories.has(path))return fail(404,'file_not_found');return json(200,{entries:[...[...directories,...files.keys()].filter(p=>p && (posix.dirname(p)==='.'?'':posix.dirname(p))===path).map(entry),...(options.extraEntries?.(path)||[])],usedBytes:1000,availableBytes:1000000})}
      let data='';for await(const chunk of req)data+=chunk;const op=JSON.parse(data);const exists=files.has(op.path)||directories.has(op.path);
      if(op.action==='MKCOL'){if(exists)return fail(409,'file_conflict');directories.add(op.path);return ok()}
      if(!exists)return fail(404,'file_not_found');if(files.has(op.path)&&op.sourceEtag!==etag(files.get(op.path)))return fail(412,'file_version_conflict');
      if(op.action==='DELETE'){if(directories.has(op.path)&&!op.recursiveConfirmed)return fail(400,'confirmation_required');for(const p of [...files.keys(),...directories])if(p===op.path||p.startsWith(op.path+'/')){files.delete(p);directories.delete(p)}return ok()}
      if(files.has(op.destination)||directories.has(op.destination))return fail(409,'file_conflict');
      for(const p of [...directories,...files.keys()])if(p===op.path||p.startsWith(op.path+'/')){const dest=op.destination+p.slice(op.path.length);if(directories.has(p))directories.add(dest);else put(dest,files.get(p));if(op.action==='MOVE'){directories.delete(p);files.delete(p)}}return ok();
    }
    if(url.pathname.endsWith('/content')) {
      if(await options.content?.(req,res,path))return;
      const old=files.get(path);if(req.headers['if-match']&&(!old||req.headers['if-match']!==etag(old)))return fail(412,'file_version_conflict');if(req.headers['if-none-match']==='*'&&old)return fail(412,'file_version_conflict');
      if(req.method==='PUT'){const chunks=[];for await(const chunk of req)chunks.push(chunk);put(path,Buffer.concat(chunks));await options.beforeUploadResponse?.(path);return ok()}
      if(!old)return fail(404,'file_not_found');const headers={'ETag':etag(old),'Accept-Ranges':'bytes','Content-Type':'application/octet-stream'};
      if(req.headers.range){const match=req.headers.range.match(/^bytes=(\d+)-(\d*)$/);if(!match)return fail(416,'invalid_range');const start=Number(match[1]),end=Math.min(old.length-1,match[2]?Number(match[2]):old.length-1);if(start>=old.length){res.setHeader('Content-Range',`bytes */${old.length}`);return fail(416,'range_unsatisfiable')}const data=old.subarray(start,end+1);res.writeHead(206,{...headers,'Content-Length':data.length,'Content-Range':`bytes ${start}-${end}/${old.length}`});res.end(req.method==='HEAD'?undefined:data);return}
      res.writeHead(200,{...headers,'Content-Length':old.length});res.end(req.method==='HEAD'?undefined:old);return;
    }
    fail(404,'not_found');
  });await new Promise(resolve=>core.listen(0,'127.0.0.1',resolve));
  const platform={binding:{origin:`http://127.0.0.1:${core.address().port}`,discovery:{clientApiBase:'/api/client/v1'},deploymentId:'deployment',userId:'user',sessionId:'session',employeeName:'测试企业账号'},async access(){return 'fixture-token'}};
  return {platform,files,directories,requests,space,put,async close(){core.closeAllConnections();await new Promise(resolve=>core.close(resolve))}};
}
