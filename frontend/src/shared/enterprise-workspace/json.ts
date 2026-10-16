/** Validate JSON, then change whitespace without round-tripping its values. */
export function formatJSONSource(source:string):string {
  JSON.parse(source)
  const tokens=source.match(/"(?:[^"\\]|\\.)*"|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null|[{}\[\],:]/g) || []
  let result='',depth=0
  const line=()=>{if(result.length+depth*2>2*1024*1024)throw new Error('格式化结果过大，请保留原始排版');result+='\n'+'  '.repeat(depth)}
  for(let index=0;index<tokens.length;index++) {
    const token=tokens[index]!,next=tokens[index+1],previous=tokens[index-1]
    if(token==='{'||token==='[') {result+=token;depth++;if(depth>256)throw new Error('JSON 层级过深，请保留原始排版');if(next!=='}'&&next!==']')line()}
    else if(token==='}'||token===']') {depth--;if(previous!=='{'&&previous!=='[')line();result+=token}
    else if(token===',') {result+=token;line()}
    else if(token===':')result+=': '
    else result+=token
  }
  return result+'\n'
}
