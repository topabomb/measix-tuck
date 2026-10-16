import { describe, expect, it } from 'vitest'
import { sessionFamily, taskStatus } from '../src/modes/chat/session-presentation'
import type { DockerTask } from '../src/shared/workspace/runtime'
const task = (id:string, parentTaskId?:string) => ({id,parentTaskId,title:id,agent:'worker',status:'idle',logicalCount:0} as DockerTask)
describe('product session hierarchy', () => {
  it('isolates a family, retains sibling and nested navigation, excludes archived children', () => {
    const tasks = [task('root'),task('a','root'),task('b','root'),task('deep','a'),task('other'),{...task('old','root'),archived:true}]
    const family = sessionFamily(tasks,'deep')
    expect(family.ancestry.map(t=>t.id)).toEqual(['root','a','deep'])
    expect(family.rows.map(r=>[r.task.id,r.depth])).toEqual([['root',0],['a',1],['deep',2],['b',1]])
  })
  it('terminates on cyclic or missing ancestry without inventing sessions', () => {
    const cycle = sessionFamily([task('a','b'),task('b','a')],'a')
    expect(cycle.rows).toHaveLength(2)
    expect(sessionFamily([task('orphan','missing')],'orphan').root?.id).toBe('orphan')
    expect(sessionFamily([], 'missing').rows).toEqual([])
  })
  it('shows explicit blockers and settled failures independently of the parent', () => {
    expect(taskStatus({...task('a'),status:'waiting',pendingInteraction:{id:'q',kind:'question',title:'q',detail:''}})).toBe('等待回答')
    expect(taskStatus({...task('b'),lastTurnReason:'error'})).toBe('失败')
    expect(taskStatus({...task('root'),lastTurnReason:'completed'})).toBe('已完成')
  })
})
