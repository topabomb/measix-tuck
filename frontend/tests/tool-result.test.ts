import {describe,it,expect} from 'vitest'
import {readTuckToolResult} from '../src/modes/chat/components/tool-result'

describe('Tuck tool result presentation',()=>{
 it('recognizes conversation tools while leaving native tools raw',()=>{
  const result={status:'completed',conversations:[{conversation_id:'task',title:'Review'}]}
  expect(readTuckToolResult('list_conversations',result)).toEqual(result)
  expect(readTuckToolResult('send_conversation_message',{status:'accepted'})).toEqual({status:'accepted'})
  expect(readTuckToolResult('list_sessions',result)).toBeNull()
  expect(readTuckToolResult('list_tasks',result)).toBeNull()
 })
 it('reads direct and official MCP envelopes without changing the result',()=>{
  const result={status:'completed',text:'First line\n\n**Second line**'}
  for(const value of [result,JSON.stringify(result),{structuredContent:result},{content:[{type:'text',text:JSON.stringify(result)}]}])expect(readTuckToolResult('inspect_attachments',value)).toEqual(result)
 })
 it('preserves partial and uncertain business outcomes',()=>{
  for(const status of ['partial','unknown'])expect(readTuckToolResult('download_runcove_files',{status,code:'upstream_error',message:'Download failed.'})?.status).toBe(status)
 })
 it('leaves external, malformed and oversized output raw',()=>{
  expect(readTuckToolResult('external_tool',{status:'completed'})).toBeNull()
  for(const value of ['{bad',null,[],{status:'success'},{content:[null]},{output:{output:{output:{output:{output:{status:'completed'}}}}}},' '.repeat(1024*1024+1)])expect(readTuckToolResult('inspect_attachments',value)).toBeNull()
 })
})
