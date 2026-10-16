import {afterEach,beforeEach,describe,it,expect,vi} from 'vitest'
const {fetcher}=vi.hoisted(()=>({fetcher:vi.fn()}))
vi.mock('../src/app/auth',()=>({authFetch:fetcher}))
const locked={enabled:true,unlocked:false,categories:['providers'],lockedCategories:['providers']}
const unlocked={...locked,unlocked:true,lockedCategories:[]}
const response=(value:unknown)=>({ok:true,json:async()=>value})
let stop=()=>{}
beforeEach(()=>{vi.resetModules();fetcher.mockReset();vi.stubGlobal('window',new EventTarget());vi.stubGlobal('BroadcastChannel',undefined)})
afterEach(()=>{stop();stop=()=>{};vi.useRealTimers();vi.unstubAllGlobals()})
describe('configuration access lifecycle',()=>{
 it('fails closed without blocking appearance',async()=>{
  const s=await import('../src/app/settings-access');expect(s.settingsLocked('providers')).toBe(true);expect(s.settingsLocked('appearance')).toBe(false)
  fetcher.mockResolvedValue(response(locked));await s.refreshSettingsAccess();expect(s.settingsLocked('agents')).toBe(false);expect(s.settingsLocked('protection')).toBe(true)
  fetcher.mockRejectedValue(new Error('offline'));await s.refreshSettingsAccess();expect(s.settingsLocked('agents')).toBe(true);expect(s.settingsLocked('appearance')).toBe(false)
 })
 it('expires immediately and rechecks server access',async()=>{
  vi.useFakeTimers();const s=await import('../src/app/settings-access');fetcher.mockResolvedValueOnce(response({...unlocked,expiresAt:Date.now()+1000})).mockResolvedValue(response(locked))
  stop=s.startSettingsAccess();await vi.advanceTimersByTimeAsync(0);expect(s.settingsLocked('providers')).toBe(false)
  await vi.advanceTimersByTimeAsync(1000);expect(s.settingsLocked('providers')).toBe(true);expect(fetcher).toHaveBeenCalledTimes(2)
 })
 it('notifies connection consumers when the same permissions recover after suspension',async()=>{
  const s=await import('../src/app/settings-access');fetcher.mockResolvedValue(response(unlocked));await s.refreshSettingsAccess()
  const states:boolean[]=[];window.addEventListener('tuck-settings-access-changed',()=>states.push(s.settingsAccess.ready))
  fetcher.mockRejectedValueOnce(new Error('offline'));await s.refreshSettingsAccess()
  const suspendedGeneration=s.settingsAccess.generation
  fetcher.mockResolvedValue(response(unlocked));await s.refreshSettingsAccess()
  expect(states).toEqual([false,true]);expect(s.settingsAccess.generation).toBeGreaterThan(suspendedGeneration);expect(s.settingsLocked('providers')).toBe(false)
  await s.refreshSettingsAccess();expect(states).toEqual([false,true])
 })
 it('does not restore stale unlocked responses after a lock',async()=>{
  const s=await import('../src/app/settings-access');fetcher.mockResolvedValue(response(unlocked));await s.refreshSettingsAccess()
  let release!:(value:unknown)=>void;fetcher.mockImplementationOnce(()=>new Promise(resolve=>release=resolve));const pending=s.refreshSettingsAccess()
  fetcher.mockResolvedValue(response(locked));await s.changeSettingsAccess('lock');release(response(unlocked));await pending
  expect(s.settingsLocked('providers')).toBe(true)
 })
 it('invalidates immediately on cross-tab changes before access returns',async()=>{
  let channel:any;vi.stubGlobal('BroadcastChannel',class{onmessage:any;constructor(){channel=this}close(){}postMessage(){}})
  const s=await import('../src/app/settings-access');fetcher.mockResolvedValue(response(unlocked));stop=s.startSettingsAccess();await s.refreshSettingsAccess()
  let release!:(value:unknown)=>void;fetcher.mockImplementationOnce(()=>new Promise(resolve=>release=resolve));channel.onmessage();expect(s.settingsLocked('providers')).toBe(true);expect(s.settingsLocked('appearance')).toBe(false)
  release(response(locked));await Promise.resolve();await Promise.resolve();expect(s.settingsLocked('providers')).toBe(true)
 })
})
