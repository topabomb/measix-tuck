import type {Settings} from './components/types'
const fields:Record<string,string[]>={providers:['providers','defaultModelId','modelDefaults','platformModelSettings','imageGenerators','defaultImageGenerator','defaultAttachmentInspectionModelId'],agents:['agents','workbench'],mcp:['localMcp','platformMcpTools']}
// Missing protected fields are only placeholders for shared editor dependencies.
// They must never become replacement values in a save request.
export function normalizeSettings(value:Settings):Settings {
 return {...value,config:{providers:[],imageGenerators:[],defaultImageGenerator:{},defaultModelId:'',modelDefaults:{contextWindow:0,maxOutputTokens:0},agents:{},workbench:{defaultAgent:'',assistants:[]},localMcp:[],...(value.config as Partial<Settings['config']>)}}
}
export function editableConfig(value:Settings){
 const config:Record<string,unknown>={...value.config}
 for(const section of value.lockedSections||[])for(const field of fields[section]||[])delete config[field]
 return config
}
