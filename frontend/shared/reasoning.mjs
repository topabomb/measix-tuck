export const reasoningEfforts = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'];
export const reasoningLabels = {default:'自动',none:'关闭（none）',minimal:'极低（minimal）',low:'低（low）',medium:'中（medium）',high:'高（high）',xhigh:'更高（xhigh）',max:'最高（max）'};
export function modelReasoningOptions(model) {
  return ['default', ...(model?.capabilities.includes('REASONING') ? model.reasoningEfforts || ['low','medium','high'] : [])];
}
