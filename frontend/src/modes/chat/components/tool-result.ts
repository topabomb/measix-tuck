// Interpret only Tuck's documented result contract; external tool output stays raw.
const tuckTools=new Set(['list_conversations','send_conversation_message','deliver_result','upload_runcove_file','download_runcove_files','generate_images','inspect_attachments'])
export function readTuckToolResult(name:string,value:unknown):Record<string,unknown>|null {
 if(!tuckTools.has(name))return null
 function read(value:unknown,depth:number):Record<string,unknown>|null {
  if(depth>4)return null
  if(typeof value==='string'){if(value.length>1024*1024)return null;try{return read(JSON.parse(value),depth+1)}catch{return null}}
  if(!value||typeof value!=='object'||Array.isArray(value))return null
  const data=value as Record<string,unknown>
  if(['completed','accepted','failed','partial','unknown'].includes(String(data.status)))return data
  for(const nested of [data.structuredContent,data.output]){const result=read(nested,depth+1);if(result)return result}
  if(Array.isArray(data.content))for(const item of data.content){const result=read(item?.text,depth+1);if(result)return result}
  return null
 }
 return read(value,0)
}
