import { createRouter, createWebHashHistory } from 'vue-router'
import type {ComputedRef,InjectionKey} from 'vue'
export const settingsPathKey:InjectionKey<ComputedRef<string>>=Symbol('settings-path')
const app = () => import('./App.vue')

export const modes = [
  { id: 'chat', label: '对话', icon: 'M4 4h16v12H9l-5 4V4Z' },
  { id: 'tasks', label: '任务', icon: 'M5 3h14v18H5V3Zm3 8 3 3 5-6' },
  { id: 'goals', label: '目标', icon: 'M21 12a9 9 0 1 1-9-9m5 9a5 5 0 1 1-5-5m0 5 9-9m-5 0h5v5' },
  { id: 'orchestration', label: '编排', icon: 'M9 2h6v6H9V2ZM2 16h6v6H2v-6Zm14 0h6v6h-6v-6ZM12 8v4M5 16v-4h14v4' },
  { id: 'workspace', label: '工作区', icon: '' },
  { id: 'settings', label: '设置', icon: '' },
] as const
export type Mode = typeof modes[number]['id']
export const modePaths: Record<Mode, string> = { chat: '/chat', workspace:'/workspace', tasks: '/tasks', goals: '/goals', orchestration: '/orchestration', settings:'/settings' }
export function conversationPath(id: string, message?: number): string { return `/chat/conversations/${encodeURIComponent(id)}${message === undefined ? '' : `/messages/${message}`}` }
export const router = createRouter({
  history: createWebHashHistory(),
  sensitive: true,
  routes: [
    { path: '/', redirect: '/chat' },
    ...['/chat', '/chat/conversations/:id', '/chat/conversations/:id/messages/:message'].map(path => ({ path, component: app, meta: { mode: 'chat' } })),
    { path:'/chat/conversations/:id/delivery', name:'chat-delivery', component:app, meta:{mode:'chat'} },
    ...[
      {path:'/tasks',name:'tasks'},
      {path:'/tasks/assistants/:agent',name:'task-list'},
      {path:'/tasks/assistants/:agent/new',name:'task-new'},
      {path:'/tasks/:id',name:'task-detail'},
      {path:'/tasks/:id/delivery',name:'task-delivery'},
      {path:'/tasks/:id/supplement',name:'task-supplement'},
    ].map(route => ({...route, component: app, meta: {mode: 'tasks'}})),
    {path:'/settings',component:app,meta:{mode:'settings'}},
    ...['providers/platform/:managedProvider','providers/platform/:managedProvider/models/:managedModel','mcp/platform/:managedMcp'].map(section=>({path:'/settings/'+section,component:app,meta:{mode:'settings',settingsSection:section.split('/')[0],managed:true}})),
    ...['providers','providers/:provider','providers/:provider/models/:model','agents','agents/:agent','agents/:agent/tools/:tool(native|tuck)','mcp','mcp/:mcp','platform','orchestration','engine','appearance','protection'].map(section=>({path:'/settings/'+section,component:app,meta:{mode:'settings',settingsSection:section.split('/')[0]}})),
    {path:'/settings/:pathMatch(.*)*',component:app,meta:{mode:'settings'}},
    ...modes.filter(mode => !['chat', 'tasks','settings'].includes(mode.id)).map(mode => ({ path: modePaths[mode.id], component: app, meta: { mode: mode.id } })),
    { path: '/reference', component: () => import('../reference/components/AgentWorkspace.vue') },
    { path: '/architecture', component: () => import('../reference/components/ArchitectureOverview.vue') },
    { path: '/:pathMatch(.*)*', component: app },
  ],
})
// Vue Router accepts trailing slashes; keep one canonical path for page switches.
router.beforeEach(to => {
  const path = to.path.replace(/\/+$/, '') || '/'
  if (path !== to.path) return {path,query:to.query,hash:to.hash,replace:true}
})
export function linkUrl(path: string): string { const url = new URL(location.href); url.hash = router.resolve(path).href.slice(1); return url.href }
export function settingsReturnPath(): string {try{const path=sessionStorage.getItem('tuck.settings.return')||'/chat';return path.startsWith('/')&&!path.startsWith('//')&&router.resolve(path).meta.mode&&router.resolve(path).meta.mode!=='settings'?path:'/chat'}catch{return '/chat'}}
router.afterEach((to,from,failure)=>{if(!failure&&to.meta.mode==='settings'&&from.meta.mode&&from.meta.mode!=='settings')try{sessionStorage.setItem('tuck.settings.return',from.fullPath)}catch{}})
