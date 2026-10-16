export const workspaceModes = ['auto', 'specified', 'none'];
export const workspaceModeLabels = {auto:'自动目录',specified:'指定目录',none:'未指定目录（原生默认）'};
export function workspacePolicy(assistant) {
  return assistant?.workspace || {mode:'auto',directory:''};
}
