// A title convention, not a second task identity or persisted source field.
export function parseTaskTitle(title) {
  if (typeof title !== 'string' || /[\x00-\x1f\x7f]/.test(title)) return null;
  const match = /^T-([A-Za-z0-9_]{4}) · ([^\r\n]+)$/.exec(title);
  return match && match[2].trim() ? { code: match[1], name: match[2] } : null;
}
export const taskName = title => parseTaskTitle(title)?.name ?? title;
export const isChannelTask = task => !task.parentTaskId && !!parseTaskTitle(task.title);
export const taskIsActive = task => task.status === 'working' || task.status === 'waiting' || !!task.pendingInteraction || (!task.queuePaused && (task.queuedPrompts || task.queue?.length || 0) > 0);
export const turnHasEnded = task => task.status === 'idle' && !!task.lastTurnReason && !taskIsActive(task);
export const taskHasEnded = task => isChannelTask(task) && turnHasEnded(task);
export const channelLimit = assistant => assistant?.taskConcurrency ?? 2;
export function makeTaskTitle(name, existingTitles = []) {
  if (typeof name !== 'string' || !name.trim() || name.trim().length > 111 || /[\x00-\x1f\x7f]/.test(name)) throw new Error('标题需为 1–111 个字符，且不能包含换行或控制字符');
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_';
  const used = new Set(existingTitles.map(title => parseTaskTitle(title)?.code));
  let code;
  do {
    code = '';
    while (code.length < 4) {
      const byte = globalThis.crypto.getRandomValues(new Uint8Array(1))[0];
      if (byte < 252) code += alphabet[byte % 63];
    }
  } while (used.has(code));
  return `T-${code} · ${name.trim()}`;
}
