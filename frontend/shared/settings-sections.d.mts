export type SettingsSectionKey = 'providers'|'agents'|'mcp'|'platform'|'orchestration'|'engine'|'appearance'
export const settingsSections: ReadonlyArray<{key:SettingsSectionKey;label:string;description:string;protectable:boolean}>
