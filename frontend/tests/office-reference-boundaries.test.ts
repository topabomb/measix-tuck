import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const root = process.cwd()
const engineModel = () => readFileSync(resolve(root, 'src/engine/model/conversation.ts'), 'utf8')
const engineContracts = () => readFileSync(resolve(root, 'src/engine/conversation/contracts.ts'), 'utf8')
const officeScenarios = () => readFileSync(resolve(root, 'src/reference/office-scenarios.ts'), 'utf8')
const diagnostics = () => readFileSync(resolve(root, 'src/reference/components/ReferenceDiagnosticsPanel.vue'), 'utf8')
const workspace = () => readFileSync(resolve(root, 'src/reference/components/AgentWorkspace.vue'), 'utf8')

describe('office Reference responsibility boundary', () => {
  it('reuses generic rendering semantics instead of adding connector or office workflow types to Engine', () => {
    const core = `${engineModel()}\n${engineContracts()}`
    expect(core).not.toMatch(/Gmail|Outlook|GoogleCalendar|MicrosoftGraph|Office365|WorkspaceConnector|MailAction|CalendarAction|SchedulePrompt|OfficeArtifact/)
    expect(core).not.toMatch(/sendEmail|createEvent|scheduleMeeting|connectorAuth|recurringTask/)

    const reference = officeScenarios()
    expect(reference).toContain("'plan'")
    expect(reference).toContain("'tool-call'")
    expect(reference).toContain("'tool-result'")
    expect(reference).toContain("'delegation'")
    expect(reference).toContain("'attachments'")
    expect(reference).toContain('ResourceRef')
    expect(reference).toContain('external side effects remain producer/host responsibilities')
    expect(reference).toContain('external adapter owns the actual side effect and any provider continuation')
    expect(reference).toContain("callId: 'meeting-followup-approval'")
    expect(reference).not.toMatch(/callId: 'meeting-followup-approval'[^\n]*status: 'running'/)
  })

  it('keeps scenario navigation and replay controls entirely in Reference', () => {
    expect(diagnostics()).toContain('Reference scenarios')
    expect(diagnostics()).toContain('reference-office-briefing')
    expect(diagnostics()).toContain('reference-office-followup')
    expect(diagnostics()).toContain('reference-agent-delegation')
    expect(workspace()).toContain('window.location.reload()')
    expect(workspace()).toContain('AGENT_REFERENCE_MESSAGE')

    const core = `${engineModel()}\n${engineContracts()}`
    expect(core).not.toMatch(/restart-agent|office-briefing|office-followup|AGENT_REFERENCE_MESSAGE/)
  })
})
