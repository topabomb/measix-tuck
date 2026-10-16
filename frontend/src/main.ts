import { createApp } from 'vue'
import App from './app/Root.vue'
import { router } from './app/router'
import './app/styles/base.css'
import './reference/styles/reference.css'
import './engine/vue/engine.css'
import './engine/vue/workbench-renderers.css'
import './reference/styles/architecture.css'

createApp(App).use(router).mount('#app')
