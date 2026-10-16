export const platformProviderPath = (id:string) => '/settings/providers/platform/'+encodeURIComponent(id)
export const platformModelPath = (model:{providerId:string;modelId:string}) => platformProviderPath(model.providerId)+'/models/'+encodeURIComponent(model.modelId)
export const platformMcpPath = (id:string) => '/settings/mcp/platform/'+encodeURIComponent(id)
export const mcpStatus = (status:string) => ({ready:'最近验证成功',pending:'待验证',disabled:'已停用',unavailable:'最近验证失败'} as Record<string,string>)[status] || status
