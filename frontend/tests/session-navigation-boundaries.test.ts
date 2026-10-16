import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const root = process.cwd()
const read = (path: string) => readFileSync(resolve(root, path), 'utf8')

describe('child-session and active-plan responsibility boundaries', () => {
  it('keeps child address semantic while session-tree navigation stays in Reference host', () => {
    const model = read('src/engine/model/conversation.ts')
    const contracts = read('src/engine/conversation/contracts.ts')
    const workspace = read('src/reference/workspace-runtime.ts')
    const childScenarios = read('src/reference/child-scenarios.ts')

    expect(model).toContain('childSessionId?: string')
    expect(model).not.toMatch(/parentSessionId|children:\s*readonly|openChildSession|sessionTree/)
    expect(contracts).not.toMatch(/parentSessionId|childSessions|openChildSession|sessionTree/)
    expect(workspace).toContain('#parentSessionIds')
    expect(workspace).toContain('parentSessionId(id: string)')
    expect(workspace).toContain('createChildScenarioTail')
    expect(childScenarios).toContain('Detailed child-session transcripts for the Reference')
    expect(childScenarios).toContain("case 'child-review-contract'")
  })

  it('models one work-plan semantic value but keeps its physical strip in the Vue adapter', () => {
    const contracts = read('src/engine/conversation/contracts.ts')
    const kernel = read('src/engine/conversation/session-kernel.ts')
    const vueIndex = read('src/engine/vue/index.ts')
    const strip = read('src/engine/vue/ActivePlanStrip.vue')
    const workspace = read('src/reference/workspace-runtime.ts')

    expect(contracts).toContain("export type WorkPlan = ContentBlockMap['plan']")
    expect(contracts).toContain('activePlan?: WorkPlan | null')
    expect(kernel).toContain('setActivePlan(plan: WorkPlan | null)')
    expect(kernel).toContain('never inferred by scanning message order or rendered DOM')
    expect(vueIndex).toContain('ActivePlanStrip')
    expect(strip).toContain('data-testid="active-plan-strip"')
    expect(strip).toContain('active-plan-popover')
    expect(workspace).toContain('#syncActivePlanFromProducerEvent')
    expect(`${contracts}\n${kernel}`).not.toMatch(/(?:popover|hover|composer|inputBox|topStrip)\??\s*:/)
  })
})
