import { describe, expect, it } from 'vitest'
import { formatJSONSource } from '../src/shared/enterprise-workspace/json'

describe('JSON source formatting',()=>{
  it('rejects excessive formatting expansion without changing the source',()=>{
    const nested='['.repeat(257)+'0'+']'.repeat(257)
    expect(()=>formatJSONSource(nested)).toThrow('层级过深')
    const expansion='['.repeat(180)+Array(6000).fill('0').join(',')+']'.repeat(180)
    expect(()=>formatJSONSource(expansion)).toThrow('结果过大')
  })
  it('preserves numeric spelling and duplicate keys',()=>{
    expect(formatJSONSource('{"id":9007199254740993,"id":-0,"limit":1e400,"fraction":1.2300}')).toBe('{\n  "id": 9007199254740993,\n  "id": -0,\n  "limit": 1e400,\n  "fraction": 1.2300\n}\n')
  })
  it('keeps string escapes, punctuation and nested empty structures intact',()=>{
    const source=String.raw`{"text":"a \"quote\" {,} \u4e2d","items":[{},[],true,false,null]}`
    const formatted=formatJSONSource(source)
    expect(JSON.parse(formatted)).toEqual(JSON.parse(source))
    expect(formatted).toContain(String.raw`"a \"quote\" {,} \u4e2d"`)
    expect(formatted).toContain('    {},\n    [],\n    true,\n    false,\n    null')
    expect(formatJSONSource(formatted)).toBe(formatted)
  })
  it('rejects malformed JSON without producing partial formatted content',()=>{
    for(const source of ['{"id":01}','{"id":}','[1,]','{"text":"unterminated}'])expect(()=>formatJSONSource(source)).toThrow()
    expect(formatJSONSource('  "hello"  ')).toBe('"hello"\n')
  })
})
