export interface LoginSession { enabled: boolean; authenticated: boolean; username?: string; csrf?: string; expiresAt?: number }
import { appUrl } from './urls'
let csrf = ''
export const csrfHeader = ():Record<string,string> => csrf ? {'X-Tuck-CSRF':csrf} : {}
export function expiredLogin(): void { csrf = ''; window.dispatchEvent(new Event('tuck-auth-expired')) }
export async function authFetch(path: string, options: RequestInit = {}): Promise<Response> {
  const headers = new Headers(options.headers)
  if (options.method && !['GET','HEAD'].includes(options.method.toUpperCase()) && csrf) headers.set('X-Tuck-CSRF',csrf)
  const response = await fetch(appUrl(path),{...options, headers, credentials:'same-origin'})
  if (response.status === 401 && !path.startsWith('/api/auth/')) expiredLogin()
  if (response.status === 403) { const value=await response.clone().json().catch(()=>null); if(value?.code==='settings_locked')window.dispatchEvent(new Event('tuck-settings-access-denied')) }
  return response
}
export async function loginSession(): Promise<LoginSession> {
  const response = await authFetch('/api/auth/session')
  if (!response.ok) throw new Error('无法读取登录状态')
  const session: LoginSession = await response.json(); csrf = session.csrf || ''; return session
}
export async function signIn(username: string, password: string, rememberMe: boolean): Promise<LoginSession> {
  const response = await authFetch('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username,password,rememberMe})})
  const result = await response.json()
  if (!response.ok) throw Object.assign(new Error(result.error || '登录失败'),{retryAfter:Number(response.headers.get('Retry-After')) || 0})
  csrf = result.csrf || ''; return result
}
export async function signOut(): Promise<void> {
  const response = await authFetch('/api/auth/logout',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'})
  if (!response.ok && response.status !== 401) throw new Error('退出失败，请重试')
  expiredLogin()
}
