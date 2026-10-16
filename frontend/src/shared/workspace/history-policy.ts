/** Product history budgets; independent of Docker Agent execution/context limits. */
export const CHAT_HISTORY = Object.freeze({
  windowMessages: 128,
  shiftMessages: 64,
  requestMessages: 64,
  cacheMessages: 512,
  cacheBytes: 8 * 1024 * 1024,
  hotSessions: 3,
})
