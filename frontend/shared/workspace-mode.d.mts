export type WorkspaceMode = 'auto' | 'specified' | 'none';
export type WorkspacePolicy = {mode:WorkspaceMode;directory?:string};
export const workspaceModes:WorkspaceMode[];
export const workspaceModeLabels:Record<WorkspaceMode,string>;
export function workspacePolicy(assistant?:{workspace?:WorkspacePolicy}):WorkspacePolicy;
