<script setup lang="ts">
import {inject,onMounted,ref,onBeforeUnmount} from 'vue'
import {useRoute} from 'vue-router'
import {router} from '../../app/router'
import {workspaceKey} from '../../shared/enterprise-workspace/state'
import EnterpriseWorkspace from '../../shared/enterprise-workspace/EnterpriseWorkspace.vue'
const route=useRoute(),owner=inject(workspaceKey)!,ready=ref(false)
let disposed=false
onBeforeUnmount(()=>{disposed=true})
onMounted(async()=>{
  // A bare first visit reuses the same owner as the chat panel. Explicit deep
  // links keep their target; no second owner or hidden preview is mounted.
  if(!['space','dir','file'].some(key=>key in route.query)&&owner.state.projection){
    await router.replace({path:route.path,query:{...route.query,space:owner.state.projection.agentSpaceId,dir:owner.state.dir||undefined,file:owner.state.active||undefined}})
  }
  if(!disposed&&route.meta.mode==='workspace')ready.value=true
})
</script>
<template><main class="workspace-mode" aria-label="企业工作区"><EnterpriseWorkspace v-if="ready" standalone /></main></template>
<style scoped>.workspace-mode{height:100%;min-width:0;min-height:0;display:flex;overflow:hidden;background:var(--tuck-background)}</style>
