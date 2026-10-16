import type {Settings} from './types'

export function imageGeneratorOptions(data:Settings){
 const locked=data.lockedSections?.includes('providers')
 const choices=(data.imageCatalog??[]).filter(r=>r.selectable&&(locked||r.source==='platform')).map(r=>({selector:`${r.source}:${r.imageId}`,label:`${r.displayName} · ${r.source==='platform'?'平台下发':'用户自定义'}`}))
 if(!locked)for(const r of data.config.imageGenerators??[])if(r.enabled&&data.config.providers.some(p=>p.id===r.providerId&&p.enabled))choices.push({selector:'local:'+r.imageId,label:r.displayName+' · 用户自定义'})
 return choices
}
