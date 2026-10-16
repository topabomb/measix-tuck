import { describe, expect, it } from 'vitest'
import { sortedTasks, taskState, taskListQuery, taskReturnTo, taskLink } from '../src/modes/tasks/task-view'
import type { DockerTask } from '../src/shared/workspace/runtime'
describe('task channel view',() => {
  it('defaults malformed URL selection and rejects return destinations outside task lists',() => {
    for (const page of ['-1','0','1.5','Infinity','9007199254740992',['2','3'],null]) {
      expect(taskListQuery({period:['all','7d'],page})).toEqual({period:'24h',page:1})
    }
    expect(taskListQuery({period:'7d',page:'2'})).toEqual({period:'7d',page:2})
    for (const target of ['https://example.com','//example.com','/chat','/tasks/assistants/root/new',['/tasks/assistants/root']]) expect(taskReturnTo(target)).toBeUndefined()
    expect(taskReturnTo('/tasks/assistants/root?period=all&page=2&returnTo=https://example.com')).toBe('/tasks/assistants/root?period=all&page=2')
  })

  it('carries only bounded return context into nested delivery links',() => {
    expect(taskLink('/tasks/a/delivery',{path:'/tasks/a/supplement',query:{returnTo:'/tasks/assistants/root?period=all&page=2',from:'detail',filePage:'3'}},'supplement')).toEqual({path:'/tasks/a/delivery',query:{returnTo:'/tasks/assistants/root?period=all&page=2',from:'supplement',via:'detail'}})
    expect(taskLink('/tasks/a',{path:'/tasks/assistants/new',query:{period:'7d',page:'2'}})).toEqual({path:'/tasks/a',query:{returnTo:'/tasks/assistants/new?period=7d&page=2'}})
  })

  it('uses created order and real pending kinds rather than idle as completion',() => {
    const a = {id:'a',title:'T-abcd · A',agent:'root',createdAt:'2026-10-10T00:00:00Z',status:'idle',queue:[]} as unknown as DockerTask
    expect(taskState(a).label).toBe('未开始')
    expect(taskState({...a,pendingInteraction:{id:'q',kind:'question',title:'问题',detail:''}}).label).toBe('等待回答')
    const b = {...a,id:'b',createdAt:'2026-10-11T00:00:00Z'}
    expect(sortedTasks([a,b,{...a,id:'plain',title:'普通对话'}]).map(task=>task.id)).toEqual(['b','a'])
  })
})
