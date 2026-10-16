export interface ChannelTask { title: string; parentTaskId?: string; status?: string; lastTurnReason?: string | null; pendingInteraction?: unknown; queuePaused?: boolean; queuedPrompts?: number; queue?: readonly unknown[] }
export function parseTaskTitle(title: string): {code: string; name: string} | null;
export function taskName(title: string): string;
export function isChannelTask(task: ChannelTask): boolean;
export function taskIsActive(task: ChannelTask): boolean;
export function taskHasEnded(task: ChannelTask): boolean;
export function turnHasEnded(task: ChannelTask): boolean;
export function channelLimit(assistant?: {taskConcurrency?: number}): number;
export function makeTaskTitle(name: string, existingTitles?: string[]): string;
