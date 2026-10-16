import type {Settings} from './types'

// Unsaved local changes must appear immediately; locked sections use only the
// public capability catalog and never require credentials or provider details.
export function inspectionModelOptions(data:Settings){
 const locked=data.lockedSections?.includes('providers')
 const catalog=(data.inspectionModels??[]).filter(m=>m.inspectionSelectable&&(locked||m.source==='platform')).map(m=>({modelId:m.modelId,label:`${m.displayName} · ${m.source==='platform'?'平台下发':'用户自定义'}`}))
 if(!locked)for(const p of data.config.providers)if(p.enabled)for(const m of p.models)if(m.enabled&&m.inputModalities.includes('TEXT')&&m.inputModalities.includes('IMAGE'))catalog.push({modelId:m.modelId,label:`${m.displayName} · 用户自定义`})
 return catalog.filter(m=>catalog.filter(other=>other.modelId===m.modelId).length===1)
}
