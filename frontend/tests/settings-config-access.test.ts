import {describe,it,expect} from 'vitest'
import {normalizeSettings,editableConfig} from '../src/modes/settings/runtime-config-access'
import type {Settings} from '../src/modes/settings/components/types'
import {inspectionModelOptions} from '../src/modes/settings/components/inspection-model-options'
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
 it('offers unsaved vision-only models and preserves locked inspection selections',()=>{
  const value=normalizeSettings({lockedSections:[],inspectionModels:[{modelId:'platform-vision',displayName:'平台视觉',source:'platform',inspectionSelectable:true}],config:{defaultAttachmentInspectionModelId:'draft-vision',providers:[{id:'draft',enabled:true,models:[{modelId:'draft-vision',displayName:'新视觉',enabled:true,inputModalities:['TEXT','IMAGE'],capabilities:[]}]}]}} as unknown as Settings)
  expect(inspectionModelOptions(value).map(m=>m.modelId)).toEqual(['platform-vision','draft-vision'])
  expect(editableConfig(value)).toHaveProperty('defaultAttachmentInspectionModelId','draft-vision')
  value.lockedSections=['providers']
  value.inspectionModels!.push({modelId:'saved-vision',displayName:'已保存视觉',source:'local',inspectionSelectable:true})
  expect(inspectionModelOptions(value).map(m=>m.modelId)).toEqual(['platform-vision','saved-vision'])
  expect(editableConfig(value)).not.toHaveProperty('defaultAttachmentInspectionModelId')
 })
})
