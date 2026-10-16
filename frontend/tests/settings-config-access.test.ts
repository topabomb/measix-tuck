import {describe,it,expect} from 'vitest'
import {normalizeSettings,editableConfig} from '../src/modes/settings/runtime-config-access'
import type {Settings} from '../src/modes/settings/components/types'
describe('partially protected configuration',()=>{
 it('fills display dependencies without sending hidden configuration back',()=>{
  const value=normalizeSettings({lockedSections:['providers','agents'],config:{localMcp:[{name:'test'}],platformMcpTools:{service:['read']}}} as unknown as Settings)
  expect(value.config.providers).toEqual([]);expect(value.config.workbench.assistants).toEqual([])
  expect(editableConfig(value)).toEqual({localMcp:[{name:'test'}],platformMcpTools:{service:['read']}})
 })
 it('retains editable drafts including removed optional values',()=>{
  const value=normalizeSettings({lockedSections:['mcp'],config:{providers:[],agents:{},workbench:{defaultAgent:'',assistants:[]},defaultModelId:'',modelDefaults:{contextWindow:64000,maxOutputTokens:4096},platformModelSettings:{}}} as unknown as Settings)
  expect(editableConfig(value)).toMatchObject({defaultModelId:'',platformModelSettings:{},modelDefaults:{contextWindow:64000,maxOutputTokens:4096}})
  expect(editableConfig(value)).not.toHaveProperty('localMcp')
 })
})
